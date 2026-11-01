package pipeline_test

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/yuntyu01/MyHome/server/internal/dbq"
	"github.com/yuntyu01/MyHome/server/internal/ingest"
	"github.com/yuntyu01/MyHome/server/internal/parser"
	"github.com/yuntyu01/MyHome/server/internal/period"
	"github.com/yuntyu01/MyHome/server/internal/pipeline"
	"github.com/yuntyu01/MyHome/server/internal/testdb"
)

// 이 파일의 알림 문구는 전부 합성 데이터다(실제 형식 아님).

type harness struct {
	t    *testing.T
	pool *pgxpool.Pool
	q    *dbq.Queries
	p    *pipeline.Processor
	ed   *pipeline.Editor
	cats map[string]int64
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	pool := testdb.New(t)
	reg, err := parser.NewRegistry("kb=test.kb;toss=test.toss")
	if err != nil {
		t.Fatal(err)
	}
	h := &harness{
		t: t, pool: pool, q: dbq.New(pool),
		p:  &pipeline.Processor{Pool: pool, Registry: reg, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))},
		ed: &pipeline.Editor{Pool: pool},
	}
	cats, err := h.q.ListCategories(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	h.cats = map[string]int64{}
	for _, c := range cats {
		h.cats[c.Name] = c.ID
	}
	return h
}

var base = time.Date(2026, 11, 10, 12, 0, 0, 0, period.KST)

// event는 원문을 저장하고 처리한 뒤 만들어진 거래(없으면 nil)와 결과를 돌려준다.
func (h *harness) event(pkg, title, text string, at time.Time) (*dbq.Transaction, pipeline.Result) {
	h.t.Helper()
	ctx := context.Background()
	req := ingest.Request{SourcePackage: pkg, Title: title, Text: text, PostedAt: at.UnixMilli()}
	raw, err := h.q.InsertRawEvent(ctx, dbq.InsertRawEventParams{
		SourcePackage: pkg, Title: title, Text: text, PostedAt: at, ContentHash: ingest.ContentHash(req),
	})
	if err != nil {
		h.t.Fatalf("원문 저장 실패: %v", err)
	}
	out, err := h.p.ProcessRawEvent(ctx, raw.ID)
	if err != nil {
		h.t.Fatalf("처리 실패: %v", err)
	}
	res := out.(pipeline.Result)
	if res.Transaction == nil {
		return nil, res
	}
	tx := h.get(res.Transaction.ID)
	return &tx, res
}

func (h *harness) get(id int64) dbq.Transaction {
	h.t.Helper()
	tx, err := h.q.GetTransaction(context.Background(), id)
	if err != nil {
		h.t.Fatal(err)
	}
	return tx
}

func (h *harness) wantStatus(tx dbq.Transaction, status string, category string) {
	h.t.Helper()
	if tx.Status != status {
		h.t.Errorf("거래 #%d status = %s (review=%v excl=%v), want %s", tx.ID, tx.Status, deref(tx.ReviewReason), deref(tx.ExclusionReason), status)
	}
	if category == "" {
		return
	}
	if tx.CategoryID == nil || *tx.CategoryID != h.cats[category] {
		h.t.Errorf("거래 #%d category = %v, want %s(%d)", tx.ID, tx.CategoryID, category, h.cats[category])
	}
}

func deref[T any](p *T) any {
	if p == nil {
		return nil
	}
	return *p
}

func (h *harness) summary(budget map[string]int64) map[string]dbq.CategorySummaryRow {
	h.t.Helper()
	ctx := context.Background()
	per := period.Containing(base, 1)
	for name, amt := range budget {
		if err := h.q.UpsertBudget(ctx, dbq.UpsertBudgetParams{PeriodStart: per.StartDate(), CategoryID: h.cats[name], Amount: amt}); err != nil {
			h.t.Fatal(err)
		}
	}
	rows, err := h.q.CategorySummary(ctx, dbq.CategorySummaryParams{PeriodStart: per.StartDate(), FromAt: per.Start, ToAt: per.End})
	if err != nil {
		h.t.Fatal(err)
	}
	out := map[string]dbq.CategorySummaryRow{}
	for _, r := range rows {
		out[r.Name] = r
	}
	return out
}

