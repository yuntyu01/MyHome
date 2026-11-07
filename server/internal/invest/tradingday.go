package invest

import (
	"sync"
	"time"
	_ "time/tzdata" // 시간대 DB가 없는 환경(scratch 이미지)에서도 America/New_York을 쓴다
)

// 미국 증시 거래일 달력 (TQ strategy/tradingday.py).
//
// 실행일(월 첫 거래일) 아침에는 그날 종가가 아직 없다. 과거 데이터만으로 "오늘이
// 첫 거래일인가"를 물으면 항상 아니라고 나온다. 그래서 미래 방향으로도 답할 수 있는
// 달력이 필요하다.
//
// 우선순위는 실데이터 > 달력이다. 데이터가 있는 달은 데이터가 정답이고, 달력은 아직
// 데이터가 없는 달에만 쓴다. 임시 휴장(9·11 등)과 조기폐장은 넣지 않는다 — 그런 날은
// 데이터가 이미 쌓인 뒤라 실데이터 경로가 처리한다.

// 날짜는 전부 UTC 자정의 time.Time으로 다룬다(달력 날짜). 시각이 섞이면 비교가 틀어진다.

// Date는 달력 날짜를 만든다.
func Date(y int, m time.Month, d int) time.Time {
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

// DayOf는 t의 그 시간대 기준 달력 날짜다.
func DayOf(t time.Time) time.Time {
	return Date(t.Year(), t.Month(), t.Day())
}

// ParseDay는 YYYY-MM-DD를 달력 날짜로 읽는다.
func ParseDay(s string) (time.Time, error) {
	return time.Parse("2006-01-02", s)
}

// FormatDay는 YYYY-MM-DD.
func FormatDay(t time.Time) string { return t.Format("2006-01-02") }

// YM은 YYYY-MM.
func YM(t time.Time) string { return t.Format("2006-01") }

// MarketCloseET는 정규장 마감 시각이다. 조기폐장(13:00)은 하루 늦게 받는 쪽으로 안전하다.
const marketCloseHourET = 16

// NewYork은 뉴욕 시간대다. 판정일·실행일·거래일은 전부 미국 장 기준이다.
var NewYork = mustLoad("America/New_York")

func mustLoad(name string) *time.Location {
	loc, err := time.LoadLocation(name)
	if err != nil {
		panic(err)
	}
	return loc
}

// pyWeekday는 월=0 … 일=6 (Python date.weekday와 같다).
func pyWeekday(t time.Time) int { return (int(t.Weekday()) + 6) % 7 }

// nthWeekday는 그 달의 n번째 weekday(월=0). n이 음수면 뒤에서부터.
func nthWeekday(year int, month time.Month, weekday, n int) time.Time {
	if n > 0 {
		d := Date(year, month, 1)
		d = d.AddDate(0, 0, ((weekday-pyWeekday(d))%7+7)%7)
		return d.AddDate(0, 0, 7*(n-1))
	}
	d := Date(year, month, 1).AddDate(0, 1, -1)
	d = d.AddDate(0, 0, -(((pyWeekday(d)-weekday)%7 + 7) % 7))
	return d.AddDate(0, 0, 7*(n+1))
}

func easter(year int) time.Time {
	a, b, c := year%19, year/100, year%100
	d, e := b/4, b%4
	g := (b - (b+8)/25 + 1) / 3
	h := (19*a + b - d - g + 15) % 30
	i, k := c/4, c%4
	l := (32 + 2*e + 2*i - h - k) % 7
	m := (a + 11*h + 22*l) / 451
	month := (h + l - 7*m + 114) / 31
	day := (h+l-7*m+114)%31 + 1
	return Date(year, time.Month(month), day)
}

// observed는 주말에 걸린 고정 휴일의 대체 휴장일이다.
func observed(d time.Time) time.Time {
	switch d.Weekday() {
	case time.Saturday:
		return d.AddDate(0, 0, -1)
	case time.Sunday:
		return d.AddDate(0, 0, 1)
	}
	return d
}

var (
	holidayMu    sync.Mutex
	holidayCache = map[int]map[time.Time]bool{}
)

// MarketHolidays는 NYSE·Nasdaq 정기 휴장일이다. 돌려준 맵은 고치지 말 것(캐시를 공유한다).
func MarketHolidays(year int) map[time.Time]bool {
	holidayMu.Lock()
	defer holidayMu.Unlock()
	if h, ok := holidayCache[year]; ok {
		return h
	}
	days := map[time.Time]bool{
		observed(Date(year, 1, 1)):     true, // 신정
		nthWeekday(year, 1, 0, 3):      true, // MLK — 1월 3번째 월
		nthWeekday(year, 2, 0, 3):      true, // 대통령의날 — 2월 3번째 월
		easter(year).AddDate(0, 0, -2): true, // 성금요일
		nthWeekday(year, 5, 0, -1):     true, // 메모리얼데이 — 5월 마지막 월
		observed(Date(year, 7, 4)):     true, // 독립기념일
		nthWeekday(year, 9, 0, 1):      true, // 노동절 — 9월 첫 월
		nthWeekday(year, 11, 3, 4):     true, // 추수감사절 — 11월 4번째 목
		observed(Date(year, 12, 25)):   true, // 크리스마스
	}
	if year >= 2022 {
		days[observed(Date(year, 6, 19))] = true // 준틴스 (2022~)
	}
	holidayCache[year] = days
	return days
}

// IsTradingDay는 정기 휴장일이 아닌 평일인가.
func IsTradingDay(d time.Time) bool {
	wd := d.Weekday()
	return wd != time.Saturday && wd != time.Sunday && !MarketHolidays(d.Year())[d]
}

// FirstTradingDay는 그 달 첫 거래일.
func FirstTradingDay(year int, month time.Month) time.Time {
	d := Date(year, month, 1)
	for !IsTradingDay(d) {
		d = d.AddDate(0, 0, 1)
	}
	return d
}

// LastTradingDay는 그 달 마지막 거래일.
func LastTradingDay(year int, month time.Month) time.Time {
	d := Date(year, month, 1).AddDate(0, 1, -1)
	for !IsTradingDay(d) {
		d = d.AddDate(0, 0, -1)
	}
	return d
}

// PreviousTradingDay는 d 직전 거래일(d 자신은 제외).
func PreviousTradingDay(d time.Time) time.Time {
	d = d.AddDate(0, 0, -1)
	for !IsTradingDay(d) {
		d = d.AddDate(0, 0, -1)
	}
	return d
}

// NextLastTradingDay는 after의 다음 달 마지막 거래일. D-N 카운트다운용.
func NextLastTradingDay(after time.Time) time.Time {
	n := Date(after.Year(), after.Month(), 1).AddDate(0, 1, 0)
	return LastTradingDay(n.Year(), n.Month())
}

// LastCompleteSession은 nowET 시점에 종가가 확정된 마지막 거래일이다.
//
// 장중에 받은 값은 종가가 아니다. 2026-08-13 마감 2시간 23분 전에 refresh가 돌아 그 순간의
// 장중 시세가 마스터에 굳었고, 이후 모든 refresh가 앵커 대조에서 멈춰 주간 갱신이 2주간 죽었다.
// 시각은 뉴욕 기준이어야 한다 — 서버 시각은 서머타임마다 마감 시각이 옮겨간다.
func LastCompleteSession(now time.Time) time.Time {
	et := now.In(NewYork)
	d := DayOf(et)
	if IsTradingDay(d) && et.Hour() >= marketCloseHourET {
		return d
	}
	return PreviousTradingDay(d)
}

// TodayET는 뉴욕 날짜의 오늘이다.
//
// 2026-08-31 판정이 스케줄 지연으로 UTC 자정을 넘겨 기동해 날짜가 9/1이 됐고 8월 판정이
// 통째로 유실됐다. UTC 자정은 뉴욕 저녁 8시라 몇 시간 밀려도 같은 날로 남는다.
func TodayET(now time.Time) time.Time { return DayOf(now.In(NewYork)) }
