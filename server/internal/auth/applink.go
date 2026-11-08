package auth

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"

	"github.com/yuntyu01/MyHome/server/internal/dbq"
	"github.com/yuntyu01/MyHome/server/internal/httpx"
)

// 앱 연결(처음 한 번):
//
//  1. 앱이 verifier(43~128자)를 만들고 challenge = base64url(sha256(verifier))를 계산해
//     브라우저로 GET /app/link?challenge=<43자>&state=<16~64자>를 연다.
//  2. Cloudflare Access(구글) 로그인 → RequireWeb이 JWT를 확인한다.
//  3. 서버가 5분짜리 1회용 코드를 만들어 myhome://auth?code=…&state=… 로 앱을 부른다(고정 주소. 요청에서 받지 않는다).
//     URL에는 코드만 싣고 기기 토큰은 절대 싣지 않는다.
//  4. 앱이 POST /v1/auth/exchange {code, verifier, device_name}으로 기기 토큰을 받는다.
//     코드를 가로챈 다른 앱은 verifier가 없어서 쓰지 못한다(PKCE S256).
const (
	AppRedirectURL = "myhome://auth"
	LinkCodeTTL    = 5 * time.Minute
)

func isB64URL(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !(c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
			return false
		}
	}
	return true
}

// ValidChallenge는 S256 challenge(base64url, 패딩 없음, 43자 = 32바이트)인지 본다.
func ValidChallenge(s string) bool {
	if len(s) != 43 || !isB64URL(s) {
		return false
	}
	b, err := base64.RawURLEncoding.DecodeString(s)
	return err == nil && len(b) == sha256.Size
}

// ValidState는 앱이 보낸 state가 base64url 16~64자인지 본다. 서버는 저장하지 않고 앱에 그대로 돌려준다.
func ValidState(s string) bool { return len(s) >= 16 && len(s) <= 64 && isB64URL(s) }

// validVerifier는 RFC 7636의 code_verifier(43~128자, A-Z a-z 0-9 - . _ ~)인지 본다.
func validVerifier(s string) bool {
	if len(s) < 43 || len(s) > 128 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !(c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' || c == '.' || c == '_' || c == '~') {
			return false
		}
	}
	return true
}

// S256은 PKCE challenge = base64url(sha256(verifier))다.
func S256(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// CreateAppLink는 email에 묶인 1회용 코드를 만들고 앱으로 돌아갈 주소(AppRedirectURL + code, state)를 돌려준다.
// challenge와 state는 호출하는 쪽이 ValidChallenge·ValidState로 먼저 확인한다.
func (h *Handlers) CreateAppLink(ctx context.Context, email, challenge, state string) (string, error) {
	if email == "" || !ValidChallenge(challenge) || !ValidState(state) {
		return "", errors.New("잘못된 앱 연결 요청")
	}
	code, err := NewToken("")
	if err != nil {
		return "", err
	}
	_, _ = h.Q.DeleteExpiredLinkCodes(ctx)
	if err := h.Q.CreateLinkCode(ctx, dbq.CreateLinkCodeParams{
		CodeHash: HashToken(code), Challenge: challenge, Email: email, ExpiresAt: time.Now().Add(LinkCodeTTL),
	}); err != nil {
		return "", err
	}
	h.Logger.Info("앱 연결 코드 발급", "email", email)
	v := url.Values{}
	v.Set("code", code)
	v.Set("state", state)
	return AppRedirectURL + "?" + v.Encode(), nil
}

type exchangeRequest struct {
	Code       string `json:"code"`
	Verifier   string `json:"verifier"`
	DeviceName string `json:"device_name"`
}

var kst = time.FixedZone("KST", 9*60*60)

// deviceName은 앞뒤 공백을 떼고 60자(rune)로 자른다. 비면 "android-YYYYMMDD"(KST).
func deviceName(s string) string {
	s = strings.TrimSpace(s)
	if utf8.RuneCountInString(s) > 60 {
		s = strings.TrimSpace(string([]rune(s)[:60]))
	}
	if s == "" {
		s = "android-" + time.Now().In(kst).Format("20060102")
	}
	return s
}

// exchange: POST /v1/auth/exchange. 인증 없음(/v1은 Access를 거치지 않는다). IP별 제한은 전역 제한기가 한다.
// 코드를 먼저 소모(1회용)한 뒤 verifier를 확인한다. 그래서 verifier가 틀린 코드도 다시 쓸 수 없다.
// 401 사유(없음·만료·사용됨·verifier 불일치)는 구분하지 않는다.
func (h *Handlers) exchange(w http.ResponseWriter, r *http.Request) {
	var in exchangeRequest
	if err := httpx.DecodeJSONLenient(r, &in); err != nil || in.Code == "" || len(in.Code) > 128 || !isB64URL(in.Code) || !validVerifier(in.Verifier) {
		httpx.Error(w, http.StatusBadRequest, "invalid request")
		return
	}
	const bad = "invalid or expired code"
	ctx := r.Context()
	_, _ = h.Q.DeleteExpiredLinkCodes(ctx)
	row, err := h.Q.ConsumeLinkCode(ctx, HashToken(in.Code))
	if errors.Is(err, pgx.ErrNoRows) {
		h.Logger.Warn("앱 연결 코드 거부: 없거나 만료·사용됨")
		httpx.Error(w, http.StatusUnauthorized, bad)
		return
	}
	if err != nil {
		h.Logger.Error("앱 연결 코드 조회 실패", "err", err)
		httpx.Error(w, http.StatusInternalServerError, "internal error")
		return
	}
	if subtle.ConstantTimeCompare([]byte(S256(in.Verifier)), []byte(row.Challenge)) != 1 {
		h.Logger.Warn("앱 연결 코드 거부: verifier 불일치(코드는 소모됨)", "email", row.Email)
		httpx.Error(w, http.StatusUnauthorized, bad)
		return
	}
	if !h.Cfg.IsAllowedEmail(row.Email) {
		h.Logger.Warn("앱 연결 코드 거부: 허용 목록에서 빠진 이메일", "email", row.Email)
		httpx.Error(w, http.StatusUnauthorized, bad)
		return
	}
	email := row.Email
	tok, d, err := IssueDeviceTokenFor(ctx, h.Q, deviceName(in.DeviceName), &email)
	if err != nil {
		h.Logger.Error("기기 토큰 발급 실패", "err", err)
		httpx.Error(w, http.StatusInternalServerError, "internal error")
		return
	}
	h.Logger.Info("기기 토큰 발급(앱 연결)", "device_id", d.ID, "email", email)
	httpx.JSON(w, http.StatusCreated, map[string]any{"token": tok, "device_id": d.ID, "device_name": d.Name})
}
