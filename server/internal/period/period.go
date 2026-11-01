// Package period는 예산 기간(월 경계) 계산을 담는다. 경계는 항상 KST 기준이다.
package period

import (
	"fmt"
	"time"
)

// KST는 한국 표준시다. 월 경계와 표시는 이 시간대로 한다.
var KST = mustLoc("Asia/Seoul")

func mustLoc(name string) *time.Location {
	loc, err := time.LoadLocation(name)
	if err != nil {
		panic(err)
	}
	return loc
}

// Period는 예산 기간 [Start, End)이다. Label은 시작일이 속한 달("2026-11")이다.
type Period struct {
	Start time.Time // KST 자정
	End   time.Time // 다음 기간 시작(KST 자정, 미포함)
	Label string
}

// StartDate는 budgets.period_start에 넣을 날짜(UTC 자정 표현)다. DB date 컬럼과 비교할 때 쓴다.
func (p Period) StartDate() time.Time {
	return time.Date(p.Start.Year(), p.Start.Month(), p.Start.Day(), 0, 0, 0, 0, time.UTC)
}

// LastDay는 기간의 마지막 날(KST)이다.
func (p Period) LastDay() time.Time { return p.End.AddDate(0, 0, -1) }

// Prev는 이전 기간이다.
func (p Period) Prev(startDay int) Period {
	s := p.Start.AddDate(0, -1, 0)
	return ForMonth(s.Year(), s.Month(), startDay)
}

// ClampStartDay는 시작일을 1~28로 맞춘다(2월에도 항상 존재하도록).
func ClampStartDay(d int) int {
	if d < 1 {
		return 1
	}
	if d > 28 {
		return 28
	}
	return d
}

// ForMonth는 year년 month월 startDay일에 시작하는 기간이다.
func ForMonth(year int, month time.Month, startDay int) Period {
	startDay = ClampStartDay(startDay)
	start := time.Date(year, month, startDay, 0, 0, 0, 0, KST)
	end := start.AddDate(0, 1, 0)
	return Period{Start: start, End: end, Label: start.Format("2006-01")}
}

// Containing은 시각 t가 속한 기간이다.
func Containing(t time.Time, startDay int) Period {
	startDay = ClampStartDay(startDay)
	k := t.In(KST)
	y, m := k.Year(), k.Month()
	if k.Day() < startDay {
		prev := time.Date(y, m, 1, 0, 0, 0, 0, KST).AddDate(0, -1, 0)
		y, m = prev.Year(), prev.Month()
	}
	return ForMonth(y, m, startDay)
}

// Parse는 "2026-11" 형식을 기간으로 바꾼다. 빈 문자열이면 now가 속한 기간이다.
func Parse(label string, startDay int, now time.Time) (Period, error) {
	if label == "" {
		return Containing(now, startDay), nil
	}
	t, err := time.ParseInLocation("2006-01", label, KST)
	if err != nil {
		return Period{}, fmt.Errorf("month는 YYYY-MM 형식이어야 합니다: %q", label)
	}
	return ForMonth(t.Year(), t.Month(), startDay), nil
}
