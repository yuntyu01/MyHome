package server

import (
	"net/http"

	"github.com/yuntyu01/MyHome/server/internal/api"
	"github.com/yuntyu01/MyHome/server/internal/auth"
	"github.com/yuntyu01/MyHome/server/internal/web"
)

// registerWeb은 PC 웹 화면을 등록한다. 템플릿은 바이너리에 내장돼 있어서 파싱 실패는 프로그램 오류다.
func registerWeb(mux *http.ServeMux, authn *auth.Authenticator, ah *auth.Handlers, a *api.API, d Deps) {
	w, err := web.New(a, authn, ah, d.Logger, ah.DevLoginEnabled())
	if err != nil {
		panic(err)
	}
	w.Register(mux)
}
