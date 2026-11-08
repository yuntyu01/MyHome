package httpx

import (
	"encoding/json"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// statusRecorder는 로그용으로 응답 코드를 기록한다.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func (r *statusRecorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }

// Logging은 요청마다 메서드·경로·상태·소요시간만 남긴다. 본문(알림 원문)은 절대 남기지 않는다.
func Logging(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		if r.URL.Path == "/healthz" {
			return
		}
		logger.Info("http",
			"method", r.Method,
			"path", r.URL.Path,
			"status", rec.status,
			"dur_ms", time.Since(start).Milliseconds(),
		)
	})
}

// Recover는 핸들러 패닉을 500으로 바꾼다.
func Recover(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if v := recover(); v != nil {
				logger.Error("panic", "path", r.URL.Path, "panic", v)
				Error(w, http.StatusInternalServerError, "internal error")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// LimitBody는 요청 본문 크기를 제한한다.
func LimitBody(maxBytes int64, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Body != nil {
			r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
		}
		next.ServeHTTP(w, r)
	})
}

// LimitBodyRoutes는 LimitBody와 같지만 routes에 있는 요청("METHOD /경로", 경로가 정확히 같을 때)은 그 크기까지
// 받는다. 사진 업로드 한 경로만 크게 열고 나머지 API는 작은 제한을 유지하려고 쓴다.
func LimitBodyRoutes(maxBytes int64, routes map[string]int64, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Body != nil {
			limit := maxBytes
			if n, ok := routes[r.Method+" "+r.URL.Path]; ok {
				limit = n
			}
			r.Body = http.MaxBytesReader(w, r.Body, limit)
		}
		next.ServeHTTP(w, r)
	})
}

// RateLimiter는 키(IP 등)별 토큰 버킷 제한기다. 오래 안 쓴 키는 주기적으로 지운다.
type RateLimiter struct {
	mu       sync.Mutex
	limiters map[string]*limiterEntry
	rps      rate.Limit
	burst    int
	lastGC   time.Time
}

type limiterEntry struct {
	lim  *rate.Limiter
	seen time.Time
}

// NewRateLimiter는 초당 rps, 순간 burst만큼 허용하는 제한기를 만든다. rps <= 0이면 제한하지 않는다.
func NewRateLimiter(rps float64, burst int) *RateLimiter {
	return &RateLimiter{limiters: map[string]*limiterEntry{}, rps: rate.Limit(rps), burst: burst, lastGC: time.Now()}
}

// Allow는 key에 대해 요청을 허용할지 판단한다.
func (rl *RateLimiter) Allow(key string) bool {
	if rl == nil || rl.rps <= 0 {
		return true
	}
	now := time.Now()
	rl.mu.Lock()
	defer rl.mu.Unlock()
	if now.Sub(rl.lastGC) > 10*time.Minute {
		for k, e := range rl.limiters {
			if now.Sub(e.seen) > 10*time.Minute {
				delete(rl.limiters, k)
			}
		}
		rl.lastGC = now
	}
	e, ok := rl.limiters[key]
	if !ok {
		e = &limiterEntry{lim: rate.NewLimiter(rl.rps, rl.burst)}
		rl.limiters[key] = e
	}
	e.seen = now
	return e.lim.Allow()
}

// RateLimitByIP는 클라이언트 IP별로 요청 수를 제한한다.
func RateLimitByIP(rl *RateLimiter, trustProxy bool, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/healthz" && !rl.Allow(ClientIP(r, trustProxy)) {
			w.Header().Set("Retry-After", "1")
			Error(w, http.StatusTooManyRequests, "too many requests")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// ClientIP는 요청자의 IP를 구한다. trustProxy면 리버스 프록시가 붙인
// X-Forwarded-For의 마지막 값(바로 앞 프록시가 본 IP)을 쓴다.
func ClientIP(r *http.Request, trustProxy bool) string {
	if trustProxy {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			parts := strings.Split(xff, ",")
			if ip := strings.TrimSpace(parts[len(parts)-1]); ip != "" {
				return ip
			}
		}
		if ip := r.Header.Get("X-Real-IP"); ip != "" {
			return ip
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// JSON은 v를 JSON으로 응답한다.
func JSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// Error는 {"error": msg} 형태로 응답한다.
func Error(w http.ResponseWriter, status int, msg string) {
	JSON(w, status, map[string]string{"error": msg})
}

// DecodeJSON은 본문을 v로 해석한다. 알 수 없는 필드는 거부한다.
func DecodeJSON(r *http.Request, v any) error {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	return dec.Decode(v)
}

// DecodeJSONLenient는 알 수 없는 필드를 무시하고 해석한다.
// 앱이 먼저 업데이트되어 필드가 늘어나도 수집이 끊기지 않게 하기 위해 수집 API에서 쓴다.
func DecodeJSONLenient(r *http.Request, v any) error {
	return json.NewDecoder(r.Body).Decode(v)
}
