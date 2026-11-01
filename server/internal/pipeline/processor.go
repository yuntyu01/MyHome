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

// Processor는 원문을 거래로 만든다.
type Processor struct {
	Pool     *pgxpool.Pool
	Registry *parser.Registry
	Logger   *slog.Logger
}

// Result는 원문 처리 결과다(수집 API 응답에 포함된다).
type Result struct {
	RawEventID  int64               `json:"raw_event_id"`
	Test        bool                `json:"test,omitempty"`
	ParseError  string              `json:"parse_error,omitempty"`
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
	return res, nil
}

func existingResult(ctx context.Context, q *dbq.Queries, raw dbq.RawEvent) Result {
	res := Result{RawEventID: raw.ID}
	if raw.ParseError != nil {
		res.ParseError = *raw.ParseError
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
	body := ev.Body()

	var after func(created dbq.Transaction) error // 거래를 넣은 뒤 할 일(원거래 취소 표시 등)
	ownCat, isOwn := e.ownAccount(pr.Kind, body)

	switch {
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
		if found {
			t.Direction, t.CategoryID, t.Status = dup.Direction, dup.CategoryID, "excluded"
			t.ExclusionReason, t.MergedInto = reason(ExcludeDuplicate), &dup.ID
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
