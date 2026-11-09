package focus

import (
	"errors"
	"reflect"
	"testing"
	"time"
)

// codeOf는 *Error의 코드다(아니면 "").
func codeOf(err error) string {
	var e *Error
	if errors.As(err, &e) {
		return e.Code
	}
	return ""
}

func TestDateRange(t *testing.T) {
	got, err := DateRange("2026-01-30", "2026-02-02")
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"2026-01-30", "2026-01-31", "2026-02-01", "2026-02-02"}; !reflect.DeepEqual(got, want) {
		t.Errorf("DateRange = %v, want %v", got, want)
	}
	if got, err := DateRange("2026-11-10", "2026-11-10"); err != nil || len(got) != 1 {
		t.Errorf("하루: %v %v", got, err)
	}
	// 62일까지 된다(2026-11-01 + 61일 = 2027-01-01)
	if got, err := DateRange("2026-11-01", "2027-01-01"); err != nil || len(got) != MaxDays || got[61] != "2027-01-01" {
		t.Errorf("62일: %d %v", len(got), err)
	}

	for _, c := range []struct{ from, to string }{
		{"", "2026-11-10"},
		{"2026-11-10", ""},
		{"2026-11-1", "2026-11-10"},
		{"2026-11-10", "11/12/2026"},
		{"2026-11-10", "2026-11-09"}, // from이 to보다 늦음
		{"2026-11-01", "2027-01-02"}, // 63일
		{"2000-01-01", "2099-12-31"},
	} {
		_, err := DateRange(c.from, c.to)
		if codeOf(err) != CodeInvalid {
			t.Errorf("DateRange(%q, %q) = %v, want INVALID", c.from, c.to, err)
		}
	}
}

func TestBuildDays(t *testing.T) {
	dates, _ := DateRange("2026-11-08", "2026-11-11")
	sessions := []Span{
		{SubjectID: "math", Start: at(t, "2026-11-10 04:00"), End: at(t, "2026-11-10 06:00")}, // 11-09 1시간, 11-10 1시간
		{SubjectID: "eng", Start: at(t, "2026-11-10 10:00"), End: at(t, "2026-11-10 12:00")},  // 11-10 2시간
		{SubjectID: "math", Start: at(t, "2026-11-10 13:00"), End: at(t, "2026-11-10 13:30")}, // 11-10 30분 더
		{SubjectID: "math", Start: at(t, "2026-11-08 03:00"), End: at(t, "2026-11-08 06:00")}, // 11-07 2시간(범위 밖, 버림) + 11-08 1시간
		{SubjectID: "eng", Start: at(t, "2026-11-12 04:00"), End: at(t, "2026-11-12 06:00")},  // 11-11 1시간 + 11-12(범위 밖)
	}
	stays := []Span{
		{Start: at(t, "2026-11-10 09:00"), End: at(t, "2026-11-10 18:00")},
		{Start: at(t, "2026-11-11 02:00"), End: at(t, "2026-11-11 07:00")}, // 11-10 3시간 + 11-11 2시간
	}
	got := BuildDays(dates, sessions, stays)
	want := []Day{
		{Date: "2026-11-08", FocusSec: 3600, StaySec: 0, Subjects: []SubjectSec{{"math", 3600}}},
		{Date: "2026-11-09", FocusSec: 3600, StaySec: 0, Subjects: []SubjectSec{{"math", 3600}}},
		{Date: "2026-11-10", FocusSec: 7200 + 5400, StaySec: 9*3600 + 3*3600,
			Subjects: []SubjectSec{{"eng", 7200}, {"math", 5400}}},
		{Date: "2026-11-11", FocusSec: 3600, StaySec: 2 * 3600, Subjects: []SubjectSec{{"eng", 3600}}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("BuildDays =\n%+v\nwant\n%+v", got, want)
	}
}

// 기록이 없는 날도 0과 빈 목록(null 아님)으로 나온다.
func TestBuildDaysZeroDays(t *testing.T) {
	dates, _ := DateRange("2026-11-01", "2026-11-03")
	got := BuildDays(dates, nil, nil)
	if len(got) != 3 {
		t.Fatalf("%d일", len(got))
	}
	for i, d := range got {
		if d.Date != dates[i] || d.FocusSec != 0 || d.StaySec != 0 || d.Subjects == nil || len(d.Subjects) != 0 {
			t.Errorf("날 %d = %+v", i, d)
		}
	}
}

// 초는 (날짜, 과목)마다 다 더한 뒤 내림한다: 0.6초 두 번은 1초. 1초가 안 되는 과목은 빠진다.
// 같은 시간이면 subjectId 순이다.
func TestBuildDaysRoundingAndOrder(t *testing.T) {
	dates := []string{"2026-11-10"}
	base := at(t, "2026-11-10 10:00")
	ms := time.Millisecond
	sessions := []Span{
		{SubjectID: "b", Start: base, End: base.Add(600 * ms)},
		{SubjectID: "b", Start: base.Add(time.Second), End: base.Add(time.Second + 600*ms)},
		{SubjectID: "a", Start: base.Add(2 * time.Second), End: base.Add(3*time.Second + 100*ms)},
		{SubjectID: "z", Start: base.Add(4 * time.Second), End: base.Add(4*time.Second + 900*ms)},
	}
	got := BuildDays(dates, sessions, []Span{{Start: base, End: base.Add(1500 * ms)}})
	want := []Day{{Date: "2026-11-10", FocusSec: 2, StaySec: 1, Subjects: []SubjectSec{{"a", 1}, {"b", 1}}}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("BuildDays = %+v, want %+v", got, want)
	}
}
