package server_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
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

type webClient struct {
	t    *testing.T
	h    http.Handler
	sess *http.Cookie
	csrf string
	q    *dbq.Queries
	proc *pipeline.Processor
}

// newWebClient는 로그인된 웹 세션을 가진 클라이언트를 만든다.
func newWebClient(t *testing.T) *webClient {
	t.Helper()
	pool := testdb.New(t)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	reg, _ := parser.NewRegistry("kb=test.kb;toss=test.toss")
	proc := &pipeline.Processor{Pool: pool, Registry: reg, Logger: logger}
	cfg := config.Config{MaxBodyBytes: 64 * 1024, RateLimitRPS: 1000, RateLimitBurst: 1000, AllowedKakaoIDs: []int64{allowedID},
		CookieSecure: true, SessionTTL: time.Hour}
	h := server.New(server.Deps{Config: cfg, Pool: pool, Logger: logger, Kakao: fakeKakao{}, Processor: proc})
	rec := webLogin(t, h, "code-allowed")
	c := &webClient{t: t, h: h, sess: cookie(rec, auth.SessionCookie), q: dbq.New(pool), proc: proc}
	page := c.get("/")
	m := regexp.MustCompile(`name="csrf_token" value="([^"]+)"`).FindStringSubmatch(page.Body.String())
	if m == nil {
		t.Fatal("CSRF 토큰 없음")
	}
	c.csrf = m[1]
	return c
}

func (c *webClient) get(path string) *httptest.ResponseRecorder {
	req := httptest.NewRequest("GET", path, nil)
	req.AddCookie(c.sess)
	return serve(c.h, req)
}

func (c *webClient) post(path string, form url.Values, htmx bool) *httptest.ResponseRecorder {
	if form == nil {
		form = url.Values{}
	}
	if !htmx {
		form.Set("csrf_token", c.csrf)
	}
	req := httptest.NewRequest("POST", path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if htmx {
		req.Header.Set("HX-Request", "true")
		req.Header.Set(auth.CSRFHeader, c.csrf)
	}
	req.AddCookie(c.sess)
	return serve(c.h, req)
}

func (c *webClient) event(text string) int64 {
	ctx := context.Background()
	raw, err := c.q.InsertRawEvent(ctx, dbq.InsertRawEventParams{SourcePackage: "test.kb", Title: "KB국민카드", Text: text, PostedAt: time.Now(), ContentHash: text + time.Now().String()})
	if err != nil {
		c.t.Fatal(err)
	}
	res, err := c.proc.ProcessRawEvent(ctx, raw.ID)
	if err != nil {
		c.t.Fatal(err)
	}
	return res.(pipeline.Result).Transaction.ID
}

func (c *webClient) cat(name string) string {
	cats, _ := c.q.ListCategories(context.Background())
	for _, x := range cats {
		if x.Name == name {
			return itoa(x.ID)
		}
	}
	return ""
}

func TestWebPagesRender(t *testing.T) {
	c := newWebClient(t)
	c.event("홍*동님 4,500원 일시불 메가커피")
	c.event("홍*동님 8,000원 일시불 모르는가게")
	for path, want := range map[string]string{
		"/":             "분류별 남은 돈",
		"/transactions": "메가커피",
		"/review":       "모르는가게",
		"/budgets":      "분류별 예산",
		"/analytics":    "분류별 비중",
		"/admin":        "가맹점 규칙",
		"/report":       "월간 리포트",
		"/reconcile":    "출처별 확정 지출",
		"/more":         "대사 도우미",
	} {
		rec := c.get(path)
		if rec.Code != 200 {
			t.Errorf("%s status %d", path, rec.Code)
			continue
		}
		if !strings.Contains(rec.Body.String(), want) {
			t.Errorf("%s에 %q가 없음", path, want)
		}
		if csp := rec.Header().Get("Content-Security-Policy"); !strings.Contains(csp, "script-src 'self'") {
			t.Errorf("%s CSP 헤더 없음", path)
		}
	}
	if rec := serve(c.h, httptest.NewRequest("GET", "/static/vendor/htmx-2.0.11.min.js", nil)); rec.Code != 200 {
		t.Errorf("정적 파일 status %d", rec.Code)
	}
	if rec := serve(c.h, httptest.NewRequest("GET", "/login", nil)); rec.Code != 200 || !strings.Contains(rec.Body.String(), "카카오로 로그인") {
		t.Errorf("로그인 화면 status %d", rec.Code)
	}
}

func TestWebReviewClassifyAndLearn(t *testing.T) {
	c := newWebClient(t)
	id := c.event("홍*동님 8,000원 일시불 골목식당")
	rec := c.post("/web/review/"+itoa(id), url.Values{"category_id": {c.cat("식비")}, "remember": {"on"}}, true)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "다음부터 자동으로 분류해요") || !strings.Contains(rec.Body.String(), "확인할 거래가 없어요") {
		t.Fatalf("확인 필요 분류: %d %s", rec.Code, rec.Body.String())
	}
	tx, _ := c.q.GetTransaction(context.Background(), id)
	if tx.Status != "confirmed" {
		t.Errorf("status = %s", tx.Status)
	}
}

