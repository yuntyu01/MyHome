package auth_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/yuntyu01/MyHome/server/internal/auth"
	"github.com/yuntyu01/MyHome/server/internal/auth/accesstest"
)

const owner = "owner@example.com"

func allowOwner(e string) bool { return e == owner }

func TestAccessVerify(t *testing.T) {
	srv := accesstest.New(t)
	v := srv.Verifier(allowOwner)
	ctx := context.Background()
	key := accesstest.Key(accesstest.KID)
	sign := func(edit func(map[string]any)) string {
		c := accesstest.Claims(owner)
		if edit != nil {
			edit(c)
		}
		return accesstest.Sign(accesstest.KID, key, nil, c)
	}

	c, err := v.Verify(ctx, sign(nil))
	if err != nil || c.Email != owner {
		t.Fatalf("유효한 토큰: %+v %v", c, err)
	}
	// aud가 문자열 하나여도, 이메일 대소문자가 달라도 된다. exp가 지났어도 허용 오차(1분) 안이면 통과
	for name, edit := range map[string]func(map[string]any){
		"aud 문자열":    func(c map[string]any) { c["aud"] = accesstest.AUD },
		"aud 여러 개":   func(c map[string]any) { c["aud"] = []string{"other", accesstest.AUD} },
		"이메일 대문자":    func(c map[string]any) { c["email"] = "Owner@Example.COM" },
		"exp 30초 지남": func(c map[string]any) { c["exp"] = time.Now().Add(-30 * time.Second).Unix() },
		"nbf 없음":     func(c map[string]any) { delete(c, "nbf") },
	} {
		if c, err := v.Verify(ctx, sign(edit)); err != nil || c.Email != owner {
			t.Errorf("%s: %+v %v", name, c, err)
		}
	}

	other := accesstest.Key("other")
	type rej struct{ tok, reason string }
	for name, tc := range map[string]rej{
		"빈 토큰":      {"", "토큰 없음"},
		"형식 아님":     {"abc.def", "JWT 형식"},
		"만료":        {sign(func(c map[string]any) { c["exp"] = time.Now().Add(-2 * time.Minute).Unix() }), "만료"},
		"exp 없음":    {sign(func(c map[string]any) { delete(c, "exp") }), "exp 없음"},
		"nbf 미래":    {sign(func(c map[string]any) { c["nbf"] = time.Now().Add(5 * time.Minute).Unix() }), "nbf"},
		"다른 aud":    {sign(func(c map[string]any) { c["aud"] = []string{"someone-else"} }), "aud"},
		"aud 없음":    {sign(func(c map[string]any) { delete(c, "aud") }), "aud"},
		"다른 iss":    {sign(func(c map[string]any) { c["iss"] = "https://evil.cloudflareaccess.com" }), "iss"},
		"iss http":  {sign(func(c map[string]any) { c["iss"] = "http://" + accesstest.Team }), "iss"},
		"허용 안 된 메일": {sign(func(c map[string]any) { c["email"] = "stranger@example.com" }), "허용되지 않은"},
		"메일 없음":     {sign(func(c map[string]any) { delete(c, "email") }), "email 없음"},
		"다른 키 서명":   {accesstest.Sign(accesstest.KID, other, nil, accesstest.Claims(owner)), "서명 불일치"},
		"alg none":  {accesstest.Sign(accesstest.KID, key, map[string]any{"alg": "none", "kid": accesstest.KID}, accesstest.Claims(owner)), "RS256"},
		"alg HS256": {accesstest.Sign(accesstest.KID, key, map[string]any{"alg": "HS256", "kid": accesstest.KID}, accesstest.Claims(owner)), "RS256"},
		"kid 없음":    {accesstest.Sign("", key, map[string]any{"alg": "RS256"}, accesstest.Claims(owner)), "kid 없음"},
		"서명 잘림": {func() string {
			s := sign(nil)
			return s[:len(s)-10]
		}(), "서명"},
		"클레임 바꿔치기": {func() string {
			good := strings.Split(sign(nil), ".")
			evil := strings.Split(accesstest.Sign(accesstest.KID, key, nil, accesstest.Claims("stranger@example.com")), ".")
			return good[0] + "." + evil[1] + "." + good[2]
		}(), "서명 불일치"},
	} {
		_, err := v.Verify(ctx, tc.tok)
		if !errors.Is(err, auth.ErrAccessRejected) || !strings.Contains(err.Error(), tc.reason) {
			t.Errorf("%s: err %v, want ErrAccessRejected(%s)", name, err, tc.reason)
		}
		if tc.tok != "" && err != nil && strings.Contains(err.Error(), tc.tok) {
			t.Errorf("%s: 에러에 토큰이 들어감", name)
		}
	}
	if srv.Hits() != 1 {
		t.Errorf("키는 처음 한 번만 받아야 함: %d", srv.Hits())
	}
}

