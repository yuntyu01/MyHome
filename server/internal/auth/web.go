package auth

import (
	"crypto/subtle"
	"errors"
	"fmt"
	"html"
	"log/slog"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/yuntyu01/MyHome/server/internal/config"
	"github.com/yuntyu01/MyHome/server/internal/dbq"
	"github.com/yuntyu01/MyHome/server/internal/httpx"
)

const stateCookie = "mh_oauth_state"

// Handlers는 카카오 로그인·로그아웃·기기 토큰 발급·기기 관리 핸들러다.
type Handlers struct {
	Cfg    config.Config
	Kakao  Kakao // nil이면 카카오 로그인 비활성(503)
	Auth   *Authenticator
	Q      *dbq.Queries
	Logger *slog.Logger
}

// Register는 라우트를 등록한다.
func (h *Handlers) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /auth/kakao/login", h.kakaoLogin)
	mux.HandleFunc("GET /auth/kakao/callback", h.kakaoCallback)
	mux.HandleFunc("POST /auth/logout", h.logout)
	mux.HandleFunc("POST /v1/auth/kakao/device", h.deviceLogin)
	if h.Cfg.AppEnv == "development" && h.Cfg.DevLoginKakaoID != 0 {
		mux.HandleFunc("GET /auth/dev-login", h.devLogin)
	}
	mux.Handle("GET /v1/devices", h.Auth.RequireAPI(http.HandlerFunc(h.listDevices)))
	mux.Handle("POST /v1/devices/{id}/revoke", h.Auth.RequireAPI(http.HandlerFunc(h.revokeDevice)))
}

func (h *Handlers) kakaoLogin(w http.ResponseWriter, r *http.Request) {
	if h.Kakao == nil {
		http.Error(w, "카카오 로그인이 설정되지 않았습니다(KAKAO_REST_API_KEY, KAKAO_REDIRECT_URI).", http.StatusServiceUnavailable)
		return
	}
	state, err := NewToken("")
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: stateCookie, Value: state, Path: "/auth/kakao", MaxAge: 600, HttpOnly: true, Secure: h.Cfg.CookieSecure, SameSite: http.SameSiteLaxMode})
	http.Redirect(w, r, h.Kakao.AuthorizeURL(state), http.StatusFound)
}

