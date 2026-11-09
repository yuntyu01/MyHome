package server_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/yuntyu01/MyHome/server/internal/auth"
	"github.com/yuntyu01/MyHome/server/internal/db"
)

// 집중 API(/v1/focus, 공부 타이머). 기기 토큰으로 부른다(건강 API 테스트의 클라이언트 hc를 같이 쓴다).
// 시각은 KST로 보내고, 응답도 KST 오프셋(+09:00)이다. 하루는 05:00 KST에 바뀐다.

const (
	focusMath = "11111111-1111-4111-8111-111111111111"
	focusEng  = "22222222-2222-4222-8222-222222222222"
)

func subjectBody(name, color string, sort int, archived bool, updatedAt string) string {
	return js(map[string]any{"name": name, "color": color, "sort": sort, "archived": archived, "updatedAt": updatedAt})
}

func sessionBody(subjectID, start, end string, deleted bool, updatedAt string) string {
	return js(map[string]any{"subjectId": subjectID, "startedAt": start, "endedAt": end, "deleted": deleted, "updatedAt": updatedAt})
}

func stayBody(place, enter, exit string, deleted bool, updatedAt string) string {
	return js(map[string]any{"place": place, "enteredAt": enter, "exitedAt": exit, "deleted": deleted, "updatedAt": updatedAt})
}

// rows는 표의 행 수다.
func (c *hc) rows(table string) int {
	c.t.Helper()
	var n int
	if err := c.pool.QueryRow(context.Background(), "SELECT count(*) FROM "+table).Scan(&n); err != nil {
		c.t.Fatal(err)
	}
	return n
}

// focusSubjects는 수학(sort 1)·영어(sort 2) 과목을 올린다.
func (c *hc) focusSubjects() {
	c.t.Helper()
	c.put("/v1/focus/subjects/"+focusMath, subjectBody("수학", "#FF8800", 1, false, "2026-11-09T09:00:00+09:00")).is(200)
	c.put("/v1/focus/subjects/"+focusEng, subjectBody("영어", "#0088FF", 2, false, "2026-11-09T09:00:00+09:00")).is(200)
}

func TestFocusAuthRequired(t *testing.T) {
	c := newHealthClient(t)
	for _, p := range []string{"GET /v1/focus/subjects", "PUT /v1/focus/subjects/" + focusMath, "PUT /v1/focus/sessions/a1",
		"PUT /v1/focus/stays/a1", "GET /v1/focus/all", "GET /v1/focus/days?from=2026-11-01&to=2026-11-02"} {
		method, path, _ := strings.Cut(p, " ")
		if r := c.serve(httptest.NewRequest(method, path, strings.NewReader("{}")), ""); r.Code != http.StatusUnauthorized {
			t.Errorf("%s: status %d, want 401", p, r.Code)
		}
	}
	if r := c.serve(httptest.NewRequest("GET", "/v1/focus/all", nil), "garbage"); r.Code != http.StatusUnauthorized {
		t.Errorf("틀린 토큰: %d", r.Code)
	}
}

// 웹 세션으로도 읽을 수 있고, 쓰기는 CSRF가 있어야 한다(RequireAPI).
func TestFocusWebSession(t *testing.T) {
	c := newWebClient(t)
	if r := c.get("/v1/focus/subjects"); r.Code != http.StatusOK || !strings.Contains(r.Body.String(), `"subjects":[]`) {
		t.Fatalf("세션 읽기 %d %s", r.Code, r.Body.String())
	}
	body := subjectBody("수학", "#FF8800", 1, false, "2026-11-09T09:00:00+09:00")
	req := httptest.NewRequest("PUT", "/v1/focus/subjects/"+focusMath, strings.NewReader(body))
	req.AddCookie(c.sess)
	if r := serve(c.h, req); r.Code != http.StatusForbidden {
		t.Fatalf("CSRF 없이 %d", r.Code)
	}
	req = httptest.NewRequest("PUT", "/v1/focus/subjects/"+focusMath, strings.NewReader(body))
	req.AddCookie(c.sess)
	req.Header.Set(auth.CSRFHeader, c.csrf)
	if r := serve(c.h, req); r.Code != http.StatusOK {
		t.Fatalf("CSRF와 함께 %d %s", r.Code, r.Body.String())
	}
}

