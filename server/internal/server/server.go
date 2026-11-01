// Package server는 HTTP 라우터와 의존성을 조립한다.
package server

import (
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/yuntyu01/MyHome/server/internal/api"
	"github.com/yuntyu01/MyHome/server/internal/auth"
	"github.com/yuntyu01/MyHome/server/internal/config"
	"github.com/yuntyu01/MyHome/server/internal/dbq"
	"github.com/yuntyu01/MyHome/server/internal/httpx"
	"github.com/yuntyu01/MyHome/server/internal/ingest"
	"github.com/yuntyu01/MyHome/server/internal/pipeline"
)

// Deps는 라우터가 쓰는 의존성이다.
type Deps struct {
	Config    config.Config
	Pool      *pgxpool.Pool
	Logger    *slog.Logger
	Processor *pipeline.Processor // nil이면 원문 저장만 한다
	Kakao     auth.Kakao          // nil이면 설정값으로 실제 카카오 클라이언트를 만든다(설정이 없으면 비활성)
}

// New는 전체 HTTP 핸들러를 만든다.
func New(d Deps) http.Handler {
	q := dbq.New(d.Pool)
	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", httpx.Healthz)

	ing := &ingest.Handler{Pool: d.Pool, Logger: d.Logger}
	if d.Processor != nil {
		ing.Processor = d.Processor
	}
	deviceLimiter := httpx.NewRateLimiter(d.Config.RateLimitRPS, d.Config.RateLimitBurst)
	mux.Handle("POST /v1/events", auth.RequireDevice(q, d.Logger, perDeviceLimit(deviceLimiter, http.HandlerFunc(ing.PostEvent))))

	authn := &auth.Authenticator{Q: q, TTL: d.Config.SessionTTL, Secure: d.Config.CookieSecure, Logger: d.Logger}
	if authn.TTL <= 0 {
		authn.TTL = 30 * 24 * time.Hour
	}
	kakao := d.Kakao
	if kakao == nil && d.Config.KakaoConfigured() {
		kakao = &auth.KakaoHTTP{
			RestAPIKey: d.Config.KakaoRestAPIKey, ClientSecret: d.Config.KakaoClientSecret, RedirectURI: d.Config.KakaoRedirectURI,
			AuthBase: d.Config.KakaoAuthBase, APIBase: d.Config.KakaoAPIBase,
		}
	}
	ah := &auth.Handlers{Cfg: d.Config, Kakao: kakao, Auth: authn, Q: q, Logger: d.Logger}
	ah.Register(mux)

	// 사용자 API: 기기 토큰(앱) 또는 웹 세션(+CSRF)
	a := &api.API{Pool: d.Pool, Editor: &pipeline.Editor{Pool: d.Pool}, Processor: d.Processor, Logger: d.Logger}
	a.Register(mux, authn.RequireAPI)

	registerWeb(mux, authn, d)

	var h http.Handler = mux
	h = httpx.LimitBody(d.Config.MaxBodyBytes, h)
	h = httpx.RateLimitByIP(httpx.NewRateLimiter(d.Config.RateLimitRPS*4, d.Config.RateLimitBurst*2), d.Config.TrustProxyHeaders, h)
	h = httpx.Recover(d.Logger, h)
	h = httpx.Logging(d.Logger, h)
	return h
}

// perDeviceLimit은 기기별로 요청 수를 제한한다. 토큰이 새어도 대량 주입을 늦춘다.
func perDeviceLimit(rl *httpx.RateLimiter, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if d, ok := auth.DeviceFromContext(r.Context()); ok {
			if !rl.Allow("device:" + strconv.FormatInt(d.ID, 10)) {
				w.Header().Set("Retry-After", "1")
				httpx.Error(w, http.StatusTooManyRequests, "too many requests")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}
