package focus

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/yuntyu01/MyHome/server/internal/httpx"
)

// Handler는 /v1/focus/* JSON API다(camelCase, 시각은 RFC3339).
type Handler struct {
	Svc    *Service
	Logger *slog.Logger
}

type handlerFunc func(w http.ResponseWriter, r *http.Request) error

// Register는 라우트를 등록한다. wrap은 인증 미들웨어다(기기 토큰 또는 웹 세션).
func (h *Handler) Register(mux *http.ServeMux, wrap func(http.Handler) http.Handler) {
	route := func(pattern string, fn handlerFunc) { mux.Handle(pattern, wrap(h.handle(fn))) }
	route("GET /v1/focus/subjects", h.listSubjects)
	route("PUT /v1/focus/subjects/{id}", h.putSubject)
	route("PUT /v1/focus/sessions/{id}", h.putSession)
	route("PUT /v1/focus/stays/{id}", h.putStay)
	route("GET /v1/focus/all", h.all)
	route("GET /v1/focus/days", h.days)
}

// handle은 에러를 {"error": 메시지, "code": 코드}로 쓴다. 모르는 에러는 로그를 남기고 500.
func (h *Handler) handle(fn handlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		err := fn(w, r)
		if err == nil {
			return
		}
		e := asError(err)
		if e == nil {
			h.Logger.Error("집중 API 오류", "path", r.URL.Path, "err", err)
			e = &Error{http.StatusInternalServerError, "INTERNAL", "internal error"}
		}
		httpx.JSON(w, e.Status, map[string]string{"error": e.Message, "code": e.Code})
	})
}

// decode는 JSON 본문을 읽는다. 모르는 필드는 무시한다(앱이 먼저 업데이트되어도 동기화가 끊기지 않게).
func decode(r *http.Request, v any) error {
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			return &Error{http.StatusRequestEntityTooLarge, CodeTooLarge, "요청이 너무 커요"}
		}
		return badRequest(CodeInvalid, "요청 형식이 잘못됐어요")
	}
	return nil
}

// pathID는 경로의 {id}다(앱이 만든 UUID: 1~64자 영문·숫자·하이픈).
func pathID(r *http.Request) (string, error) {
	id := r.PathValue("id")
	if !ValidID(id) {
		return "", invalid("id", "1~64자 영문·숫자·하이픈이어야 해요")
	}
	return id, nil
}

// listSubjects: GET /v1/focus/subjects → {"subjects": [...]}(숨긴 것 포함, sort·name 순).
func (h *Handler) listSubjects(w http.ResponseWriter, r *http.Request) error {
	out, err := h.Svc.Subjects(r.Context())
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"subjects": out})
	return nil
}

// putSubject: PUT /v1/focus/subjects/{id} → 저장된 과목.
func (h *Handler) putSubject(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r)
	if err != nil {
		return err
	}
	var in SubjectInput
	if err := decode(r, &in); err != nil {
		return err
	}
	out, err := h.Svc.PutSubject(r.Context(), id, in)
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusOK, out)
	return nil
}

// putSession: PUT /v1/focus/sessions/{id} → 저장된 세션.
func (h *Handler) putSession(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r)
	if err != nil {
		return err
	}
	var in SessionInput
	if err := decode(r, &in); err != nil {
		return err
	}
	out, err := h.Svc.PutSession(r.Context(), id, in)
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusOK, out)
	return nil
}

// putStay: PUT /v1/focus/stays/{id} → 저장된 머무름.
func (h *Handler) putStay(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r)
	if err != nil {
		return err
	}
	var in StayInput
	if err := decode(r, &in); err != nil {
		return err
	}
	out, err := h.Svc.PutStay(r.Context(), id, in)
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusOK, out)
	return nil
}

// all: GET /v1/focus/all → {"subjects", "sessions", "stays"}(지운 것·숨긴 것 포함, 앱 복원용).
func (h *Handler) all(w http.ResponseWriter, r *http.Request) error {
	out, err := h.Svc.All(r.Context())
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusOK, out)
	return nil
}

// days: GET /v1/focus/days?from=YYYY-MM-DD&to=YYYY-MM-DD → {"days": [...]}(최대 62일, 하루는 05:00 KST부터).
func (h *Handler) days(w http.ResponseWriter, r *http.Request) error {
	q := r.URL.Query()
	out, err := h.Svc.Days(r.Context(), q.Get("from"), q.Get("to"))
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"days": out})
	return nil
}