func TestFocusSubjectUpsertAndList(t *testing.T) {
	c := newHealthClient(t)
	c.get("/v1/focus/subjects").is(200).size("subjects", 0)

	// 이름 앞뒤 공백은 지우고 색은 대문자로 맞춘다. 시각은 KST로 돌려준다(밀리초 그대로)
	c.put("/v1/focus/subjects/"+focusEng, subjectBody("영어", "#0088ff", 2, false, "2026-11-09T00:00:00Z")).is(200)
	c.put("/v1/focus/subjects/"+focusMath, subjectBody("  수학 ", "#ff8800", 1, false, "2026-11-09T01:00:00.123Z")).is(200).
		eq("id", focusMath).eq("name", "수학").eq("color", "#FF8800").eq("sort", 1).eq("archived", false).
		eq("updatedAt", "2026-11-09T10:00:00.123+09:00")
	c.get("/v1/focus/subjects").is(200).size("subjects", 2).
		eq("subjects.0.id", focusMath).eq("subjects.1.id", focusEng).eq("subjects.1.color", "#0088FF").
		eq("subjects.1.updatedAt", "2026-11-09T09:00:00+09:00")

	// 더 새로운 값은 덮어쓴다. 숨긴 과목도 목록에 나온다
	c.put("/v1/focus/subjects/"+focusEng, subjectBody("영어 회화", "#0088FF", 0, true, "2026-11-09T12:00:00+09:00")).is(200).
		eq("name", "영어 회화").eq("archived", true).eq("sort", 0)
	c.get("/v1/focus/subjects").is(200).size("subjects", 2).
		eq("subjects.0.id", focusEng).eq("subjects.0.archived", true).eq("subjects.1.id", focusMath)
	if n := c.rows("focus_subjects"); n != 2 {
		t.Fatalf("과목 %d줄", n)
	}

	// 검증 실패는 저장하지 않는다
	path := "/v1/focus/subjects/" + focusMath
	c.put(path, subjectBody("수학", "red", 1, false, "2026-11-10T00:00:00+09:00")).code(400, "INVALID")
	c.put(path, subjectBody("수학", "#FF88", 1, false, "2026-11-10T00:00:00+09:00")).code(400, "INVALID")
	c.put(path, subjectBody("  ", "#FF8800", 1, false, "2026-11-10T00:00:00+09:00")).code(400, "INVALID")
	c.put(path, subjectBody(strings.Repeat("가", 41), "#FF8800", 1, false, "2026-11-10T00:00:00+09:00")).code(400, "INVALID")
	c.put(path, `{"name":"수학","color":"#FF8800"}`).code(400, "INVALID") // updatedAt 없음
	c.put(path, `{"name":"수학","color":"#FF8800","updatedAt":"2026-11-10"}`).code(400, "INVALID")
	c.put(path, `not json`).code(400, "INVALID")
	c.put(path, ``).code(400, "INVALID")
	c.put("/v1/focus/subjects/bad_id", subjectBody("수학", "#FF8800", 1, false, "2026-11-10T00:00:00+09:00")).code(400, "INVALID")
	c.put("/v1/focus/subjects/"+strings.Repeat("a", 65), subjectBody("수학", "#FF8800", 1, false, "2026-11-10T00:00:00+09:00")).code(400, "INVALID")
	c.get("/v1/focus/subjects").is(200).size("subjects", 2).eq("subjects.1.name", "수학")
}

// last-write-wins: 저장된 updatedAt이 더 새로우면 바꾸지 않고 저장된 값을 200으로 돌려준다. 같으면 덮어쓴다.
func TestFocusLastWriteWins(t *testing.T) {
	c := newHealthClient(t)
	sp := "/v1/focus/subjects/" + focusMath
	c.put(sp, subjectBody("수학", "#FF8800", 1, false, "2026-11-09T12:00:00+09:00")).is(200)
	c.put(sp, subjectBody("옛 이름", "#000000", 9, true, "2026-11-09T11:59:59+09:00")).is(200).
		eq("name", "수학").eq("color", "#FF8800").eq("sort", 1).eq("archived", false).eq("updatedAt", "2026-11-09T12:00:00+09:00")
	// 같은 순간을 다른 시간대로 보내도 같은 시각이다(덮어씀)
	c.put(sp, subjectBody("수학Ⅰ", "#FF8800", 1, false, "2026-11-09T03:00:00Z")).is(200).eq("name", "수학Ⅰ")
	c.get("/v1/focus/subjects").is(200).eq("subjects.0.name", "수학Ⅰ")

	ssp := "/v1/focus/sessions/s1"
	c.put(ssp, sessionBody(focusMath, "2026-11-10T10:00:00+09:00", "2026-11-10T11:00:00+09:00", false, "2026-11-10T11:00:00+09:00")).is(200)
	c.put(ssp, sessionBody(focusMath, "2026-11-10T10:00:00+09:00", "2026-11-10T10:30:00+09:00", true, "2026-11-10T10:59:00+09:00")).is(200).
		eq("deleted", false).eq("endedAt", "2026-11-10T11:00:00+09:00").eq("updatedAt", "2026-11-10T11:00:00+09:00")

	stp := "/v1/focus/stays/st1"
	c.put(stp, stayBody("시립도서관", "2026-11-10T09:00:00+09:00", "2026-11-10T12:00:00+09:00", false, "2026-11-10T12:00:00+09:00")).is(200)
	c.put(stp, stayBody("독서실", "2026-11-10T09:00:00+09:00", "2026-11-10T10:00:00+09:00", false, "2026-11-10T11:00:00+09:00")).is(200).
		eq("place", "시립도서관").eq("exitedAt", "2026-11-10T12:00:00+09:00")

	c.get("/v1/focus/all").is(200).size("sessions", 1).eq("sessions.0.deleted", false).size("stays", 1).eq("stays.0.place", "시립도서관")
}

