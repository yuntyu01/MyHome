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
}

func newClient(t *testing.T) *client {
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
	h := server.New(server.Deps{Config: cfg, Pool: pool, Logger: logger, Processor: &pipeline.Processor{Pool: pool, Registry: reg, Logger: logger}})
	return &client{t: t, h: h, token: tok, q: q}
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
	c.event("test.toss", "토스", "이번 주 혜택을 확인해 보세요", time.Now())
	var raw struct {
		RawEvents []struct {
			ParseError *string `json:"parse_error"`
		} `json:"raw_events"`
	}
	c.do("GET", "/v1/raw-events?failed=true", nil, &raw)
	if len(raw.RawEvents) != 1 || raw.RawEvents[0].ParseError == nil {
		t.Errorf("해석 실패 원문 목록: %+v", raw)
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
