package invest

import (
	"math"
	"math/rand"
	"testing"
)

// 계좌 산술 검증 (TQ tests/test_account.py).
//
// 핵심 관문
//  1. 정수주·잔여현금 이월
//  2. 세금 — 연간 통산 · 기본공제 · 손실 이월 없음 · 매도 시 현금 안 깎음
//  3. 교체 → 적립 순서 (반대면 매도 실현손익이 오염된다)

const (
	tFX = 1400.0
	tC  = 1_500_000.0
)

func unit(px float64) float64 { return px * (1 + FeeRate) }
func net(px float64) float64  { return px * (1 - FeeRate - SECFeeRate) }

func approx(t *testing.T, got, want, tol float64, what string) {
	t.Helper()
	if math.Abs(got-want) > tol {
		t.Fatalf("%s: got %.10f want %.10f", what, got, want)
	}
}

func must(t *testing.T) func(Account, error) Account {
	return func(a Account, err error) Account {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		return a
	}
}

func empty() Account {
	return Account{RealizedGainKRW: map[int]float64{}, TaxPaidKRW: map[int]float64{}}
}

func TestContributeAndBuyMax(t *testing.T) {
	a := must(t)(Contribute(empty(), tC, tFX))
	approx(t, a.CashUSD, tC/tFX, 1e-9, "환전")
	a = must(t)(BuyMax(a, "TQQQ", 100, tFX, FeeRate))
	if a.Shares != 10 {
		t.Fatalf("정수주 %d", a.Shares)
	}
	approx(t, a.CashUSD, 1071.428571-10*unit(100), 1e-6, "잔여")
	approx(t, a.CostKRW, 10*unit(100)*tFX, 1e-6, "취득원가")

	// 잔여 현금은 다음 달에 쓰인다
	left := a.CashUSD
	a = must(t)(Contribute(a, tC, tFX))
	approx(t, a.CashUSD, left+tC/tFX, 1e-9, "이월")
	a = must(t)(BuyMax(a, "TQQQ", 100, tFX, FeeRate))
	if a.Shares != 21 {
		t.Fatalf("이월분 덕에 21주여야 한다: %d", a.Shares)
	}
}

func TestBuyRejectsMixingSymbols(t *testing.T) {
	a := must(t)(Contribute(empty(), tC, tFX))
	a = must(t)(BuyMax(a, "TQQQ", 100, tFX, FeeRate))
	if _, err := BuyMax(a, "QQQM", 100, tFX, FeeRate); err == nil {
		t.Fatal("전량 교체 규칙 위반을 막지 않았다")
	}
	if _, err := BuyExact(a, "QQQM", 1, 100, tFX, FeeRate); err == nil {
		t.Fatal("BuyExact도 막아야 한다")
	}
}

func TestSellAllRealizesGainAfterFee(t *testing.T) {
	a := must(t)(Contribute(empty(), tC, tFX))
	a = must(t)(BuyMax(a, "TQQQ", 100, tFX, FeeRate))
	cost := a.CostKRW
	b := must(t)(SellAll(a, 120, tFX, 2026, FeeRate))
	if b.Shares != 0 || b.Symbol != "" {
		t.Fatalf("%+v", b)
	}
	proceeds := 10 * net(120) * tFX
	approx(t, b.RealizedGainKRW[2026], proceeds-cost, 1e-6, "실현손익")
	if len(a.RealizedGainKRW) != 0 {
		t.Fatal("원본 계좌가 바뀌었다 (순수 함수 위반)")
	}
	// 세금은 생겼지만 현금은 안 깎인다
	approx(t, b.CashUSD, a.CashUSD+10*net(120), 1e-9, "현금")
	approx(t, b.NetValueKRW(0, tFX), b.CashUSD*tFX-b.TaxLiabilityKRW(), 1e-6, "세후 평가")
}

func TestTaxRules(t *testing.T) {
	a := Account{RealizedGainKRW: map[int]float64{2026: 10_000_000}, TaxPaidKRW: map[int]float64{}}
	approx(t, a.TaxDueKRW(2026), (10_000_000-AnnualDeductKRW)*TaxRate, 1e-6, "기본공제")
	if (Account{RealizedGainKRW: map[int]float64{2026: 2_000_000}}).TaxDueKRW(2026) != 0 {
		t.Fatal("공제 이하는 비과세")
	}
	l := Account{RealizedGainKRW: map[int]float64{2025: -5_000_000, 2026: 5_000_000}}
	if l.TaxDueKRW(2025) != 0 {
		t.Fatal("손실 연도 세금")
	}
	approx(t, l.TaxDueKRW(2026), (5_000_000-AnnualDeductKRW)*TaxRate, 1e-6, "손실 이월 없음")
}

func TestPaidTaxStopsBeingDeducted(t *testing.T) {
	a := Account{RealizedGainKRW: map[int]float64{2026: 10_000_000, 2027: 4_000_000}, TaxPaidKRW: map[int]float64{}}
	due := a.TaxDueKRW(2026)
	paid := a.Clone()
	paid.TaxPaidKRW[2026] = due
	approx(t, paid.TaxLiabilityKRW(), a.TaxDueKRW(2027), 1e-6, "다른 해는 남는다")
	if paid.TaxUnpaidKRW(2026) != 0 {
		t.Fatal("낸 세금이 미납으로 남았다")
	}
	approx(t, paid.NetValueKRW(80, 1400)-a.NetValueKRW(80, 1400), due, 1e-6, "평가금액 회복")
}

