package auth

import (
	"context"
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Cloudflare Access가 보호하는 요청에 붙는 JWT. 헤더를 먼저 보고, 없으면 브라우저 쿠키를 본다.
const (
	AccessHeader = "Cf-Access-Jwt-Assertion"
	AccessCookie = "CF_Authorization"
)

var (
	// ErrAccessRejected는 JWT가 없거나 형식·서명·클레임이 맞지 않을 때다(403).
	ErrAccessRejected = errors.New("access jwt rejected")
	// ErrAccessKeys는 서명 키(JWKS)를 가져오지 못했을 때다(503). 사용자 잘못이 아니다.
	ErrAccessKeys = errors.New("access keys unavailable")
)

func rejected(reason string) error { return fmt.Errorf("%w: %s", ErrAccessRejected, reason) }

// AccessClaims는 검증을 통과한 JWT에서 쓰는 값이다.
type AccessClaims struct {
	Email string // 소문자
	Sub   string
}

// AccessVerifier는 Cloudflare Access JWT(RS256)를 표준 라이브러리만으로 검증한다.
// Access가 잘못 설정돼 요청이 그대로 들어와도 서버가 막는 안전망이다.
//
// 키는 https://<팀>/cdn-cgi/access/certs 의 keys[](kid, n, e)에서 받아 캐시하고,
// 모르는 kid가 오면 다시 받는다(성공한 뒤로는 MinRefetch 간격, 실패하면 5초 간격으로 제한).
type AccessVerifier struct {
	Issuer     string                  // "https://<팀>.cloudflareaccess.com"
	Audience   string                  // CF_ACCESS_AUD
	Allowed    func(email string) bool // 허용 이메일 확인(필수)
	CertsURL   string                  // 비우면 Issuer + "/cdn-cgi/access/certs"
	Client     *http.Client            // 비우면 10초 타임아웃
	Now        func() time.Time        // 비우면 time.Now
	MinRefetch time.Duration           // 기본 1분
	Leeway     time.Duration           // exp·nbf 허용 오차, 기본 1분

	mu        sync.Mutex
	keys      map[string]*rsa.PublicKey
	fetchedAt time.Time // 마지막으로 받으려고 한 시각
	lastOK    bool
	fetches   int
}

// NewAccessVerifier는 팀 도메인("<팀>.cloudflareaccess.com", https:// 있어도 됨)과 AUD로 검증기를 만든다.
func NewAccessVerifier(teamDomain, audience string, allowed func(string) bool) *AccessVerifier {
	team := strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(teamDomain), "https://"), "/")
	return &AccessVerifier{Issuer: "https://" + team, Audience: audience, Allowed: allowed}
}

const failRetry = 5 * time.Second

func (v *AccessVerifier) now() time.Time {
	if v.Now != nil {
		return v.Now()
	}
	return time.Now()
}

// Fetches는 지금까지 키를 받으려고 한 횟수다(테스트·진단용).
func (v *AccessVerifier) Fetches() int {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.fetches
}

// AccessToken은 요청에서 Access JWT를 꺼낸다. 헤더가 우선이다.
func AccessToken(r *http.Request) string {
	if t := strings.TrimSpace(r.Header.Get(AccessHeader)); t != "" {
		return t
	}
	if c, err := r.Cookie(AccessCookie); err == nil {
		return strings.TrimSpace(c.Value)
	}
	return ""
}

type jwtHeader struct {
	Alg string `json:"alg"`
	Kid string `json:"kid"`
}

// audience는 문자열 하나 또는 문자열 배열인 aud 클레임이다.
type audience []string

func (a *audience) UnmarshalJSON(b []byte) error {
	var one string
	if err := json.Unmarshal(b, &one); err == nil {
		*a = audience{one}
		return nil
	}
	var many []string
	if err := json.Unmarshal(b, &many); err != nil {
		return err
	}
	*a = many
	return nil
}

type jwtClaims struct {
	Aud   audience `json:"aud"`
	Iss   string   `json:"iss"`
	Exp   *float64 `json:"exp"`
	Nbf   *float64 `json:"nbf"`
	Email string   `json:"email"`
	Sub   string   `json:"sub"`
}

func b64(s string) ([]byte, error) {
	return base64.RawURLEncoding.DecodeString(strings.TrimRight(s, "="))
}

