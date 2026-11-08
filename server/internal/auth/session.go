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
	Email   string       // 웹 세션은 항상, 기기는 앱 연결로 발급했을 때만(CLI·예전 카카오 발급은 빈 값)
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
//
// 웹 화면(RequireWeb):
//   - Access가 있으면(운영) 모든 요청에 유효한 Cloudflare Access JWT가 있어야 한다. 세션은 CSRF용이고 JWT 이메일에 묶는다.
//   - Access가 없고 DevMode면(로컬 개발) 세션 쿠키만 본다. 세션은 /auth/dev-login이 만든다.
//   - 둘 다 아니면 웹 화면을 열지 않는다(설정 누락. serve는 시작 단계에서 거른다).
//
// 앱 API(RequireAPI)는 Access를 거치지 않는다. 기기 토큰(Bearer) 또는 세션 쿠키 + CSRF.
type Authenticator struct {
	Q       *dbq.Queries
	TTL     time.Duration
	Secure  bool
	Logger  *slog.Logger
	Access  *AccessVerifier         // nil이면 Access 검사 없음
	DevMode bool                    // Access 없이 세션 쿠키만으로 웹을 연다(APP_ENV=development)
	Allowed func(email string) bool // 세션 이메일이 아직 허용 목록에 있는지(nil이면 확인 안 함)
}

// CreateSession은 이메일에 묶인 세션을 만들고 쿠키를 심는다(HttpOnly, Secure, SameSite=Lax).
func (a *Authenticator) CreateSession(ctx context.Context, w http.ResponseWriter, email string) (dbq.Session, error) {
	raw, err := NewToken("")
	if err != nil {
		return dbq.Session{}, err
	}
	csrf, err := NewToken("")
	if err != nil {
		return dbq.Session{}, err
	}
	exp := time.Now().Add(a.TTL)
	s, err := a.Q.CreateSession(ctx, dbq.CreateSessionParams{IDHash: HashToken(raw), Email: email, CsrfToken: csrf, ExpiresAt: exp})
	if err != nil {
		return dbq.Session{}, err
	}
	http.SetCookie(w, &http.Cookie{
		Name: SessionCookie, Value: raw, Path: "/", Expires: exp, MaxAge: int(a.TTL.Seconds()),
		HttpOnly: true, Secure: a.Secure, SameSite: http.SameSiteLaxMode,
	})
	_, _ = a.Q.DeleteExpiredSessions(ctx)
	return s, nil
}

// DestroySession은 로그아웃 처리다.
func (a *Authenticator) DestroySession(ctx context.Context, w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(SessionCookie); err == nil && c.Value != "" {
		_ = a.Q.DeleteSession(ctx, HashToken(c.Value))
	}
	http.SetCookie(w, &http.Cookie{Name: SessionCookie, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, Secure: a.Secure, SameSite: http.SameSiteLaxMode})
}

// session은 쿠키의 활성 세션을 찾는다. 허용 목록에서 빠진 이메일의 세션은 없는 것으로 본다.
func (a *Authenticator) session(r *http.Request) (dbq.Session, bool, error) {
	c, err := r.Cookie(SessionCookie)
	if err != nil || c.Value == "" {
		return dbq.Session{}, false, nil
	}
	s, err := a.Q.GetActiveSession(r.Context(), HashToken(c.Value))
	if errors.Is(err, pgx.ErrNoRows) {
		return dbq.Session{}, false, nil
	}
	if err != nil {
		return dbq.Session{}, false, err
	}
	if a.Allowed != nil && !a.Allowed(s.Email) {
		return dbq.Session{}, false, nil
	}
	_ = a.Q.TouchSession(r.Context(), s.IDHash)
	return s, true, nil
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
		if d.Email != nil {
			u.Email = *d.Email
		}
		return u, true, nil
	}
	s, ok, err := a.session(r)
	if err != nil || !ok {
		return User{}, false, err
	}
	return User{Email: s.Email, Session: &s, CSRF: s.CsrfToken}, true, nil
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

// RequireWeb은 웹 화면용 인증 미들웨어다. 규칙은 Authenticator 주석.
func (a *Authenticator) RequireWeb(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var u User
		switch {
		case a.Access != nil:
			var ok bool
			if u, ok = a.accessUser(w, r); !ok {
				return
			}
		case a.DevMode:
			s, ok, err := a.session(r)
			if err != nil {
				a.Logger.Error("인증 조회 실패", "err", err)
				http.Error(w, "internal error", http.StatusInternalServerError)
				return
			}
			if !ok {
				if r.Header.Get("HX-Request") == "true" {
					w.Header().Set("HX-Redirect", "/login")
					w.WriteHeader(http.StatusUnauthorized)
					return
				}
				http.Redirect(w, r, "/login", http.StatusSeeOther)
				return
			}
			u = User{Email: s.Email, Session: &s, CSRF: s.CsrfToken}
		default:
			a.Logger.Error("웹 로그인 설정 없음: CF_ACCESS_TEAM_DOMAIN·CF_ACCESS_AUD가 없고 개발 모드도 아님")
			writePage(w, http.StatusServiceUnavailable, "웹 로그인이 설정되지 않았어요",
				"서버에 Cloudflare Access 설정(CF_ACCESS_TEAM_DOMAIN, CF_ACCESS_AUD)이 없습니다.", "", "")
			return
		}
		if !CheckCSRF(r, u) {
			http.Error(w, "CSRF 토큰이 맞지 않습니다. 페이지를 새로고침하세요.", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r.WithContext(withUser(r.Context(), u)))
	})
}

// accessUser는 Access JWT를 검증하고, 그 이메일에 묶인 세션을 찾거나 새로 만든다.
// 실패하면 응답을 쓰고 ok=false.
func (a *Authenticator) accessUser(w http.ResponseWriter, r *http.Request) (User, bool) {
	claims, err := a.Access.Verify(r.Context(), AccessToken(r))
	if errors.Is(err, ErrAccessKeys) {
		a.Logger.Error("Access 서명 키를 가져오지 못함", "err", err)
		writePage(w, http.StatusServiceUnavailable, "잠시 후 다시 시도해 주세요",
			"로그인 확인에 필요한 Cloudflare Access 키를 가져오지 못했습니다.", "/", "다시 시도")
		return User{}, false
	}
	if err != nil {
		a.Logger.Warn("Access JWT 거부", "reason", err.Error(), "path", r.URL.Path)
		if r.Header.Get("HX-Request") == "true" {
			w.Header().Set("HX-Redirect", "/")
			w.WriteHeader(http.StatusUnauthorized)
			return User{}, false
		}
		writePage(w, http.StatusForbidden, "Cloudflare Access 로그인이 필요해요",
			"이 화면은 Cloudflare Access(구글 로그인)를 거쳐서, 등록된 계정으로만 열 수 있습니다. 가계부 주소를 다시 열어 로그인하세요.",
			"/", "다시 로그인")
		return User{}, false
	}
	s, ok, err := a.session(r)
	if err != nil {
		a.Logger.Error("인증 조회 실패", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return User{}, false
	}
	if ok && s.Email != claims.Email {
		// 다른 계정의 세션이다. 버리고 지금 계정으로 새로 만든다.
		_ = a.Q.DeleteSession(r.Context(), s.IDHash)
		ok = false
	}
	if !ok {
		if s, err = a.CreateSession(r.Context(), w, claims.Email); err != nil {
			a.Logger.Error("세션 생성 실패", "err", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return User{}, false
		}
	}
	return User{Email: s.Email, Session: &s, CSRF: s.CsrfToken}, true
}
