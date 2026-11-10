// Package server는 HTTP 라우터와 의존성을 조립한다.
package server

import (
	"context"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/yuntyu01/MyHome/server/internal/ai"
	"github.com/yuntyu01/MyHome/server/internal/api"
	"github.com/yuntyu01/MyHome/server/internal/auth"
	"github.com/yuntyu01/MyHome/server/internal/config"
	"github.com/yuntyu01/MyHome/server/internal/dbq"
	"github.com/yuntyu01/MyHome/server/internal/focus"
	"github.com/yuntyu01/MyHome/server/internal/health"
	"github.com/yuntyu01/MyHome/server/internal/health/foods"
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
	// Access는 웹 화면의 Cloudflare Access JWT 검증기다. nil이면 설정값(CF_ACCESS_TEAM_DOMAIN, CF_ACCESS_AUD)으로 만들고,
	// 설정도 없으면 Access 검사 없이 APP_ENV=development에서만 웹을 연다(테스트는 가짜 키 서버를 쓰는 검증기를 넣는다).
	Access *auth.AccessVerifier

	// 건강(핏두잉 이식). nil이면 설정값으로 만든다(테스트는 가짜를 넣는다).
	Analyzer ai.FoodAnalyzer
	Foods    *foods.Lookup
	Now      func() time.Time // 건강 API의 "오늘"(KST). nil이면 time.Now
}

// New는 전체 HTTP 핸들러를 만든다.
func New(d Deps) http.Handler {
	q := dbq.New(d.Pool)
	mux := http.NewServeMux()

	// DB까지 확인한다(2초 한도). 풀이 없으면(테스트 등) 프로세스만 본다.
	var ping func(context.Context) error
	if d.Pool != nil {
		ping = d.Pool.Ping
	}
	mux.HandleFunc("GET /healthz", httpx.Healthz(ping))

	ing := &ingest.Handler{Pool: d.Pool, Logger: d.Logger}
	if d.Processor != nil {
		ing.Processor = d.Processor
	}
	deviceLimiter := httpx.NewRateLimiter(d.Config.RateLimitRPS, d.Config.RateLimitBurst)
	mux.Handle("POST /v1/events", auth.RequireDevice(q, d.Logger, perDeviceLimit(deviceLimiter, http.HandlerFunc(ing.PostEvent))))

	access := d.Access
	if access == nil && d.Config.AccessConfigured() {
		access = auth.NewAccessVerifier(d.Config.CFAccessTeamDomain, d.Config.CFAccessAUD, d.Config.IsAllowedEmail)
	}
	authn := &auth.Authenticator{Q: q, TTL: d.Config.SessionTTL, Secure: d.Config.CookieSecure, Logger: d.Logger,
		Access: access, DevMode: access == nil && d.Config.AppEnv == "development", Allowed: d.Config.IsAllowedEmail}
	if authn.TTL <= 0 {
		authn.TTL = 30 * 24 * time.Hour
	}
	ah := &auth.Handlers{Cfg: d.Config, Auth: authn, Q: q, Logger: d.Logger}
	ah.Register(mux)

	// 사용자 API: 기기 토큰(앱) 또는 웹 세션(+CSRF)
	a := &api.API{Pool: d.Pool, Editor: &pipeline.Editor{Pool: d.Pool}, Processor: d.Processor, Logger: d.Logger, Suggestions: d.Config.AIClassify}
	a.Register(mux, authn.RequireAPI)

	registerWeb(mux, authn, ah, a, d)

	// 건강(핏두잉 이식): 기기 토큰(앱) 또는 웹 세션
	newHealth(d).Register(mux, authn.RequireAPI)

	// 집중(공부 타이머): 기기 토큰(앱) 또는 웹 세션
	newFocus(d).Register(mux, authn.RequireAPI)

	var h http.Handler = mux
	// 끼니 사진 분석만 사진 크기(최대 16MB)까지 받는다. 나머지는 MAX_BODY_BYTES 그대로
	h = httpx.LimitBodyRoutes(d.Config.MaxBodyBytes, map[string]int64{"POST " + health.AnalyzePath: health.MaxAnalyzeBytes, "PUT " + health.MealPhotosPath: health.MaxAnalyzeBytes, "POST " + health.BodyScanPath: health.MaxAnalyzeBytes}, h)
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

// newHealth는 건강 API를 조립한다. 분석기는 AI_PROVIDER로 고른다(설정 검증은 config.Load가 한다).
func newHealth(d Deps) *health.Handler {
	analyzer := d.Analyzer
	if analyzer == nil {
		a, err := ai.New(d.Config.AIProvider, d.Config.GeminiAPIKey, d.Config.GeminiModel)
		if err != nil {
			panic(err) // config.Load가 먼저 거른다. 여기 오면 프로그램 오류다
		}
		analyzer = a
	}
	fl := d.Foods
	if fl == nil {
		// 식약처 키가 없어도 Open Food Facts(키 없음)로 찾는다
		fl = foods.New(d.Config.FoodSafetyAPIKey, d.Config.DataGoKrAPIKey, "", "").WithOFF("")
	}
	return &health.Handler{Svc: &health.Service{Pool: d.Pool, Now: d.Now, PhotoDir: d.Config.MealPhotoDir}, Analyzer: analyzer, Foods: fl, Logger: d.Logger}
}

// newFocus는 집중(공부 타이머) API를 조립한다. 앱이 먼저 저장하고 올리는 백업이라 바깥 의존성이 없다.
func newFocus(d Deps) *focus.Handler {
	return &focus.Handler{Svc: &focus.Service{Pool: d.Pool}, Logger: d.Logger}
}
