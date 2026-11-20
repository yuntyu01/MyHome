package invest

import (
	"math"
	"testing"
	"time"
)

// 매일 기록이 달 마지막 거래일에 보여 주는 신호는 그달 판정과 같아야 한다. 다르면 화면이 판정과 다른 말을 한다.
func TestDailySignalOnTheLastTradingDayIsTheMonthlyJudgment(t *testing.T) {
	in := loadPrices(t)
	from := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	daily, err := DailySignals(in, from)
	if err != nil {
		t.Fatal(err)
	}
	monthly, err := MonthlySignals(in, "2024-01")
	if err != nil {
		t.Fatal(err)
	}
	byDay := map[time.Time]DailySignal{}
	for _, d := range daily {
		byDay[d.Day] = d
	}
	if len(monthly) < 24 {
		t.Fatalf("판정 %d달뿐", len(monthly))
	}
	for _, m := range monthly {
		d, ok := byDay[m.Day]
		if !ok {
			t.Fatalf("%s 매일 신호가 없다", FormatDay(m.Day))
		}
		if d.Pos != m.Pos || d.C1 != m.C1 || d.C2 != m.C2 || d.SPX != m.SPX || d.SMA185 != m.SMA185 || d.Vol != m.Vol {
			t.Fatalf("%s 매일 %+v ≠ 판정 %+v", FormatDay(m.Day), d, m)
		}
	}
	// 첫 줄은 from 이후다
	if daily[0].Day.Before(from) {
		t.Fatalf("from 앞 날이 섞였다: %s", FormatDay(daily[0].Day))
	}
}

// 잘린 이력에 걸면 워밍업 구간이 비어 조용히 틀리는 대신 멈춘다.
func TestDailySignalsRefuseATruncatedSeries(t *testing.T) {
	in := loadPrices(t)
	n := len(in.Days)
	cut := SignalInputs{Days: in.Days[n-100:], SPX: in.SPX[n-100:], NDX: in.NDX[n-100:]}
	if _, err := DailySignals(cut, cut.Days[50]); err == nil {
		t.Fatal("185일이 안 되는 이력으로 신호를 냈다")
	}
}

func TestValueMatchesTheSummaryFormula(t *testing.T) {
	a := Account{Symbol: "TQQQ", Shares: 10, CashUSD: 2}
	v := Value(a, 1_000_000, 80, 1400, 5000)
	if v.StockKRW != 1_120_000 || v.CashKRW != 2800+5000 || v.GainKRW != 1_120_000+2800-1_000_000 {
		t.Fatalf("%+v", v)
	}
	if v.TotalKRW() != 1_127_800 {
		t.Fatalf("총액 %v", v.TotalKRW())
	}
	if g, ok := v.GainRatio(); !ok || math.Abs(g-0.1228) > 1e-9 {
		t.Fatalf("수익률 %v %v", g, ok)
	}
	if _, ok := Value(Account{}, 0, 0, 1400, 0).GainRatio(); ok {
		t.Fatal("원금 0인데 수익률을 냈다")
	}
}
