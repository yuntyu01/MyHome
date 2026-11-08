package pipeline

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/yuntyu01/MyHome/server/internal/classify"
	"github.com/yuntyu01/MyHome/server/internal/db"
	"github.com/yuntyu01/MyHome/server/internal/dbq"
	"github.com/yuntyu01/MyHome/server/internal/parser"
)

// TestTitleMarker로 시작하는 제목은 앱의 테스트 알림이다. 원문은 저장하지만 거래로 만들지 않는다.
const TestTitleMarker = "[MyHome 테스트]"

// cancelLookback은 취소 알림의 원거래를 찾을 기간이다.
const cancelLookback = 120 * 24 * time.Hour

// repostWindow 안에 같은 앱이 같은 알림(제목·본문 동일)을 또 올리면 앞의 것과 같은 건으로 본다.
const repostWindow = 10 * time.Minute

// cancelDupWindow 안에 다른 앱이 같은 금액의 취소를 알리면 같은 환불로 본다(KB·토스가 각각 알림).
const cancelDupWindow = 24 * time.Hour

// Processor는 원문을 거래로 만든다.
type Processor struct {
	Pool     *pgxpool.Pool
	Registry *parser.Registry
	Logger   *slog.Logger
	// OnNoRule은 규칙이 없어서(no_rule) 확인 필요가 된 거래를 커밋한 뒤 부른다(AI 분류 추천 작업 깨우기).
	// 수집 응답을 늦추지 않게 막지 않는 함수여야 한다(suggest.Worker.Notify). nil이면 부르지 않는다.
	OnNoRule func()
}

// Result는 원문 처리 결과다(수집 API 응답에 포함된다).
type Result struct {
	RawEventID  int64               `json:"raw_event_id"`
	Test        bool                `json:"test,omitempty"`
	ParseError  string              `json:"parse_error,omitempty"`
	Ignored     string              `json:"ignored,omitempty"` // 결제 알림이 아니라서 무시한 사유
	Transaction *TransactionSummary `json:"transaction,omitempty"`
}

// TransactionSummary는 처리로 만들어진 거래 요약이다.
type TransactionSummary struct {
	ID              int64   `json:"id"`
	Status          string  `json:"status"`
	Direction       string  `json:"direction"`
	Amount          int64   `json:"amount"`
	Flow            string  `json:"flow"`
	Merchant        string  `json:"merchant"`
	CategoryID      *int64  `json:"category_id,omitempty"`
	ReviewReason    *string `json:"review_reason,omitempty"`
	ExclusionReason *string `json:"exclusion_reason,omitempty"`
	MergedInto      *int64  `json:"merged_into,omitempty"`
	CancelsID       *int64  `json:"cancels_id,omitempty"`
}

func summarize(t dbq.Transaction) *TransactionSummary {
	return &TransactionSummary{
		ID: t.ID, Status: t.Status, Direction: t.Direction, Amount: t.Amount, Flow: t.Flow, Merchant: t.Merchant,
		CategoryID: t.CategoryID, ReviewReason: t.ReviewReason, ExclusionReason: t.ExclusionReason,
		MergedInto: t.MergedInto, CancelsID: t.CancelsID,
	}
}

// ProcessRawEvent는 원문 하나를 처리한다. 이미 처리됐으면 기존 결과를 돌려준다(멱등).
func (p *Processor) ProcessRawEvent(ctx context.Context, rawEventID int64) (any, error) {
	var res Result
	err := db.InTx(ctx, p.Pool, func(tx pgx.Tx) error {
		q := dbq.New(tx)
		if err := q.AcquireProcessingLock(ctx); err != nil {
			return err
		}
		raw, err := q.GetRawEventForUpdate(ctx, rawEventID)
		if err != nil {
			return err
		}
		if raw.ProcessedAt != nil {
			res = existingResult(ctx, q, raw)
			return nil
		}
		e, err := loadEnv(ctx, q)
		if err != nil {
			return err
		}
		res, err = p.processLocked(ctx, q, e, raw)
		return err
	})
	if err != nil {
		return nil, err
	}
	if t := res.Transaction; p.OnNoRule != nil && t != nil && t.Status == "needs_review" && t.ReviewReason != nil && *t.ReviewReason == ReviewNoRule {
		p.OnNoRule()
	}
	return res, nil
}

