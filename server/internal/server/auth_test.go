package server_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/yuntyu01/MyHome/server/internal/auth"
	"github.com/yuntyu01/MyHome/server/internal/auth/accesstest"
	"github.com/yuntyu01/MyHome/server/internal/config"
	"github.com/yuntyu01/MyHome/server/internal/parser"
	"github.com/yuntyu01/MyHome/server/internal/pipeline"
	"github.com/yuntyu01/MyHome/server/internal/server"
	"github.com/yuntyu01/MyHome/server/internal/testdb"
)

// 로그인: 웹은 Cloudflare Access JWT(가짜 키 서버로 서명), 앱은 1회용 코드 + PKCE로 기기 토큰을 받는다.

const (
	ownerEmail  = "owner@example.com"
	secondEmail = "second@example.com"
)

type authEnv struct {
	h    http.Handler
	acc  *accesstest.Server
	pool *pgxpool.Pool
}

// newAuthEnv는 서버를 만든다. access가 true면 가짜 Access 검증기를 넣는다.
func newAuthEnv(t *testing.T, access bool, mutate func(*config.Config)) *authEnv {
	t.Helper()
	pool := testdb.New(t)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	reg, _ := parser.NewRegistry("")
	cfg := config.Config{
		MaxBodyBytes: 64 * 1024, RateLimitRPS: 1000, RateLimitBurst: 1000,
		AllowedEmails: []string{ownerEmail, secondEmail}, CookieSecure: true, SessionTTL: time.Hour, AppEnv: "production",
	}
	env := &authEnv{pool: pool}
	if access {
		env.acc = accesstest.New(t)
		cfg.CFAccessTeamDomain, cfg.CFAccessAUD = accesstest.Team, accesstest.AUD
	}
	if mutate != nil {
		mutate(&cfg)
	}
	d := server.Deps{Config: cfg, Pool: pool, Logger: logger, Processor: &pipeline.Processor{Pool: pool, Registry: reg, Logger: logger}}
	if access {
		d.Access = env.acc.Verifier(cfg.IsAllowedEmail)
	}
	env.h = server.New(d)
	return env
}