func TestSeedRuleClassification(t *testing.T) {
	h := newHarness(t)
	tx, _ := h.event("test.kb", "KB국민카드", "홍*동님 11/10 12:00 12,345원 일시불 스타벅스강남점 누적 50,000원", base)
	h.wantStatus(*tx, "confirmed", "카페·간식")
	if tx.Direction != "expense" || tx.Flow != "out" || tx.Amount != 12345 || tx.Source != "kb" {
		t.Errorf("거래 값 이상: %+v", tx)
	}
}

func TestInstallmentRecordsFullAmount(t *testing.T) {
	h := newHarness(t)
	tx, _ := h.event("test.kb", "KB국민카드", "홍*동님 450,000원 할부(3개월) 하이마트", base)
	if tx.Amount != 450000 || tx.InstallmentMonths != 3 {
		t.Errorf("할부는 승인 시점에 전액 기록: amount=%d months=%d", tx.Amount, tx.InstallmentMonths)
	}
}

func TestDuplicate_TossFirstThenKB(t *testing.T) {
	h := newHarness(t)
	toss, _ := h.event("test.toss", "토스페이", "스타벅스 역삼점에서 5,600원 결제했어요", base)
	kb, _ := h.event("test.kb", "KB국민카드", "홍*동님 5,600원 일시불 토스페이먼츠", base.Add(70*time.Second))
	h.wantStatus(*toss, "confirmed", "카페·간식")
	h.wantStatus(*kb, "excluded", "")
	if kb.MergedInto == nil || *kb.MergedInto != toss.ID || deref(kb.ExclusionReason) != pipeline.ExcludeDuplicate {
		t.Errorf("KB 건이 토스 건에 합쳐져야 함: %+v", kb)
	}
	if h.get(toss.ID).Merchant != "스타벅스 역삼점" {
		t.Error("구체적인 가맹점명 유지")
	}
}

func TestDuplicate_KBFirstThenToss_ImprovesMerchant(t *testing.T) {
	h := newHarness(t)
	kb, _ := h.event("test.kb", "KB국민카드", "홍*동님 5,600원 일시불 토스페이먼츠", base)
	h.wantStatus(*kb, "needs_review", "")
	if deref(kb.ReviewReason) != pipeline.ReviewPaymentGateway {
		t.Errorf("결제대행사 이름은 확인 필요: %v", deref(kb.ReviewReason))
	}
	toss, _ := h.event("test.toss", "토스페이", "스타벅스 역삼점에서 5,600원 결제했어요", base.Add(-30*time.Second))
	h.wantStatus(*toss, "excluded", "")
	kb2 := h.get(kb.ID)
	if kb2.Merchant != "스타벅스 역삼점" {
		t.Errorf("대표 거래 가맹점이 구체적인 이름으로 바뀌어야 함: %q", kb2.Merchant)
	}
	h.wantStatus(kb2, "confirmed", "카페·간식")
}

func TestNoDuplicate_SameAppOrOutsideWindow(t *testing.T) {
	h := newHarness(t)
	a, _ := h.event("test.kb", "KB국민카드", "홍*동님 4,500원 일시불 메가커피", base)
	b, _ := h.event("test.kb", "KB국민카드", "홍*동님 4,500원 일시불 메가커피", base.Add(time.Minute))
	c, _ := h.event("test.toss", "토스페이", "메가커피에서 4,500원 결제했어요", base.Add(10*time.Minute))
	for _, tx := range []*dbq.Transaction{a, b, c} {
		h.wantStatus(*tx, "confirmed", "카페·간식")
	}
}

func TestCancel_Full(t *testing.T) {
	h := newHarness(t)
	orig, _ := h.event("test.kb", "KB국민카드", "홍*동님 12,345원 일시불 스타벅스강남점", base)
	cancel, _ := h.event("test.kb", "KB국민카드 승인취소", "홍*동님 12,345원 스타벅스강남점", base.Add(time.Hour))
	h.wantStatus(h.get(orig.ID), "excluded", "")
	if deref(h.get(orig.ID).ExclusionReason) != pipeline.ExcludeCancelled {
		t.Error("원거래 제외 사유 cancelled")
	}
	h.wantStatus(*cancel, "excluded", "")
	if cancel.CancelsID == nil || *cancel.CancelsID != orig.ID {
		t.Error("취소 건이 원거래를 가리켜야 함")
	}
	if s := h.summary(nil); s["카페·간식"].Spent != 0 {
		t.Errorf("전액 취소 후 지출 0이어야 함: %d", s["카페·간식"].Spent)
	}
}

