package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// ErrKakaoUnauthorized는 카카오가 토큰·코드를 거부했을 때 반환한다.
var ErrKakaoUnauthorized = errors.New("카카오 인증 실패")

// KakaoTokenInfo는 액세스 토큰 정보다.
type KakaoTokenInfo struct {
	ID    int64 `json:"id"`     // 회원번호
	AppID int64 `json:"app_id"` // 토큰을 발급한 카카오 앱
}

// Kakao는 카카오 API 호출 인터페이스다. 테스트에서는 가짜 구현을 쓴다.
type Kakao interface {
	// AuthorizeURL은 웹 로그인 시작 주소다.
	AuthorizeURL(state string) string
	// ExchangeCode는 인가 코드를 액세스 토큰으로 바꾼다.
	ExchangeCode(ctx context.Context, code string) (string, error)
	// TokenInfo는 액세스 토큰의 회원번호와 앱 ID를 확인한다.
	TokenInfo(ctx context.Context, accessToken string) (KakaoTokenInfo, error)
}

// KakaoHTTP는 실제 카카오 REST API 구현이다.
//
// 사용하는 엔드포인트(주소는 설정으로 바꿀 수 있다):
//   - GET  {auth}/oauth/authorize            웹 로그인 시작
//   - POST {auth}/oauth/token                인가 코드 → 토큰
//   - GET  {api}/v1/user/access_token_info   토큰의 회원번호·앱 ID
//
// 작업 환경에서 카카오 API에 접속해 확인하지 못했다. 동작이 다르면 docs/KAKAO_SETUP.md의 확인 절차를 따른다.
type KakaoHTTP struct {
	RestAPIKey   string
	ClientSecret string
	RedirectURI  string
	AuthBase     string
	APIBase      string
	Client       *http.Client
}

func (k *KakaoHTTP) client() *http.Client {
	if k.Client != nil {
		return k.Client
	}
	return &http.Client{Timeout: 10 * time.Second}
}

func (k *KakaoHTTP) AuthorizeURL(state string) string {
	v := url.Values{}
	v.Set("client_id", k.RestAPIKey)
	v.Set("redirect_uri", k.RedirectURI)
	v.Set("response_type", "code")
	v.Set("state", state)
	return strings.TrimRight(k.AuthBase, "/") + "/oauth/authorize?" + v.Encode()
}

func (k *KakaoHTTP) ExchangeCode(ctx context.Context, code string) (string, error) {
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("client_id", k.RestAPIKey)
	form.Set("redirect_uri", k.RedirectURI)
	form.Set("code", code)
	if k.ClientSecret != "" {
		form.Set("client_secret", k.ClientSecret)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(k.AuthBase, "/")+"/oauth/token", strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded;charset=utf-8")
	var out struct {
		AccessToken string `json:"access_token"`
	}
	if err := k.do(req, &out); err != nil {
		return "", err
	}
	if out.AccessToken == "" {
		return "", ErrKakaoUnauthorized
	}
	return out.AccessToken, nil
}

func (k *KakaoHTTP) TokenInfo(ctx context.Context, accessToken string) (KakaoTokenInfo, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(k.APIBase, "/")+"/v1/user/access_token_info", nil)
	if err != nil {
		return KakaoTokenInfo{}, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	var out KakaoTokenInfo
	if err := k.do(req, &out); err != nil {
		return KakaoTokenInfo{}, err
	}
	if out.ID == 0 {
		return out, ErrKakaoUnauthorized
	}
	return out, nil
}

func (k *KakaoHTTP) do(req *http.Request, out any) error {
	resp, err := k.client().Do(req)
	if err != nil {
		return fmt.Errorf("카카오 API 호출 실패: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusBadRequest {
		return ErrKakaoUnauthorized
	}
	if resp.StatusCode >= 300 {
		return fmt.Errorf("카카오 API 응답 %d", resp.StatusCode)
	}
	return json.Unmarshal(body, out)
}
