package focus

import (
	"testing"
	"time"

	"github.com/yuntyu01/MyHome/server/internal/period"
)

// at은 "2026-11-10 04:00" 같은 KST 시각이다.
func at(t *testing.T, s string) time.Time {
	t.Helper()
	v, err := time.ParseInLocation("2006-01-02 15:04", s, period.KST)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

type part struct{ date, start, end string }

func checkParts(t *testing.T, got []DayPart, want []part) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("조각 %d개, want %d: %+v", len(got), len(want), got)
	}
	for i, w := range want {
		g := got[i]
		if g.Date != w.date || !g.Start.Equal(at(t, w.start)) || !g.End.Equal(at(t, w.end)) {
			t.Errorf("조각 %d = {%s %s %s}, want %+v", i, g.Date, g.Start.Format("2006-01-02 15:04"), g.End.Format("2006-01-02 15:04"), w)
		}
		if g.Start.Location() != period.KST || g.End.Location() != period.KST {
			t.Errorf("조각 %d 시간대 %v/%v, want KST", i, g.Start.Location(), g.End.Location())
		}
	}
}

func TestSplitByDay(t *testing.T) {
	cases := []struct {
		name       string
		start, end string
		want       []part
	}{
		{"하루 안", "2026-11-10 10:00", "2026-11-10 12:00",
			[]part{{"2026-11-10", "2026-11-10 10:00", "2026-11-10 12:00"}}},
		{"05:00을 넘으면 둘로 나뉜다", "2026-11-10 04:00", "2026-11-10 06:00",
			[]part{{"2026-11-09", "2026-11-10 04:00", "2026-11-10 05:00"}, {"2026-11-10", "2026-11-10 05:00", "2026-11-10 06:00"}}},
		{"자정만 넘으면 전날 하나", "2026-11-09 23:00", "2026-11-10 02:00",
			[]part{{"2026-11-09", "2026-11-09 23:00", "2026-11-10 02:00"}}},
		{"자정 뒤 05:00 전은 전날", "2026-11-10 00:30", "2026-11-10 04:59",
			[]part{{"2026-11-09", "2026-11-10 00:30", "2026-11-10 04:59"}}},
		{"05:00에 끝나면 앞 날짜만(길이 0 조각 없음)", "2026-11-10 04:00", "2026-11-10 05:00",
			[]part{{"2026-11-09", "2026-11-10 04:00", "2026-11-10 05:00"}}},
		{"05:00에 시작하면 그날", "2026-11-10 05:00", "2026-11-10 06:00",
			[]part{{"2026-11-10", "2026-11-10 05:00", "2026-11-10 06:00"}}},
		{"딱 하루(05:00~05:00)", "2026-11-10 05:00", "2026-11-11 05:00",
			[]part{{"2026-11-10", "2026-11-10 05:00", "2026-11-11 05:00"}}},
		{"여러 날", "2026-11-09 03:00", "2026-11-11 07:00", []part{
			{"2026-11-08", "2026-11-09 03:00", "2026-11-09 05:00"},
			{"2026-11-09", "2026-11-09 05:00", "2026-11-10 05:00"},
			{"2026-11-10", "2026-11-10 05:00", "2026-11-11 05:00"},
			{"2026-11-11", "2026-11-11 05:00", "2026-11-11 07:00"}}},
		{"해 넘김", "2026-12-31 23:00", "2027-01-01 06:00",
			[]part{{"2026-12-31", "2026-12-31 23:00", "2027-01-01 05:00"}, {"2027-01-01", "2027-01-01 05:00", "2027-01-01 06:00"}}},
		{"월 넘김(새벽 공부는 지난달)", "2026-12-01 01:00", "2026-12-01 02:00",
			[]part{{"2026-11-30", "2026-12-01 01:00", "2026-12-01 02:00"}}},
		{"윤년 2월 29일", "2028-02-29 04:30", "2028-02-29 05:30",
			[]part{{"2028-02-28", "2028-02-29 04:30", "2028-02-29 05:00"}, {"2028-02-29", "2028-02-29 05:00", "2028-02-29 05:30"}}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) { checkParts(t, SplitByDay(at(t, c.start), at(t, c.end)), c.want) })
	}
}

func TestSplitByDayEmpty(t *testing.T) {
	s := at(t, "2026-11-10 10:00")
	if got := SplitByDay(s, s); got != nil {
		t.Errorf("길이 0: %+v", got)
	}
	if got := SplitByDay(s, s.Add(-time.Minute)); got != nil {
		t.Errorf("거꾸로: %+v", got)
	}
}

// UTC로 들어와도 KST 05:00에서 나눈다(19:30Z = 04:30 KST).
func TestSplitByDayUTCInput(t *testing.T) {
	s := time.Date(2026, 11, 9, 19, 30, 0, 0, time.UTC)
	got := SplitByDay(s, s.Add(time.Hour))
	checkParts(t, got, []part{{"2026-11-09", "2026-11-10 04:30", "2026-11-10 05:00"}, {"2026-11-10", "2026-11-10 05:00", "2026-11-10 05:30"}})
	if got[0].Duration() != 30*time.Minute || got[1].Duration() != 30*time.Minute {
		t.Errorf("길이 %v %v", got[0].Duration(), got[1].Duration())
	}
}

// 1초 아래 단위도 잃지 않는다: 조각 길이의 합은 원래 길이다.
func TestSplitByDayKeepsSubSecond(t *testing.T) {
	s := at(t, "2026-11-09 03:00").Add(123 * time.Millisecond)
	e := at(t, "2026-11-11 07:00").Add(456 * time.Millisecond)
	var sum time.Duration
	parts := SplitByDay(s, e)
	for i, p := range parts {
		sum += p.Duration()
		if i > 0 && !p.Start.Equal(parts[i-1].End) {
			t.Errorf("조각 %d가 앞 조각과 이어지지 않음", i)
		}
	}
	if sum != e.Sub(s) {
		t.Errorf("합 %v, want %v", sum, e.Sub(s))
	}
}

func TestStudyDate(t *testing.T) {
	cases := map[string]string{
		"2026-11-10 00:00": "2026-11-09",
		"2026-11-10 04:59": "2026-11-09",
		"2026-11-10 05:00": "2026-11-10",
		"2026-11-10 23:59": "2026-11-10",
		"2027-01-01 03:00": "2026-12-31",
	}
	for in, want := range cases {
		if got := StudyDate(at(t, in)); got != want {
			t.Errorf("StudyDate(%s) = %s, want %s", in, got, want)
		}
	}
	// 04:59:59.999 KST(UTC로 들어옴)는 전날
	if got := StudyDate(time.Date(2026, 11, 9, 19, 59, 59, 999e6, time.UTC)); got != "2026-11-09" {
		t.Errorf("UTC 입력 %s", got)
	}
}

func TestDayBounds(t *testing.T) {
	s, e, err := DayBounds("2026-11-10")
	if err != nil {
		t.Fatal(err)
	}
	if !s.Equal(at(t, "2026-11-10 05:00")) || !e.Equal(at(t, "2026-11-11 05:00")) {
		t.Errorf("DayBounds = %v ~ %v", s, e)
	}
	for _, bad := range []string{"", "2026-11", "2026/11/10", "2026-13-01", "2026-02-30"} {
		if _, _, err := DayBounds(bad); err == nil {
			t.Errorf("DayBounds(%q) 오류여야 함", bad)
		}
	}
}
