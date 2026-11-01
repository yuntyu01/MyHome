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

	"github.com/yuntyu01/MyHome/server/internal/auth"
	"github.com/yuntyu01/MyHome/server/internal/config"
	"github.com/yuntyu01/MyHome/server/internal/parser"
	"github.com/yuntyu01/MyHome/server/internal/pipeline"
	"github.com/yuntyu01/MyHome/server/internal/server"
	"github.com/yuntyu01/MyHome/server/internal/testdb"
)

const (
	allowedID  = int64(1111)
	strangerID = int64(9999)
	ourAppID   = int64(777)
)

// fakeKakao는 카카오 API 가짜 구현이다.
type fakeKakao struct{}

func (fakeKakao) AuthorizeURL(state string) string {
	return "https://kakao.example/authorize?state=" + url.QueryEscape(state)
}

func (fakeKakao) ExchangeCode(_ context.Context, code string) (string, error) {
	switch code {
	case "code-allowed":
		return "tok-allowed", nil
	case "code-stranger":
		return "tok-stranger", nil
	}
	return "", auth.ErrKakaoUnauthorized
}

func (fakeKakao) TokenInfo(_ context.Context, tok string) (auth.KakaoTokenInfo, error) {
	switch tok {
	case "tok-allowed":
		return auth.KakaoTokenInfo{ID: allowedID, AppID: ourAppID}, nil
	case "tok-stranger":
		return auth.KakaoTokenInfo{ID: strangerID, AppID: ourAppID}, nil
	case "tok-otherapp":
		return auth.KakaoTokenInfo{ID: allowedID, AppID: 1}, nil
	}
	return auth.KakaoTokenInfo{}, auth.ErrKakaoUnauthorized
}

func newAuthServer(t *testing.T) http.Handler {
	t.Helper()
	pool := testdb.New(t)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	reg, _ := parser.NewRegistry("")
	cfg := config.Config{
		MaxBodyBytes: 64 * 1024, RateLimitRPS: 1000, RateLimitBurst: 1000,
		AllowedKakaoIDs: []int64{allowedID}, KakaoAppID: ourAppID,
		CookieSecure: true, SessionTTL: time.Hour, AppEnv: "production",
	}
	return server.New(server.Deps{Config: cfg, Pool: pool, Logger: logger, Kakao: fakeKakao{},
		Processor: &pipeline.Processor{Pool: pool, Registry: reg, Logger: logger}})
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

// webLogin은 카카오 웹 로그인 흐름을 끝까지 진행하고 응답을 돌려준다.
func webLogin(t *testing.T, h http.Handler, code string) *httptest.ResponseRecorder {
	t.Helper()
	start := serve(h, httptest.NewRequest("GET", "/auth/kakao/login", nil))
	if start.Code != http.StatusFound {
		t.Fatalf("로그인 시작 status %d", start.Code)
	}
	st := cookie(start, "mh_oauth_state")
	if st == nil || !st.HttpOnly {
		t.Fatal("state 쿠키 필요")
	}
	cb := httptest.NewRequest("GET", "/auth/kakao/callback?code="+code+"&state="+url.QueryEscape(st.Value), nil)
	cb.AddCookie(st)
	return serve(h, cb)
}

func TestWebLogin_AllowedAccount(t *testing.T) {
	h := newAuthServer(t)
	rec := webLogin(t, h, "code-allowed")
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("callback status %d: %s", rec.Code, rec.Body.String())
	}
	sess := cookie(rec, auth.SessionCookie)
	if sess == nil || !sess.HttpOnly || !sess.Secure || sess.SameSite != http.SameSiteLaxMode {
		t.Fatalf("세션 쿠키 속성 이상: %+v", sess)
	}

	// 세션으로 API 조회
	req := httptest.NewRequest("GET", "/v1/summary", nil)
	req.AddCookie(sess)
	if code := serve(h, req).Code; code != 200 {
		t.Fatalf("세션 API status %d", code)
	}

	// CSRF 토큰은 웹 페이지에 들어 있다
	home := httptest.NewRequest("GET", "/", nil)
	home.AddCookie(sess)
	page := serve(h, home)
	m := regexp.MustCompile(`name="csrf_token" value="([^"]+)"`).FindStringSubmatch(page.Body.String())
	if m == nil {
		t.Fatalf("CSRF 토큰을 페이지에서 못 찾음: %s", page.Body.String())
	}

	body := `{"amount":1000,"merchant":"현금","category_id":1}`
	noCSRF := httptest.NewRequest("POST", "/v1/transactions", strings.NewReader(body))
	noCSRF.AddCookie(sess)
	if code := serve(h, noCSRF).Code; code != http.StatusForbidden {
		t.Errorf("CSRF 없는 세션 POST status %d, want 403", code)
	}
	withCSRF := httptest.NewRequest("POST", "/v1/transactions", strings.NewReader(body))
	withCSRF.AddCookie(sess)
	withCSRF.Header.Set(auth.CSRFHeader, m[1])
	if code := serve(h, withCSRF).Code; code != http.StatusCreated {
		t.Errorf("CSRF 있는 세션 POST status %d", code)
	}

	// 로그아웃 후에는 세션이 무효
	out := httptest.NewRequest("POST", "/auth/logout", strings.NewReader("csrf_token="+url.QueryEscape(m[1])))
	out.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	out.AddCookie(sess)
	serve(h, out)
	after := httptest.NewRequest("GET", "/v1/summary", nil)
	after.AddCookie(sess)
	if code := serve(h, after).Code; code != http.StatusUnauthorized {
		t.Errorf("로그아웃 후 status %d", code)
	}
}