// Verify는 JWT의 알고리즘(RS256만), 서명, aud, iss, exp/nbf, 허용 이메일을 확인한다.
// 실패하면 ErrAccessRejected 또는 ErrAccessKeys를 감싼 에러다. 에러에 토큰 값은 넣지 않는다.
func (v *AccessVerifier) Verify(ctx context.Context, token string) (AccessClaims, error) {
	if token == "" {
		return AccessClaims{}, rejected("토큰 없음")
	}
	if len(token) > 16<<10 {
		return AccessClaims{}, rejected("토큰이 너무 김")
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return AccessClaims{}, rejected("JWT 형식 아님")
	}
	hb, err := b64(parts[0])
	if err != nil {
		return AccessClaims{}, rejected("헤더 해석 실패")
	}
	var h jwtHeader
	if err := json.Unmarshal(hb, &h); err != nil {
		return AccessClaims{}, rejected("헤더 해석 실패")
	}
	if h.Alg != "RS256" {
		return AccessClaims{}, rejected("알고리즘이 RS256이 아님")
	}
	if h.Kid == "" {
		return AccessClaims{}, rejected("kid 없음")
	}
	sig, err := b64(parts[2])
	if err != nil {
		return AccessClaims{}, rejected("서명 해석 실패")
	}
	key, err := v.key(ctx, h.Kid)
	if err != nil {
		return AccessClaims{}, err
	}
	sum := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if err := rsa.VerifyPKCS1v15(key, crypto.SHA256, sum[:], sig); err != nil {
		return AccessClaims{}, rejected("서명 불일치")
	}

	pb, err := b64(parts[1])
	if err != nil {
		return AccessClaims{}, rejected("클레임 해석 실패")
	}
	var c jwtClaims
	if err := json.Unmarshal(pb, &c); err != nil {
		return AccessClaims{}, rejected("클레임 해석 실패")
	}
	if c.Iss != v.Issuer {
		return AccessClaims{}, rejected("iss 불일치")
	}
	audOK := false
	for _, a := range c.Aud {
		if v.Audience != "" && a == v.Audience {
			audOK = true
		}
	}
	if !audOK {
		return AccessClaims{}, rejected("aud 불일치")
	}
	leeway := v.Leeway
	if leeway <= 0 {
		leeway = time.Minute
	}
	now := v.now()
	if c.Exp == nil {
		return AccessClaims{}, rejected("exp 없음")
	}
	if now.After(unix(*c.Exp).Add(leeway)) {
		return AccessClaims{}, rejected("만료됨")
	}
	if c.Nbf != nil && now.Add(leeway).Before(unix(*c.Nbf)) {
		return AccessClaims{}, rejected("아직 유효하지 않음(nbf)")
	}
	email := strings.ToLower(strings.TrimSpace(c.Email))
	if email == "" {
		return AccessClaims{}, rejected("email 없음")
	}
	if v.Allowed == nil || !v.Allowed(email) {
		return AccessClaims{}, rejected("허용되지 않은 이메일")
	}
	return AccessClaims{Email: email, Sub: c.Sub}, nil
}

func unix(f float64) time.Time { return time.Unix(int64(f), 0) }

// key는 kid에 맞는 공개키를 돌려준다. 모르는 kid면 제한 간격 안에서 한 번 다시 받는다.
// 받는 동안 잠금을 잡아서 동시에 여러 번 받지 않는다.
func (v *AccessVerifier) key(ctx context.Context, kid string) (*rsa.PublicKey, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if k, ok := v.keys[kid]; ok {
		return k, nil
	}
	wait := v.MinRefetch
	if wait <= 0 {
		wait = time.Minute
	}
	if !v.lastOK {
		wait = failRetry
	}
	now := v.now()
	if !v.fetchedAt.IsZero() && now.Sub(v.fetchedAt) < wait {
		if !v.lastOK {
			return nil, fmt.Errorf("%w: 직전 키 받기 실패", ErrAccessKeys)
		}
		return nil, rejected("모르는 kid")
	}
	v.fetchedAt = now
	v.fetches++
	keys, err := v.fetch(ctx)
	if err != nil {
		v.lastOK = false
		return nil, fmt.Errorf("%w: %v", ErrAccessKeys, err)
	}
	v.lastOK = true
	v.keys = keys
	if k, ok := keys[kid]; ok {
		return k, nil
	}
	return nil, rejected("모르는 kid")
}

type jwk struct {
	Kid string `json:"kid"`
	Kty string `json:"kty"`
	N   string `json:"n"`
	E   string `json:"e"`
}

func (v *AccessVerifier) fetch(ctx context.Context) (map[string]*rsa.PublicKey, error) {
	u := v.CertsURL
	if u == "" {
		u = v.Issuer + "/cdn-cgi/access/certs"
	}
	client := v.Client
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, errors.New("키 주소 연결 실패")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("키 주소 응답 %d", resp.StatusCode)
	}
	var set struct {
		Keys []jwk `json:"keys"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&set); err != nil {
		return nil, errors.New("키 목록 해석 실패")
	}
	out := map[string]*rsa.PublicKey{}
	for _, k := range set.Keys {
		if k.Kty != "RSA" || k.Kid == "" {
			continue
		}
		nb, err1 := b64(k.N)
		eb, err2 := b64(k.E)
		if err1 != nil || err2 != nil || len(eb) == 0 || len(eb) > 4 {
			continue
		}
		e := 0
		for _, b := range eb {
			e = e<<8 | int(b)
		}
		n := new(big.Int).SetBytes(nb)
		if e < 3 || n.BitLen() < 2048 {
			continue
		}
		out[k.Kid] = &rsa.PublicKey{N: n, E: e}
	}
	if len(out) == 0 {
		return nil, errors.New("쓸 수 있는 RSA 키 없음")
	}
	return out, nil
}