func serve(h http.Handler, req *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func cookie(rec *httptest.ResponseRecorder, name string) *http.Cookie {
	for _, c := range rec.Result().Cookies() {
		if c.Name == name {
			return c
		}
	}
	return nil
}

// webReq는 jwt(빈 값이면 없음)와 쿠키를 붙인 요청이다.
func webReq(method, path, jwt string, body io.Reader, cookies ...*http.Cookie) *http.Request {
	req := httptest.NewRequest(method, path, body)
	if jwt != "" {
		req.Header.Set(auth.AccessHeader, jwt)
	}
	for _, c := range cookies {
		if c != nil {
			req.AddCookie(c)
		}
	}
	return req
}

var csrfRe = regexp.MustCompile(`name="csrf_token" value="([^"]+)"`)

func csrfOf(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	m := csrfRe.FindStringSubmatch(rec.Body.String())
	if m == nil {
		t.Fatalf("CSRF 토큰을 페이지에서 못 찾음: %d %s", rec.Code, rec.Body.String())
	}
	return m[1]
}

func TestAccessWebLogin(t *testing.T) {
	env := newAuthEnv(t, true, nil)
	jwt := accesstest.Token(ownerEmail)
	first := serve(env.h, webReq("GET", "/", jwt, nil))
	if first.Code != 200 {
		t.Fatalf("유효한 JWT 홈 status %d: %s", first.Code, first.Body.String())
	}
	sess := cookie(first, auth.SessionCookie)
	if sess == nil || !sess.HttpOnly || !sess.Secure || sess.SameSite != http.SameSiteLaxMode {
		t.Fatalf("세션 쿠키 속성 이상: %+v", sess)
	}
	csrf := csrfOf(t, first)

	// 같은 계정이면 세션을 다시 쓴다(새 쿠키 없음, CSRF 같음)
	again := serve(env.h, webReq("GET", "/transactions", jwt, nil, sess))
	if again.Code != 200 || cookie(again, auth.SessionCookie) != nil || csrfOf(t, again) != csrf {
		t.Fatalf("세션 재사용: %d", again.Code)
	}

	// 앱 API(/v1)는 Access를 거치지 않는다. 세션 쿠키 + CSRF로도 된다
	if code := serve(env.h, webReq("GET", "/v1/summary", "", nil, sess)).Code; code != 200 {
		t.Fatalf("세션 API status %d", code)
	}
	body := `{"amount":1000,"merchant":"현금","category_id":1}`
	if code := serve(env.h, webReq("POST", "/v1/transactions", "", strings.NewReader(body), sess)).Code; code != http.StatusForbidden {
		t.Errorf("CSRF 없는 세션 POST status %d, want 403", code)
	}
	withCSRF := webReq("POST", "/v1/transactions", "", strings.NewReader(body), sess)
	withCSRF.Header.Set(auth.CSRFHeader, csrf)
	if code := serve(env.h, withCSRF).Code; code != http.StatusCreated {
		t.Errorf("CSRF 있는 세션 POST status %d", code)
	}

	// 웹 폼: JWT + 세션 + CSRF면 된다. JWT가 빠지면 세션이 있어도 403(안전망)
	form := func(jwt string) int {
		req := webReq("POST", "/admin/cash", jwt, strings.NewReader("amount=1000&category_id=1&csrf_token="+url.QueryEscape(csrf)), sess)
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		return serve(env.h, req).Code
	}
	if code := form(jwt); code != http.StatusSeeOther {
		t.Errorf("웹 폼 status %d", code)
	}
	if code := form(""); code != http.StatusForbidden {
		t.Errorf("JWT 없는 웹 폼 status %d, want 403", code)
	}

	// CF_Authorization 쿠키로 와도 된다(헤더가 없을 때)
	byCookie := serve(env.h, webReq("GET", "/", "", nil, sess, &http.Cookie{Name: auth.AccessCookie, Value: jwt}))
	if byCookie.Code != 200 {
		t.Errorf("Access 쿠키 status %d", byCookie.Code)
	}
}

func TestAccessWebRejections(t *testing.T) {
	env := newAuthEnv(t, true, nil)
	sess := cookie(serve(env.h, webReq("GET", "/", accesstest.Token(ownerEmail), nil)), auth.SessionCookie)
	key := accesstest.Key(accesstest.KID)
	sign := func(edit func(map[string]any)) string {
		c := accesstest.Claims(ownerEmail)
		edit(c)
		return accesstest.Sign(accesstest.KID, key, nil, c)
	}
	for name, jwt := range map[string]string{
		"없음":        "",
		"만료":        sign(func(c map[string]any) { c["exp"] = time.Now().Add(-10 * time.Minute).Unix() }),
		"다른 aud":    sign(func(c map[string]any) { c["aud"] = []string{"other-app"} }),
		"다른 iss":    sign(func(c map[string]any) { c["iss"] = "https://evil.cloudflareaccess.com" }),
		"서명 불일치":    accesstest.Sign(accesstest.KID, accesstest.Key("attacker"), nil, accesstest.Claims(ownerEmail)),
		"허용 안 된 메일": sign(func(c map[string]any) { c["email"] = "stranger@example.com" }),
		"쓰레기":       "not-a-jwt",
	} {
		for _, withSession := range []bool{false, true} {
			var c *http.Cookie
			if withSession {
				c = sess // 유효한 세션이 있어도 JWT가 틀리면 막는다
			}
			rec := serve(env.h, webReq("GET", "/", jwt, nil, c))
			if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "Cloudflare Access") {
				t.Errorf("%s(세션 %v): status %d", name, withSession, rec.Code)
			}
			if cookie(rec, auth.SessionCookie) != nil {
				t.Errorf("%s: 세션이 만들어지면 안 됨", name)
			}
		}
	}
	// htmx 요청은 401 + HX-Redirect: / (Access 로그인으로 다시)
	hx := webReq("POST", "/web/transactions/1", "", nil, sess)
	hx.Header.Set("HX-Request", "true")
	if rec := serve(env.h, hx); rec.Code != http.StatusUnauthorized || rec.Header().Get("HX-Redirect") != "/" {
		t.Errorf("htmx: %d %q", rec.Code, rec.Header().Get("HX-Redirect"))
	}
	// 앱 연결 화면도 Access 뒤에 있다
	if code := serve(env.h, webReq("GET", "/app/link?challenge="+auth.S256(strings.Repeat("v", 43))+"&state="+strings.Repeat("s", 32), "", nil)).Code; code != http.StatusForbidden {
		t.Errorf("JWT 없는 앱 연결 status %d", code)
	}
	// 카카오 경로는 없다
	for _, p := range []string{"GET /auth/kakao/login", "GET /auth/kakao/callback", "POST /v1/auth/kakao/device", "GET /auth/dev-login"} {
		method, path, _ := strings.Cut(p, " ")
		if code := serve(env.h, webReq(method, path, accesstest.Token(ownerEmail), nil)).Code; code != http.StatusNotFound {
			t.Errorf("%s status %d, want 404", p, code)
		}
	}
}