func TestWebLogin_RejectsUnknownAccount(t *testing.T) {
	h := newAuthServer(t)
	rec := webLogin(t, h, "code-stranger")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("허용 안 된 계정 status %d", rec.Code)
	}
	if cookie(rec, auth.SessionCookie) != nil {
		t.Fatal("세션이 만들어지면 안 됨")
	}
	if !strings.Contains(rec.Body.String(), "9999") {
		t.Error("본인 회원번호를 안내해야 함(허용 목록 등록용)")
	}
}

func TestWebLogin_StateMismatch(t *testing.T) {
	h := newAuthServer(t)
	req := httptest.NewRequest("GET", "/auth/kakao/callback?code=code-allowed&state=forged", nil)
	req.AddCookie(&http.Cookie{Name: "mh_oauth_state", Value: "real"})
	if code := serve(h, req).Code; code != http.StatusBadRequest {
		t.Errorf("state 불일치 status %d", code)
	}
}

func TestWebPagesRedirectToLogin(t *testing.T) {
	h := newAuthServer(t)
	rec := serve(h, httptest.NewRequest("GET", "/", nil))
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/login" {
		t.Errorf("미로그인 홈 → /login: %d %s", rec.Code, rec.Header().Get("Location"))
	}
}

func deviceLogin(h http.Handler, accessToken string) (*httptest.ResponseRecorder, string) {
	b, _ := json.Marshal(map[string]string{"access_token": accessToken, "device_name": "galaxy"})
	rec := serve(h, httptest.NewRequest("POST", "/v1/auth/kakao/device", bytes.NewReader(b)))
	var out struct{ Token string }
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec, out.Token
}

func postEvent(h http.Handler, token string, text string) int {
	b, _ := json.Marshal(map[string]any{"source_package": "test.pkg", "title": "합성", "text": text, "posted_at": time.Now().UnixMilli()})
	req := httptest.NewRequest("POST", "/v1/events", bytes.NewReader(b))
	req.Header.Set("Authorization", "Bearer "+token)
	return serve(h, req).Code
}

func TestAppLogin_IssuesDeviceToken_RevokedTokenRejected(t *testing.T) {
	h := newAuthServer(t)
	rec, tok := deviceLogin(h, "tok-allowed")
	if rec.Code != http.StatusCreated || !strings.HasPrefix(tok, auth.TokenPrefix) {
		t.Fatalf("기기 토큰 발급 status %d body %s", rec.Code, rec.Body.String())
	}
	if code := postEvent(h, tok, "1,000원 합성"); code != http.StatusCreated {
		t.Fatalf("발급 토큰으로 수집 status %d", code)
	}

	// 기기 목록에서 자기 자신을 찾아 폐기
	req := httptest.NewRequest("GET", "/v1/devices", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	var list struct {
		Devices []struct {
			ID      int64
			Current bool
		}
	}
	_ = json.Unmarshal(serve(h, req).Body.Bytes(), &list)
	if len(list.Devices) != 1 || !list.Devices[0].Current {
		t.Fatalf("기기 목록: %+v", list)
	}
	rv := httptest.NewRequest("POST", "/v1/devices/"+itoa(list.Devices[0].ID)+"/revoke", nil)
	rv.Header.Set("Authorization", "Bearer "+tok)
	if code := serve(h, rv).Code; code != 200 {
		t.Fatalf("폐기 status %d", code)
	}
	if code := postEvent(h, tok, "2,000원 합성"); code != http.StatusUnauthorized {
		t.Errorf("폐기한 토큰으로 수집 status %d, want 401", code)
	}
}

func TestAppLogin_Rejections(t *testing.T) {
	h := newAuthServer(t)
	if rec, _ := deviceLogin(h, "tok-stranger"); rec.Code != http.StatusForbidden {
		t.Errorf("허용 안 된 계정 status %d", rec.Code)
	}
	if rec, _ := deviceLogin(h, "tok-otherapp"); rec.Code != http.StatusForbidden {
		t.Errorf("다른 앱 토큰 status %d", rec.Code)
	}
	if rec, _ := deviceLogin(h, "garbage"); rec.Code != http.StatusUnauthorized {
		t.Errorf("잘못된 토큰 status %d", rec.Code)
	}
}
