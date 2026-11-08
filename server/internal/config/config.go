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
	// ParserIgnorePatterns는 결제 알림이 아니라서 무시할 문구를 더한다(내장 광고·증권 규칙 외). 형식: "문구1;문구2"
	ParserIgnorePatterns string

	// AppEnv: production(기본) 또는 development
	AppEnv string

	// 웹 로그인: Cloudflare Access(구글). 웹 화면은 Access가 막고, 서버도 Access JWT를 직접 검증한다(안전망).
	// 앱 API(/v1/*)는 Access를 거치지 않고 기기 토큰으로 인증한다.
	CFAccessTeamDomain string   // "<팀>.cloudflareaccess.com" (https:// 없이 저장)
	CFAccessAUD        string   // Access 애플리케이션의 Audience(AUD) 태그
	AllowedEmails      []string // 허용할 구글 이메일(소문자로 저장, 비교도 소문자)

	CookieSecure bool
	SessionTTL   time.Duration
	// DevLoginEmail은 APP_ENV=development이고 Access를 설정하지 않았을 때만 쓰는 로컬 개발용 로그인 우회다.
	DevLoginEmail string

	// Obsolete는 설정돼 있지만 더 이상 쓰지 않는 환경변수 이름이다(값은 담지 않는다). serve가 경고로 남긴다.
	Obsolete []string

	// 건강(핏두잉 이식). 끼니 사진 분석: AIProvider는 mock | gemini.
	// 비우면 GEMINI_API_KEY가 있을 때 gemini, 없으면 mock(키 없이 고정 결과)이다.
	AIProvider   string
	GeminiAPIKey string
	GeminiModel  string
	// AIClassify는 가계부 AI 분류 추천(AI_CLASSIFY=on|off, 기본 on)이다. 켜져 있으면 serve가 규칙 없는 지출의 가맹점 이름을
	// AIProvider로 물어 추천을 붙인다(gemini는 같은 키, mock은 낱말 표로 늘 같은 답). off면 묻지도 보여주지도 않는다.
	AIClassify bool
	// 바코드 → 제품 영양성분(식품안전나라 + 공공데이터포털). 둘 다 있어야 켜진다.
	FoodSafetyAPIKey string
	// MealPhotoDir은 끼니 사진을 저장할 폴더(MEAL_PHOTO_DIR)다. 비우면 서버에 사진을 저장하지 않는다(분석만)
	MealPhotoDir   string
	DataGoKrAPIKey string
}

// DefaultGeminiModel은 GEMINI_MODEL 기본값이다(핏두잉과 같다. ai.DefaultModel과 맞춘다).
const DefaultGeminiModel = "gemini-3.5-flash-lite"

// AccessConfigured는 Cloudflare Access JWT 검증에 필요한 값(팀 도메인, AUD)이 있는지 반환한다.
func (c Config) AccessConfigured() bool {
	return c.CFAccessTeamDomain != "" && c.CFAccessAUD != ""
}

// IsAllowedEmail은 이메일이 허용 목록에 있는지 대소문자 없이 비교한다.
func (c Config) IsAllowedEmail(email string) bool {
	e := normalizeEmail(email)
	if e == "" {
		return false
	}
	for _, a := range c.AllowedEmails {
		if a == e {
			return true
		}
	}
	return false
}

// ValidateServe는 웹 서버(serve)에만 필요한 설정을 검사한다. 운영(APP_ENV가 development가 아님)에서는
// Cloudflare Access 설정(팀 도메인, AUD, 허용 이메일)이 하나라도 없으면 시작하지 않는다.
// invest·migrate 같은 다른 명령은 웹 로그인 설정이 없어도 돈다.
func (c Config) ValidateServe() error {
	if c.AppEnv == "development" {
		return nil
	}
	var missing []string
	if c.CFAccessTeamDomain == "" {
		missing = append(missing, "CF_ACCESS_TEAM_DOMAIN")
	}
	if c.CFAccessAUD == "" {
		missing = append(missing, "CF_ACCESS_AUD")
	}
	if len(c.AllowedEmails) == 0 {
		missing = append(missing, "ALLOWED_EMAILS")
	}
	if len(missing) > 0 {
		return fmt.Errorf("운영 모드(APP_ENV=%s)에서는 Cloudflare Access 설정이 필요합니다. 없는 값: %s (docs/ACCESS_SETUP.md)",
			c.AppEnv, strings.Join(missing, ", "))
	}
	return nil
}

// obsoleteKeys는 카카오 로그인을 없애면서(2026-11-08) 더 이상 읽지 않는 환경변수다.
var obsoleteKeys = []string{
	"KAKAO_REST_API_KEY", "KAKAO_CLIENT_SECRET", "KAKAO_REDIRECT_URI", "KAKAO_APP_ID",
	"KAKAO_AUTH_BASE", "KAKAO_API_BASE", "ALLOWED_KAKAO_IDS", "DEV_LOGIN_KAKAO_ID",
}