// 같은 PUT을 두 번 보내도 한 줄이고 응답이 같다(앱 재전송).
func TestFocusSessionIdempotent(t *testing.T) {
	c := newHealthClient(t)
	c.focusSubjects()
	body := sessionBody(focusMath, "2026-11-10T04:00:00+09:00", "2026-11-10T06:00:00.5+09:00", false, "2026-11-10T06:00:01+09:00")
	first := c.put("/v1/focus/sessions/s1", body).is(200).
		eq("id", "s1").eq("subjectId", focusMath).eq("startedAt", "2026-11-10T04:00:00+09:00").
		eq("endedAt", "2026-11-10T06:00:00.5+09:00").eq("deleted", false).eq("updatedAt", "2026-11-10T06:00:01+09:00")
	second := c.put("/v1/focus/sessions/s1", body).is(200)
	if first.Raw != second.Raw {
		t.Fatalf("다시 보낸 응답이 다름:\n%s\n%s", first.Raw, second.Raw)
	}
	if n := c.rows("focus_sessions"); n != 1 {
		t.Fatalf("세션 %d줄, want 1", n)
	}

	stay := stayBody("시립도서관", "2026-11-10T09:00:00+09:00", "2026-11-10T12:00:00+09:00", false, "2026-11-10T12:00:00+09:00")
	c.put("/v1/focus/stays/st1", stay).is(200)
	c.put("/v1/focus/stays/st1", stay).is(200).eq("id", "st1").eq("place", "시립도서관")
	if n := c.rows("focus_stays"); n != 1 {
		t.Fatalf("머무름 %d줄, want 1", n)
	}
}

func TestFocusSessionValidation(t *testing.T) {
	c := newHealthClient(t)
	c.focusSubjects()
	p := "/v1/focus/sessions/s1"
	upd := "2026-11-10T12:00:00+09:00"

	// 과목을 먼저 올려야 한다
	c.put(p, sessionBody("33333333-3333-4333-8333-333333333333", "2026-11-10T10:00:00+09:00", "2026-11-10T11:00:00+09:00", false, upd)).
		code(400, "UNKNOWN_SUBJECT")
	c.put(p, sessionBody("", "2026-11-10T10:00:00+09:00", "2026-11-10T11:00:00+09:00", false, upd)).code(400, "INVALID")
	c.put(p, sessionBody(focusMath, "2026-11-10T10:00:00+09:00", "2026-11-10T10:00:00+09:00", false, upd)).code(400, "INVALID_RANGE")
	c.put(p, sessionBody(focusMath, "2026-11-10T10:00:00+09:00", "2026-11-10T09:00:00+09:00", false, upd)).code(400, "INVALID_RANGE")
	c.put(p, sessionBody(focusMath, "2026-11-10T10:00:00+09:00", "2026-11-11T10:00:01+09:00", false, upd)).code(400, "TOO_LONG")
	c.put(p, `{"subjectId":"`+focusMath+`","endedAt":"2026-11-10T11:00:00+09:00","updatedAt":"`+upd+`"}`).code(400, "INVALID")
	c.put("/v1/focus/sessions/a.b", sessionBody(focusMath, "2026-11-10T10:00:00+09:00", "2026-11-10T11:00:00+09:00", false, upd)).
		code(400, "INVALID")
	if n := c.rows("focus_sessions"); n != 0 {
		t.Fatalf("실패한 요청이 저장됨: %d줄", n)
	}
	// 딱 24시간은 된다
	c.put(p, sessionBody(focusMath, "2026-11-10T10:00:00+09:00", "2026-11-11T10:00:00+09:00", false, upd)).is(200)

	// 머무름
	sp := "/v1/focus/stays/st1"
	c.put(sp, stayBody(" ", "2026-11-10T09:00:00+09:00", "2026-11-10T12:00:00+09:00", false, upd)).code(400, "INVALID")
	c.put(sp, stayBody(strings.Repeat("a", 41), "2026-11-10T09:00:00+09:00", "2026-11-10T12:00:00+09:00", false, upd)).code(400, "INVALID")
	c.put(sp, stayBody("도서관", "2026-11-10T09:00:00+09:00", "2026-11-10T09:00:00+09:00", false, upd)).code(400, "INVALID_RANGE")
	c.put(sp, stayBody("도서관", "2026-11-10T09:00:00+09:00", "2026-11-11T09:30:00+09:00", false, upd)).code(400, "TOO_LONG")
	c.put(sp, `{"place":"도서관","enteredAt":"2026-11-10T09:00:00+09:00","updatedAt":"`+upd+`"}`).code(400, "INVALID") // 아직 안 나옴
	if n := c.rows("focus_stays"); n != 0 {
		t.Fatalf("실패한 요청이 저장됨: %d줄", n)
	}
}