func existingResult(ctx context.Context, q *dbq.Queries, raw dbq.RawEvent) Result {
	res := Result{RawEventID: raw.ID}
	if raw.ParseError != nil {
		res.ParseError = *raw.ParseError
	}
	if raw.IgnoreReason != nil {
		res.Ignored = *raw.IgnoreReason
	}
	if txs, err := q.ListTransactionsByRawEvent(ctx, &raw.ID); err == nil && len(txs) > 0 {
		res.Transaction = summarize(txs[0])
	}
	return res
}

// processLocked는 처리 잠금과 원문 행 잠금을 잡은 상태에서 호출된다.
func (p *Processor) processLocked(ctx context.Context, q *dbq.Queries, e *env, raw dbq.RawEvent) (Result, error) {
	res := Result{RawEventID: raw.ID}

	if strings.HasPrefix(strings.TrimSpace(raw.Title), TestTitleMarker) {
		res.Test = true
		return res, q.MarkRawEventProcessed(ctx, dbq.MarkRawEventProcessedParams{ID: raw.ID})
	}

	// 1. 파싱
	ev := parser.Event{Package: raw.SourcePackage, Title: raw.Title, Text: raw.Text, BigText: raw.BigText, PostedAt: raw.PostedAt}
	pr, perr := p.Registry.Parse(ev)
	var ign *parser.IgnoredError
	if errors.As(perr, &ign) {
		// 광고·증권·안내 알림: 거래를 만들지 않고 무시 사유만 남긴다(해석 실패로 세지 않음).
		res.Ignored = ign.Reason
		return res, q.MarkRawEventProcessed(ctx, dbq.MarkRawEventProcessedParams{ID: raw.ID, IgnoreReason: &ign.Reason})
	}
	if perr != nil {
		// 금액을 못 뽑으면 거래를 만들지 않는다. 원문은 그대로 남고 재처리할 수 있다.
		msg := perr.Error()
		res.ParseError = msg
		return res, q.MarkRawEventProcessed(ctx, dbq.MarkRawEventProcessedParams{ID: raw.ID, ParseError: &msg})
	}

	rawID := raw.ID
	flow := pr.Kind.Flow()
	t := dbq.InsertTransactionParams{
		RawEventID:        &rawID,
		OccurredAt:        pr.OccurredAt,
		Amount:            pr.Amount,
		Flow:              flow,
		Merchant:          pr.Merchant,
		Source:            pr.Source,
		Kind:              string(pr.Kind),
		InstallmentMonths: int32(pr.Installment), // 할부도 승인 시점에 전액 기록
		Balance:           pr.Balance,
	}
	if pr.Foreign != "" {
		t.Memo = "외화 " + pr.Foreign
	}
	body := ev.Body()

	var after func(created dbq.Transaction) error // 거래를 넣은 뒤 할 일(원거래 취소 표시 등)
	ownCat, isOwn := e.ownAccount(pr.Kind, body)
	// 같은 앱이 같은 알림을 다시 올린 것인지 먼저 본다(이체·취소도 두 번 세지 않게).
	orig, isRepost, err := p.findRepost(ctx, q, raw)
	if err != nil {
		return res, err
	}

	switch {
	case isRepost:
		markDuplicate(&t, orig)
	case pr.Amount == 0 && pr.Foreign != "":
		// 외화만 있는 승인·취소: 원화 금액을 지어내지 않는다. 분류는 제안만 하고 금액은 사용자가 넣는다.
		t.Direction, t.Status, t.ReviewReason = "expense", "needs_review", reason(ReviewForeign)
		if cl := e.classify(pr.Merchant, pr.Kind, flow); pr.Kind == parser.KindApproval && cl.categoryID != nil {
			t.CategoryID, t.Direction = cl.categoryID, cl.direction
		}
	// 2. 예외 규칙
	case e.isCardPayment(pr.Kind, body):
		t.Direction, t.Status, t.ExclusionReason = "transfer", "excluded", reason(ExcludeCardPayment)
	case isOwn:
		id := ownCat.ID
		t.Direction, t.Status, t.CategoryID = "transfer", "confirmed", &id
	case e.isPrepaidCharge(pr.Kind, body):
		t.Direction, t.Status, t.ExclusionReason = "transfer", "excluded", reason(ExcludePrepaid)
	default:
		// 3. 중복 합치기
		dup, found, err := p.findDuplicate(ctx, q, e, raw, t)
		if err != nil {
			return res, err
		}
		if !found && pr.Kind == parser.KindCancel {
			// 다른 앱이 먼저 알린 같은 환불이면 또 상계하지 않는다(다른 결제에서 한 번 더 빠지는 것 방지).
			dup, found, err = p.findCancelDuplicate(ctx, q, e, raw, t)
			if err != nil {
				return res, err
			}
		}
		if found {
			markDuplicate(&t, dup)
			after = func(dbq.Transaction) error { return p.mergeMerchant(ctx, q, e, dup, pr.Merchant) }
			break
		}
		// 4. 취소 상계
		if pr.Kind == parser.KindCancel {
			fn, err := p.applyCancel(ctx, q, &t)
			if err != nil {
				return res, err
			}
			after = fn
			break
		}
		// 5·6. 규칙 분류 → 못 하면 확인 필요
		cl := e.classify(pr.Merchant, pr.Kind, flow)
		t.CategoryID, t.Direction, t.Status, t.ReviewReason = cl.categoryID, cl.direction, cl.status, cl.reviewReason
	}

	created, err := q.InsertTransaction(ctx, t)
	if err != nil {
		return res, fmt.Errorf("거래 저장 실패: %w", err)
	}
	if after != nil {
		if err := after(created); err != nil {
			return res, err
		}
	}
	if err := q.MarkRawEventProcessed(ctx, dbq.MarkRawEventProcessedParams{ID: raw.ID}); err != nil {
		return res, err
	}
	res.Transaction = summarize(created)
	p.log().Info("거래 생성", "raw_event_id", raw.ID, "transaction_id", created.ID, "status", created.Status, "source", created.Source)
	return res, nil
}