// 세션은 JWT 이메일에 묶인다. 다른 계정의 JWT가 오면 그 세션을 버리고 새로 만든다.
func TestAccessSessionEmailMismatch(t *testing.T) {
	env := newAuthEnv(t, true, nil)
	first := serve(env.h, webReq("GET", "/", accesstest.Token(ownerEmail), nil))
	s1, csrf1 := cookie(first, auth.SessionCookie), csrfOf(t, first)

	other := serve(env.h, webReq("GET", "/", accesstest.Token(secondEmail), nil, s1))
	s2 := cookie(other, auth.SessionCookie)
	if other.Code != 200 || s2 == nil || s2.Value == s1.Value || csrfOf(t, other) == csrf1 {
		t.Fatalf("다른 계정이면 새 세션: %d %+v", other.Code, s2)
	}
	if code := serve(env.h, webReq("GET", "/v1/summary", "", nil, s1)).Code; code != http.StatusUnauthorized {
		t.Errorf("버린 세션 status %d, want 401", code)
	}
	// 옛 세션의 CSRF로는 다른 계정 세션에서 상태를 바꾸지 못한다
	req := webReq("POST", "/admin/cash", accesstest.Token(secondEmail), strings.NewReader("amount=1&category_id=1&csrf_token="+url.QueryEscape(csrf1)), s2)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if code := serve(env.h, req).Code; code != http.StatusForbidden {
		t.Errorf("옛 CSRF status %d", code)
	}
}

// 키 서버가 죽으면 403이 아니라 503(사용자 잘못이 아님).
func TestAccessKeysDown(t *testing.T) {
	env := newAuthEnv(t, true, nil)
	env.acc.Close()
	if rec := serve(env.h, webReq("GET", "/", accesstest.Token(ownerEmail), nil)); rec.Code != http.StatusServiceUnavailable {
		t.Errorf("키 서버 실패 status %d", rec.Code)
	}
}

// 운영 설정인데 Access가 없으면 웹 화면을 열지 않는다(serve는 시작 단계에서 거른다).
func TestWebClosedWithoutAccessInProduction(t *testing.T) {
	env := newAuthEnv(t, false, func(c *config.Config) { c.DevLoginEmail = ownerEmail })
	if code := serve(env.h, webReq("GET", "/", "", nil)).Code; code != http.StatusServiceUnavailable {
		t.Errorf("홈 status %d, want 503", code)
	}
	if code := serve(env.h, webReq("GET", "/auth/dev-login", "", nil)).Code; code != http.StatusNotFound {
		t.Errorf("운영에서 개발용 로그인 status %d, want 404", code)
	}
}

