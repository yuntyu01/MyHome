package balance

import (
	"testing"
	"time"
)

func at(min int) time.Time { return time.Date(2026, 11, 15, 9, min, 0, 0, time.UTC) }

func row(id int64, min int, amount int64, flow string, bal int64) Anchor {
	return Anchor{ID: id, OccurredAt: at(min), Amount: amount, Flow: flow, Balance: bal, Source: "toss"}
}

// 앞 잔액 ± 이번 금액이 이번 잔액과 다르면 빈 돈. 같은 알림 재게시는 한 번, 금액 모르는 거래(0원)는 거기서 다시 잇는다
func TestChain(t *testing.T) {
	rows := []Anchor{
		row(1, 0, 100_000, "in", 1_100_000),
		row(2, 1, 30_000, "out", 1_070_000), // 맞음
		row(3, 2, 30_000, "out", 1_070_000), // 재게시
		row(4, 3, 20_000, "out", 1_000_000), // 1,050,000이어야 함 → 50,000 나간 돈을 놓침
		row(5, 4, 0, "out", 900_000),        // 외화만(금액 모름): 따지지 않고 다시 잇는다
		row(6, 5, 10_000, "in", 915_000),    // 910,000이어야 함 → 5,000 들어온 돈을 놓침
	}
	gaps := Chain(rows)
	if len(gaps) != 2 {
		t.Fatalf("%+v", gaps)
	}
	if g := gaps[0]; g.ToID != 4 || g.FromID != 2 || g.Amount != -50_000 || g.FromBalance != 1_070_000 || g.ToBalance != 1_000_000 {
		t.Errorf("나간 빈 돈 %+v", g)
	}
	if g := gaps[1]; g.ToID != 6 || g.FromID != 5 || g.Amount != 5_000 {
		t.Errorf("들어온 빈 돈 %+v", g)
	}
	if len(Chain(rows[:3])) != 0 || len(Chain(nil)) != 0 {
		t.Error("맞는 줄에 빈 돈")
	}
}

func TestWon(t *testing.T) {
	for in, want := range map[int64]string{0: "0", 999: "999", 1000: "1,000", 1234567: "1,234,567", -50000: "-50,000"} {
		if got := won(in); got != want {
			t.Errorf("%d: %s", in, got)
		}
	}
}
