package focus

import (
	"sort"
	"time"
)

// MaxDays는 /v1/focus/days 한 번에 볼 수 있는 날 수다(두 달).
const MaxDays = 62

// Day는 공부 날짜 하루의 합계다. 기록이 없는 날도 0으로 나온다.
type Day struct {
	Date     string       `json:"date"`
	FocusSec int64        `json:"focusSec"` // subjects[].sec의 합
	StaySec  int64        `json:"staySec"`
	Subjects []SubjectSec `json:"subjects"` // 많이 한 순(같으면 subjectId 순). 0초인 과목은 없다
}

// SubjectSec은 하루 동안 과목 하나에 쓴 시간이다.
type SubjectSec struct {
	SubjectID string `json:"subjectId"`
	Sec       int64  `json:"sec"`
}

// Span은 집계할 구간 하나다. 세션이면 SubjectID가 과목이고, 머무름이면 비어 있다.
type Span struct {
	SubjectID  string
	Start, End time.Time
}

// DateRange는 from~to(둘 다 포함) 공부 날짜 목록이다. 형식이 틀리거나, from이 to보다 늦거나, MaxDays를 넘으면 400.
func DateRange(from, to string) ([]string, error) {
	f, err := parseDateParam("from", from)
	if err != nil {
		return nil, err
	}
	t, err := parseDateParam("to", to)
	if err != nil {
		return nil, err
	}
	if t.Before(f) {
		return nil, invalid("to", "from보다 앞일 수 없어요")
	}
	n := int(t.Sub(f)/(24*time.Hour)) + 1 // UTC 자정끼리라 늘 24시간 단위다
	if n > MaxDays {
		return nil, invalid("to", "기간은 62일까지예요")
	}
	out := make([]string, n)
	for i := range out {
		out[i] = f.AddDate(0, 0, i).Format(dateLayout)
	}
	return out, nil
}

func parseDateParam(field, s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, invalid(field, "필수예요")
	}
	t, err := time.Parse(dateLayout, s)
	if err != nil {
		return time.Time{}, invalid(field, "YYYY-MM-DD 형식이어야 해요")
	}
	return t, nil
}

// BuildDays는 dates(공부 날짜, 순서대로)마다 세션 시간(과목별)과 머무름 시간을 더한다.
// 구간은 날짜 경계(05:00 KST)에서 나누고, dates 밖으로 나간 조각은 버린다.
// 초는 (날짜, 과목)마다 길이를 다 더한 뒤 내림한다. focusSec은 과목별 초의 합이다(화면 합계가 맞게).
func BuildDays(dates []string, sessions, stays []Span) []Day {
	idx := make(map[string]int, len(dates))
	for i, d := range dates {
		idx[d] = i
	}
	focus := make([]map[string]time.Duration, len(dates))
	stay := make([]time.Duration, len(dates))
	for _, s := range sessions {
		for _, p := range SplitByDay(s.Start, s.End) {
			i, ok := idx[p.Date]
			if !ok {
				continue
			}
			if focus[i] == nil {
				focus[i] = map[string]time.Duration{}
			}
			focus[i][s.SubjectID] += p.Duration()
		}
	}
	for _, s := range stays {
		for _, p := range SplitByDay(s.Start, s.End) {
			if i, ok := idx[p.Date]; ok {
				stay[i] += p.Duration()
			}
		}
	}

	out := make([]Day, len(dates))
	for i, d := range dates {
		day := Day{Date: d, StaySec: int64(stay[i] / time.Second), Subjects: []SubjectSec{}}
		for id, dur := range focus[i] {
			sec := int64(dur / time.Second)
			if sec == 0 {
				continue
			}
			day.Subjects = append(day.Subjects, SubjectSec{SubjectID: id, Sec: sec})
			day.FocusSec += sec
		}
		sort.Slice(day.Subjects, func(a, b int) bool {
			x, y := day.Subjects[a], day.Subjects[b]
			if x.Sec != y.Sec {
				return x.Sec > y.Sec
			}
			return x.SubjectID < y.SubjectID
		})
		out[i] = day
	}
	return out
}