// 모르는 kid가 오면 한 번 다시 받는다. 다시 받기는 1분에 한 번으로 제한한다.
func TestAccessUnknownKidRefetch(t *testing.T) {
	srv := accesstest.New(t)
	now := time.Now()
	v := srv.Verifier(allowOwner)
	v.Now = func() time.Time { return now }
	ctx := context.Background()
	if _, err := v.Verify(ctx, accesstest.Token(owner)); err != nil || srv.Hits() != 1 {
		t.Fatalf("첫 검증: %v hits=%d", err, srv.Hits())
	}

	// 키 교체: k2가 새로 생김
	srv.Serve(accesstest.KID, "k2")
	k2 := accesstest.Sign("k2", accesstest.Key("k2"), nil, accesstest.Claims(owner))
	// 방금 받았으니(1분 안) 다시 받지 않고 거부
	if _, err := v.Verify(ctx, k2); !errors.Is(err, auth.ErrAccessRejected) || srv.Hits() != 1 {
		t.Fatalf("1분 안 모르는 kid: %v hits=%d", err, srv.Hits())
	}
	now = now.Add(61 * time.Second)
	if c, err := v.Verify(ctx, k2); err != nil || c.Email != owner || srv.Hits() != 2 {
		t.Fatalf("모르는 kid → 한 번 다시 받기: %v hits=%d", err, srv.Hits())
	}
	// 받은 뒤에는 캐시에서
	if _, err := v.Verify(ctx, k2); err != nil || srv.Hits() != 2 {
		t.Fatalf("캐시: %v hits=%d", err, srv.Hits())
	}
	// 키 목록에도 없는 kid: 한 번 받고, 바로 또 와도 다시 받지 않는다
	now = now.Add(61 * time.Second)
	k3 := accesstest.Sign("k3", accesstest.Key("k3"), nil, accesstest.Claims(owner))
	for range 3 {
		if _, err := v.Verify(ctx, k3); !errors.Is(err, auth.ErrAccessRejected) {
			t.Fatalf("목록에 없는 kid: %v", err)
		}
	}
	if srv.Hits() != 3 {
		t.Errorf("목록에 없는 kid로 다시 받은 횟수: hits=%d, want 3", srv.Hits())
	}
}

// 키 서버가 죽었으면 ErrAccessKeys(503용). 실패 뒤에는 5초 동안 다시 받지 않는다.
func TestAccessKeysUnavailable(t *testing.T) {
	hits := 0
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer bad.Close()
	now := time.Now()
	v := auth.NewAccessVerifier("https://"+accesstest.Team+"/", accesstest.AUD, allowOwner)
	v.CertsURL, v.Now = bad.URL, func() time.Time { return now }
	if v.Issuer != accesstest.Issuer {
		t.Fatalf("issuer %q", v.Issuer)
	}
	for range 2 {
		if _, err := v.Verify(context.Background(), accesstest.Token(owner)); !errors.Is(err, auth.ErrAccessKeys) {
			t.Fatalf("키 서버 실패: %v", err)
		}
	}
	if hits != 1 {
		t.Errorf("실패 직후 다시 받음: %d", hits)
	}
	now = now.Add(6 * time.Second)
	_, _ = v.Verify(context.Background(), accesstest.Token(owner))
	if hits != 2 {
		t.Errorf("5초 뒤 다시 받아야 함: %d", hits)
	}
}

func TestAccessTokenFromRequest(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	r.AddCookie(&http.Cookie{Name: auth.AccessCookie, Value: "from-cookie"})
	if got := auth.AccessToken(r); got != "from-cookie" {
		t.Errorf("쿠키: %q", got)
	}
	r.Header.Set(auth.AccessHeader, "from-header")
	if got := auth.AccessToken(r); got != "from-header" {
		t.Errorf("헤더 우선: %q", got)
	}
}
