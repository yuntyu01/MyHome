// Package server는 HTTP 라우터와 의존성을 조립한다.
package server

import (
	"log/slog"
	"net/http"
	"strconv"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/yuntyu01/MyHome/server/internal/auth"
	"github.com/yuntyu01/MyHome/server/internal/config"
	"github.com/yuntyu01/MyHome/server/internal/dbq"
	"github.com/yuntyu01/MyHome/server/internal/httpx"
	"github.com/yuntyu01/MyHome/server/internal/ingest"
)

// Deps는 라우터가 쓰는 의존성이다.
type Deps struct {
	Config    config.Config
	Pool      *pgxpool.Pool
	Logger    *slog.Logger
	Processor ingest.Processor
}

// New는 전체 HTTP 핸들러를 만든다.
func New(d Deps) http.Handler {
	q := dbq.New(d.Pool)
	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", httpx.Healthz)

	ing := &ingest.Handler{Pool: d.Pool, Processor: d.Processor, Logger: d.Logger}
	deviceLimiter := httpx.NewRateLimiter(d.Config.RateLimitRPS, d.Config.RateLimitBurst)
	mux.Handle("POST /v1/events", auth.RequireDevice(q, d.Logger, perDeviceLimit(deviceLimiter, http.HandlerFunc(ing.PostEvent))))

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
