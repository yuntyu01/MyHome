package invest

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

// TQ tests/test_account.py · test_state.py · test_run.py 중 순수 함수 몫으로 아직 없던 것.

// 장중에는 그날이 완결 거래일이 아니다. 개장 직후·주말은 직전 거래일이 완결이다.
func TestLastCompleteSessionWaitsForTheClose(t *testing.T) {
	et := func(m time.Month, d, hh, mm int) time.Time { return time.Date(2026, m, d, hh, mm, 0, 0, NewYork) }
	cases := []struct {
		now  time.Time
		want time.Time
	}{
		{et(8, 13, 12, 37), Date(2026, 8, 12)}, // 목 장중
		{et(8, 13, 16, 0), Date(2026, 8, 13)},  // 목 마감
		{et(8, 14, 9, 30), Date(2026, 8, 13)},  // 금 개장 — 아직 목요일이 마지막
		{et(8, 16, 12, 0), Date(2026, 8, 14)},  // 일요일
	}
	for _, c := range cases {
		if got := LastCompleteSession(c.now); !got.Equal(c.want) {
			t.Errorf("%s → %s, 기대 %s", c.now, FormatDay(got), FormatDay(c.want))
		}
	}
}

// 연간 통산 — 같은 해 안의 이익과 손실은 상계된 뒤 공제·세율이 붙는다.
func TestSameYearGainsAndLossesNetOff(t *testing.T) {
	a := must(t)(BuyMax(must(t)(Contribute(empty(), 30_000_000, tFX)), "TQQQ", 100, tFX, FeeRate))
	won := must(t)(SellAll(a, 150, tFX, 2026, FeeRate))
	g1 := won.RealizedGainKRW[2026]
	b := must(t)(BuyMax(won, "QQQM", 100, tFX, FeeRate))
	lost := must(t)(SellAll(b, 90, tFX, 2026, FeeRate))
	g2 := lost.RealizedGainKRW[2026] - g1
	if g1 <= 0 || g2 >= 0 || g1+g2 <= AnnualDeductKRW {
		t.Fatalf("이익 %v 손실 %v", g1, g2)
	}
	approx(t, lost.TaxDueKRW(2026), (g1+g2-AnnualDeductKRW)*TaxRate, 1e-6, "통산 뒤 과세")
	if lost.TaxDueKRW(2026) >= won.TaxDueKRW(2026) {
		t.Fatal("같은 해 손실이 이익을 줄이지 않았다")
	}
	n := Account{RealizedGainKRW: map[int]float64{2026: 8_000_000 - 3_000_000}}
	approx(t, n.TaxDueKRW(2026), (5_000_000-AnnualDeductKRW)*TaxRate, 1e-6, "상계")
}

func TestRejectsBadFXPriceAndAmountWithReasons(t *testing.T) {
	if _, err := Contribute(empty(), tC, 0); err == nil || !strings.Contains(err.Error(), "환율") {
		t.Fatalf("%v", err)
	}
	if _, err := Contribute(empty(), -1, tFX); err == nil || !strings.Contains(err.Error(), "적립액") {
		t.Fatalf("%v", err)
	}
	a := must(t)(BuyMax(must(t)(Contribute(empty(), tC, tFX)), "QQQM", 100, tFX, FeeRate))
	if _, err := SellAll(a, 0, tFX, 2026, FeeRate); err == nil || !strings.Contains(err.Error(), "체결단가 이상치") {
		t.Fatalf("단가 0 매도: %v", err)
	}
}

// 🔴 두 요율을 하나로 합치면 조용히 어긋난다. 지금은 값이 같아서 더 위험하다 — 섞어 써도 티가 안 난다.
// 값 비교로는 못 잡으므로 소스에서 별개로 선언돼 있는지, 명령이 백테스트 가정을 쓰지 않는지 본다.
func TestBacktestAndBrokerRatesAreSeparate(t *testing.T) {
	if BacktestFeeRate != 0.001 || FeeRate != 0.001 {
		t.Fatalf("요율 %v %v", BacktestFeeRate, FeeRate)
	}
	b, err := os.ReadFile("account.go")
	if err != nil {
		t.Fatal(err)
	}
	src := string(b)
	for _, re := range []string{`(?m)^\s*BacktestFeeRate\s*=\s*0\.001\b`, `(?m)^\s*FeeRate\s*=\s*0\.001\b`} {
		if !regexp.MustCompile(re).MatchString(src) {
			t.Errorf("상수로 따로 선언돼 있지 않다: %s", re)
		}
	}
	if regexp.MustCompile(`FeeRate\s*=\s*BacktestFeeRate|BacktestFeeRate\s*=\s*FeeRate`).MatchString(src) {
		t.Error("한쪽이 다른 쪽의 별칭이다")
	}
	// 계좌를 움직이는 명령은 실제 요율만 쓴다
	files, _ := filepath.Glob(filepath.Join("run", "*.go"))
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(b), "BacktestFeeRate") {
			t.Errorf("%s가 백테스트 요율로 계좌를 계산한다", f)
		}
	}
	if len(files) == 0 {
		t.Fatal("명령 소스를 못 찾았다")
	}
}

// 요율이 오르면 실현손익은 줄어야 한다 — 세금이 과다 계상되지 않도록.
// 살아 있는 FeeRate에 묶지 않는다 — 비교 대상과 같아지면 단조성 검사가 조용히 죽는다.
func TestHigherFeesNeverIncreaseRealizedGain(t *testing.T) {
	a := must(t)(BuyMax(must(t)(Contribute(empty(), tC, tFX)), "QQQM", 100, tFX, FeeRate))
	cheap := must(t)(SellAll(a, 150, tFX, 2026, 0.001))
	real := must(t)(SellAll(a, 150, tFX, 2026, 0.0025))
	if real.RealizedGainKRW[2026] >= cheap.RealizedGainKRW[2026] || real.TaxDueKRW(2026) > cheap.TaxDueKRW(2026) {
		t.Fatalf("요율이 올랐는데 실현손익이 줄지 않았다: %v vs %v", real.RealizedGainKRW[2026], cheap.RealizedGainKRW[2026])
	}
}

// 이탈 사유 재표시(6개월)는 연·말일을 넘긴다 — 2월 말과 윤년을 보정한다.
func TestPlusMonthsHandlesYearAndMonthEnd(t *testing.T) {
	cases := []struct{ from, want time.Time }{
		{Date(2026, 9, 1), Date(2027, 3, 1)},
		{Date(2026, 8, 31), Date(2027, 2, 28)},
		{Date(2027, 8, 31), Date(2028, 2, 29)},
	}
	for _, c := range cases {
		if got := PlusMonths(c.from, 6); !got.Equal(c.want) {
			t.Errorf("%s +6개월 = %s, 기대 %s", FormatDay(c.from), FormatDay(got), FormatDay(c.want))
		}
	}
}
