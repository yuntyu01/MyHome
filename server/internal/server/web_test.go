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

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/yuntyu01/MyHome/server/internal/auth"
	"github.com/yuntyu01/MyHome/server/internal/auth/accesstest"
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
	h    http.Handler // 모든 요청에 Access JWT(owner)를 붙인다(Cloudflare Access를 거친 브라우저처럼)
	raw  http.Handler // JWT를 붙이지 않는 원래 서버
	sess *http.Cookie
	csrf string
	pool *pgxpool.Pool
	q    *dbq.Queries
	proc *pipeline.Processor
}

// withJWT는 요청에 Access JWT 헤더가 없으면 붙인다.
func withJWT(h http.Handler, jwt string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get(auth.AccessHeader) == "" {
			r.Header.Set(auth.AccessHeader, jwt)
		}
		h.ServeHTTP(w, r)
	})
}

// newWebClient는 Access(가짜 키 서버)로 로그인한 웹 세션을 가진 클라이언트를 만든다.
func newWebClient(t *testing.T) *webClient { t.Helper(); return newWebClientWith(t, nil) }

// newWebClientWith는 설정을 고친 서버로 웹 클라이언트를 만든다(mutate가 nil이면 기본).
func newWebClientWith(t *testing.T, mutate func(*config.Config)) *webClient {
	t.Helper()
	pool := testdb.New(t)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	reg, _ := parser.NewRegistry("kb=test.kb;toss=test.toss")
	proc := &pipeline.Processor{Pool: pool, Registry: reg, Logger: logger}
	acc := accesstest.New(t)
	cfg := config.Config{MaxBodyBytes: 64 * 1024, RateLimitRPS: 1000, RateLimitBurst: 1000, AllowedEmails: []string{ownerEmail},
		CFAccessTeamDomain: accesstest.Team, CFAccessAUD: accesstest.AUD, CookieSecure: true, SessionTTL: time.Hour, AppEnv: "production"}
	if mutate != nil {
		mutate(&cfg)
	}
	raw := server.New(server.Deps{Config: cfg, Pool: pool, Logger: logger, Processor: proc, Access: acc.Verifier(cfg.IsAllowedEmail)})
	c := &webClient{t: t, h: withJWT(raw, accesstest.Token(ownerEmail)), raw: raw, pool: pool, q: dbq.New(pool), proc: proc}
	page := c.get("/")
	if page.Code != http.StatusOK {
		t.Fatalf("첫 화면 status %d", page.Code)
	}
	c.sess = cookie(page, auth.SessionCookie)
	m := regexp.MustCompile(`name="csrf_token" value="([^"]+)"`).FindStringSubmatch(page.Body.String())
	if c.sess == nil || m == nil {
		t.Fatal("세션 쿠키나 CSRF 토큰 없음")
	}
	c.csrf = m[1]
	return c
}

func (c *webClient) get(path string) *httptest.ResponseRecorder {
	req := httptest.NewRequest("GET", path, nil)
	if c.sess != nil {
		req.AddCookie(c.sess)
	}
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
		"/":                     "분류별 남은 돈",
		"/transactions":         "메가커피",
		"/review":               "모르는가게",
		"/budgets":              "분류별 예산",
		"/analytics":            "분류별 비중",
		"/admin":                "현금 입력",
		"/admin?tab=categories": "새 분류 이름",
		"/admin?tab=rules":      "자동 분류 규칙",
		"/admin?tab=settings":   "예산 기간 시작일",
		"/admin?tab=failed":     "읽지 못한 알림",
		"/admin?tab=devices":    "마지막 사용",
		"/report":               "월간 리포트",
		"/reconcile":            "결제 수단별 지출",
		"/more":                 "명세서 대조",
		"/invest":               "아직 옮기지 않았어요",
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
	if rec := serve(c.h, httptest.NewRequest("GET", "/login", nil)); rec.Code != 200 || !strings.Contains(rec.Body.String(), "Cloudflare Access") ||
		strings.Contains(rec.Body.String(), "카카오") {
		t.Errorf("로그인 화면 status %d", rec.Code)
	}
}

// 화면에는 개발용 말 대신 사용자가 아는 말을 쓰고, 관리 화면은 고른 탭만 보여준다.
func TestWebUsesPlainWords(t *testing.T) {
	c := newWebClient(t)
	c.event("홍*동님 4,500원 일시불 메가커피")
	c.event("홍*동님 8,000원 일시불 모르는가게")
	if body := c.get("/reconcile").Body.String(); strings.Contains(body, "PARSER_PACKAGES") || !strings.Contains(body, "KB국민카드") {
		t.Error("명세서 대조: 결제 수단을 이름으로 보여줘야 함")
	}
	if body := c.get("/review").Body.String(); strings.Contains(body, "규칙 없음") || !strings.Contains(body, "처음 보는 가맹점") {
		t.Error("확인 필요: 사유를 쉬운 말로 보여줘야 함")
	}
	month := time.Now().In(period.KST).Format("2006-01")
	if body := c.get("/").Body.String(); strings.Contains(body, month+" 남은 돈") || !strings.Contains(body, "월 남은 돈") {
		t.Error("홈: 기간을 달 이름으로 보여줘야 함")
	}
	admin := c.get("/admin?tab=rules").Body.String()
	if strings.Contains(admin, "action=\"/admin/cash\"") {
		t.Error("관리: 고른 탭만 보여줘야 함")
	}
	if !strings.Contains(admin, "<td>CU</td><td class=\"nowrap\">이름으로 시작</td>") || !strings.Contains(admin, "</html>") {
		t.Error("관리: 기본 규칙 표가 끝까지 그려져야 함")
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
	if !strings.Contains(rec.Body.String(), "<details open>") {
		t.Error("방금 고친 행은 펼쳐진 채로 돌아와야 함")
	}
	if body := c.get("/transactions").Body.String(); !strings.Contains(body, "<details >") {
		t.Error("목록의 행은 접혀 있어야 함")
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
