package config

import (
	"strings"
	"testing"
)

func TestNormalizeDatabaseURL(t *testing.T) {
	for _, tc := range []struct{ input, want string }{
		{"postgresql+psycopg2://app:pass@postgres:5432/app?sslmode=disable", "postgresql://app:pass@postgres:5432/app?sslmode=disable"},
		{"postgres://user:pass@host/db", "postgres://user:pass@host/db"},
		{"host=localhost user=app", "host=localhost user=app"},
	} {
		if got := normalizeDatabaseURL(tc.input); got != tc.want {
			t.Errorf("normalizeDatabaseURL(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

// AI_PROVIDER 기본값: 키가 있으면 gemini, 없으면 mock. gemini인데 키가 없으면 시작하지 않는다.
func TestAIProviderDefault(t *testing.T) {
	for _, tc := range []struct {
		provider, key, want, err string
	}{
		{"", "", "mock", ""},
		{"", "k", "gemini", ""},
		{"mock", "k", "mock", ""},
		{"Gemini", "k", "gemini", ""},
		{"gemini", "", "", "GEMINI_API_KEY"},
		{"openai", "", "", "AI_PROVIDER"},
	} {
		t.Setenv("AI_PROVIDER", tc.provider)
		t.Setenv("GEMINI_API_KEY", tc.key)
		c, err := Load()
		if tc.err != "" {
			if err == nil || !strings.Contains(err.Error(), tc.err) {
				t.Errorf("%q/%q: err %v", tc.provider, tc.key, err)
			}
			if tc.key != "" && strings.Contains(err.Error(), tc.key) {
				t.Errorf("에러에 키가 있음: %v", err)
			}
			continue
		}
		if err != nil || c.AIProvider != tc.want {
			t.Errorf("%q/%q: got %q, %v", tc.provider, tc.key, c.AIProvider, err)
		}
		if c.GeminiModel != DefaultGeminiModel {
			t.Errorf("model %q", c.GeminiModel)
		}
	}
}

// AI_CLASSIFY: 기본 on, off로 끈다. 모르는 값이면 시작하지 않는다.
func TestAIClassifySwitch(t *testing.T) {
	for _, tc := range []struct {
		v    string
		want bool
		err  bool
	}{
		{"", true, false}, {"on", true, false}, {"OFF", false, false}, {"off", false, false},
		{"false", false, false}, {"1", true, false}, {"maybe", true, true},
	} {
		t.Setenv("AI_CLASSIFY", tc.v)
		c, err := Load()
		if (err != nil) != tc.err || (err != nil && !strings.Contains(err.Error(), "AI_CLASSIFY")) {
			t.Errorf("%q: err %v", tc.v, err)
		}
		if c.AIClassify != tc.want {
			t.Errorf("%q: AIClassify %v", tc.v, c.AIClassify)
		}
	}
}

// 운영(APP_ENV가 development가 아님)에서는 Access 설정(팀 도메인, AUD, 허용 이메일)이 다 있어야 serve가 시작한다.
func TestValidateServeRequiresAccess(t *testing.T) {
	set := func(env, team, aud, emails string) Config {
		t.Helper()
		t.Setenv("APP_ENV", env)
		t.Setenv("CF_ACCESS_TEAM_DOMAIN", team)
		t.Setenv("CF_ACCESS_AUD", aud)
		t.Setenv("ALLOWED_EMAILS", emails)
		c, err := Load()
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		return c
	}
	err := set("", "", "", "").ValidateServe()
	for _, k := range []string{"CF_ACCESS_TEAM_DOMAIN", "CF_ACCESS_AUD", "ALLOWED_EMAILS"} {
		if err == nil || !strings.Contains(err.Error(), k) {
			t.Errorf("운영 기본값에서 %s 누락을 알려야 함: %v", k, err)
		}
	}
	if err := set("production", "team.cloudflareaccess.com", "aud", "").ValidateServe(); err == nil || !strings.Contains(err.Error(), "ALLOWED_EMAILS") {
		t.Errorf("허용 이메일 없음: %v", err)
	}
	if err := set("production", "team.cloudflareaccess.com", "", "me@example.com").ValidateServe(); err == nil || !strings.Contains(err.Error(), "CF_ACCESS_AUD") {
		t.Errorf("AUD 없음: %v", err)
	}
	c := set("production", "team.cloudflareaccess.com", "aud", "me@example.com")
	if err := c.ValidateServe(); err != nil || !c.AccessConfigured() {
		t.Errorf("다 있으면 통과: %v", err)
	}
	if err := set("development", "", "", "").ValidateServe(); err != nil {
		t.Errorf("개발 모드는 Access 없이 시작: %v", err)
	}
}

func TestAccessConfigValues(t *testing.T) {
	for in, want := range map[string]string{
		"team.cloudflareaccess.com":          "team.cloudflareaccess.com",
		"https://Team.cloudflareaccess.com/": "team.cloudflareaccess.com",
		" https://team.cloudflareaccess.com": "team.cloudflareaccess.com",
	} {
		t.Setenv("CF_ACCESS_TEAM_DOMAIN", in)
		c, err := Load()
		if err != nil || c.CFAccessTeamDomain != want {
			t.Errorf("팀 도메인 %q → %q, %v", in, c.CFAccessTeamDomain, err)
		}
	}
	for _, bad := range []string{"team.cloudflareaccess.com/cdn-cgi", "http://team.cloudflareaccess.com", "team", "team.example.com:443"} {
		t.Setenv("CF_ACCESS_TEAM_DOMAIN", bad)
		if _, err := Load(); err == nil || !strings.Contains(err.Error(), "CF_ACCESS_TEAM_DOMAIN") {
			t.Errorf("잘못된 팀 도메인 %q: %v", bad, err)
		}
	}
	t.Setenv("CF_ACCESS_TEAM_DOMAIN", "")

	t.Setenv("ALLOWED_EMAILS", " Me@Example.com , other@example.com ")
	c, err := Load()
	if err != nil || len(c.AllowedEmails) != 2 {
		t.Fatalf("이메일 목록: %v %v", c.AllowedEmails, err)
	}
	if !c.IsAllowedEmail("me@example.com") || !c.IsAllowedEmail("ME@EXAMPLE.COM ") || c.IsAllowedEmail("me@example.co") || c.IsAllowedEmail("") {
		t.Error("대소문자 없이 정확히 같은 이메일만 허용")
	}
	t.Setenv("ALLOWED_EMAILS", "not-an-email")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "ALLOWED_EMAILS") {
		t.Errorf("잘못된 이메일: %v", err)
	}
	t.Setenv("ALLOWED_EMAILS", "")

	// 개발용 로그인은 개발 모드에서만
	t.Setenv("DEV_LOGIN_EMAIL", "me@example.com")
	t.Setenv("APP_ENV", "production")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "DEV_LOGIN_EMAIL") {
		t.Errorf("운영에서 DEV_LOGIN_EMAIL: %v", err)
	}
	t.Setenv("APP_ENV", "development")
	if c, err := Load(); err != nil || c.DevLoginEmail != "me@example.com" {
		t.Errorf("개발 모드 DEV_LOGIN_EMAIL: %q %v", c.DevLoginEmail, err)
	}

	// 예전 카카오 변수는 읽지 않고 이름만 경고용으로 모은다(값은 담지 않는다)
	t.Setenv("ALLOWED_KAKAO_IDS", "12345")
	t.Setenv("KAKAO_REST_API_KEY", "secret-kakao-key")
	c, _ = Load()
	if strings.Join(c.Obsolete, ",") != "KAKAO_REST_API_KEY,ALLOWED_KAKAO_IDS" {
		t.Errorf("Obsolete = %v", c.Obsolete)
	}
	if strings.Contains(strings.Join(c.Obsolete, ","), "secret") {
		t.Error("값이 들어가면 안 됨")
	}
}