// Load는 환경변수에서 설정을 읽는다.
func Load() (Config, error) {
	var errs []string
	c := Config{
		HTTPAddr:             str("HTTP_ADDR", ":8080"),
		DatabaseURL:          normalizeDatabaseURL(os.Getenv("DATABASE_URL")),
		LogLevel:             str("LOG_LEVEL", "info"),
		MigrateOnStart:       boolean("MIGRATE_ON_START", true, &errs),
		MaxBodyBytes:         int64(integer("MAX_BODY_BYTES", 64*1024, &errs)),
		RateLimitRPS:         float("RATE_LIMIT_RPS", 5, &errs),
		RateLimitBurst:       integer("RATE_LIMIT_BURST", 30, &errs),
		TrustProxyHeaders:    boolean("TRUST_PROXY_HEADERS", false, &errs),
		CLITokenIssueEnabled: boolean("CLI_TOKEN_ISSUE_ENABLED", true, &errs),
		ParserPackages:       os.Getenv("PARSER_PACKAGES"),
		ParserIgnorePatterns: os.Getenv("PARSER_IGNORE_PATTERNS"),
		AppEnv:               str("APP_ENV", "production"),
		CFAccessAUD:          strings.TrimSpace(os.Getenv("CF_ACCESS_AUD")),
		AllowedEmails:        emailList("ALLOWED_EMAILS", &errs),
		CookieSecure:         boolean("COOKIE_SECURE", true, &errs),
		SessionTTL:           duration("SESSION_TTL", 30*24*time.Hour, &errs),
		DevLoginEmail:        normalizeEmail(os.Getenv("DEV_LOGIN_EMAIL")),
		AIProvider:           strings.ToLower(strings.TrimSpace(os.Getenv("AI_PROVIDER"))),
		GeminiAPIKey:         strings.TrimSpace(os.Getenv("GEMINI_API_KEY")),
		GeminiModel:          str("GEMINI_MODEL", DefaultGeminiModel),
		AIClassify:           onOff("AI_CLASSIFY", true, &errs),
		FoodSafetyAPIKey:     strings.TrimSpace(os.Getenv("FOODSAFETY_API_KEY")),
		MealPhotoDir:         strings.TrimSpace(os.Getenv("MEAL_PHOTO_DIR")),
		DataGoKrAPIKey:       strings.TrimSpace(os.Getenv("DATA_GO_KR_API_KEY")),
	}
	c.CFAccessTeamDomain = teamDomain("CF_ACCESS_TEAM_DOMAIN", &errs)
	for _, k := range obsoleteKeys {
		if os.Getenv(k) != "" {
			c.Obsolete = append(c.Obsolete, k)
		}
	}
	switch c.AIProvider {
	case "":
		c.AIProvider = "mock"
		if c.GeminiAPIKey != "" {
			c.AIProvider = "gemini"
		}
	case "mock":
	case "gemini":
		if c.GeminiAPIKey == "" {
			errs = append(errs, "AI_PROVIDER=gemini에는 GEMINI_API_KEY가 필요합니다")
		}
	default:
		errs = append(errs, "AI_PROVIDER는 mock 또는 gemini여야 합니다")
	}
	if c.DevLoginEmail != "" && c.AppEnv != "development" {
		errs = append(errs, "DEV_LOGIN_EMAIL은 APP_ENV=development에서만 쓸 수 있습니다")
	}
	if len(errs) > 0 {
		return c, fmt.Errorf("환경변수 오류: %s", strings.Join(errs, "; "))
	}
	return c, nil
}

// KoDeploy supplies a SQLAlchemy URL; pgx accepts the equivalent PostgreSQL URL.
func normalizeDatabaseURL(value string) string {
	const sqlalchemyPrefix = "postgresql+psycopg2://"
	if strings.HasPrefix(value, sqlalchemyPrefix) {
		return "postgresql://" + strings.TrimPrefix(value, sqlalchemyPrefix)
	}
	return value
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

// onOff는 on/off(true/false, 1/0도 받음) 스위치를 읽는다.
func onOff(key string, def bool, errs *[]string) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(key))) {
	case "":
		return def
	case "on", "true", "1":
		return true
	case "off", "false", "0":
		return false
	}
	*errs = append(*errs, key+"는 on 또는 off여야 합니다")
	return def
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

func normalizeEmail(s string) string { return strings.ToLower(strings.TrimSpace(s)) }

// emailList는 쉼표로 구분한 이메일 목록을 소문자로 읽는다.
func emailList(key string, errs *[]string) []string {
	var out []string
	for _, s := range list(key) {
		e := normalizeEmail(s)
		if strings.Count(e, "@") != 1 || strings.HasPrefix(e, "@") || strings.HasSuffix(e, "@") || strings.ContainsAny(e, " \t<>") {
			*errs = append(*errs, key+"는 쉼표로 구분한 이메일 주소여야 합니다")
			return nil
		}
		out = append(out, e)
	}
	return out
}

// teamDomain은 Access 팀 도메인을 "<팀>.cloudflareaccess.com" 모양으로 읽는다. https://와 끝의 /는 떼어 준다.
func teamDomain(key string, errs *[]string) string {
	v := strings.ToLower(strings.TrimSpace(os.Getenv(key)))
	v = strings.TrimSuffix(strings.TrimPrefix(v, "https://"), "/")
	if v == "" {
		return ""
	}
	for _, r := range v {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' || r == '.') {
			*errs = append(*errs, key+"는 <팀>.cloudflareaccess.com 형식이어야 합니다")
			return ""
		}
	}
	if !strings.Contains(v, ".") || strings.HasPrefix(v, ".") || strings.HasSuffix(v, ".") {
		*errs = append(*errs, key+"는 <팀>.cloudflareaccess.com 형식이어야 합니다")
		return ""
	}
	return v
}