func (h *Handlers) kakaoCallback(w http.ResponseWriter, r *http.Request) {
	if h.Kakao == nil {
		http.Error(w, "카카오 로그인이 설정되지 않았습니다.", http.StatusServiceUnavailable)
		return
	}
	c, err := r.Cookie(stateCookie)
	state := r.URL.Query().Get("state")
	if err != nil || state == "" || subtle.ConstantTimeCompare([]byte(c.Value), []byte(state)) != 1 {
		h.page(w, http.StatusBadRequest, "로그인 실패", "로그인 요청이 만료됐거나 올바르지 않습니다. 다시 시도해 주세요.")
		return
	}
	http.SetCookie(w, &http.Cookie{Name: stateCookie, Value: "", Path: "/auth/kakao", MaxAge: -1, HttpOnly: true, Secure: h.Cfg.CookieSecure, SameSite: http.SameSiteLaxMode})
	if e := r.URL.Query().Get("error"); e != "" {
		h.page(w, http.StatusUnauthorized, "로그인 취소", "카카오 로그인이 취소됐습니다.")
		return
	}
	code := r.URL.Query().Get("code")
	if code == "" {
		h.page(w, http.StatusBadRequest, "로그인 실패", "인가 코드가 없습니다.")
		return
	}
	tok, err := h.Kakao.ExchangeCode(r.Context(), code)
	if err != nil {
		h.Logger.Warn("카카오 코드 교환 실패", "err", err)
		h.page(w, http.StatusUnauthorized, "로그인 실패", "카카오 인증에 실패했습니다.")
		return
	}
	info, err := h.Kakao.TokenInfo(r.Context(), tok)
	if err != nil {
		h.Logger.Warn("카카오 토큰 확인 실패", "err", err)
		h.page(w, http.StatusUnauthorized, "로그인 실패", "카카오 인증에 실패했습니다.")
		return
	}
	if !h.Cfg.IsAllowedKakaoID(info.ID) {
		h.Logger.Warn("허용되지 않은 카카오 회원번호", "kakao_id", info.ID)
		h.page(w, http.StatusForbidden, "허용되지 않은 계정",
			fmt.Sprintf("이 가계부는 등록된 계정만 쓸 수 있습니다. 본인 계정이라면 서버 환경변수 ALLOWED_KAKAO_IDS에 회원번호 %d를 추가하세요.", info.ID))
		return
	}
	if err := h.Auth.CreateSession(r.Context(), w, info.ID); err != nil {
		h.Logger.Error("세션 생성 실패", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	h.Logger.Info("웹 로그인", "kakao_id", info.ID)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// devLogin은 로컬 개발용 로그인 우회다. APP_ENV=development + DEV_LOGIN_KAKAO_ID일 때만 등록된다.
func (h *Handlers) devLogin(w http.ResponseWriter, r *http.Request) {
	if !h.Cfg.IsAllowedKakaoID(h.Cfg.DevLoginKakaoID) {
		http.Error(w, "DEV_LOGIN_KAKAO_ID가 ALLOWED_KAKAO_IDS에 없습니다.", http.StatusForbidden)
		return
	}
	if err := h.Auth.CreateSession(r.Context(), w, h.Cfg.DevLoginKakaoID); err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (h *Handlers) logout(w http.ResponseWriter, r *http.Request) {
	u, ok, _ := h.Auth.Authenticate(r)
	if ok && !CheckCSRF(r, u) {
		http.Error(w, "CSRF 토큰이 맞지 않습니다.", http.StatusForbidden)
		return
	}
	h.Auth.DestroySession(r.Context(), w, r)
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

type deviceLoginRequest struct {
	AccessToken string `json:"access_token"`
	DeviceName  string `json:"device_name"`
}

// deviceLogin: 앱이 카카오 SDK로 받은 액세스 토큰을 서버가 검증한 뒤 기기 토큰을 발급한다.
func (h *Handlers) deviceLogin(w http.ResponseWriter, r *http.Request) {
	if h.Kakao == nil {
		httpx.Error(w, http.StatusServiceUnavailable, "kakao login not configured")
		return
	}
	var in deviceLoginRequest
	if err := httpx.DecodeJSON(r, &in); err != nil || in.AccessToken == "" {
		httpx.Error(w, http.StatusBadRequest, "access_token is required")
		return
	}
	info, err := h.Kakao.TokenInfo(r.Context(), in.AccessToken)
	if errors.Is(err, ErrKakaoUnauthorized) {
		httpx.Error(w, http.StatusUnauthorized, "kakao token rejected")
		return
	}
	if err != nil {
		h.Logger.Error("카카오 토큰 확인 실패", "err", err)
		httpx.Error(w, http.StatusBadGateway, "kakao api error")
		return
	}
	// 다른 앱에서 받은 카카오 토큰으로 기기 토큰을 받지 못하게 앱 ID를 확인한다.
	if h.Cfg.KakaoAppID != 0 && info.AppID != h.Cfg.KakaoAppID {
		h.Logger.Warn("다른 카카오 앱의 토큰", "app_id", info.AppID)
		httpx.Error(w, http.StatusForbidden, "token from another app")
		return
	}
	if !h.Cfg.IsAllowedKakaoID(info.ID) {
		h.Logger.Warn("허용되지 않은 카카오 회원번호(앱)", "kakao_id", info.ID)
		httpx.JSON(w, http.StatusForbidden, map[string]any{"error": "kakao account not allowed", "kakao_id": info.ID})
		return
	}
	name := strings.TrimSpace(in.DeviceName)
	if name == "" || utf8.RuneCountInString(name) > 60 {
		name = "android-" + time.Now().Format("20060102")
	}
	kid := info.ID
	tok, d, err := IssueDeviceTokenFor(r.Context(), h.Q, name, &kid)
	if err != nil {
		h.Logger.Error("기기 토큰 발급 실패", "err", err)
		httpx.Error(w, http.StatusInternalServerError, "internal error")
		return
	}
	h.Logger.Info("기기 토큰 발급(카카오)", "device_id", d.ID, "kakao_id", info.ID)
	httpx.JSON(w, http.StatusCreated, map[string]any{"token": tok, "device_id": d.ID, "device_name": d.Name})
}

// DeviceDTO는 기기 목록 항목이다(토큰 해시는 내보내지 않는다).
type DeviceDTO struct {
	ID         int64      `json:"id"`
	Name       string     `json:"name"`
	CreatedAt  time.Time  `json:"created_at"`
	LastSeenAt *time.Time `json:"last_seen_at"`
	RevokedAt  *time.Time `json:"revoked_at"`
	ViaKakao   bool       `json:"via_kakao"`
	Current    bool       `json:"current"`
}

// ListDevices는 기기 목록이다(웹에서도 쓴다).
func (h *Handlers) ListDevices(r *http.Request) ([]DeviceDTO, error) {
	devs, err := h.Q.ListDevices(r.Context())
	if err != nil {
		return nil, err
	}
	u, _ := UserFromContext(r.Context())
	out := make([]DeviceDTO, 0, len(devs))
	for _, d := range devs {
		out = append(out, DeviceDTO{ID: d.ID, Name: d.Name, CreatedAt: d.CreatedAt, LastSeenAt: d.LastSeenAt, RevokedAt: d.RevokedAt,
			ViaKakao: d.KakaoID != nil, Current: u.Device != nil && u.Device.ID == d.ID})
	}
	return out, nil
}

func (h *Handlers) listDevices(w http.ResponseWriter, r *http.Request) {
	out, err := h.ListDevices(r)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "internal error")
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"devices": out})
}

func (h *Handlers) revokeDevice(w http.ResponseWriter, r *http.Request) {
	var id int64
	if _, err := fmt.Sscan(r.PathValue("id"), &id); err != nil || id <= 0 {
		httpx.Error(w, http.StatusBadRequest, "invalid id")
		return
	}
	d, err := h.Q.RevokeDevice(r.Context(), id)
	if err != nil {
		httpx.Error(w, http.StatusNotFound, "not found")
		return
	}
	h.Logger.Info("기기 토큰 폐기", "device_id", d.ID)
	httpx.JSON(w, http.StatusOK, map[string]any{"id": d.ID, "revoked_at": d.RevokedAt})
}

// page는 간단한 안내 페이지를 쓴다.
func (h *Handlers) page(w http.ResponseWriter, status int, title, msg string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	fmt.Fprintf(w, `<!doctype html><html lang="ko"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>%s</title>
<style>body{font-family:system-ui,sans-serif;max-width:32rem;margin:4rem auto;padding:0 1rem;line-height:1.6;color-scheme:light dark}a{color:#2e7d5b}</style></head>
<body><h1>%s</h1><p>%s</p><p><a href="/login">로그인 화면으로</a></p></body></html>`, html.EscapeString(title), html.EscapeString(title), html.EscapeString(msg))
}
