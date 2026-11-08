package health

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/yuntyu01/MyHome/server/internal/period"
)

const dateLayout = "2006-01-02"

// Date는 사용자 달력 날짜 "YYYY-MM-DD"(KST)다. JSON에서는 문자열이다.
type Date string

// ParseDate는 "YYYY-MM-DD"를 읽는다.
func ParseDate(s string) (Date, error) {
	t, err := time.Parse(dateLayout, s)
	if err != nil {
		return "", fmt.Errorf("날짜 형식이 아니에요: %q", s)
	}
	return Date(t.Format(dateLayout)), nil
}

// DateOf는 DB에서 읽은 date 값(자정 UTC)을 Date로 바꾼다.
func DateOf(t time.Time) Date { return Date(t.Format(dateLayout)) }

// Today는 now의 KST 날짜다.
func Today(now time.Time) Date { return Date(now.In(period.KST).Format(dateLayout)) }

// IsZero는 비어 있는지다.
func (d Date) IsZero() bool { return d == "" }

// Time은 DB 인자용 값(그날 자정 UTC)이다.
func (d Date) Time() time.Time {
	t, _ := time.Parse(dateLayout, string(d))
	return t
}

// AddDays는 n일 뒤(음수면 앞) 날짜다.
func (d Date) AddDays(n int) Date { return Date(d.Time().AddDate(0, 0, n).Format(dateLayout)) }

// Weekday0은 0=월 ... 6=일이다.
func (d Date) Weekday0() int { return (int(d.Time().Weekday()) + 6) % 7 }

// Monday는 d가 든 주의 월요일이다(주는 월요일에 시작한다).
func (d Date) Monday() Date { return d.AddDays(-d.Weekday0()) }

// UnmarshalJSON은 "YYYY-MM-DD"만 받는다. null은 빈 값(필수 검사에서 걸린다).
func (d *Date) UnmarshalJSON(b []byte) error {
	var s *string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	if s == nil {
		*d = ""
		return nil
	}
	v, err := ParseDate(*s)
	if err != nil {
		return err
	}
	*d = v
	return nil
}

// ParseMonth는 "2026-09"의 첫날과 마지막 날이다.
func ParseMonth(s string) (first, last Date, err error) {
	t, err := time.Parse("2006-01", s)
	if err != nil {
		return "", "", fmt.Errorf("월 형식이 아니에요: %q", s)
	}
	return Date(t.Format(dateLayout)), Date(t.AddDate(0, 1, -1).Format(dateLayout)), nil
}