func (p *Processor) log() *slog.Logger {
	if p.Logger == nil {
		return slog.Default()
	}
	return p.Logger
}

// markDuplicate는 거래를 dup(또는 dup이 이미 합쳐진 대표 거래)에 합친 중복으로 표시한다.
func markDuplicate(t *dbq.InsertTransactionParams, dup dbq.Transaction) {
	root := dup.ID
	if dup.MergedInto != nil {
		root = *dup.MergedInto
	}
	t.Direction, t.CategoryID, t.Status = dup.Direction, dup.CategoryID, "excluded"
	t.ExclusionReason, t.MergedInto, t.ReviewReason = reason(ExcludeDuplicate), &root, nil
}

// findRepost는 같은 앱이 같은 알림을 repostWindow 안에 다시 올린 경우 앞서 만든 거래를 찾는다.
// 게시 시각(postTime)만 달라서 원문 해시로는 걸러지지 않는 경우다.
func (p *Processor) findRepost(ctx context.Context, q *dbq.Queries, raw dbq.RawEvent) (dbq.Transaction, bool, error) {
	orig, err := q.FindRepostOriginal(ctx, dbq.FindRepostOriginalParams{
		RawEventID: raw.ID, SourcePackage: raw.SourcePackage, Title: raw.Title, Text: raw.Text, BigText: raw.BigText,
		WindowFrom: raw.PostedAt.Add(-repostWindow), WindowTo: raw.PostedAt.Add(repostWindow),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return dbq.Transaction{}, false, nil
	}
	return orig, err == nil, err
}

// dedupeKinds는 중복으로 합칠 수 있는 종류다. 승인(충전)은 승인끼리, 송금·출금은 그끼리, 취소는 취소끼리 합친다.
// 종류를 모르는 건(unknown)은 내용으로 가릴 수 없어서 같은 방향이면 합친다.
func dedupeKinds(k parser.Kind) []string {
	s := func(ks ...parser.Kind) []string {
		out := []string{string(parser.KindUnknown)}
		for _, x := range ks {
			out = append(out, string(x))
		}
		return out
	}
	switch k {
	case parser.KindApproval, parser.KindCharge:
		return s(parser.KindApproval, parser.KindCharge)
	case parser.KindTransferOut, parser.KindWithdrawal:
		return s(parser.KindTransferOut, parser.KindWithdrawal)
	case parser.KindCancel, parser.KindDeposit:
		return s(k)
	}
	return s(parser.KindApproval, parser.KindCharge, parser.KindTransferOut, parser.KindWithdrawal, parser.KindCancel, parser.KindDeposit)
}

// findCancelDuplicate는 다른 앱이 cancelDupWindow 안에 먼저 알린 같은 금액·같은(또는 비교 못 하는) 가맹점의 취소를 찾는다.
func (p *Processor) findCancelDuplicate(ctx context.Context, q *dbq.Queries, e *env, raw dbq.RawEvent, t dbq.InsertTransactionParams) (dbq.Transaction, bool, error) {
	cands, err := q.FindCancelDuplicates(ctx, dbq.FindCancelDuplicatesParams{
		SourcePackage: raw.SourcePackage, Amount: t.Amount,
		WindowFrom: t.OccurredAt.Add(-cancelDupWindow), WindowTo: t.OccurredAt.Add(cancelDupWindow), OccurredAt: t.OccurredAt,
	})
	if err != nil {
		return dbq.Transaction{}, false, err
	}
	for _, c := range cands {
		if compatibleMerchant(c.Merchant, t.Merchant, e.cfg.ReviewMerchantPatterns) {
			return c, true, nil
		}
	}
	return dbq.Transaction{}, false, nil
}

// compatibleMerchant: 이름이 비슷하거나, 한쪽이 비었거나 결제대행사 이름뿐이라 비교할 수 없으면 같은 가게로 본다.
func compatibleMerchant(a, b string, gateways []string) bool {
	if classify.SimilarMerchant(a, b) {
		return true
	}
	ra, ga := classify.StripGateway(a, gateways)
	rb, gb := classify.StripGateway(b, gateways)
	return (ga && ra == "") || (gb && rb == "")
}

// findDuplicate는 다른 앱에서 온 같은 거래(토스페이+KB 등)를 찾는다.
func (p *Processor) findDuplicate(ctx context.Context, q *dbq.Queries, e *env, raw dbq.RawEvent, t dbq.InsertTransactionParams) (dbq.Transaction, bool, error) {
	if e.cfg.DedupeWindow <= 0 {
		return dbq.Transaction{}, false, nil
	}
	dup, err := q.FindDedupeCandidate(ctx, dbq.FindDedupeCandidateParams{
		ExcludeID:     0,
		SourcePackage: raw.SourcePackage,
		Flow:          t.Flow,
		Amount:        t.Amount,
		Kinds:         dedupeKinds(parser.Kind(t.Kind)),
		WindowFrom:    t.OccurredAt.Add(-e.cfg.DedupeWindow),
		WindowTo:      t.OccurredAt.Add(e.cfg.DedupeWindow),
		OccurredAt:    t.OccurredAt,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return dbq.Transaction{}, false, nil
	}
	if err != nil {
		return dbq.Transaction{}, false, err
	}
	return dup, true, nil
}

// mergeMerchant는 중복으로 합친 대표 거래의 가맹점명을 더 구체적인 쪽으로 바꾸고,
// 그 덕에 분류할 수 있게 됐으면 다시 분류한다.
func (p *Processor) mergeMerchant(ctx context.Context, q *dbq.Queries, e *env, keep dbq.Transaction, other string) error {
	best := classify.MoreSpecific(keep.Merchant, other, e.cfg.ReviewMerchantPatterns)
	if best == keep.Merchant || keep.ManuallyEdited {
		return nil
	}
	upd := updateParamsFrom(keep)
	upd.Merchant = best
	if keep.Status == "needs_review" && keep.ReviewReason != nil {
		switch *keep.ReviewReason {
		case ReviewNoMerchant, ReviewNoRule, ReviewPaymentGateway:
			cl := e.classify(best, parser.Kind(keep.Kind), keep.Flow)
			upd.CategoryID, upd.Direction, upd.Status, upd.ReviewReason = cl.categoryID, cl.direction, cl.status, cl.reviewReason
		}
	}
	_, err := q.UpdateTransaction(ctx, upd)
	return err
}

// applyCancel은 취소 알림의 원거래를 찾아 상계한다.
// 전액 취소: 원거래와 취소 건 모두 제외. 부분 취소: 취소 건을 같은 분류의 음(-)의 지출로 확정.
// 원거래를 못 찾으면 확인 필요.
func (p *Processor) applyCancel(ctx context.Context, q *dbq.Queries, t *dbq.InsertTransactionParams) (func(dbq.Transaction) error, error) {
	cands, err := q.FindCancelCandidates(ctx, dbq.FindCancelCandidatesParams{
		ExcludeID:  0,
		Amount:     t.Amount,
		WindowFrom: t.OccurredAt.Add(-cancelLookback),
		WindowTo:   t.OccurredAt.Add(time.Minute),
	})
	if err != nil {
		return nil, err
	}
	t.Direction = "expense"
	var orig *dbq.Transaction
	full := false
	for i := range cands { // 전액 일치 우선(최근 것부터)
		if cands[i].Amount == t.Amount && classify.SimilarMerchant(cands[i].Merchant, t.Merchant) {
			orig, full = &cands[i], true
			break
		}
	}
	if orig == nil && strings.TrimSpace(t.Merchant) != "" {
		for i := range cands { // 부분 취소는 가맹점이 확실히 같을 때만
			if strings.TrimSpace(cands[i].Merchant) != "" && classify.SimilarMerchant(cands[i].Merchant, t.Merchant) {
				orig = &cands[i]
				break
			}
		}
	}
	if orig == nil {
		t.Status, t.ReviewReason = "needs_review", reason(ReviewCancelNoMatch)
		return nil, nil
	}
	o := *orig
	t.CancelsID, t.CategoryID = &o.ID, o.CategoryID
	if t.Merchant == "" {
		t.Merchant = o.Merchant
	}
	if full {
		t.Status, t.ExclusionReason = "excluded", reason(ExcludeCancellation)
		return func(dbq.Transaction) error {
			upd := updateParamsFrom(o)
			upd.Status, upd.ExclusionReason, upd.ReviewReason = "excluded", reason(ExcludeCancelled), nil
			_, err := q.UpdateTransaction(ctx, upd)
			return err
		}, nil
	}
	if o.CategoryID == nil {
		t.Status, t.ReviewReason = "needs_review", reason(ReviewCancelPartial)
	} else {
		t.Status = "confirmed"
	}
	return nil, nil
}

// updateParamsFrom은 기존 거래 값으로 UpdateTransactionParams를 채운다.
func updateParamsFrom(t dbq.Transaction) dbq.UpdateTransactionParams {
	return dbq.UpdateTransactionParams{
		ID: t.ID, OccurredAt: t.OccurredAt, Amount: t.Amount, Flow: t.Flow, Merchant: t.Merchant, Memo: t.Memo,
		Direction: t.Direction, CategoryID: t.CategoryID, SettleCategoryID: t.SettleCategoryID, Status: t.Status,
		ExclusionReason: t.ExclusionReason, ReviewReason: t.ReviewReason, MergedInto: t.MergedInto,
		CancelsID: t.CancelsID, ManuallyEdited: t.ManuallyEdited,
	}
}
