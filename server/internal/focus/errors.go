package focus

import (
	"errors"
	"net/http"
)

// Error는 집중 API 오류다. 응답은 건강 API와 같은 {"error": 메시지, "code": 코드}다.
// 메시지는 화면에 그대로 보여줄 수 있는 한국어다.
type Error struct {
	Status  int
	Code    string
	Message string
}

func (e *Error) Error() string { return e.Message }

// 오류 코드. 앱은 400을 받으면 같은 요청을 다시 보내지 않는다(UNKNOWN_SUBJECT만 과목을 먼저 올린 뒤 다시 보낸다).
const (
	CodeInvalid        = "INVALID"         // 필드 하나가 잘못됨 ("field: msg")
	CodeInvalidRange   = "INVALID_RANGE"   // 끝 시각이 시작 시각보다 늦지 않음
	CodeTooLong        = "TOO_LONG"        // 한 기록이 24시간을 넘음
	CodeUnknownSubject = "UNKNOWN_SUBJECT" // 세션의 과목이 서버에 없음
	CodeTooLarge       = "TOO_LARGE"       // 본문이 너무 큼(413)
)

func badRequest(code, msg string) *Error { return &Error{http.StatusBadRequest, code, msg} }

// invalid는 필드 하나의 검증 실패다(400 INVALID "field: msg").
func invalid(field, msg string) *Error { return badRequest(CodeInvalid, field+": "+msg) }

// asError는 에러를 응답으로 바꾼다. 모르는 에러면 nil(500으로 처리하고 로그를 남긴다).
func asError(err error) *Error {
	var e *Error
	if errors.As(err, &e) {
		return e
	}
	return nil
}