func TestCancel_Partial(t *testing.T) {
	h := newHarness(t)
	h.event("test.kb", "KB국민카드", "홍*동님 30,000원 일시불 올리브영 강남점", base)
	cancel, _ := h.event("test.kb", "KB국민카드 승인취소", "홍*동님 10,000원 올리브영 강남점", base.Add(time.Hour))
	h.wantStatus(*cancel, "confirmed", "쇼핑")
	if cancel.Flow != "in" || cancel.Direction != "expense" {
		t.Errorf("부분 취소는 flow=in 지출: %+v", cancel)
	}
	if s := h.summary(map[string]int64{"쇼핑": 100000}); s["쇼핑"].Spent != 20000 || s["쇼핑"].Remaining != 80000 {
		t.Errorf("부분 취소 반영: %+v", s["쇼핑"])
	}
}

func TestCancel_Unmatched(t *testing.T) {
	h := newHarness(t)
	cancel, _ := h.event("test.kb", "KB국민카드 승인취소", "홍*동님 9,900원 어딘가상점", base)
	h.wantStatus(*cancel, "needs_review", "")
	if deref(cancel.ReviewReason) != pipeline.ReviewCancelNoMatch {
		t.Errorf("review = %v", deref(cancel.ReviewReason))
	}
}

func TestCardPaymentExcluded(t *testing.T) {
	h := newHarness(t)
	tx, _ := h.event("test.toss", "토스뱅크", "출금 812,340원 | KB국민카드 결제대금\n잔액 2,307,660원", base)
	h.wantStatus(*tx, "excluded", "")
	if deref(tx.ExclusionReason) != pipeline.ExcludeCardPayment || tx.Direction != "transfer" {
		t.Errorf("카드대금 제외: %+v", tx)
	}
	if tx.Balance == nil || *tx.Balance != 2307660 {
		t.Error("잔액 추출")
	}
}

func TestOwnTransferToInvestment(t *testing.T) {
	h := newHarness(t)
	tx, _ := h.event("test.toss", "토스뱅크", "토스증권으로 500,000원을 옮겼어요", base)
	h.wantStatus(*tx, "confirmed", "투자·저축")
	if tx.Direction != "transfer" {
		t.Errorf("direction = %s", tx.Direction)
	}
	per := period.Containing(base, 1)
	tot, err := h.q.PeriodTotals(context.Background(), dbq.PeriodTotalsParams{FromAt: per.Start, ToAt: per.End})
	if err != nil {
		t.Fatal(err)
	}
	if tot.Investment != 500000 {
		t.Errorf("투자·저축 집계 = %d", tot.Investment)
	}
	for name, row := range h.summary(nil) {
		if row.Spent != 0 {
			t.Errorf("내 계좌 이체가 지출(%s)에 잡힘", name)
		}
	}
}

func TestPrepaidChargeExcluded_TmoneyIsTransport(t *testing.T) {
	h := newHarness(t)
	charge, _ := h.event("test.kakaopay", "카카오페이", "카카오페이머니 50,000원 충전 완료", base)
	h.wantStatus(*charge, "excluded", "")
	if deref(charge.ExclusionReason) != pipeline.ExcludePrepaid {
		t.Errorf("머니 충전 제외: %v", deref(charge.ExclusionReason))
	}
	tm, _ := h.event("test.tmoney", "티머니", "티머니 30,000원 충전되었습니다", base.Add(time.Hour))
	h.wantStatus(*tm, "confirmed", "교통")
}

func TestFriendTransferNeedsReview(t *testing.T) {
	h := newHarness(t)
	tx, _ := h.event("test.toss", "토스", "김민수님에게 25,000원을 보냈어요", base)
	h.wantStatus(*tx, "needs_review", "")
	if deref(tx.ReviewReason) != pipeline.ReviewTransfer {
		t.Errorf("review = %v", deref(tx.ReviewReason))
	}
	// seed의 contains 규칙은 송금에 적용하지 않는다(예: '김밥' seed가 '김밥천국사장'에게 보낸 송금을 식비로 만들면 안 됨)
	tx2, _ := h.event("test.toss", "토스", "김밥천국사장님에게 8,000원을 보냈어요", base.Add(time.Hour))
	h.wantStatus(*tx2, "needs_review", "")
}

