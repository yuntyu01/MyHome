package focus

import (
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/yuntyu01/MyHome/server/internal/dbq"
	"github.com/yuntyu01/MyHome/server/internal/period"
)

// 검증 한도. DB CHECK(00013_focus.sql)와 같다.
const (
	MaxIDLen    = 64
	MaxNameLen  = 40             // 과목 이름·장소 이름(글자 수)
	MaxDuration = 24 * time.Hour // 세션·머무름 하나의 최대 길이
)

var (
	idRe    = regexp.MustCompile(`^[A-Za-z0-9-]{1,64}$`)
	colorRe = regexp.MustCompile(`^#[0-9A-Fa-f]{6}$`)
)

// ValidID는 앱이 만든 id(UUID)인지다: 1~64자, 영문·숫자·하이픈.
func ValidID(id string) bool { return idRe.MatchString(id) }

// ── 요청 ──────────────────────────────────────────────────────────────
// 시각은 RFC3339(오프셋 포함)다. 모르는 필드는 무시한다(앱이 먼저 업데이트되어도 동기화가 끊기지 않게).

// SubjectInput은 PUT /v1/focus/subjects/{id} 본문이다.
type SubjectInput struct {
	Name      string    `json:"name"`
	Color     string    `json:"color"`
	Sort      int32     `json:"sort"`
	Archived  bool      `json:"archived"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// normalize는 이름 앞뒤 공백을 지우고 색을 대문자로 맞춘 뒤 검증한다.
func (in *SubjectInput) normalize() error {
	in.Name = strings.TrimSpace(in.Name)
	if err := checkName("name", in.Name); err != nil {
		return err
	}
	if !colorRe.MatchString(in.Color) {
		return invalid("color", "#RRGGBB 형식이어야 해요")
	}
	in.Color = strings.ToUpper(in.Color)
	if in.UpdatedAt.IsZero() {
		return invalid("updatedAt", "필수예요")
	}
	return nil
}

// SessionInput은 PUT /v1/focus/sessions/{id} 본문이다.
type SessionInput struct {
	SubjectID string    `json:"subjectId"`
	StartedAt time.Time `json:"startedAt"`
	EndedAt   time.Time `json:"endedAt"`
	Deleted   bool      `json:"deleted"`
	UpdatedAt time.Time `json:"updatedAt"`
}

func (in *SessionInput) normalize() error {
	switch {
	case in.SubjectID == "":
		return invalid("subjectId", "필수예요")
	case !ValidID(in.SubjectID):
		return invalid("subjectId", "1~64자 영문·숫자·하이픈이어야 해요")
	}
	if err := checkSpan("startedAt", "endedAt", in.StartedAt, in.EndedAt); err != nil {
		return err
	}
	if in.UpdatedAt.IsZero() {
		return invalid("updatedAt", "필수예요")
	}
	return nil
}

// StayInput은 PUT /v1/focus/stays/{id} 본문이다. 장소 이름만 받는다(좌표는 폰 밖으로 나가지 않는다).
type StayInput struct {
	Place     string    `json:"place"`
	EnteredAt time.Time `json:"enteredAt"`
	ExitedAt  time.Time `json:"exitedAt"`
	Deleted   bool      `json:"deleted"`
	UpdatedAt time.Time `json:"updatedAt"`
}

func (in *StayInput) normalize() error {
	in.Place = strings.TrimSpace(in.Place)
	if err := checkName("place", in.Place); err != nil {
		return err
	}
	if err := checkSpan("enteredAt", "exitedAt", in.EnteredAt, in.ExitedAt); err != nil {
		return err
	}
	if in.UpdatedAt.IsZero() {
		return invalid("updatedAt", "필수예요")
	}
	return nil
}

// checkName은 1~MaxNameLen 글자이고 제어 문자(줄바꿈, NUL 등)가 없는지 본다. 공백은 미리 지운다.
func checkName(field, s string) error {
	if s == "" {
		return invalid(field, "필수예요")
	}
	if utf8.RuneCountInString(s) > MaxNameLen {
		return invalid(field, "40자까지예요")
	}
	if strings.IndexFunc(s, unicode.IsControl) >= 0 {
		return invalid(field, "줄바꿈·제어 문자는 쓸 수 없어요")
	}
	return nil
}

// checkSpan은 시작·끝 시각이 있고, 끝이 시작보다 늦고, 길이가 MaxDuration(24시간) 이하인지 본다.
func checkSpan(startField, endField string, start, end time.Time) error {
	switch {
	case start.IsZero():
		return invalid(startField, "필수예요")
	case end.IsZero():
		return invalid(endField, "필수예요")
	case !end.After(start):
		return badRequest(CodeInvalidRange, endField+": "+startField+"보다 늦어야 해요")
	case end.Sub(start) > MaxDuration:
		return badRequest(CodeTooLong, "기록 하나는 24시간까지예요")
	}
	return nil
}

// ── 응답 ──────────────────────────────────────────────────────────────
// 시각은 KST 오프셋을 붙인 RFC3339다(다른 MyHome API와 같다).

// Subject는 저장된 과목이다.
type Subject struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Color     string    `json:"color"`
	Sort      int32     `json:"sort"`
	Archived  bool      `json:"archived"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// Session은 저장된 공부 세션이다.
type Session struct {
	ID        string    `json:"id"`
	SubjectID string    `json:"subjectId"`
	StartedAt time.Time `json:"startedAt"`
	EndedAt   time.Time `json:"endedAt"`
	Deleted   bool      `json:"deleted"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// Stay는 저장된 장소 머무름이다.
type Stay struct {
	ID        string    `json:"id"`
	Place     string    `json:"place"`
	EnteredAt time.Time `json:"enteredAt"`
	ExitedAt  time.Time `json:"exitedAt"`
	Deleted   bool      `json:"deleted"`
	UpdatedAt time.Time `json:"updatedAt"`
}

func kst(t time.Time) time.Time { return t.In(period.KST) }

func subjectOf(r dbq.FocusSubject) Subject {
	return Subject{ID: r.ID, Name: r.Name, Color: r.Color, Sort: r.Sort, Archived: r.Archived, UpdatedAt: kst(r.UpdatedAt)}
}

func sessionOf(r dbq.FocusSession) Session {
	return Session{ID: r.ID, SubjectID: r.SubjectID, StartedAt: kst(r.StartedAt), EndedAt: kst(r.EndedAt),
		Deleted: r.Deleted, UpdatedAt: kst(r.UpdatedAt)}
}

func stayOf(r dbq.FocusStay) Stay {
	return Stay{ID: r.ID, Place: r.Place, EnteredAt: kst(r.EnteredAt), ExitedAt: kst(r.ExitedAt),
		Deleted: r.Deleted, UpdatedAt: kst(r.UpdatedAt)}
}
