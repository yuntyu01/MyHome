package period

import (
	"testing"
	"time"
)

func TestContainingDefaultStartDay(t *testing.T) {
	// 10/31 23:59 KST는 10월 기간, 11/01 00:00 KST는 11월 기간
	p := Containing(time.Date(2026, 10, 31, 23, 59, 0, 0, KST), 1)
	if p.Label != "2026-10" {
		t.Errorf("label = %s", p.Label)
	}
	// UTC로 10/31 15:00 = KST 11/01 00:00
	p = Containing(time.Date(2026, 10, 31, 15, 0, 0, 0, time.UTC), 1)
	if p.Label != "2026-11" || !p.Start.Equal(time.Date(2026, 11, 1, 0, 0, 0, 0, KST)) {
		t.Errorf("KST 경계 오류: %+v", p)
	}
}

func TestPaydayStart(t *testing.T) {
	// 월급날 25일 시작: 11/10은 10/25~11/24 기간
	p := Containing(time.Date(2026, 11, 10, 12, 0, 0, 0, KST), 25)
	if p.Label != "2026-10" || p.End.Day() != 25 || p.End.Month() != 11 {
		t.Errorf("기간 오류: %+v", p)
	}
	p = Containing(time.Date(2026, 1, 5, 12, 0, 0, 0, KST), 25)
	if p.Label != "2025-12" {
		t.Errorf("연도 넘김 오류: %s", p.Label)
	}
	if ClampStartDay(31) != 28 || ClampStartDay(0) != 1 {
		t.Error("시작일 범위 오류")
	}
}

func TestParseAndPrev(t *testing.T) {
	p, err := Parse("2026-03", 1, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if p.Prev(1).Label != "2026-02" || p.LastDay().Day() != 31 {
		t.Errorf("prev/lastday 오류")
	}
	if p.StartDate() != time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC) {
		t.Errorf("StartDate = %v", p.StartDate())
	}
	if _, err := Parse("2026/03", 1, time.Now()); err == nil {
		t.Error("형식 오류를 잡아야 함")
	}
}
