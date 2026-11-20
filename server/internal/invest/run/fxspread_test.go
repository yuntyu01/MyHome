package run

import (
	"strings"
	"testing"

	"github.com/yuntyu01/MyHome/server/internal/invest"
	"github.com/yuntyu01/MyHome/server/internal/invest/broker"
)

// 환전 스프레드: 결제 내역의 적용 환율을 그 체결 거래일의 usdkrw_mkt와, 환전 거래를 그 시각 토스 기준율과 견준다.
// 읽기만 한다(합성 데이터).
func TestFXSpreadComparesAppliedRatesWithTheMidRate(t *testing.T) {
	h := newHarness(t, seedOpt{})
	cb := withCash(h)
	h.day = d(8, 10)
	// 7/31 마스터 usdkrw_mkt = 1400. 8/3(한국) 결제 = 7/31 체결
	cb.rows = []broker.Settlement{
		{Day: invest.Date(2026, 8, 3), Symbol: "TQQQ", Side: broker.Buy, Qty: 6, Price: 64.9, Amount: 389.4, Net: 389.79, FX: 1407, Settled: true},
		{Day: invest.Date(2026, 8, 3), Symbol: "QQQM", Side: broker.Sell, Qty: 3, Price: 283, Amount: 849, Net: 848.1, FX: 1393, Settled: true},
		{Day: invest.Date(2026, 8, 4), Symbol: "TQQQ", Side: broker.Buy, Qty: 1, Settled: false}, // 미결제는 뺀다
	}
	cb.flows = []broker.CashFlow{{Kind: broker.FlowExchangeIn, Currency: "USD", Amount: 400, FX: 1450.01, At: atKST(8, 3, 10, 0)}}
	cb.fx["2026-08-02"] = 1448.53 // 한국 8/3 10:00 = 뉴욕 8/2 밤
	before := h.pos()
	if rc := h.app.FXSpread(h.ctx, FXSpreadOpts{From: "2026-08-01"}); rc != 0 {
		t.Fatalf("rc %d %s", rc, h.err)
	}
	out := h.out.String()
	for _, want := range []string{
		"결제 2026-08-03(거래 2026-07-31) TQQQ 매수 6주 · 적용 1,407.00 · 기준 1,400.00 · +0.50%",
		"QQQM 매도 3주 · 적용 1,393.00 · 기준 1,400.00 · -0.50%",
		"매수 1건 · 금액 가중 평균 +0.50%",
		"매도 1건 · 금액 가중 평균 -0.50%",
		"$400.00 · 적용 1,450.01 · 기준 1,448.53 · +0.10%",
		"환전 1건",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("%q 없음:\n%s", want, out)
		}
	}
	if strings.Contains(out, "1주") {
		t.Fatalf("미결제를 셌다:\n%s", out)
	}
	if after := h.pos(); after.Holdings.Shares != before.Holdings.Shares || after.ContributedKRW != before.ContributedKRW {
		t.Fatal("읽기 전용인데 장부를 바꿨다")
	}
}