func TestDevLogin(t *testing.T) {
	dev := func(c *config.Config) { c.AppEnv, c.DevLoginEmail, c.CookieSecure = "development", ownerEmail, false }
	env := newAuthEnv(t, false, dev)
	if rec := serve(env.h, webReq("GET", "/", "", nil)); rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/login" {
		t.Errorf("개발 모드 미로그인 → /login: %d %s", rec.Code, rec.Header().Get("Location"))
	}
	if rec := serve(env.h, webReq("GET", "/login", "", nil)); !strings.Contains(rec.Body.String(), `href="/auth/dev-login"`) {
		t.Errorf("로그인 화면에 개발용 로그인 링크가 없음")
	}
	login := serve(env.h, webReq("GET", "/auth/dev-login", "", nil))
	sess := cookie(login, auth.SessionCookie)
	if login.Code != http.StatusSeeOther || sess == nil {
		t.Fatalf("개발용 로그인: %d", login.Code)
	}
	home := serve(env.h, webReq("GET", "/", "", nil, sess))
	if home.Code != 200 {
		t.Fatalf("개발용 세션 홈 status %d", home.Code)
	}
	// 로그아웃: Access가 없으면 /login으로
	out := webReq("POST", "/auth/logout", "", strings.NewReader("csrf_token="+url.QueryEscape(csrfOf(t, home))), sess)
	out.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if rec := serve(env.h, out); rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/login" {
		t.Errorf("개발 모드 로그아웃: %d %s", rec.Code, rec.Header().Get("Location"))
	}
	if code := serve(env.h, webReq("GET", "/", "", nil, sess)).Code; code != http.StatusSeeOther {
		t.Errorf("로그아웃 뒤 status %d", code)
	}

	// 허용 목록에 없는 개발용 이메일은 403
	bad := newAuthEnv(t, false, func(c *config.Config) { dev(c); c.DevLoginEmail = "nobody@example.com" })
	if code := serve(bad.h, webReq("GET", "/auth/dev-login", "", nil)).Code; code != http.StatusForbidden {
		t.Errorf("허용 안 된 개발용 이메일 status %d", code)
	}
	// 개발 모드라도 Access를 설정하면 개발용 로그인은 없고 JWT가 필요하다
	withAccess := newAuthEnv(t, true, dev)
	if code := serve(withAccess.h, webReq("GET", "/auth/dev-login", "", nil)).Code; code != http.StatusNotFound {
		t.Errorf("Access 설정 시 개발용 로그인 status %d", code)
	}
	if code := serve(withAccess.h, webReq("GET", "/", "", nil)).Code; code != http.StatusForbidden {
		t.Errorf("Access 설정 시 JWT 없는 홈 status %d", code)
	}
}

func TestAccessLogout(t *testing.T) {
	env := newAuthEnv(t, true, nil)
	jwt := accesstest.Token(ownerEmail)
	home := serve(env.h, webReq("GET", "/", jwt, nil))
	sess, csrf := cookie(home, auth.SessionCookie), csrfOf(t, home)

	noCSRF := webReq("POST", "/auth/logout", jwt, nil, sess)
	if code := serve(env.h, noCSRF).Code; code != http.StatusForbidden {
		t.Errorf("CSRF 없는 로그아웃 status %d", code)
	}
	out := webReq("POST", "/auth/logout", jwt, strings.NewReader("csrf_token="+url.QueryEscape(csrf)), sess)
	out.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := serve(env.h, out)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != auth.AccessLogoutPath {
		t.Errorf("로그아웃 → Access 로그아웃: %d %s", rec.Code, rec.Header().Get("Location"))
	}
	if c := cookie(rec, auth.SessionCookie); c == nil || c.MaxAge >= 0 {
		t.Errorf("세션 쿠키를 지워야 함: %+v", c)
	}
	if code := serve(env.h, webReq("GET", "/v1/summary", "", nil, sess)).Code; code != http.StatusUnauthorized {
		t.Errorf("로그아웃 뒤 세션 status %d", code)
	}
	// 로그인 화면은 Access로 가는 링크만
	if body := serve(env.h, webReq("GET", "/login", jwt, nil)).Body.String(); !strings.Contains(body, `href="/"`) || strings.Contains(body, "dev-login") {
		t.Errorf("운영 로그인 화면: %s", body)
	}
}

// ---- 앱 연결(1회용 코드 + PKCE) ----

