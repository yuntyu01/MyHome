package server_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
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

// 수집 응답과 GET /v1/events/notice가 원래 알림을 지울지 알려 준다: 알아본 결제만 dismiss,
// 규칙 없는 지출은 AI가 켜져 있으면 wait(판정 대기), 꺼져 있으면 keep. 사용자가 분류하면 dismiss.
func TestEventNotice(t *testing.T) {
	c := newClient(t)
	now := time.Now()
	known := c.event("test.kb", "KB국민카드", "홍*동님 4,500원 일시불 메가커피 역삼점", now)
	unknown := c.event("test.kb", "KB국민카드", "홍*동님 9,000원 일시불 처음보는가게", now.Add(time.Second))
	if known["notice"] != "dismiss" || unknown["notice"] != "keep" {
		t.Fatalf("수집 응답 notice: %v / %v", known["notice"], unknown["notice"])
	}
	kid, uid := int64(known["id"].(float64)), int64(unknown["id"].(float64))
	txID := int64(unknown["result"].(map[string]any)["transaction"].(map[string]any)["id"].(float64))
	if code := c.do("PATCH", fmt.Sprintf("/v1/transactions/%d", txID), map[string]any{"category_id": c.catID("식비")}, nil); code != 200 {
		t.Fatalf("patch %d", code)
	}
	var out struct {
		Notices []struct {
			ID     int64
			Notice string
		}
	}
	if code := c.do("GET", fmt.Sprintf("/v1/events/notice?ids=%d,%d,999999", kid, uid), nil, &out); code != 200 {
		t.Fatalf("notice %d", code)
	}
	want := []string{"dismiss", "dismiss", "keep"}
	if len(out.Notices) != 3 {
		t.Fatalf("notices %+v", out.Notices)
	}
	for i, n := range out.Notices {
		if n.Notice != want[i] {
			t.Errorf("%d: %s, 기대 %s", n.ID, n.Notice, want[i])
		}
	}
	if code := c.do("GET", "/v1/events/notice?ids=1,x", nil, nil); code != 400 {
		t.Errorf("잘못된 id: %d", code)
	}

	ai := newClientWith(t, func(cfg *config.Config) { cfg.AIClassify = true })
	if r := ai.event("test.kb", "KB국민카드", "홍*동님 9,000원 일시불 처음보는가게", now); r["notice"] != "wait" {
		t.Errorf("AI 켜짐: %v", r["notice"])
	}
}

// 토스뱅크 알림 잔액으로 빈 돈을 찾고(요약·목록), 채우면 확인 필요 거래가 생기고 빈 돈이 닫힌다. 무시도 닫는다
func TestBalanceGaps(t *testing.T) {
	c := newClient(t)
	now := time.Now().Add(-time.Hour)
	c.event("test.toss", "토스뱅크", "입금 100,000원 | 홍길동\n잔액 1,100,000원", now)
	c.event("test.toss", "토스뱅크", "출금 30,000원 | 테스트상점\n잔액 1,070,000원", now.Add(time.Minute))
	c.event("test.toss", "토스뱅크", "출금 20,000원 | 다른상점\n잔액 1,000,000원", now.Add(2*time.Minute)) // 50,000 놓침
	c.event("test.toss", "토스뱅크", "입금 10,000원 | 김철수\n잔액 1,015,000원", now.Add(3*time.Minute))  // 5,000 놓침

	var sum struct {
		Balances []struct {
			Label    string
			Balance  int64
			OpenGaps int `json:"open_gaps"`
		}
		NeedsReview int64 `json:"needs_review"`
	}
	if code := c.do("GET", "/v1/summary", nil, &sum); code != 200 {
		t.Fatalf("summary %d", code)
	}
	if len(sum.Balances) != 1 || sum.Balances[0].Label != "토스뱅크" || sum.Balances[0].Balance != 1_015_000 || sum.Balances[0].OpenGaps != 2 {
		t.Fatalf("요약 잔액 %+v", sum.Balances)
	}
	review := sum.NeedsReview

	type gap struct {
		ToID   int64 `json:"to_id"`
		Amount int64
	}
	var list struct {
		Accounts []struct{ Gaps []gap }
	}
	if code := c.do("GET", "/v1/balances", nil, &list); code != 200 || len(list.Accounts) != 1 || len(list.Accounts[0].Gaps) != 2 {
		t.Fatalf("목록 %d %+v", code, list)
	}
	in, out := list.Accounts[0].Gaps[0], list.Accounts[0].Gaps[1] // 최근 것부터
	if in.Amount != 5_000 || out.Amount != -50_000 {
		t.Fatalf("빈 돈 %+v %+v", in, out)
	}

	var filled struct {
		Transaction map[string]any
	}
	if code := c.do("POST", fmt.Sprintf("/v1/balances/gaps/%d/fill", out.ToID), nil, &filled); code != 201 {
		t.Fatalf("채우기 %d", code)
	}
	tx := filled.Transaction
	if tx["amount"].(float64) != 50_000 || tx["flow"] != "out" || tx["status"] != "needs_review" || tx["review_reason"] != "balance_gap" || tx["merchant"] != "토스뱅크 빈 돈" {
		t.Fatalf("채운 거래 %+v", tx)
	}
	if code := c.do("POST", fmt.Sprintf("/v1/balances/gaps/%d/fill", out.ToID), nil, nil); code != 409 {
		t.Errorf("두 번 채우기 %d", code)
	}
	if code := c.do("POST", fmt.Sprintf("/v1/balances/gaps/%d/ignore", in.ToID), nil, nil); code != 200 {
		t.Fatalf("무시 %d", code)
	}
	if code := c.do("GET", "/v1/summary", nil, &sum); code != 200 || sum.Balances[0].OpenGaps != 0 || sum.NeedsReview != review+1 {
		t.Fatalf("처리 뒤 %+v review=%d→%d", sum.Balances, review, sum.NeedsReview)
	}
}

