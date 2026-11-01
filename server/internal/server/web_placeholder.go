package server

import (
	"fmt"
	"html"
	"net/http"

	"github.com/yuntyu01/MyHome/server/internal/auth"
)

// registerWeb은 웹 화면 라우트를 등록한다. (Phase 4: 로그인 화면만. Phase 5에서 웹 전체로 교체)
func registerWeb(mux *http.ServeMux, authn *auth.Authenticator, d Deps) {
	mux.HandleFunc("GET /login", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		dev := ""
		if d.Config.AppEnv == "development" && d.Config.DevLoginKakaoID != 0 {
			dev = `<p><a href="/auth/dev-login">개발용 로그인</a></p>`
		}
		fmt.Fprintf(w, `<!doctype html><html lang="ko"><head><meta charset="utf-8"><title>로그인</title></head>
<body><h1>MyHome 가계부</h1><p><a href="/auth/kakao/login">카카오로 로그인</a></p>%s</body></html>`, dev)
	})
	mux.Handle("GET /{$}", authn.RequireWeb(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, _ := auth.UserFromContext(r.Context())
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, `<!doctype html><html lang="ko"><head><meta charset="utf-8"><title>MyHome</title></head>
<body><p>로그인됨 (회원번호 %d)</p><form method="post" action="/auth/logout"><input type="hidden" name="csrf_token" value="%s"><button>로그아웃</button></form></body></html>`,
			u.KakaoID, html.EscapeString(u.CSRF))
	})))
}