func pkce(t *testing.T) (verifier, challenge string) {
	t.Helper()
	v, err := auth.NewToken("") // 43자 base64url = RFC 7636 verifier 문자 집합 안
	if err != nil {
		t.Fatal(err)
	}
	return v, auth.S256(v)
}

const testState = "c3RhdGUtZnJvbS10aGUtYXBwLTEyMzQ1" // 32자 base64url

var appURLRe = regexp.MustCompile(`myhome://[^"'\s>]*`)

// link는 Access 로그인한 브라우저로 /app/link를 열고 돌려받은 코드를 꺼낸다.
func link(t *testing.T, env *authEnv, challenge, state string) string {
	t.Helper()
	rec := serve(env.h, webReq("GET", "/app/link?challenge="+challenge+"&state="+state, accesstest.Token(ownerEmail), nil))
	if rec.Code != 200 {
		t.Fatalf("앱 연결 화면 status %d: %s", rec.Code, rec.Body.String())
	}
	m := regexp.MustCompile(`myhome://auth\?code=([A-Za-z0-9_-]+)&amp;state=` + state + `"`).FindStringSubmatch(rec.Body.String())
	if m == nil {
		t.Fatalf("돌아갈 주소가 없음: %s", rec.Body.String())
	}
	return m[1]
}

func exchange(env *authEnv, code, verifier, name string) *httptest.ResponseRecorder {
	b, _ := json.Marshal(map[string]string{"code": code, "verifier": verifier, "device_name": name})
	return serve(env.h, httptest.NewRequest("POST", "/v1/auth/exchange", bytes.NewReader(b)))
}

func errMsg(rec *httptest.ResponseRecorder) string {
	var out struct{ Error string }
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return out.Error
}

func TestAppLinkPage(t *testing.T) {
	env := newAuthEnv(t, true, nil)
	_, challenge := pkce(t)
	// 돌아갈 주소를 요청으로 바꾸려 해도 무시한다
	rec := serve(env.h, webReq("GET", "/app/link?challenge="+challenge+"&state="+testState+
		"&redirect_uri="+url.QueryEscape("https://evil.example/cb")+"&redirect=evil://x", accesstest.Token(ownerEmail), nil))
	if rec.Code != 200 {
		t.Fatalf("status %d", rec.Code)
	}
	body := rec.Body.String()
	urls := appURLRe.FindAllString(body, -1)
	if len(urls) != 2 {
		t.Fatalf("myhome:// 주소는 meta refresh와 버튼 두 곳: %v", urls)
	}
	m := regexp.MustCompile(`^myhome://auth\?code=([A-Za-z0-9_-]{43})&amp;state=` + testState + `$`).FindStringSubmatch(urls[0])
	if m == nil || urls[1] != urls[0] {
		t.Fatalf("고정 주소 myhome://auth + code + state만: %v", urls)
	}
	if !strings.Contains(body, `<meta http-equiv="refresh" content="0;url=`+urls[0]+`">`) || !strings.Contains(body, `href="`+urls[0]+`"`) ||
		!strings.Contains(body, "앱으로 돌아가기") {
		t.Errorf("meta refresh와 버튼: %s", body)
	}
	if strings.Contains(body, "evil") || strings.Contains(body, "<script>") || strings.Contains(body, auth.TokenPrefix) {
		t.Errorf("요청 주소·인라인 스크립트·기기 토큰이 들어가면 안 됨: %s", body)
	}
	if csp := rec.Header().Get("Content-Security-Policy"); !strings.Contains(csp, "script-src 'self'") || rec.Header().Get("Cache-Control") != "no-store" {
		t.Errorf("보안 헤더: %q %q", csp, rec.Header().Get("Cache-Control"))
	}
	// DB에는 코드 해시만(원문 없음)
	var n int
	var email string
	if err := env.pool.QueryRow(context.Background(),
		`SELECT count(*), max(email) FROM app_link_codes WHERE code_hash = $1 AND challenge = $2 AND used_at IS NULL AND expires_at > now() + interval '4 minutes'`,
		auth.HashToken(m[1]), challenge).Scan(&n, &email); err != nil || n != 1 || email != ownerEmail {
		t.Errorf("저장된 코드: n=%d email=%q err=%v", n, email, err)
	}
}

