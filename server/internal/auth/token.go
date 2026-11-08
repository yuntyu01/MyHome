// Package auth는 기기 토큰, 웹 세션, Cloudflare Access 로그인, 앱 연결(1회용 코드 + PKCE)을 담당한다.
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"strings"

	"github.com/yuntyu01/MyHome/server/internal/dbq"
)

// TokenPrefix는 기기 토큰 앞에 붙는 접두사다. 로그나 저장소에 실수로 섞였을 때 알아보기 쉽게 한다.
const TokenPrefix = "mhd_"

// NewToken은 32바이트 난수로 만든 토큰 원문을 반환한다. 원문은 사용자에게 한 번만 보여준다.
func NewToken(prefix string) (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return prefix + base64.RawURLEncoding.EncodeToString(b), nil
}

// HashToken은 DB에 저장할 토큰 해시(SHA-256)를 반환한다.
// 토큰은 256비트 난수라서 솔트 없이 SHA-256으로 충분하다.
func HashToken(token string) []byte {
	sum := sha256.Sum256([]byte(token))
	return sum[:]
}

// BearerToken은 Authorization 헤더에서 Bearer 토큰을 꺼낸다.
func BearerToken(header string) (string, bool) {
	const p = "Bearer "
	if len(header) < len(p) || !strings.EqualFold(header[:len(p)], p) {
		return "", false
	}
	tok := strings.TrimSpace(header[len(p):])
	return tok, tok != ""
}

// DeviceCreator는 기기를 만든다.
type DeviceCreator interface {
	CreateDevice(ctx context.Context, arg dbq.CreateDeviceParams) (dbq.Device, error)
}

// IssueDeviceToken은 새 기기와 토큰을 만든다. 반환한 토큰 원문은 다시 조회할 수 없다.
func IssueDeviceToken(ctx context.Context, q DeviceCreator, name string) (string, dbq.Device, error) {
	return IssueDeviceTokenFor(ctx, q, name, nil)
}

// IssueDeviceTokenFor는 계정(email, 앱 연결)에 묶인 기기 토큰을 발급한다. CLI 발급은 nil.
func IssueDeviceTokenFor(ctx context.Context, q DeviceCreator, name string, email *string) (string, dbq.Device, error) {
	tok, err := NewToken(TokenPrefix)
	if err != nil {
		return "", dbq.Device{}, err
	}
	d, err := q.CreateDevice(ctx, dbq.CreateDeviceParams{Name: name, TokenHash: HashToken(tok), Email: email})
	if err != nil {
		return "", dbq.Device{}, err
	}
	return tok, d, nil
}
