package auth

import (
	"fmt"
	"html"
	"log/slog"
	"net/http"
	"time"

	"github.com/yuntyu01/MyHome/server/internal/config"
	"github.com/yuntyu01/MyHome/server/internal/dbq"
	"github.com/yuntyu01/MyHome/server/internal/httpx"
)

// AccessLogoutPath는 Cloudflare Access 로그아웃 주소다(같은 호스트, Access가 처리한다).
const AccessLogoutPath = "/cdn-cgi/access/logout"

// Handlers는 로그아웃·개발용 로그인·앱 연결(코드 교환)·기기 관리 핸들러다.
// 웹 로그인 자체는 Cloudflare Access가 하고, 서버는 RequireWeb에서 JWT를 검증한다.
type Handlers struct {
	Cfg    config.Config
	Auth   *Authenticator
	Q      *dbq.Queries
	Logger *slog.Logger
}

// DevLoginEnabled는 개발용 로그인(/auth/dev-login)을 열지다.
// APP_ENV=development이고 DEV_LOGIN_EMAIL이 있으며 Access 검사가 꺼져 있을 때만 연다.
func (h *Handlers) DevLoginEnabled() bool {
	return h.Cfg.AppEnv == "development" && h.Cfg.DevLoginEmail != "" && h.Auth.Access == nil
}

// Register는 라우트를 등록한다. 앱 연결 화면(GET /app/link)은 web 패키지가 등록한다(화면 틀을 같이 쓰려고).
func (h *Handlers) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /auth/logout", h.logout)
	if h.DevLoginEnabled() {
		mux.HandleFunc("GET /auth/dev-login", h.devLogin)
	}
	mux.HandleFunc("POST /v1/auth/exchange", h.exchange)
	mux.Handle("GET /v1/devices", h.Auth.RequireAPI(http.HandlerFunc(h.listDevices)))
	mux.Handle("POST /v1/devices/{id}/revoke", h.Auth.RequireAPI(http.HandlerFunc(h.revokeDevice)))
}

// devLogin은 로컬 개발용 로그인 우회다. DevLoginEnabled일 때만 등록된다.
func (h *Handlers) devLogin(w http.ResponseWriter, r *http.Request) {
	if !h.Cfg.IsAllowedEmail(h.Cfg.DevLoginEmail) {
		http.Error(w, "DEV_LOGIN_EMAIL이 ALLOWED_EMAILS에 없습니다.", http.StatusForbidden)
		return
	}
	if _, err := h.Auth.CreateSession(r.Context(), w, h.Cfg.DevLoginEmail); err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// logout은 서버 세션을 지운다. Access를 쓰면 Access 로그아웃으로 보내 구글 로그인 상태(Access 쿠키)도 끊는다.
func (h *Handlers) logout(w http.ResponseWriter, r *http.Request) {
	u, ok, _ := h.Auth.Authenticate(r)
	if ok && !CheckCSRF(r, u) {
		http.Error(w, "CSRF 토큰이 맞지 않습니다.", http.StatusForbidden)
		return
	}
	h.Auth.DestroySession(r.Context(), w, r)
	if h.Auth.Access != nil {
		http.Redirect(w, r, AccessLogoutPath, http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

// DeviceDTO는 기기 목록 항목이다(토큰 해시는 내보내지 않는다).
type DeviceDTO struct {
	ID         int64      `json:"id"`
	Name       string     `json:"name"`
	CreatedAt  time.Time  `json:"created_at"`
	LastSeenAt *time.Time `json:"last_seen_at"`
	RevokedAt  *time.Time `json:"revoked_at"`
	Email      string     `json:"email,omitempty"`
	Via        string     `json:"via"` // app_link(앱 연결) | kakao(예전 카카오 로그인) | cli(직접 발급)
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
		dto := DeviceDTO{ID: d.ID, Name: d.Name, CreatedAt: d.CreatedAt, LastSeenAt: d.LastSeenAt, RevokedAt: d.RevokedAt,
			Via: "cli", Current: u.Device != nil && u.Device.ID == d.ID}
		switch {
		case d.Email != nil:
			dto.Email, dto.Via = *d.Email, "app_link"
		case d.KakaoID != nil:
			dto.Via = "kakao"
		}
		out = append(out, dto)
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

// writePage는 간단한 안내 페이지를 쓴다. href가 비어 있으면 링크를 넣지 않는다.
func writePage(w http.ResponseWriter, status int, title, msg, href, linkText string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	link := ""
	if href != "" {
		link = fmt.Sprintf(`<p><a href="%s">%s</a></p>`, html.EscapeString(href), html.EscapeString(linkText))
	}
	fmt.Fprintf(w, `<!doctype html><html lang="ko"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>%s</title>
<style>body{font-family:system-ui,sans-serif;max-width:32rem;margin:4rem auto;padding:0 1rem;line-height:1.6;color-scheme:light dark}a{color:inherit}</style></head>
<body><h1>%s</h1><p>%s</p>%s</body></html>`, html.EscapeString(title), html.EscapeString(title), html.EscapeString(msg), link)
}