// /days는 05:00 KST에서 날을 나누고, 기록 없는 날도 0으로 채우고, 지운 기록은 뺀다. /all은 지운 것·숨긴 것까지 준다.
func TestFocusDaysAndAll(t *testing.T) {
	c := newHealthClient(t)
	c.focusSubjects()
	upd := "2026-11-11T12:00:00+09:00"
	put := func(id, subject, start, end string, deleted bool) {
		t.Helper()
		c.put("/v1/focus/sessions/"+id, sessionBody(subject, start, end, deleted, upd)).is(200)
	}
	put("s1", focusMath, "2026-11-10T04:00:00+09:00", "2026-11-10T06:00:00+09:00", false) // 11-09 1시간 + 11-10 1시간
	put("s2", focusEng, "2026-11-10T10:00:00+09:00", "2026-11-10T10:30:00+09:00", false)  // 11-10 30분
	put("s3", focusMath, "2026-11-10T13:00:00+09:00", "2026-11-10T14:00:00+09:00", false)
	put("s4", focusMath, "2026-11-07T20:00:00+09:00", "2026-11-07T23:00:00+09:00", false) // 범위 밖(11-07)
	put("s5", focusEng, "2026-11-08T03:00:00+09:00", "2026-11-08T05:30:00+09:00", false)  // 11-07 2시간(범위 밖) + 11-08 30분
	// s3을 지운다(더 새로운 updatedAt으로 deleted=true)
	c.put("/v1/focus/sessions/s3", sessionBody(focusMath, "2026-11-10T13:00:00+09:00", "2026-11-10T14:00:00+09:00", true,
		"2026-11-11T13:00:00+09:00")).is(200).eq("deleted", true)

	stay := func(id, place, enter, exit string, deleted bool) {
		t.Helper()
		c.put("/v1/focus/stays/"+id, stayBody(place, enter, exit, deleted, upd)).is(200)
	}
	stay("st1", "시립도서관", "2026-11-10T09:00:00+09:00", "2026-11-10T12:00:00+09:00", false) // 11-10 3시간
	stay("st2", "독서실", "2026-11-11T03:00:00+09:00", "2026-11-11T06:00:00+09:00", false)   // 11-10 2시간 + 11-11 1시간
	stay("st3", "카페", "2026-11-10T15:00:00+09:00", "2026-11-10T16:00:00+09:00", true)     // 지움

	// 영어는 숨겨도 기록은 센다
	c.put("/v1/focus/subjects/"+focusEng, subjectBody("영어", "#0088FF", 2, true, "2026-11-11T12:00:00+09:00")).is(200)

	r := c.get("/v1/focus/days?from=2026-11-08&to=2026-11-11").is(200).size("days", 4)
	r.eq("days.0.date", "2026-11-08").eq("days.0.focusSec", 1800).eq("days.0.staySec", 0).
		size("days.0.subjects", 1).eq("days.0.subjects.0.subjectId", focusEng).eq("days.0.subjects.0.sec", 1800)
	r.eq("days.1.date", "2026-11-09").eq("days.1.focusSec", 3600).eq("days.1.staySec", 0).
		size("days.1.subjects", 1).eq("days.1.subjects.0.subjectId", focusMath).eq("days.1.subjects.0.sec", 3600)
	r.eq("days.2.date", "2026-11-10").eq("days.2.focusSec", 5400).eq("days.2.staySec", 5*3600).
		size("days.2.subjects", 2).eq("days.2.subjects.0.subjectId", focusMath).eq("days.2.subjects.0.sec", 3600).
		eq("days.2.subjects.1.subjectId", focusEng).eq("days.2.subjects.1.sec", 1800)
	r.eq("days.3.date", "2026-11-11").eq("days.3.focusSec", 0).eq("days.3.staySec", 3600).size("days.3.subjects", 0)
	if !strings.Contains(r.Raw, `"subjects":[]`) {
		t.Errorf("빈 날의 subjects는 null이 아니라 []여야 함: %s", r.Raw)
	}

	// 하루만: 04:00~06:00 세션은 11-10 몫 1시간만
	c.get("/v1/focus/days?from=2026-11-10&to=2026-11-10").is(200).size("days", 1).eq("days.0.focusSec", 5400)
	// 기록 없는 기간도 날마다 0
	c.get("/v1/focus/days?from=2026-10-01&to=2026-10-03").is(200).size("days", 3).eq("days.2.date", "2026-10-03").eq("days.2.focusSec", 0)

	// /all: 지운 것·숨긴 것 포함, 시작 시각 순
	a := c.get("/v1/focus/all").is(200).size("subjects", 2).size("sessions", 5).size("stays", 3)
	a.eq("subjects.0.id", focusMath).eq("subjects.1.archived", true)
	a.eq("sessions.0.id", "s4").eq("sessions.1.id", "s5").eq("sessions.2.id", "s1").eq("sessions.4.id", "s3").eq("sessions.4.deleted", true)
	a.eq("sessions.2.startedAt", "2026-11-10T04:00:00+09:00").eq("sessions.2.subjectId", focusMath)
	a.eq("stays.0.id", "st1").eq("stays.1.id", "st3").eq("stays.1.deleted", true).eq("stays.2.place", "독서실")
}

