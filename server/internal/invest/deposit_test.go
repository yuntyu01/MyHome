package invest

import (
	"testing"
	"time"
)

func TestBuyDayFollowsTheDepositDay(t *testing.T) {
	cases := []struct {
		name          string
		y             int
		m             time.Month
		dd            int
		day, deadline string
	}{
		{"입금일 없음 = 첫 거래일, 기한은 10일", 2026, 12, 0, "2026-12-01", "2026-12-10"},
		{"5일이 토요일이면 월요일", 2026, 12, 5, "2026-12-07", "2026-12-10"},
		{"첫 거래일보다 이르면 첫 거래일 (1월 1일 휴장), 10일이 일요일이면 기한은 8일(금)", 2027, 1, 1, "2027-01-04", "2027-01-08"},
		{"상한 7일로 줄인다(7일 토 → 9일 월)", 2026, 11, 23, "2026-11-09", "2026-11-10"},
		{"10일이 토요일이면 기한은 9일(금)", 2026, 10, 2, "2026-10-02", "2026-10-09"},
	}
	for _, c := range cases {
		if got := FormatDay(BuyDay(c.y, c.m, c.dd)); got != c.day {
			t.Errorf("%s: BuyDay %s, want %s", c.name, got, c.day)
		}
		if got := FormatDay(BuyDeadline(c.y, c.m)); got != c.deadline {
			t.Errorf("%s: BuyDeadline %s, want %s", c.name, got, c.deadline)
		}
	}
}

func TestDepositSeenNeedsHalfThePlan(t *testing.T) {
	if DepositSeen(10_000, 600_000) {
		t.Fatal("소액 입금으로 월급 이체 전에 사면 안 된다")
	}
	if !DepositSeen(300_000, 600_000) || !DepositSeen(600_000, 600_000) {
		t.Fatal("적립액의 절반 이상이면 들어온 것")
	}
	if !DepositSeen(1, 0) || DepositSeen(0, 0) {
		t.Fatal("적립액이 없으면 1원이라도")
	}
}
