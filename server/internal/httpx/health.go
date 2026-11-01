// Package httpx는 HTTP 공통 처리(헬스체크, 미들웨어, JSON 응답)를 담는다.
package httpx

import "net/http"

// Healthz는 서버가 살아 있으면 200을 반환한다.
func Healthz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"ok"}`))
}
