package toss

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Token은 access token이다. 캐시가 유효하면 재사용한다 (AUTH 그룹은 초당 5회 제한).
//
// 🔴 토스는 클라이언트당 유효 토큰이 하나뿐이다 — 다시 발급하면 앞의 것이 죽는다.
// 그래서 프로세스를 넘어 재사용하도록 파일(0600)에 두고, 만료 5분 전부터는 새로 받는다.
func (c *Client) Token(ctx context.Context) (string, error) {
	c.tokenMu.Lock()
	defer c.tokenMu.Unlock()
	path, err := c.tokenFile()
	if err != nil {
		return "", err
	}
	if !c.forceIssue {
		if tok := c.cachedToken(path); tok != "" {
			return tok, nil
		}
	}
	form := url.Values{
		"grant_type":    {"client_credentials"},
		"client_id":     {c.Config.AppKey},
		"client_secret": {c.Config.AppSecret},
	}
	raw, err := c.request(ctx, http.MethodPost, "/oauth2/token",
		reqOpts{form: form, noAccount: true, noAuth: true, noEnvelope: true})
	if err != nil {
		return "", err
	}
	var res struct {
		AccessToken any `json:"access_token"`
		ExpiresIn   any `json:"expires_in"`
	}
	if err := decode(raw, "POST /oauth2/token", &res); err != nil {
		return "", err
	}
	tok := strings.TrimSpace(orText(res.AccessToken))
	if tok == "" {
		return "", &Error{Message: "토큰 발급 실패 — 응답에 access_token 없음"}
	}
	if err := c.saveToken(path, tok, c.now().Add(max(ttlOf(res.ExpiresIn), tokenMinTTL))); err != nil {
		return "", err
	}
	c.forceIssue = false
	return tok, nil
}

// ttlOf는 expires_in(초)이다. 없거나 읽을 수 없으면 짧게 잡는다.
func ttlOf(v any) time.Duration {
	if !truthy(v) {
		return tokenFallbackTTL
	}
	switch x := v.(type) {
	case json.Number:
		if n, err := x.Int64(); err == nil {
			return time.Duration(n) * time.Second
		}
		if f, err := x.Float64(); err == nil {
			return time.Duration(int64(f)) * time.Second
		}
	case string:
		if n, err := strconv.Atoi(strings.TrimSpace(x)); err == nil {
			return time.Duration(n) * time.Second
		}
	}
	return tokenFallbackTTL
}

type tokenCache struct {
	Token     any `json:"token"`
	ExpiresAt any `json:"expires_at"`
}

func (c *Client) cachedToken(path string) string {
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return ""
	}
	if err != nil {
		c.warn("캐시 읽기 실패 — 재발급")
		return ""
	}
	var d tokenCache
	if err := json.Unmarshal(b, &d); err != nil {
		c.warn("캐시 손상 (JSON 아님) — 재발급")
		return ""
	}
	tok, ok := d.Token.(string)
	if !ok || strings.TrimSpace(tok) == "" {
		c.warn("캐시 손상 (token 비어 있음) — 재발급")
		return ""
	}
	s, _ := d.ExpiresAt.(string)
	exp, err := parseISO(s)
	if err != nil {
		c.warn("캐시 손상 (expires_at 형식 이상) — 재발급")
		return ""
	}
	if !exp.Add(-tokenMargin).After(c.now()) {
		return ""
	}
	return tok
}

// parseISO는 시간대가 없으면 UTC로 본다.
func parseISO(s string) (time.Time, error) {
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t, nil
	}
	return time.ParseInLocation("2006-01-02T15:04:05.999999999", s, time.UTC)
}

// saveToken은 0600 원자적 쓰기다 — 권한 없는 창이 단 한 순간도 없어야 한다.
// 임시 파일은 처음부터 0600으로 만들어지고(os.CreateTemp), rename으로 갈아끼운다.
func (c *Client) saveToken(path, tok string, expiresAt time.Time) error {
	payload, err := json.Marshal(tokenCache{Token: tok, ExpiresAt: expiresAt.UTC().Format(time.RFC3339Nano)})
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return &Error{Message: "토큰 캐시 폴더를 만들 수 없다", Err: err}
	}
	f, err := os.CreateTemp(dir, filepath.Base(path)+".*.tmp")
	if err != nil {
		return &Error{Message: "토큰 캐시를 쓸 수 없다", Err: err}
	}
	tmp := f.Name()
	done := false
	defer func() {
		if !done {
			_ = os.Remove(tmp)
		}
	}()
	_, werr := f.Write(payload)
	if cerr := f.Close(); werr == nil {
		werr = cerr
	}
	if werr == nil {
		werr = os.Chmod(tmp, 0o600)
	}
	if werr == nil {
		werr = os.Rename(tmp, path)
	}
	if werr != nil {
		return &Error{Message: "토큰 캐시를 쓸 수 없다", Err: werr}
	}
	done = true
	return nil
}

func (c *Client) dropToken() {
	c.tokenMu.Lock()
	defer c.tokenMu.Unlock()
	c.forceIssue = true
	path, err := c.tokenFile()
	if err != nil {
		return
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		c.warn("캐시 삭제 실패 — 재발급만 시도")
	}
}