func TestParseFailureKeepsRaw(t *testing.T) {
	h := newHarness(t)
	tx, res := h.event("test.toss", "토스", "이번 주 혜택을 확인해 보세요", base)
	if tx != nil || res.ParseError == "" {
		t.Fatalf("해석 실패 기대: tx=%v res=%+v", tx, res)
	}
	failed, err := h.q.ListFailedRawEvents(context.Background(), 10)
	if err != nil || len(failed) != 1 || failed[0].Text != "이번 주 혜택을 확인해 보세요" {
		t.Fatalf("실패 원문 목록: %v %+v", err, failed)
	}
}

func TestAmountOnlyBecomesNeedsReview(t *testing.T) {
	h := newHarness(t)
	tx, _ := h.event("test.unknown", "알림", "12,000원", base)
	h.wantStatus(*tx, "needs_review", "")
}

func TestTestNotificationIsNotATransaction(t *testing.T) {
	h := newHarness(t)
	tx, res := h.event("com.yuntyu.myhome", "[MyHome 테스트] 수집 확인", "테스트 1,000원", base)
	if tx != nil || !res.Test {
		t.Fatalf("테스트 알림은 거래가 아님: %+v", res)
	}
}

func TestIdempotentProcessing(t *testing.T) {
	h := newHarness(t)
	tx, res := h.event("test.kb", "KB국민카드", "홍*동님 3,000원 일시불 GS25 역삼점", base)
	again, err := h.p.ProcessRawEvent(context.Background(), res.RawEventID)
	if err != nil {
		t.Fatal(err)
	}
	if again.(pipeline.Result).Transaction.ID != tx.ID {
		t.Error("두 번 처리해도 같은 거래")
	}
}

func TestLearnRuleFromReview(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	a, _ := h.event("test.kb", "KB국민카드", "홍*동님 7,000원 일시불 동네분식", base)
	b, _ := h.event("test.kb", "KB국민카드", "홍*동님 6,000원 일시불 동네분식", base.Add(time.Hour))
	h.wantStatus(*a, "needs_review", "")
	cat := h.cats["식비"]
	res, err := h.ed.EditTransaction(ctx, a.ID, pipeline.Edit{CategoryID: &cat, RememberRule: true})
	if err != nil {
		t.Fatal(err)
	}
	if res.RuleID == nil || res.AlsoUpdated != 1 {
		t.Errorf("규칙 저장·함께 분류: %+v", res)
	}
	h.wantStatus(h.get(a.ID), "confirmed", "식비")
	h.wantStatus(h.get(b.ID), "confirmed", "식비")
	if !h.get(a.ID).ManuallyEdited {
		t.Error("수정한 거래는 manually_edited")
	}
	c, _ := h.event("test.kb", "KB국민카드", "홍*동님 8,000원 일시불 동네분식", base.Add(2*time.Hour))
	h.wantStatus(*c, "confirmed", "식비")
}

func TestSettlementAddsBackToBudget(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	h.event("test.kb", "KB국민카드", "홍*동님 40,000원 일시불 배달의민족", base)
	dep, _ := h.event("test.toss", "토스", "이영희님이 20,000원을 보냈어요", base.Add(time.Hour))
	h.wantStatus(*dep, "needs_review", "")
	if deref(dep.ReviewReason) != pipeline.ReviewDeposit {
		t.Errorf("입금은 성격 확인 필요: %v", deref(dep.ReviewReason))
	}
	settle, food := h.cats["정산"], h.cats["식비"]
	if _, err := h.ed.EditTransaction(ctx, dep.ID, pipeline.Edit{CategoryID: &settle, SettleCategoryID: &food, RememberRule: true}); err != nil {
		t.Fatal(err)
	}
	s := h.summary(map[string]int64{"식비": 100000})
	if s["식비"].Spent != 40000 || s["식비"].Settled != 20000 || s["식비"].Remaining != 80000 {
		t.Errorf("정산 되돌림: %+v", s["식비"])
	}
	// 같은 사람의 다음 입금은 학습 규칙으로 자동 정산
	dep2, _ := h.event("test.toss", "토스", "이영희님이 5,000원을 보냈어요", base.Add(2*time.Hour))
	h.wantStatus(*dep2, "confirmed", "정산")
	if dep2.Direction != "income" {
		t.Errorf("정산 direction = %s", dep2.Direction)
	}
}

