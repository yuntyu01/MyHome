package invest

import (
	"testing"
	"time"
)

// 거래일 달력 검증 (TQ tests/test_tradingday.py). 달력이 틀리면 판정·실행이 엉뚱한 날 돈다.

func TestFirstTradingDay(t *testing.T) {
	cases := []struct {
		y    int
		m    time.Month
		want time.Time
	}{
		{2026, 8, Date(2026, 8, 3)},   // 8/1 토 → 8/3 월
		{2026, 9, Date(2026, 9, 1)},   // 9/1 화 (노동절은 9/7)
		{2027, 9, Date(2027, 9, 1)},   // 9/1 수 (노동절 9/6)
		{2027, 1, Date(2027, 1, 4)},   // 1/1 금=신정 → 1/4 월
		{2026, 1, Date(2026, 1, 2)},   // 1/1 목=신정 → 1/2 금
		{2028, 5, Date(2028, 5, 1)},   // 5/1 월
		{2026, 11, Date(2026, 11, 2)}, // 11/1 일 → 11/2 월
	}
	for _, c := range cases {
		if got := FirstTradingDay(c.y, c.m); !got.Equal(c.want) {
			t.Errorf("%d-%02d 첫 거래일 %s, 기대 %s", c.y, c.m, FormatDay(got), FormatDay(c.want))
		}
	}
}

func TestLastTradingDay(t *testing.T) {
	cases := []struct {
		y    int
		m    time.Month
		want time.Time
	}{
		{2026, 8, Date(2026, 8, 31)},
		{2027, 1, Date(2027, 1, 29)}, // 1/31 일 → 1/29 금
		{2027, 5, Date(2027, 5, 28)}, // 5/31 월=메모리얼데이 → 5/28 금
		{2026, 5, Date(2026, 5, 29)}, // 5/31 일 → 5/29 금
		{2026, 12, Date(2026, 12, 31)},
		{2027, 12, Date(2027, 12, 31)},
		{2026, 10, Date(2026, 10, 30)}, // 10/31 토 → 10/30 금
	}
	for _, c := range cases {
		if got := LastTradingDay(c.y, c.m); !got.Equal(c.want) {
			t.Errorf("%d-%02d 마지막 거래일 %s, 기대 %s", c.y, c.m, FormatDay(got), FormatDay(c.want))
		}
	}
}

func TestKnownHolidays(t *testing.T) {
	for _, d := range []time.Time{
		Date(2025, 9, 1),   // 노동절이 9/1
		Date(2026, 4, 3),   // 성금요일
		Date(2026, 11, 26), // 추수감사절
		Date(2026, 7, 3),   // 독립기념일 대체 (7/4 토)
		Date(2027, 12, 24), // 크리스마스 대체 (12/25 토)
		Date(2026, 6, 19),  // 준틴스
	} {
		if IsTradingDay(d) {
			t.Errorf("%s는 휴장일", FormatDay(d))
		}
	}
	if !IsTradingDay(Date(2021, 6, 18)) {
		t.Error("준틴스는 2022년부터")
	}
}

// 실데이터의 거래일과 달력이 맞는지 — 데이터가 있는 구간에서 달력이 근사로서 쓸 만한지.
func TestCalendarAgreesWithRealData(t *testing.T) {
	in := loadPrices(t)
	have := map[time.Time]bool{}
	for _, d := range in.Days {
		have[d] = true
	}
	// 임시 휴장(9·11, 샌디, 장례 등)은 달력에 없다. 2010년 이후 정기 휴장만 대조한다
	known := map[time.Time]bool{
		Date(2012, 10, 29): true, Date(2012, 10, 30): true, // 샌디
		Date(2018, 12, 5): true, // 부시 장례
		Date(2025, 1, 9):  true, // 카터 장례
	}
	for d := Date(2010, 1, 1); !d.After(Date(2026, 7, 31)); d = d.AddDate(0, 0, 1) {
		if known[d] {
			continue
		}
		if IsTradingDay(d) != have[d] {
			t.Errorf("%s 달력 %v · 데이터 %v", FormatDay(d), IsTradingDay(d), have[d])
		}
	}
}

func TestNextAndPrevious(t *testing.T) {
	if got := NextLastTradingDay(Date(2026, 12, 15)); !got.Equal(Date(2027, 1, 29)) {
		t.Fatalf("해 넘김 %s", FormatDay(got))
	}
	if got := PreviousTradingDay(Date(2026, 11, 2)); !got.Equal(Date(2026, 10, 30)) {
		t.Fatalf("주말 건너뛰기 %s", FormatDay(got))
	}
	if got := PreviousTradingDay(Date(2026, 11, 27)); !got.Equal(Date(2026, 11, 25)) {
		t.Fatalf("추수감사절 건너뛰기 %s", FormatDay(got))
	}
}

// 장중에는 그날이 완결 거래일이 아니다 (2026-08-13 사고).
func TestLastCompleteSession(t *testing.T) {
	intraday := time.Date(2026, 8, 13, 13, 37, 0, 0, NewYork)
	if got := LastCompleteSession(intraday); !got.Equal(Date(2026, 8, 12)) {
		t.Fatalf("장중 %s", FormatDay(got))
	}
	after := time.Date(2026, 8, 13, 16, 0, 0, 0, NewYork)
	if got := LastCompleteSession(after); !got.Equal(Date(2026, 8, 13)) {
		t.Fatalf("마감 후 %s", FormatDay(got))
	}
	// UTC로 넘겨도 뉴욕 기준으로 판단한다 (서머타임)
	utc := time.Date(2026, 8, 13, 19, 59, 0, 0, time.UTC) // 15:59 EDT
	if got := LastCompleteSession(utc); !got.Equal(Date(2026, 8, 12)) {
		t.Fatalf("UTC 입력 %s", FormatDay(got))
	}
}

func TestTodayET(t *testing.T) {
	// UTC 자정을 넘겨도 뉴욕은 아직 전날이다 (2026-08-31 판정 유실 사고)
	now := time.Date(2026, 9, 1, 1, 35, 0, 0, time.UTC)
	if got := TodayET(now); !got.Equal(Date(2026, 8, 31)) {
		t.Fatalf("%s", FormatDay(got))
	}
}