func TestFocusDaysValidation(t *testing.T) {
	c := newHealthClient(t)
	for _, q := range []string{"", "?from=2026-11-01", "?to=2026-11-01", "?from=2026-11-1&to=2026-11-02", "?from=2026-11-01&to=nope",
		"?from=2026-11-02&to=2026-11-01", "?from=2026-11-01&to=2027-01-02"} {
		c.get("/v1/focus/days"+q).code(400, "INVALID")
	}
	// 62일까지 된다
	c.get("/v1/focus/days?from=2026-11-01&to=2027-01-01").is(200).size("days", 62).eq("days.61.date", "2027-01-01")
}

// 00013을 내렸다 다시 올릴 수 있다.
func TestFocusMigrationDownUp(t *testing.T) {
	c := newHealthClient(t)
	ctx := context.Background()
	url := c.pool.Config().ConnString() // 테스트 스키마(search_path)가 들어 있다
	// 00013 아래(12)까지 내린다. 뒤에 마이그레이션이 더 생겨도 같은 것을 본다
	if err := db.Migrate(ctx, url, "down-to", "12"); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"focus_subjects", "focus_sessions", "focus_stays"} {
		var exists bool
		_ = c.pool.QueryRow(ctx, "SELECT to_regclass($1) IS NOT NULL", table).Scan(&exists)
		if exists {
			t.Fatalf("down 뒤에도 %s 표가 있음", table)
		}
	}
	if err := db.Migrate(ctx, url, "up"); err != nil {
		t.Fatal(err)
	}
	c.focusSubjects()
	// DB도 직접 막는다: 24시간 넘는 세션, 형식이 틀린 색
	if _, err := c.pool.Exec(ctx, `INSERT INTO focus_sessions (id, subject_id, started_at, ended_at, updated_at)
		VALUES ('x', $1, '2026-11-10T00:00:00+09:00', '2026-11-11T00:00:01+09:00', now())`, focusMath); err == nil {
		t.Error("24시간 제약")
	}
	if _, err := c.pool.Exec(ctx, `INSERT INTO focus_subjects (id, name, color, updated_at) VALUES ('y', '과목', 'red', now())`); err == nil {
		t.Error("색 제약")
	}
}
