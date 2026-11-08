// Package httpx는 HTTP 공통 처리(헬스체크, 미들웨어, JSON 응답)를 담는다.
package httpx

import (
	"context"
	"net/http"
	"time"
)

// healthTimeout은 헬스체크의 DB 확인 시간 한도다. 늦으면 실패로 본다.
const healthTimeout = 2 * time.Second

// Healthz는 DB까지 닿으면 200, 아니면 503을 반환한다. ping이 nil이면 프로세스만 본다.
// 원인은 응답에 자세히 쓰지 않는다(누구나 부를 수 있는 주소라서).
func Healthz(ping func(context.Context) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		if ping != nil {
			ctx, cancel := context.WithTimeout(r.Context(), healthTimeout)
			defer cancel()
			if err := ping(ctx); err != nil {
				w.WriteHeader(http.StatusServiceUnavailable)
				_, _ = w.Write([]byte(`{"status":"db_unavailable"}`))
				return
			}
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}
}
