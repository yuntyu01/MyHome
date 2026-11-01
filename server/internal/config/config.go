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

var (
	_ = duration
	_ = list
)
