package httpx

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestHealthz(t *testing.T) {
	rec := httptest.NewRecorder()
	Healthz(func(context.Context) error { return nil })(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"ok"`) {
		t.Fatalf("status = %d %s, want 200", rec.Code, rec.Body.String())
	}
}

func TestHealthzDBDown(t *testing.T) {
	rec := httptest.NewRecorder()
	Healthz(func(context.Context) error { return errors.New("connection refused 10.0.0.1") })(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "10.0.0.1") {
		t.Error("오류 내용을 응답에 쓰면 안 됨")
	}
}

// DB가 응답하지 않으면 2초 안에 실패로 끝난다.
func TestHealthzTimeout(t *testing.T) {
	rec := httptest.NewRecorder()
	start := time.Now()
	Healthz(func(ctx context.Context) error { <-ctx.Done(); return ctx.Err() })(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rec.Code != http.StatusServiceUnavailable || time.Since(start) > 3*time.Second {
		t.Fatalf("status = %d, 걸린 시간 %s", rec.Code, time.Since(start))
	}
}
