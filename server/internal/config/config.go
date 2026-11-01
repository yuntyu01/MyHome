// Package config는 환경변수에서 서버 설정을 읽는다. 비밀값은 전부 환경변수로만 받는다.
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config는 서버 전체 설정이다.
type Config struct {
	HTTPAddr    string
	DatabaseURL string
	LogLevel    string

	MigrateOnStart bool

	MaxBodyBytes      int64
	RateLimitRPS      float64
	RateLimitBurst    int
	TrustProxyHeaders bool

	// CLITokenIssueEnabled가 false면 `myhome token issue`(비상용 토큰 발급)를 막는다.
	CLITokenIssueEnabled bool

	// ParserPackages는 패키지명 → 파서 매핑이다. 형식: "kb=패키지1,패키지2;toss=패키지3"
	ParserPackages string

	// AppEnv: production(기본) 또는 development
	AppEnv string

	// 카카오 로그인. 키가 없으면 카카오 로그인 경로는 503을 반환한다.
	KakaoRestAPIKey   string
	KakaoClientSecret string
	KakaoRedirectURI  string
	KakaoAppID        int64 // 설정하면 앱 로그인 토큰이 이 앱에서 발급됐는지 확인한다
	KakaoAuthBase     string
	KakaoAPIBase      string
	AllowedKakaoIDs   []int64

	CookieSecure bool
	SessionTTL   time.Duration
	// DevLoginKakaoID는 APP_ENV=development일 때만 쓰는 로컬 개발용 로그인 우회다.
	DevLoginKakaoID int64
}

// KakaoConfigured는 웹 카카오 로그인에 필요한 값이 모두 있는지 반환한다.
func (c Config) KakaoConfigured() bool {
	return c.KakaoRestAPIKey != "" && c.KakaoRedirectURI != ""
}

// IsAllowedKakaoID는 회원번호가 허용 목록에 있는지 반환한다.
func (c Config) IsAllowedKakaoID(id int64) bool {
	for _, a := range c.AllowedKakaoIDs {
		if a == id {
			return true
		}
	}
	return false
}

// Load는 환경변수에서 설정을 읽는다.
func Load() (Config, error) {
	var errs []string
	c := Config{
		HTTPAddr:             str("HTTP_ADDR", ":8080"),
		DatabaseURL:          os.Getenv("DATABASE_URL"),
		LogLevel:             str("LOG_LEVEL", "info"),
		MigrateOnStart:       boolean("MIGRATE_ON_START", true, &errs),
		MaxBodyBytes:         int64(integer("MAX_BODY_BYTES", 64*1024, &errs)),
		RateLimitRPS:         float("RATE_LIMIT_RPS", 5, &errs),
		RateLimitBurst:       integer("RATE_LIMIT_BURST", 30, &errs),
		TrustProxyHeaders:    boolean("TRUST_PROXY_HEADERS", false, &errs),
		CLITokenIssueEnabled: boolean("CLI_TOKEN_ISSUE_ENABLED", true, &errs),
		ParserPackages:       os.Getenv("PARSER_PACKAGES"),
		AppEnv:               str("APP_ENV", "production"),
		KakaoRestAPIKey:      os.Getenv("KAKAO_REST_API_KEY"),
		KakaoClientSecret:    os.Getenv("KAKAO_CLIENT_SECRET"),
		KakaoRedirectURI:     os.Getenv("KAKAO_REDIRECT_URI"),
		KakaoAppID:           int64(integer("KAKAO_APP_ID", 0, &errs)),
		KakaoAuthBase:        str("KAKAO_AUTH_BASE", "https://kauth.kakao.com"),
		KakaoAPIBase:         str("KAKAO_API_BASE", "https://kapi.kakao.com"),
		AllowedKakaoIDs:      int64List("ALLOWED_KAKAO_IDS", &errs),
		CookieSecure:         boolean("COOKIE_SECURE", true, &errs),
		SessionTTL:           duration("SESSION_TTL", 30*24*time.Hour, &errs),
		DevLoginKakaoID:      int64(integer("DEV_LOGIN_KAKAO_ID", 0, &errs)),
	}
	if c.DevLoginKakaoID != 0 && c.AppEnv != "development" {
		errs = append(errs, "DEV_LOGIN_KAKAO_ID는 APP_ENV=development에서만 쓸 수 있습니다")
	}
	if len(errs) > 0 {
		return c, fmt.Errorf("환경변수 오류: %s", strings.Join(errs, "; "))
	}
	return c, nil
}

// RequireDB는 DATABASE_URL이 없으면 에러를 반환한다.
func (c Config) RequireDB() error {
	if c.DatabaseURL == "" {
		return fmt.Errorf("DATABASE_URL이 필요합니다")
	}
	return nil
}

func str(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return def
}

func boolean(key string, def bool, errs *[]string) bool {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		*errs = append(*errs, key+"는 true/false여야 합니다")
		return def
	}
	return b
}

func integer(key string, def int, errs *[]string) int {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		*errs = append(*errs, key+"는 정수여야 합니다")
		return def
	}
	return n
}

func float(key string, def float64, errs *[]string) float64 {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		*errs = append(*errs, key+"는 숫자여야 합니다")
		return def
	}
	return f
}

func duration(key string, def time.Duration, errs *[]string) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		*errs = append(*errs, key+"는 기간(예: 720h)이어야 합니다")
		return def
	}
	return d
}

func list(key string) []string {
	var out []string
	for _, s := range strings.Split(os.Getenv(key), ",") {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}

func int64List(key string, errs *[]string) []int64 {
	var out []int64
	for _, s := range list(key) {
		n, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			*errs = append(*errs, key+"는 쉼표로 구분한 숫자여야 합니다")
			return nil
		}
		out = append(out, n)
	}
	return out
}