func TestMonthlyStepSellsBeforeContributing(t *testing.T) {
	a := must(t)(MonthlyStep(empty(), "QQQM", tC, tFX, map[string]float64{"QQQM": 300}, 2026, FeeRate))
	b := must(t)(MonthlyStep(a, "TQQQ", tC, tFX, map[string]float64{"QQQM": 310, "TQQQ": 60}, 2026, FeeRate))
	proceeds := float64(a.Shares) * net(310) * tFX
	approx(t, b.RealizedGainKRW[2026], proceeds-a.CostKRW, 1e-6, "매도 손익에 적립금이 섞이지 않는다")
	if b.Symbol != "TQQQ" {
		t.Fatal(b.Symbol)
	}
	if _, err := MonthlyStep(a, "TQQQ", tC, tFX, map[string]float64{"TQQQ": 60}, 2026, FeeRate); err == nil {
		t.Fatal("매도 레그 단가 없이 진행했다")
	}
}

func TestRejectsBadFXAndPrice(t *testing.T) {
	if _, err := Contribute(empty(), tC, 0); err == nil {
		t.Fatal("환율 0")
	}
	if _, err := Contribute(empty(), -1, tFX); err == nil {
		t.Fatal("음수 적립")
	}
	if _, err := BuyMax(must(t)(Contribute(empty(), tC, tFX)), "TQQQ", 0, tFX, FeeRate); err == nil {
		t.Fatal("단가 0")
	}
}

func TestSECFeeOnlyOnSells(t *testing.T) {
	a := must(t)(Contribute(empty(), tC, tFX))
	bought := must(t)(BuyExact(a, "TQQQ", 5, 100, tFX, FeeRate))
	approx(t, bought.CostKRW, 5*100*(1+FeeRate)*tFX, 1e-6, "매수에는 SEC 수수료 없음")
	sold := must(t)(SellAll(bought, 100, tFX, 2026, FeeRate))
	approx(t, sold.RealizedGainKRW[2026], 5*100*(1-FeeRate-SECFeeRate)*tFX-bought.CostKRW, 1e-6, "매도에만")
}

func TestBuyingNothingDoesNotClaimSymbol(t *testing.T) {
	a := must(t)(Contribute(empty(), 10_000, tFX))
	a = must(t)(BuyMax(a, "TQQQ", 100, tFX, FeeRate))
	if a.Shares != 0 || a.Symbol != "" || a.CashUSD <= 0 {
		t.Fatalf("0주면 보유가 아니다: %+v", a)
	}
	b := must(t)(MonthlyStep(a, "QQQM", tC, tFX, map[string]float64{"QQQM": 300}, 2026, FeeRate))
	if b.Symbol != "QQQM" || b.Shares == 0 {
		t.Fatalf("다음 달 다른 종목을 살 수 있어야 한다: %+v", b)
	}
	out := must(t)(SellAll(Account{Symbol: "TQQQ"}, 50, tFX, 2026, FeeRate))
	if out.Symbol != "" || out.Shares != 0 {
		t.Fatal("빈 포지션 매도가 종목을 지우지 않았다")
	}
}

func TestMonthlyStepInvariantsOverRandomRuns(t *testing.T) {
	rnd := rand.New(rand.NewSource(7))
	for i := 0; i < 300; i++ {
		a := empty()
		months := 1 + rnd.Intn(24)
		for m := 0; m < months; m++ {
			tgt := []string{"TQQQ", "QQQM"}[rnd.Intn(2)]
			px := map[string]float64{"TQQQ": 5 + rnd.Float64()*295, "QQQM": 50 + rnd.Float64()*550}
			contrib := []float64{0, 300_000, 600_000}[rnd.Intn(3)]
			a = must(t)(MonthlyStep(a, tgt, contrib, 1000+rnd.Float64()*600, px, 2020+m/12, FeeRate))
			if a.Shares < 0 || a.CashUSD < -1e-9 || a.CostKRW < -1e-6 || (a.Shares > 0) != (a.Symbol != "") ||
				(a.Shares == 0 && a.CostKRW > 1e-6) || a.TaxLiabilityKRW() < -1e-9 {
				t.Fatalf("불변식 위반 %d/%d: %+v", i, m, a)
			}
		}
	}
}

func TestPreflightCatchesEachFailure(t *testing.T) {
	ok := PreflightCtx{
		ExecDate: Date(2026, 11, 2), SignalDate: Date(2026, 10, 30), SPX: 7580, Vol: 0.227,
		SMASampleCount: SMAPeriod, VolSampleCount: VolWindow, ApplyYM: "2026-11", InApplyMonth: true,
		ExecDay: Date(2026, 11, 2), Switches12M: 2, Target: "TQQQ", Current: "TQQQ",
	}
	if f := Preflight(ok); len(f) != 0 {
		t.Fatalf("정상인데 실패: %v", f)
	}
	cases := map[string]func(c *PreflightCtx){
		"낡음":     func(c *PreflightCtx) { c.SignalDate = Date(2026, 10, 20) },
		"SPX":    func(c *PreflightCtx) { c.SPX = 999 },
		"변동성":    func(c *PreflightCtx) { c.Vol = 0 },
		"SMA 표본": func(c *PreflightCtx) { c.SMASampleCount = 184 },
		"변동성 표본": func(c *PreflightCtx) { c.VolSampleCount = 20 },
		"적용월":    func(c *PreflightCtx) { c.InApplyMonth = false },
		"룩어헤드":   func(c *PreflightCtx) { c.SignalDate = c.ExecDate },
		"전환":     func(c *PreflightCtx) { c.Switches12M = 7 },
		"유령 매도":  func(c *PreflightCtx) { c.SellLegs = []string{"QQQM"} },
		"불필요":    func(c *PreflightCtx) { c.SellLegs = []string{"TQQQ"} },
	}
	for name, mut := range cases {
		c := ok
		mut(&c)
		if f := Preflight(c); len(f) == 0 {
			t.Errorf("%s 검사가 죽어 있다", name)
		}
	}
}
