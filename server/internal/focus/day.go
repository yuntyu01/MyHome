package focus

import (
	"fmt"
	"time"

	"github.com/yuntyu01/MyHome/server/internal/period"
)

// DayStartHour는 공부 날짜가 바뀌는 시각(KST)이다. 하루는 05:00부터 다음 날 05:00까지라서
// 자정을 넘겨 공부해도 전날 기록으로 센다(열품타와 같다). KST는 서머타임이 없어서 하루는 늘 24시간이다.
const DayStartHour = 5

const dateLayout = "2006-01-02"

// DayPart는 날짜 경계로 자른 구간 하나다. [Start, End), 둘 다 KST.
type DayPart struct {
	Date  string // 공부 날짜 "YYYY-MM-DD"
	Start time.Time
	End   time.Time
}

// Duration은 구간 길이다.
func (p DayPart) Duration() time.Duration { return p.End.Sub(p.Start) }

// StudyDate는 시각 t가 속한 공부 날짜다(05:00 KST 전이면 전날).
func StudyDate(t time.Time) string {
	return t.In(period.KST).Add(-DayStartHour * time.Hour).Format(dateLayout)
}

// DayBounds는 공부 날짜 date("YYYY-MM-DD")의 [시작, 끝) 시각이다(그날 05:00 ~ 다음 날 05:00 KST).
func DayBounds(date string) (start, end time.Time, err error) {
	d, err := time.ParseInLocation(dateLayout, date, period.KST)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("날짜 형식이 아니에요: %q", date)
	}
	start = time.Date(d.Year(), d.Month(), d.Day(), DayStartHour, 0, 0, 0, period.KST)
	return start, start.AddDate(0, 0, 1), nil
}

// SplitByDay는 [start, end)를 공부 날짜 경계(05:00 KST)에서 잘라 날짜 순으로 돌려준다.
// 경계에 딱 걸치는 끝(예: 04:00~05:00)은 앞 날짜에만 들어간다(길이 0인 조각은 만들지 않는다).
// end가 start보다 늦지 않으면 nil이다.
func SplitByDay(start, end time.Time) []DayPart {
	s, e := start.In(period.KST), end.In(period.KST)
	var out []DayPart
	for s.Before(e) {
		shifted := s.Add(-DayStartHour * time.Hour)
		y, m, d := shifted.Date()
		next := time.Date(y, m, d+1, DayStartHour, 0, 0, 0, period.KST)
		partEnd := e
		if next.Before(e) {
			partEnd = next
		}
		out = append(out, DayPart{Date: shifted.Format(dateLayout), Start: s, End: partEnd})
		s = partEnd
	}
	return out
}