// 읽지 못한 알림을 앱에서 처리한다: 결제 아님으로 치우거나, 금액·분류를 넣어 적는다(그 원문에 달린 확정 거래).
// 처리한 것은 목록·요약 수에서 빠지고, 폰의 원래 알림은 지워도 된다(notice dismiss). 두 번 처리하면 400
func TestResolveUnreadNotifications(t *testing.T) {
	c := newClient(t)
	posted := time.Now().Add(-10 * time.Minute).Truncate(time.Second)
	a := c.event("test.toss", "토스", "결제 내역을 확인해 주세요", posted)
	b := c.event("test.toss", "토스", "결제 알림을 다시 확인해 주세요", posted.Add(time.Minute))
	aid, bid := int64(a["id"].(float64)), int64(b["id"].(float64))
	failed := func() int {
		var raw struct {
			RawEvents []struct{ ID int64 } `json:"raw_events"`
		}
		c.do("GET", "/v1/raw-events?failed=true", nil, &raw)
		return len(raw.RawEvents)
	}
	if n := failed(); n != 2 {
		t.Fatalf("처리 전 %d", n)
	}
	if code := c.do("POST", fmt.Sprintf("/v1/raw-events/%d/ignore", aid), nil, nil); code != 200 {
		t.Fatalf("결제 아님 %d", code)
	}
	var created struct {
		Transaction map[string]any
	}
	body := map[string]any{"amount": 12_000, "merchant": "합성분식", "category_id": c.catID("식비")}
	if code := c.do("POST", fmt.Sprintf("/v1/raw-events/%d/record", bid), body, &created); code != 201 {
		t.Fatalf("적기 %d", code)
	}
	tx := created.Transaction
	if tx["status"] != "confirmed" || tx["amount"].(float64) != 12_000 || tx["category"] != "식비" || int64(tx["raw_event_id"].(float64)) != bid {
		t.Fatalf("적은 거래 %+v", tx)
	}
	if at, _ := time.Parse(time.RFC3339, tx["occurred_at"].(string)); !at.Equal(posted.Add(time.Minute)) {
		t.Errorf("알림 시각으로 적어야 함: %v", tx["occurred_at"])
	}
	if code := c.do("POST", fmt.Sprintf("/v1/raw-events/%d/record", bid), body, nil); code != 400 {
		t.Errorf("두 번 적기 %d", code)
	}
	if n := failed(); n != 0 {
		t.Errorf("처리 뒤 %d", n)
	}
	var sum struct {
		FailedEvents int64 `json:"failed_events"`
	}
	if c.do("GET", "/v1/summary", nil, &sum); sum.FailedEvents != 0 {
		t.Errorf("요약 읽지 못한 알림 %d", sum.FailedEvents)
	}
	var out struct {
		Notices []struct{ Notice string }
	}
	c.do("GET", fmt.Sprintf("/v1/events/notice?ids=%d,%d", aid, bid), nil, &out)
	if len(out.Notices) != 2 || out.Notices[0].Notice != "dismiss" || out.Notices[1].Notice != "dismiss" {
		t.Errorf("처리한 알림은 지움: %+v", out.Notices)
	}
}

// 앱 오류 보고는 로그에만 남기고 200. 빈 보고는 400
func TestClientErrorReport(t *testing.T) {
	c := newClient(t)
	if code := c.do("POST", "/v1/client-errors", map[string]any{"kind": "crash", "message": "java.lang.IllegalStateException: 합성", "app_version": "1.0(1)"}, nil); code != 200 {
		t.Fatalf("보고 %d", code)
	}
	if code := c.do("POST", "/v1/client-errors", map[string]any{"kind": "crash"}, nil); code != 400 {
		t.Errorf("빈 보고 %d", code)
	}
}