func TestManualCashEntry(t *testing.T) {
	h := newHarness(t)
	tx, err := h.ed.CreateManual(context.Background(), pipeline.Manual{Amount: 5000, Merchant: "붕어빵", CategoryID: h.cats["카페·간식"]}, base)
	if err != nil {
		t.Fatal(err)
	}
	h.wantStatus(tx, "confirmed", "카페·간식")
	if tx.Source != "manual" || !tx.ManuallyEdited {
		t.Errorf("수동 입력: %+v", tx)
	}
	if _, err := h.ed.CreateManual(context.Background(), pipeline.Manual{Amount: 0, CategoryID: h.cats["식비"]}, base); err == nil {
		t.Error("0원은 거부")
	}
}

func TestReprocessPreservesManualEdits(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	a, _ := h.event("test.kb", "KB국민카드", "홍*동님 9,000원 일시불 새로운식당", base)
	b, _ := h.event("test.kb", "KB국민카드", "홍*동님 15,000원 일시불 다른새가게", base.Add(time.Hour))
	h.wantStatus(*a, "needs_review", "")
	// b는 사용자가 직접 '생활'로 고침(규칙 저장 안 함)
	life := h.cats["생활"]
	if _, err := h.ed.EditTransaction(ctx, b.ID, pipeline.Edit{CategoryID: &life}); err != nil {
		t.Fatal(err)
	}
	// 규칙을 새로 추가한 뒤 재처리하면 a는 새 규칙으로 분류되고 b는 그대로
	flow := "out"
	if _, err := h.q.UpsertRule(ctx, dbq.UpsertRuleParams{Pattern: "새로운식당", MatchType: "contains", CategoryID: h.cats["식비"], Flow: &flow, Priority: 50, CreatedFrom: "manual"}); err != nil {
		t.Fatal(err)
	}
	from, to := base.Add(-time.Hour), base.Add(24*time.Hour)
	plan, err := h.p.Reprocess(ctx, from, to, false)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Applied || plan.RawEvents != 1 || plan.SkippedManual != 1 {
		t.Errorf("계획: %+v", plan)
	}
	rep, err := h.p.Reprocess(ctx, from, to, true)
	if err != nil {
		t.Fatal(err)
	}
	if !rep.Applied || rep.Created != 1 {
		t.Errorf("재처리 결과: %+v", rep)
	}
	txs, _ := h.q.ListTransactions(ctx, dbq.ListTransactionsParams{FromAt: from, ToAt: to, Lim: 100})
	if len(txs) != 2 {
		t.Fatalf("거래 수 = %d", len(txs))
	}
	for _, x := range txs {
		switch x.Merchant {
		case "새로운식당":
			if x.Status != "confirmed" || *x.CategoryID != h.cats["식비"] {
				t.Errorf("재처리로 분류돼야 함: %+v", x)
			}
		case "다른새가게":
			if x.ID != b.ID || *x.CategoryID != life {
				t.Errorf("수동 수정 보존 실패: %+v", x)
			}
		}
	}
}

func TestReprocessKeepsDuplicateAndCancelLinksConsistent(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	h.event("test.toss", "토스페이", "스타벅스 역삼점에서 5,600원 결제했어요", base)
	h.event("test.kb", "KB국민카드", "홍*동님 5,600원 일시불 토스페이먼츠", base.Add(30*time.Second))
	h.event("test.kb", "KB국민카드", "홍*동님 20,000원 일시불 교보문고", base.Add(time.Hour))
	h.event("test.kb", "KB국민카드 승인취소", "홍*동님 20,000원 교보문고", base.Add(48*time.Hour))
	// 원거래들만 기간에 포함해도 연결된 건까지 함께 재처리되어야 한다.
	if _, err := h.p.Reprocess(ctx, base.Add(-time.Minute), base.Add(2*time.Hour), true); err != nil {
		t.Fatal(err)
	}
	s := h.summary(nil)
	if s["카페·간식"].Spent != 5600 || s["문화·여가"].Spent != 0 {
		t.Errorf("재처리 후 합계: 카페 %d, 문화 %d", s["카페·간식"].Spent, s["문화·여가"].Spent)
	}
	n, _ := h.q.CountNeedsReview(ctx)
	if n != 0 {
		t.Errorf("확인 필요 %d건", n)
	}
}
