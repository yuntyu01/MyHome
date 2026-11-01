package httpx

import (
	"net/http/httptest"
	"testing"
)

func TestRateLimiterBurst(t *testing.T) {
	rl := NewRateLimiter(1, 3)
	for i := 0; i < 3; i++ {
		if !rl.Allow("a") {
			t.Fatalf("%d번째 요청이 거부됨", i+1)
		}
	}
	if rl.Allow("a") {
		t.Fatal("burst 초과 요청이 허용됨")
	}
	if !rl.Allow("b") {
		t.Fatal("다른 키는 영향받지 않아야 함")
	}
	if !NewRateLimiter(0, 0).Allow("x") {
		t.Fatal("rps<=0이면 제한 없음")
	}
}

func TestClientIP(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "10.0.0.1:1234"
	r.Header.Set("X-Forwarded-For", "1.1.1.1, 2.2.2.2")
	if got := ClientIP(r, false); got != "10.0.0.1" {
		t.Errorf("trustProxy=false: %s", got)
	}
	if got := ClientIP(r, true); got != "2.2.2.2" {
		t.Errorf("trustProxy=true: %s", got)
	}
}
