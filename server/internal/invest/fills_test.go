package invest

import (
	"math"
	"testing"
)

// 체결을 다시 쌓으면 계좌 산술과 비트 단위로 같아야 한다 — 매수·전량 매도·다시 매수·앱 밖 매수를 섞는다.
func TestReplayMatchesTheAccountArithmeticBitForBit(t *testing.T) {
	a := Account{Symbol: "TQQQ", Shares: 37, CostKRW: 3_412_345.125, CashUSD: 431.875,
		RealizedGainKRW: map[int]float64{2025: -12_345.5}, TaxPaidKRW: map[int]float64{}}
	fills := OpeningFills(a, Date(2026, 7, 31), "2026-08", FillFromOpening)
	day := Date(2026, 8, 3)
	var tr Traded
	var err error

	a, _ = Contribute(a, 612_345.67, 1388.4)
	if a, tr, err = BuyExactT(a, "TQQQ", 6, 65.4321, 1388.4, FeeRate); err != nil {
		t.Fatal(err)
	}
	fills = append(fills, tr.Fill(day, "2026-08", FillFromTrade))
	if a, tr, err = SellAllT(a, 44.8244, 1391.5, 2026, FeeRate); err != nil {
		t.Fatal(err)
	}
	fills = append(fills, tr.Fill(Date(2026, 10, 2), "2026-10", FillFromTrade))
	var ts []Traded
	if a, ts, err = MonthlyStepT(a, "QQQM", 600_000, 1391.5, map[string]float64{"QQQM": 250.9904}, 2026, FeeRate); err != nil {
		t.Fatal(err)
	}
	fills = append(fills, FillsOf(ts, Date(2026, 10, 5), "2026-10", FillFromTrade)...)
	// 앱 밖 매수: 지불액이 취득원가에 그대로
	a.Shares += 2
	a.CostKRW += 700_000.25
	fills = append(fills, Fill{Day: Date(2026, 10, 15), YM: "2026-10", Symbol: "QQQM", Side: FillBuy, Shares: 2, KRW: 700_000.25, Source: FillFromAdjust})

	b, err := Replay(fills)
	if err != nil {
		t.Fatal(err)
	}
	if d := b.Diff(a); len(d) > 0 {
		t.Fatalf("다시 쌓은 값이 다르다: %v", d)
	}
	if b.Shares != a.Shares || math.Float64bits(b.CostKRW) != math.Float64bits(a.CostKRW) || len(b.RealizedGainKRW) != 2 {
		t.Fatalf("%+v vs %+v", b, a)
	}
}

// 여는 잔고는 그 앞 줄을 지운다. 매도는 그때 취득원가로 손익을 내고 연도는 날짜의 연도다.
func TestReplayOpeningResetsAndSellUsesTheDayYear(t *testing.T) {
	fills := []Fill{
		{Side: FillBuy, Symbol: "QQQM", Shares: 3, KRW: 1_000_000},
		{Side: FillOpening, Symbol: "TQQQ", Shares: 10, KRW: 900_000},
		{Side: FillRealized, Day: Date(2025, 1, 1), KRW: -5},
		{Side: FillSell, Symbol: "TQQQ", Shares: 10, KRW: 1_000_000, Day: Date(2027, 1, 4)},
	}
	b, err := Replay(fills)
	if err != nil {
		t.Fatal(err)
	}
	if b.Symbol != "" || b.Shares != 0 || b.CostKRW != 0 || b.RealizedGainKRW[2025] != -5 || b.RealizedGainKRW[2027] != 100_000 {
		t.Fatalf("%+v", b)
	}
	if _, err := Replay([]Fill{{Side: FillBuy, Symbol: "TQQQ", Shares: 1, KRW: 1}, {Side: FillBuy, Symbol: "QQQM", Shares: 1, KRW: 1}}); err == nil {
		t.Fatal("전량 교체 규칙 위반을 쌓았다")
	}
}

// 0과 -0, 없는 연도와 0원 연도는 다르다 — 대조는 비트와 키까지 본다.
func TestBooksDiffIsBitExact(t *testing.T) {
	x := 0.1 // 상수끼리 더하면 컴파일러가 정확히 0.3으로 접는다 — 변수로 float64 덧셈을 하게 한다
	a := Account{Symbol: "TQQQ", Shares: 1, CostKRW: x + 0.2, RealizedGainKRW: map[int]float64{2026: 0}}
	b := Books{Symbol: "TQQQ", Shares: 1, CostKRW: 0.3, RealizedGainKRW: map[int]float64{}}
	if d := b.Diff(a); len(d) != 2 {
		t.Fatalf("차이 %v", d)
	}
}
