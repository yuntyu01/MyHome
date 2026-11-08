package health

import (
	"errors"
	"net/http"

	"github.com/yuntyu01/MyHome/server/internal/ai"
)

// Error는 건강 API 오류다. 응답은 MyHome 모양 {"error": 메시지}에 핏두잉 오류 코드 {"code": ...}를 더한다.
// 메시지는 화면에 그대로 보여줄 수 있는 한국어다.
type Error struct {
	Status  int
	Code    string
	Message string
}

func (e *Error) Error() string { return e.Message }

func badRequest(code, msg string) *Error { return &Error{http.StatusBadRequest, code, msg} }
func conflict(code, msg string) *Error   { return &Error{http.StatusConflict, code, msg} }

// invalid는 필드 하나의 검증 실패다(400 INVALID "field: msg").
func invalid(field, msg string) *Error { return badRequest("INVALID", field+": "+msg) }

// asError는 에러를 응답으로 바꾼다. 모르는 에러면 nil(500으로 처리하고 로그를 남긴다).
func asError(err error) *Error {
	var e *Error
	if errors.As(err, &e) {
		return e
	}
	var fe *ai.FieldError
	if errors.As(err, &fe) {
		return invalid(fe.Field, fe.Msg)
	}
	return nil
}