func TestWebTransactionRowUpdate(t *testing.T) {
	c := newWebClient(t)
	id := c.event("홍*동님 4,500원 일시불 메가커피")
	rec := c.post("/web/transactions/"+itoa(id), url.Values{"memo": {"친구랑"}}, true)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `value="친구랑"`) || !strings.HasPrefix(strings.TrimSpace(rec.Body.String()), "<div id=\"tx-") {
		t.Fatalf("행 갱신: %d %s", rec.Code, rec.Body.String())
	}
	rec = c.post("/web/transactions/"+itoa(id), url.Values{"status": {"excluded"}}, true)
	if !strings.Contains(rec.Body.String(), "제외 해제") {
		t.Errorf("제외 후 행에 '제외 해제' 버튼")
	}
}

func TestWebBudgetsSaveAndCopy(t *testing.T) {
	c := newWebClient(t)
	month := time.Now().In(period.KST).Format("2006-01")
	rec := c.post("/budgets", url.Values{"month": {month}, "amount_" + c.cat("식비"): {"300,000"}}, false)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("예산 저장 status %d", rec.Code)
	}
	page := c.get("/budgets?month=" + month)
	if !strings.Contains(page.Body.String(), `value="300,000"`) {
		t.Error("저장한 예산이 보여야 함")
	}
	next := period.ForMonth(time.Now().In(period.KST).Year(), time.Now().In(period.KST).Month()+1, 1).Label
	rec = c.post("/budgets/copy", url.Values{"month": {next}}, false)
	if rec.Code != http.StatusSeeOther || !strings.Contains(rec.Header().Get("Location"), "1%EA%B0%9C") {
		t.Errorf("예산 복사: %d %s", rec.Code, rec.Header().Get("Location"))
	}
}

func TestWebAdminForms(t *testing.T) {
	c := newWebClient(t)
	if rec := c.post("/admin/cash", url.Values{"amount": {"5,000"}, "category_id": {c.cat("카페·간식")}, "merchant": {"붕어빵"}}, false); rec.Code != http.StatusSeeOther || strings.Contains(rec.Header().Get("Location"), "err=") {
		t.Errorf("현금 입력: %d %s", rec.Code, rec.Header().Get("Location"))
	}
	if rec := c.post("/admin/rules", url.Values{"pattern": {"동네빵집"}, "match_type": {"contains"}, "category_id": {c.cat("카페·간식")}, "flow": {"out"}, "priority": {"50"}}, false); strings.Contains(rec.Header().Get("Location"), "err=") {
		t.Errorf("규칙 추가 실패: %s", rec.Header().Get("Location"))
	}
	if rec := c.post("/admin/settings", url.Values{"budget_start_day": {"25"}, "dedupe_window_seconds": {"120"}, "own_accounts": {"토스증권=investment\n내 저축=own_transfer"}, "prepaid_keywords": {"카카오페이머니, 쿠팡캐시"}}, false); strings.Contains(rec.Header().Get("Location"), "err=") {
		t.Errorf("설정 저장 실패: %s", rec.Header().Get("Location"))
	}
	cfg, _ := pipeline.LoadConfig(context.Background(), c.q)
	if cfg.BudgetStartDay != 25 || len(cfg.OwnAccounts) != 2 || len(cfg.PrepaidKeywords) != 2 {
		t.Errorf("설정 반영: %+v", cfg)
	}
	if rec := c.post("/admin/categories", url.Values{"name": {"반려동물"}, "kind": {"expense"}}, false); strings.Contains(rec.Header().Get("Location"), "err=") {
		t.Errorf("분류 추가 실패")
	}
}

func TestWebRequiresCSRF(t *testing.T) {
	c := newWebClient(t)
	req := httptest.NewRequest("POST", "/admin/cash", strings.NewReader("amount=1000&category_id=1"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(c.sess)
	if code := serve(c.h, req).Code; code != http.StatusForbidden {
		t.Errorf("CSRF 없는 폼 status %d, want 403", code)
	}
}