func TestAppLinkMalformed(t *testing.T) {
	env := newAuthEnv(t, true, nil)
	_, ch := pkce(t)
	for name, q := range map[string]string{
		"challenge 없음":    "state=" + testState,
		"challenge 42자":   "challenge=" + ch[:42] + "&state=" + testState,
		"challenge 44자":   "challenge=" + ch + "A&state=" + testState,
		"challenge 문자":    "challenge=" + ch[:42] + "%2B&state=" + testState,
		"state 없음":        "challenge=" + ch,
		"state 15자":       "challenge=" + ch + "&state=" + testState[:15],
		"state 65자":       "challenge=" + ch + "&state=" + strings.Repeat("a", 65),
		"state 문자":        "challenge=" + ch + "&state=" + testState[:20] + "%2F" + testState[:5],
		"plain challenge": "challenge=" + strings.Repeat("a", 20) + "&state=" + testState,
	} {
		if code := serve(env.h, webReq("GET", "/app/link?"+q, accesstest.Token(ownerEmail), nil)).Code; code != http.StatusBadRequest {
			t.Errorf("%s: status %d, want 400", name, code)
		}
	}
	var n int
	_ = env.pool.QueryRow(context.Background(), `SELECT count(*) FROM app_link_codes`).Scan(&n)
	if n != 0 {
		t.Errorf("잘못된 요청으로 코드가 생김: %d", n)
	}
}

func TestAppLinkExchange(t *testing.T) {
	env := newAuthEnv(t, true, nil)
	verifier, challenge := pkce(t)
	code := link(t, env, challenge, testState)

	// 형식이 틀린 요청은 400이고 코드를 쓰지 않는다
	for name, body := range map[string]string{
		"JSON 아님":       `not json`,
		"code 없음":       `{"verifier":"` + verifier + `"}`,
		"verifier 42자":  `{"code":"` + code + `","verifier":"` + verifier[:42] + `"}`,
		"verifier 129자": `{"code":"` + code + `","verifier":"` + strings.Repeat("a", 129) + `"}`,
		"verifier 문자":   `{"code":"` + code + `","verifier":"` + verifier[:42] + `+"}`,
		"code 문자":       `{"code":"` + code[:40] + `/+=","verifier":"` + verifier + `"}`,
	} {
		rec := serve(env.h, httptest.NewRequest("POST", "/v1/auth/exchange", strings.NewReader(body)))
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status %d, want 400", name, rec.Code)
		}
	}

	// 성공: Access JWT 없이(/v1은 Access를 거치지 않는다) 기기 토큰을 받는다
	rec := exchange(env, code, verifier, "  galaxy  ")
	var out struct {
		Token      string `json:"token"`
		DeviceID   int64  `json:"device_id"`
		DeviceName string `json:"device_name"`
	}
	if rec.Code != http.StatusCreated || json.Unmarshal(rec.Body.Bytes(), &out) != nil || !strings.HasPrefix(out.Token, auth.TokenPrefix) ||
		out.DeviceID <= 0 || out.DeviceName != "galaxy" {
		t.Fatalf("교환 status %d body %s", rec.Code, rec.Body.String())
	}
	if code := postEvent(env.h, out.Token, "1,000원 합성"); code != http.StatusCreated {
		t.Fatalf("받은 토큰으로 수집 status %d", code)
	}

	// 다시 쓰면 401
	again := exchange(env, code, verifier, "galaxy")
	if again.Code != http.StatusUnauthorized {
		t.Errorf("재사용 status %d", again.Code)
	}
	unknown := exchange(env, strings.Repeat("A", 43), verifier, "x")
	if unknown.Code != http.StatusUnauthorized || errMsg(unknown) != errMsg(again) {
		t.Errorf("없는 코드: %d %q vs %q", unknown.Code, errMsg(unknown), errMsg(again))
	}

	// 기기 목록: 앱 연결로 발급, 계정 이메일
	req := httptest.NewRequest("GET", "/v1/devices", nil)
	req.Header.Set("Authorization", "Bearer "+out.Token)
	var list struct {
		Devices []struct {
			ID      int64
			Email   string
			Via     string
			Current bool
		}
	}
	_ = json.Unmarshal(serve(env.h, req).Body.Bytes(), &list)
	if len(list.Devices) != 1 || !list.Devices[0].Current || list.Devices[0].Email != ownerEmail || list.Devices[0].Via != "app_link" {
		t.Fatalf("기기 목록: %+v", list)
	}
	// 폐기한 토큰은 거부
	rv := httptest.NewRequest("POST", "/v1/devices/"+itoa(list.Devices[0].ID)+"/revoke", nil)
	rv.Header.Set("Authorization", "Bearer "+out.Token)
	if code := serve(env.h, rv).Code; code != 200 {
		t.Fatalf("폐기 status %d", code)
	}
	if code := postEvent(env.h, out.Token, "2,000원 합성"); code != http.StatusUnauthorized {
		t.Errorf("폐기한 토큰으로 수집 status %d, want 401", code)
	}
}

