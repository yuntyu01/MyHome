package server_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/yuntyu01/MyHome/server/internal/auth"
	"github.com/yuntyu01/MyHome/server/internal/config"
	"github.com/yuntyu01/MyHome/server/internal/dbq"
	"github.com/yuntyu01/MyHome/server/internal/parser"
	"github.com/yuntyu01/MyHome/server/internal/period"
	"github.com/yuntyu01/MyHome/server/internal/pipeline"
	"github.com/yuntyu01/MyHome/server/internal/server"
	"github.com/yuntyu01/MyHome/server/internal/testdb"
)

// 알림 문구는 합성 데이터다.

type client struct {
	t     *testing.T
	h     http.Handler
	token string
	q     *dbq.Queries
	pool  *pgxpool.Pool
}

func newClient(t *testing.T) *client { t.Helper(); return newClientWith(t, nil) }

// newClientWith는 설정을 고친 서버로 클라이언트를 만든다(mutate가 nil이면 기본).
func newClientWith(t *testing.T, mutate func(*config.Config)) *client {
	t.Helper()
	pool := testdb.New(t)
	q := dbq.New(pool)
	tok, _, err := auth.IssueDeviceToken(context.Background(), q, "phone")
	if err != nil {
		t.Fatal(err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	reg, _ := parser.NewRegistry("kb=test.kb;toss=test.toss")
	cfg := config.Config{MaxBodyBytes: 64 * 1024, RateLimitRPS: 1000, RateLimitBurst: 1000}
	if mutate != nil {
		mutate(&cfg)
	}
	h := server.New(server.Deps{Config: cfg, Pool: pool, Logger: logger, Processor: &pipeline.Processor{Pool: pool, Registry: reg, Logger: logger}})
	return &client{t: t, h: h, token: tok, q: q, pool: pool}
}

func (c *client) do(method, path string, body any, out any) int {
	c.t.Helper()
	var r io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		r = bytes.NewReader(b)
	}
	req := httptest.NewRequest(method, path, r)
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c.h.ServeHTTP(rec, req)
	if out != nil && rec.Code < 300 {
		if err := json.Unmarshal(rec.Body.Bytes(), out); err != nil {
			c.t.Fatalf("%s %s 응답 해석 실패: %v %s", method, path, err, rec.Body.String())
		}
	}
	return rec.Code
}

func month() string { return time.Now().In(period.KST).Format("2006-01") }

func (c *client) event(pkg, title, text string, at time.Time) map[string]any {
	var resp map[string]any
	code := c.do("POST", "/v1/events", map[string]any{"source_package": pkg, "title": title, "text": text, "posted_at": at.UnixMilli()}, &resp)
	if code != http.StatusCreated {
		c.t.Fatalf("event status %d", code)
	}
	return resp
}

func (c *client) catID(name string) int64 {
	cats, _ := c.q.ListCategories(context.Background())
	for _, x := range cats {
		if x.Name == name {
			return x.ID
		}
	}
	c.t.Fatalf("분류 없음: %s", name)
	return 0
}

func TestEndToEnd_EventToSummary(t *testing.T) {
	c := newClient(t)
	now := time.Now()

	resp := c.event("test.kb", "KB국민카드", "홍*동님 4,500원 일시불 메가커피 역삼점", now)
	result := resp["result"].(map[string]any)
	tx := result["transaction"].(map[string]any)
	if tx["status"] != "confirmed" {
		t.Fatalf("즉시 처리되어 확정돼야 함: %+v", tx)
	}
	c.event("test.kb", "KB국민카드", "홍*동님 9,000원 일시불 처음보는가게", now.Add(time.Second))

	// 예산 설정 후 요약
	if code := c.do("PUT", "/v1/budgets", map[string]any{"month": month(), "items": []map[string]any{{"category_id": c.catID("카페·간식"), "amount": 50000}}}, nil); code != 200 {
		t.Fatalf("budgets status %d", code)
	}
	var sum struct {
		Totals struct {
			Budget, Spent, Remaining int64
		}
		Categories []struct {
			Name                     string
			Budget, Spent, Remaining int64
		}
		Recent      []map[string]any
		NeedsReview int64 `json:"needs_review"`
	}
	if code := c.do("GET", "/v1/summary?month="+month(), nil, &sum); code != 200 {
		t.Fatalf("summary status %d", code)
	}
	var cafe int64 = -1
	for _, x := range sum.Categories {
		if x.Name == "카페·간식" {
			cafe = x.Remaining
		}
	}
	if cafe != 45500 || sum.Totals.Spent != 4500 || sum.NeedsReview != 1 || len(sum.Recent) != 2 {
		t.Errorf("요약 이상: cafe=%d totals=%+v review=%d recent=%d", cafe, sum.Totals, sum.NeedsReview, len(sum.Recent))
	}
}

func TestReviewQueueClassifyAndLearn(t *testing.T) {
	c := newClient(t)
	c.event("test.kb", "KB국민카드", "홍*동님 9,000원 일시불 동네국밥", time.Now())
	var q struct{ Transactions []struct{ ID int64 } }
	c.do("GET", "/v1/review", nil, &q)
	if len(q.Transactions) != 1 {
		t.Fatalf("확인 필요 1건 기대: %d", len(q.Transactions))
	}
	var patched struct {
		Transaction struct {
			Status   string
			Category string
		}
		RuleID *int64 `json:"rule_id"`
	}
	code := c.do("PATCH", "/v1/transactions/"+itoa(q.Transactions[0].ID), map[string]any{"category_id": c.catID("식비"), "remember_rule": true}, &patched)
	if code != 200 || patched.Transaction.Status != "confirmed" || patched.Transaction.Category != "식비" || patched.RuleID == nil {
		t.Fatalf("분류 실패: %d %+v", code, patched)
	}
	resp := c.event("test.kb", "KB국민카드", "홍*동님 9,500원 일시불 동네국밥", time.Now().Add(time.Minute))
	if resp["result"].(map[string]any)["transaction"].(map[string]any)["status"] != "confirmed" {
		t.Error("학습한 규칙으로 자동 분류돼야 함")
	}
}

func TestManualEntryAndValidation(t *testing.T) {
	c := newClient(t)
	if code := c.do("POST", "/v1/transactions", map[string]any{"amount": 3000, "merchant": "붕어빵", "category_id": c.catID("카페·간식")}, nil); code != 201 {
		t.Errorf("현금 입력 status %d", code)
	}
	if code := c.do("POST", "/v1/transactions", map[string]any{"amount": -1, "category_id": c.catID("식비")}, nil); code != 400 {
		t.Errorf("음수 금액 status %d", code)
	}
	if code := c.do("PUT", "/v1/settings/budget_start_day", 31, nil); code != 400 {
		t.Errorf("잘못된 시작일 status %d", code)
	}
	if code := c.do("PUT", "/v1/settings/budget_start_day", 25, nil); code != 200 {
		t.Errorf("시작일 변경 status %d", code)
	}
	if code := c.do("PUT", "/v1/settings/unknown_key", 1, nil); code != 400 {
		t.Errorf("모르는 설정 키 status %d", code)
	}
	if code := c.do("POST", "/v1/rules", map[string]any{"pattern": "(", "match_type": "regex", "category_id": c.catID("식비")}, nil); code != 400 {
		t.Errorf("잘못된 정규식 status %d", code)
	}
	if code := c.do("POST", "/v1/rules", map[string]any{"pattern": "동네빵집", "category_id": c.catID("카페·간식")}, nil); code != 201 {
		t.Errorf("규칙 추가 status %d", code)
	}
	if code := c.do("DELETE", "/v1/categories/"+itoa(c.catID("정산")), nil, nil); code != 400 {
		t.Errorf("특수 분류 보관 금지 status %d", code)
	}
	if code := c.do("DELETE", "/v1/categories/"+itoa(c.catID("경조사")), nil, nil); code != 200 {
		t.Errorf("일반 분류 보관 status %d", code)
	}
	if code := c.do("GET", "/v1/transactions?from=2026-13-01&to=x", nil, nil); code != 400 {
		t.Errorf("잘못된 날짜 status %d", code)
	}
}

func TestFailedRawEventsAndAnalytics(t *testing.T) {
	c := newClient(t)
	c.event("test.toss", "토스", "결제 내역을 확인해 주세요", time.Now())
	c.event("test.toss", "토스", "이번 주 혜택을 확인해 보세요", time.Now()) // 광고: 무시(실패 아님)
	var raw struct {
		RawEvents []struct {
			ParseError *string `json:"parse_error"`
		} `json:"raw_events"`
	}
	c.do("GET", "/v1/raw-events?failed=true", nil, &raw)
	if len(raw.RawEvents) != 1 || raw.RawEvents[0].ParseError == nil {
		t.Errorf("해석 실패 원문 목록: %+v", raw)
	}
	// 요약에 읽지 못한 알림 수가 나온다(앱 홈·위젯 표시용). 처리 못 하고 오래 남은 원문도 센다.
	ctx := context.Background()
	stuck, err := c.q.InsertRawEvent(ctx, dbq.InsertRawEventParams{SourcePackage: "test.kb", Title: "KB국민카드", Text: "홍*동님 1,000원 일시불 GS25", PostedAt: time.Now(), ContentHash: "stuck-1"})
	if err != nil {
		t.Fatal(err)
	}
	var sum struct {
		FailedEvents int64 `json:"failed_events"`
	}
	c.do("GET", "/v1/summary", nil, &sum)
	if sum.FailedEvents != 1 {
		t.Errorf("failed_events = %d, want 1(방금 받은 미처리 원문은 아직 안 셈)", sum.FailedEvents)
	}
	if _, err := c.pool.Exec(ctx, "UPDATE raw_events SET received_at = now() - interval '1 hour' WHERE id = $1", stuck.ID); err != nil {
		t.Fatal(err)
	}
	c.do("GET", "/v1/summary", nil, &sum)
	if sum.FailedEvents != 2 {
		t.Errorf("failed_events = %d, want 2", sum.FailedEvents)
	}
	var an struct {
		Daily []struct{ Day string }
	}
	if code := c.do("GET", "/v1/analytics", nil, &an); code != 200 || len(an.Daily) < 28 {
		t.Errorf("분석 status %d, 일수 %d", code, len(an.Daily))
	}
}

func TestUserAPIRequiresAuth(t *testing.T) {
	c := newClient(t)
	c.token = "nope"
	if code := c.do("GET", "/v1/summary", nil, nil); code != 401 {
		t.Errorf("인증 없이 요약 status %d", code)
	}
}

func itoa(n int64) string { b, _ := json.Marshal(n); return string(b) }

func TestMonthlyReportAndReconcile(t *testing.T) {
	c := newClient(t)
	now := time.Now()
	c.event("test.kb", "KB국민카드", "홍*동님 12,000원 일시불 메가커피", now)
	c.event("test.kb", "KB국민카드", "홍*동님 8,000원 일시불 메가커피", now.Add(time.Minute))
	c.event("test.toss", "토스페이", "메가커피에서 8,000원 결제했어요", now.Add(90*time.Second))      // 중복 → 제외
	c.event("test.toss", "토스뱅크", "출금 500,000원 | KB국민카드 결제대금", now.Add(2*time.Minute)) // 카드대금 → 제외
	if code := c.do("PUT", "/v1/budgets", map[string]any{"month": month(), "items": []map[string]any{{"category_id": c.catID("카페·간식"), "amount": 15000}}}, nil); code != 200 {
		t.Fatalf("budgets %d", code)
	}

	var rep struct {
		Totals       struct{ Spent int64 }
		ExpenseCount int64 `json:"expense_count"`
		TopMerchants []struct {
			Merchant string
			Spent    int64
		} `json:"top_merchants"`
		OverBudget []struct{ Name string } `json:"over_budget"`
	}
	if code := c.do("GET", "/v1/reports/monthly?month="+month(), nil, &rep); code != 200 {
		t.Fatalf("report status %d", code)
	}
	if rep.Totals.Spent != 20000 || rep.ExpenseCount != 2 || len(rep.TopMerchants) != 1 || rep.TopMerchants[0].Spent != 20000 || len(rep.OverBudget) != 1 {
		t.Errorf("리포트 이상: %+v", rep)
	}

	var rc struct {
		Sources []struct {
			Source string
			Spent  int64
		}
		Excluded []struct {
			Reason string
			Count  int64
		}
		History []struct{ Sum int64 }
	}
	if code := c.do("GET", "/v1/reconcile?month="+month(), nil, &rc); code != 200 {
		t.Fatalf("reconcile status %d", code)
	}
	if len(rc.Sources) != 1 || rc.Sources[0].Source != "kb" || rc.Sources[0].Spent != 20000 {
		t.Errorf("출처별 합계: %+v", rc.Sources)
	}
	reasons := map[string]int64{}
	for _, e := range rc.Excluded {
		reasons[e.Reason] += e.Count
	}
	if reasons["duplicate"] != 1 || reasons["card_payment"] != 1 {
		t.Errorf("제외 내역: %+v", rc.Excluded)
	}
	if len(rc.History) != 6 || rc.History[0].Sum != 20000 {
		t.Errorf("최근 6개 기간: %+v", rc.History)
	}
}

// 새 기간에 예산이 없으면 예산이 있던 가장 최근 기간의 예산을 한 번 복사한다(위젯이 '예산 없음'이 되지 않게).
func TestBudgetRolloverToNewPeriod(t *testing.T) {
	c := newClient(t)
	ctx := context.Background()
	cur := period.Containing(time.Now(), 1)
	twoAgo := cur.Prev(1).Prev(1) // 지난달은 비어 있어도 그 전 예산을 가져온다
	cafe := c.catID("카페·간식")
	if err := c.q.UpsertBudget(ctx, dbq.UpsertBudgetParams{PeriodStart: twoAgo.StartDate(), CategoryID: cafe, Amount: 100000}); err != nil {
		t.Fatal(err)
	}
	var sum struct {
		Totals     struct{ Budget int64 } `json:"totals"`
		Categories []struct {
			ID     int64 `json:"id"`
			Budget int64 `json:"budget"`
		} `json:"categories"`
	}
	c.do("GET", "/v1/summary", nil, &sum)
	if sum.Totals.Budget != 100000 {
		t.Fatalf("이월된 예산 총계 = %d", sum.Totals.Budget)
	}
	// 사용자가 이번 기간 예산을 바꾸면 다시 덮어쓰지 않는다.
	if err := c.q.UpsertBudget(ctx, dbq.UpsertBudgetParams{PeriodStart: cur.StartDate(), CategoryID: cafe, Amount: 50000}); err != nil {
		t.Fatal(err)
	}
	c.do("GET", "/v1/summary", nil, &sum)
	if sum.Totals.Budget != 50000 {
		t.Errorf("사용자가 정한 예산 유지 = %d", sum.Totals.Budget)
	}
	// 지난 기간을 볼 때는 복사하지 않는다.
	prev := cur.Prev(1)
	c.do("GET", "/v1/summary?month="+prev.Label, nil, &sum)
	if has, _ := c.q.HasBudgets(ctx, prev.StartDate()); has || sum.Totals.Budget != 0 {
		t.Errorf("지난 기간에 예산을 만들면 안 됨: %d", sum.Totals.Budget)
	}
}

// /healthz는 DB까지 확인한다.
func TestHealthzChecksDB(t *testing.T) {
	c := newClient(t)
	rec := httptest.NewRecorder()
	c.h.ServeHTTP(rec, httptest.NewRequest("GET", "/healthz", nil))
	if rec.Code != 200 {
		t.Fatalf("DB가 살아 있으면 200: %d", rec.Code)
	}
	c.pool.Close()
	rec = httptest.NewRecorder()
	c.h.ServeHTTP(rec, httptest.NewRequest("GET", "/healthz", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("DB에 못 닿으면 503: %d", rec.Code)
	}
}
