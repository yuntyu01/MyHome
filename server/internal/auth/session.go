package auth

import (
	"context"
	"crypto/subtle"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/yuntyu01/MyHome/server/internal/dbq"
	"github.com/yuntyu01/MyHome/server/internal/httpx"
)

// SessionCookie는 웹 세션 쿠키 이름이다.
const SessionCookie = "mh_session"

// CSRFHeader는 세션 인증 요청의 상태 변경 메서드에 필요한 헤더다.
const CSRFHeader = "X-CSRF-Token"

// User는 인증된 사용자(웹 세션 또는 기기 토큰)다.
type User struct {
	KakaoID int64
	Device  *dbq.Device  // 기기 토큰으로 인증했으면 설정
	Session *dbq.Session // 웹 세션으로 인증했으면 설정
	CSRF    string       // 웹 세션의 CSRF 토큰(템플릿에서 씀)
}

const userKey ctxKey = 100

// UserFromContext는 인증된 사용자를 꺼낸다.
func UserFromContext(ctx context.Context) (User, bool) {
	u, ok := ctx.Value(userKey).(User)
	return u, ok
}

func withUser(ctx context.Context, u User) context.Context {
	return context.WithValue(ctx, userKey, u)
}

// Authenticator는 기기 토큰과 웹 세션을 둘 다 처리한다.
type Authenticator struct {
	Q      *dbq.Queries
	TTL    time.Duration
	Secure bool
	Logger *slog.Logger
}

// CreateSession은 세션을 만들고 쿠키를 심는다(HttpOnly, Secure, SameSite=Lax).
func (a *Authenticator) CreateSession(ctx context.Context, w http.ResponseWriter, kakaoID int64) error {
	raw, err := NewToken("")
	if err != nil {
		return err
	}
	csrf, err := NewToken("")
	if err != nil {
		return err
	}
	exp := time.Now().Add(a.TTL)
	if _, err := a.Q.CreateSession(ctx, dbq.CreateSessionParams{IDHash: HashToken(raw), KakaoID: kakaoID, CsrfToken: csrf, ExpiresAt: exp}); err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{
		Name: SessionCookie, Value: raw, Path: "/", Expires: exp, MaxAge: int(a.TTL.Seconds()),
		HttpOnly: true, Secure: a.Secure, SameSite: http.SameSiteLaxMode,
	})
	_, _ = a.Q.DeleteExpiredSessions(ctx)
	return nil
}

// DestroySession은 로그아웃 처리다.
func (a *Authenticator) DestroySession(ctx context.Context, w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(SessionCookie); err == nil && c.Value != "" {
		_ = a.Q.DeleteSession(ctx, HashToken(c.Value))
	}
	http.SetCookie(w, &http.Cookie{Name: SessionCookie, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, Secure: a.Secure, SameSite: http.SameSiteLaxMode})
}

// Authenticate는 요청의 사용자를 찾는다. Bearer 토큰이 있으면 기기 토큰만 본다.
func (a *Authenticator) Authenticate(r *http.Request) (User, bool, error) {
	if _, has := BearerToken(r.Header.Get("Authorization")); has {
		d, ok, err := AuthenticateDevice(r, a.Q)
		if err != nil || !ok {
			return User{}, false, err
		}
		_ = a.Q.TouchDevice(r.Context(), d.ID)
		u := User{Device: &d}
		if d.KakaoID != nil {
			u.KakaoID = *d.KakaoID
		}
		return u, true, nil
	}
	c, err := r.Cookie(SessionCookie)
	if err != nil || c.Value == "" {
		return User{}, false, nil
	}
	s, err := a.Q.GetActiveSession(r.Context(), HashToken(c.Value))
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, false, nil
	}
	if err != nil {
		return User{}, false, err
	}
	_ = a.Q.TouchSession(r.Context(), s.IDHash)
	return User{KakaoID: s.KakaoID, Session: &s, CSRF: s.CsrfToken}, true, nil
}

func unsafeMethod(m string) bool {
	return m != http.MethodGet && m != http.MethodHead && m != http.MethodOptions
}

// CheckCSRF는 세션 인증 요청의 상태 변경 메서드에 CSRF 토큰(헤더 또는 폼 필드 csrf_token)이 맞는지 본다.
// 기기 토큰(Bearer) 요청은 쿠키를 쓰지 않으므로 검사하지 않는다.
func CheckCSRF(r *http.Request, u User) bool {
	if u.Session == nil || !unsafeMethod(r.Method) {
		return true
	}
	got := r.Header.Get(CSRFHeader)
	if got == "" {
		got = r.PostFormValue("csrf_token")
	}
	return got != "" && subtle.ConstantTimeCompare([]byte(got), []byte(u.CSRF)) == 1
}

// RequireAPI는 JSON API용 인증 미들웨어다. 실패하면 401/403 JSON.
func (a *Authenticator) RequireAPI(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, ok, err := a.Authenticate(r)
		if err != nil {
			a.Logger.Error("인증 조회 실패", "err", err)
			httpx.Error(w, http.StatusInternalServerError, "internal error")
			return
		}
		if !ok {
			httpx.Error(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		if !CheckCSRF(r, u) {
			httpx.Error(w, http.StatusForbidden, "csrf token mismatch")
			return
		}
		next.ServeHTTP(w, r.WithContext(withUser(r.Context(), u)))
	})
}

// RequireWeb은 웹 화면용 인증 미들웨어다. 로그인 안 했으면 /login으로 보낸다.
func (a *Authenticator) RequireWeb(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, ok, err := a.Authenticate(r)
		if err != nil {
			a.Logger.Error("인증 조회 실패", "err", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		if !ok || u.Session == nil {
			if r.Header.Get("HX-Request") == "true" {
				w.Header().Set("HX-Redirect", "/login")
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		if !CheckCSRF(r, u) {
			http.Error(w, "CSRF 토큰이 맞지 않습니다. 페이지를 새로고침하세요.", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r.WithContext(withUser(r.Context(), u)))
	})
}
