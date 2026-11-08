// Package accesstest는 테스트용 가짜 Cloudflare Access다: RSA 키를 만들어 JWKS를 httptest 서버로 내주고 JWT를 서명한다.
// 실제 Cloudflare에 접속하지 않는다. 테스트에서만 쓴다.
package accesstest

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/yuntyu01/MyHome/server/internal/auth"
)

const (
	Team   = "test.cloudflareaccess.com"
	Issuer = "https://" + Team
	AUD    = "4714c1358e65fe4b408ad6d432a5f878f08194bdb4752441fd56faefa9b2b6f2"
	KID    = "k1"
)

var (
	keyMu sync.Mutex
	keys  = map[string]*rsa.PrivateKey{} // 테스트 바이너리 안에서 kid별로 한 번만 만든다(느려서)
)

// Key는 kid 이름으로 만든 RSA 2048 개인키다(같은 이름이면 같은 키).
func Key(kid string) *rsa.PrivateKey {
	keyMu.Lock()
	defer keyMu.Unlock()
	if k, ok := keys[kid]; ok {
		return k
	}
	k, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		panic(err)
	}
	keys[kid] = k
	return k
}

// Server는 JWKS를 내주는 가짜 Access 서버다.
type Server struct {
	*httptest.Server
	mu     sync.Mutex
	served []string // 내주는 kid
	hits   int
}

// New는 kid "k1"을 내주는 서버를 띄운다. 테스트가 끝나면 닫는다.
func New(t testing.TB) *Server {
	s := &Server{served: []string{KID}}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.hits++
		kids := append([]string(nil), s.served...)
		s.mu.Unlock()
		type jwk struct {
			Kid string `json:"kid"`
			Kty string `json:"kty"`
			Alg string `json:"alg"`
			Use string `json:"use"`
			N   string `json:"n"`
			E   string `json:"e"`
		}
		out := struct {
			Keys []jwk `json:"keys"`
		}{}
		for _, kid := range kids {
			pub := Key(kid).PublicKey
			out.Keys = append(out.Keys, jwk{Kid: kid, Kty: "RSA", Alg: "RS256", Use: "sig",
				N: b64(pub.N.Bytes()), E: b64(big.NewInt(int64(pub.E)).Bytes())})
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(out)
	}))
	t.Cleanup(s.Close)
	return s
}

// Serve는 내주는 kid 목록을 바꾼다(키 교체 흉내).
func (s *Server) Serve(kids ...string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.served = kids
}

// Hits는 JWKS를 받아 간 횟수다.
func (s *Server) Hits() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.hits
}

// Verifier는 이 서버의 키 주소를 쓰는 검증기다(팀 도메인 Team, AUD).
func (s *Server) Verifier(allowed func(string) bool) *auth.AccessVerifier {
	v := auth.NewAccessVerifier(Team, AUD, allowed)
	v.CertsURL = s.URL
	v.Client = s.Client()
	return v
}

// Claims는 email에 대한 유효한 클레임이다(1시간 뒤 만료). 테스트가 고쳐 쓴다.
func Claims(email string) map[string]any {
	now := time.Now()
	return map[string]any{
		"aud": []string{AUD}, "iss": Issuer, "email": email, "sub": "user-" + email, "type": "app",
		"iat": now.Unix(), "nbf": now.Add(-time.Minute).Unix(), "exp": now.Add(time.Hour).Unix(),
	}
}

// Token은 email로 서명한 유효한 JWT다.
func Token(email string) string { return Sign(KID, Key(KID), nil, Claims(email)) }

// Sign은 JWT를 RS256으로 서명한다. header가 nil이면 {"alg":"RS256","kid":kid,"typ":"JWT"}.
func Sign(kid string, key *rsa.PrivateKey, header, claims map[string]any) string {
	if header == nil {
		header = map[string]any{"alg": "RS256", "kid": kid, "typ": "JWT"}
	}
	hb, _ := json.Marshal(header)
	cb, _ := json.Marshal(claims)
	signing := b64(hb) + "." + b64(cb)
	sum := sha256.Sum256([]byte(signing))
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, sum[:])
	if err != nil {
		panic(err)
	}
	return signing + "." + b64(sig)
}

func b64(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }
