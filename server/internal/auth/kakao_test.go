package auth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// 카카오 서버 흉내: 요청 형식(폼 필드, Bearer 헤더)을 확인한다.
func TestKakaoHTTP(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/oauth/token":
			_ = r.ParseForm()
			if r.Method != "POST" || r.PostForm.Get("grant_type") != "authorization_code" || r.PostForm.Get("client_id") != "rest" ||
				r.PostForm.Get("client_secret") != "sec" || r.PostForm.Get("redirect_uri") != "https://me/cb" {
				w.WriteHeader(400)
				return
			}
			if r.PostForm.Get("code") != "good" {
				w.WriteHeader(400)
				return
			}
			_, _ = w.Write([]byte(`{"access_token":"at","token_type":"bearer"}`))
		case "/v1/user/access_token_info":
			if r.Header.Get("Authorization") != "Bearer at" {
				w.WriteHeader(401)
				return
			}
			_, _ = w.Write([]byte(`{"id":1234,"expires_in":100,"app_id":77}`))
		default:
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()
	k := &KakaoHTTP{RestAPIKey: "rest", ClientSecret: "sec", RedirectURI: "https://me/cb", AuthBase: srv.URL, APIBase: srv.URL}

	u, _ := url.Parse(k.AuthorizeURL("st"))
	if !strings.HasSuffix(u.Path, "/oauth/authorize") || u.Query().Get("state") != "st" || u.Query().Get("client_id") != "rest" || u.Query().Get("response_type") != "code" {
		t.Errorf("authorize URL: %s", u)
	}
	tok, err := k.ExchangeCode(context.Background(), "good")
	if err != nil || tok != "at" {
		t.Fatalf("ExchangeCode = %q, %v", tok, err)
	}
	if _, err := k.ExchangeCode(context.Background(), "bad"); !errors.Is(err, ErrKakaoUnauthorized) {
		t.Errorf("잘못된 코드: %v", err)
	}
	info, err := k.TokenInfo(context.Background(), "at")
	if err != nil || info.ID != 1234 || info.AppID != 77 {
		t.Fatalf("TokenInfo = %+v, %v", info, err)
	}
	if _, err := k.TokenInfo(context.Background(), "nope"); !errors.Is(err, ErrKakaoUnauthorized) {
		t.Errorf("잘못된 토큰: %v", err)
	}
}