func TestAppLinkExpiredAndMismatch(t *testing.T) {
	env := newAuthEnv(t, true, nil)
	ctx := context.Background()

	// 만료
	v1, c1 := pkce(t)
	code1 := link(t, env, c1, testState)
	if _, err := env.pool.Exec(ctx, `UPDATE app_link_codes SET expires_at = now() - interval '1 second' WHERE code_hash = $1`, auth.HashToken(code1)); err != nil {
		t.Fatal(err)
	}
	expired := exchange(env, code1, v1, "")
	if expired.Code != http.StatusUnauthorized {
		t.Errorf("만료 status %d", expired.Code)
	}

	// verifier가 틀리면 401이고 코드는 버려진다(맞는 verifier로도 다시 못 씀)
	v2, c2 := pkce(t)
	code2 := link(t, env, c2, testState)
	wrong, _ := pkce(t)
	mismatch := exchange(env, code2, wrong, "")
	if mismatch.Code != http.StatusUnauthorized || errMsg(mismatch) != errMsg(expired) {
		t.Errorf("verifier 불일치: %d %q", mismatch.Code, errMsg(mismatch))
	}
	if code := exchange(env, code2, v2, "").Code; code != http.StatusUnauthorized {
		t.Errorf("불일치로 버려진 코드 status %d", code)
	}
	var devices int
	_ = env.pool.QueryRow(ctx, `SELECT count(*) FROM devices`).Scan(&devices)
	if devices != 0 {
		t.Errorf("실패한 교환으로 기기가 생김: %d", devices)
	}

	// 기본 이름(android-YYYYMMDD)과 60자 자르기
	v3, c3 := pkce(t)
	rec := exchange(env, link(t, env, c3, testState), v3, "")
	if rec.Code != http.StatusCreated || !regexp.MustCompile(`"device_name":"android-\d{8}"`).MatchString(rec.Body.String()) {
		t.Errorf("기본 이름: %d %s", rec.Code, rec.Body.String())
	}
	v4, c4 := pkce(t)
	rec = exchange(env, link(t, env, c4, testState), v4, strings.Repeat("폰", 70))
	var out struct {
		DeviceName string `json:"device_name"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	if rec.Code != http.StatusCreated || utf8.RuneCountInString(out.DeviceName) != 60 {
		t.Errorf("긴 이름: %d %q", rec.Code, out.DeviceName)
	}
	// 만료된 코드는 다음 발급·교환 때 지워진다
	var left int
	_ = env.pool.QueryRow(ctx, `SELECT count(*) FROM app_link_codes WHERE code_hash = $1`, auth.HashToken(code1)).Scan(&left)
	if left != 0 {
		t.Errorf("만료 코드가 남아 있음")
	}
}

func postEvent(h http.Handler, token string, text string) int {
	b, _ := json.Marshal(map[string]any{"source_package": "test.pkg", "title": "합성", "text": text, "posted_at": time.Now().UnixMilli()})
	req := httptest.NewRequest("POST", "/v1/events", bytes.NewReader(b))
	req.Header.Set("Authorization", "Bearer "+token)
	return serve(h, req).Code
}
