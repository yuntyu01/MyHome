package run

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/yuntyu01/MyHome/server/internal/invest"
	"github.com/yuntyu01/MyHome/server/internal/invest/broker"
)

// cashBroker는 입금·환전 내역과 원화 잔고를 주는 증권사다(토스). 나머지는 settlingBroker 그대로다.
type cashBroker struct {
	*settlingBroker
	flows []broker.CashFlow
	krw   float64
	from  []time.Time
}

func (c *cashBroker) CashFlows(_ context.Context, from time.Time) ([]broker.CashFlow, error) {
	c.from = append(c.from, from)
	return c.flows, nil
}

func (c *cashBroker) CashKRW(context.Context) (float64, error) { return c.krw, nil }

func withCash(h *harness) *cashBroker {
	cb := &cashBroker{settlingBroker: &settlingBroker{fakeBroker: h.fb, fx: map[string]float64{}}}
	h.app.Broker = func(context.Context) (broker.Broker, error) { return cb, nil }
	return cb
}

func atKST(m time.Month, d, hh, mm int) time.Time {
	return time.Date(2026, m, d, hh, mm, 0, 0, broker.KST)
}

func (h *harness) flow(step string) []invest.FlowEvent {
	h.t.Helper()
	all, err := h.st.LoadFlow(h.ctx, "2026-08")
	if err != nil {
		h.t.Fatal(err)
	}
	var out []invest.FlowEvent
	for _, e := range all {
		if e.Step == step {
			out = append(out, e)
		}
	}
	return out
}

// 입금일을 정하면 돈이 안 보이는 동안은 사지 않고, 입금일 + 3일이 지나면 있는 돈으로 산다.
func TestTradeWaitsForTheDepositDay(t *testing.T) {
	h := newHarness(t, seedOpt{})
	cb := withCash(h)
	if err := h.st.SetDepositDay(h.ctx, 5); err != nil {
		t.Fatal(err)
	}
	for _, day := range []int{3, 4, 5, 6, 7} { // 8/5(수)가 입금일, 기한은 8/8(토) → 8/10(월)
		h.out.Reset()
		if rc := h.trade(d(8, day), false); rc != 0 {
			t.Fatalf("8/%d rc %d: %s", day, rc, h.err)
		}
		if len(h.fb.attempts) != 0 {
			t.Fatalf("8/%d 입금 전에 주문했다: %+v", day, h.fb.attempts)
		}
		if !strings.Contains(h.out.String(), "입금 기다림") {
			t.Fatalf("8/%d: %s", day, h.out)
		}
	}
	if len(cb.from) == 0 || !cb.from[0].Equal(time.Date(2026, 8, 1, 0, 0, 0, 0, broker.KST)) {
		t.Fatalf("입금 내역은 적용월 1일(한국)부터 본다: %v", cb.from)
	}
	if rc := h.trade(d(8, 10), false); rc != 0 {
		t.Fatalf("rc %d: %s", rc, h.err)
	}
	if len(h.fb.placed) != 1 || !strings.Contains(h.err.String(), "입금이 안 보인다") {
		t.Fatalf("기한이 지나면 있는 돈으로 산다: %+v / %s", h.fb.placed, h.err)
	}
	// 상태 점검도 기한 전에는 미체결로 알리지 않는다
	if err := h.st.SetDepositDay(h.ctx, 20); err != nil {
		t.Fatal(err)
	}
	h.day = d(8, 12)
	if rc := h.app.Status(h.ctx, StatusOpts{Date: "2026-08-12"}); rc != 0 {
		t.Fatalf("입금일 전 status가 실패했다: %s", h.err)
	}
}

// 입금이 보이면 입금일 전이라도 바로 산다. 입금·주문·체결이 단계 기록에 남는다.
func TestTradeBuysOnceTheDepositShowsAndRecordsTheSteps(t *testing.T) {
	h := newHarness(t, seedOpt{})
	cb := withCash(h)
	cb.krw = 600000
	if err := h.st.SetDepositDay(h.ctx, 5); err != nil {
		t.Fatal(err)
	}
	cb.flows = []broker.CashFlow{{Kind: broker.FlowDeposit, Currency: "KRW", Amount: 10000, At: atKST(8, 2, 9, 0)}} // 소액은 무시
	if rc := h.trade(execDay, false); rc != 0 || len(h.fb.attempts) != 0 {
		t.Fatalf("소액 입금에 샀다: rc %d %+v", rc, h.fb.attempts)
	}
	cb.flows = append(cb.flows, broker.CashFlow{Kind: broker.FlowDeposit, Currency: "KRW", Amount: 600000, At: atKST(8, 3, 14, 2)})
	if rc := h.trade(d(8, 4), false); rc != 0 {
		t.Fatalf("rc %d: %s", rc, h.err)
	}
	if len(h.fb.placed) != 1 {
		t.Fatalf("입금이 보였는데 안 샀다: %s", h.out)
	}
	if deps := h.flow(invest.StepDeposit); len(deps) != 2 || deps[1].KRW != 600000 {
		t.Fatalf("입금 기록 = %+v", deps)
	}
	o := h.fb.placed[0]
	if ords := h.flow(invest.StepOrder); len(ords) != 1 || ords[0].Qty != o.Qty || ords[0].Symbol != "TQQQ" || ords[0].OrderID != o.OrderID {
		t.Fatalf("주문 기록 = %+v", ords)
	}
	// 체결 다음 날: 통합증거금 환전이 내역에 보이고 그 시각 기준율과 같이 남는다
	h.fb.fill(o.OrderID, o.Qty, 64.90, "")
	cb.fx["2026-08-04"] = 1420.0
	cb.flows = append(cb.flows,
		broker.CashFlow{Kind: broker.FlowExchangeOut, Currency: "KRW", Amount: 590000, At: atKST(8, 5, 0, 18)},
		broker.CashFlow{Kind: broker.FlowExchangeIn, Currency: "USD", Amount: 415.2, FX: 1421.0, At: atKST(8, 5, 0, 18)})
	for _, day := range []int{5, 6} {
		if rc := h.trade(d(8, day), false); rc != 0 {
			t.Fatalf("rc %d: %s", rc, h.err)
		}
	}
	if fills := h.flow(invest.StepFill); len(fills) != 1 || fills[0].Qty != o.Qty || fills[0].Price != 64.90 {
		t.Fatalf("체결 기록 = %+v", fills)
	}
	ex := h.flow(invest.StepExchange)
	if len(ex) != 1 || ex[0].KRW != 590000 || ex[0].USD != 415.2 || ex[0].FX != 1421.0 || ex[0].Mid != 1420.0 {
		t.Fatalf("환전 기록 = %+v", ex)
	}
	if len(cb.fxAt) != 1 {
		t.Fatalf("이미 남긴 환전의 기준율을 다시 물었다: %d", len(cb.fxAt))
	}
	if h.pos().ExecutedYM == nil {
		t.Fatal("체결이 반영되지 않았다")
	}
}

// 입금일을 정하지 않으면 예전 그대로 첫 거래일에 산다.
func TestTradeWithoutDepositDayBuysOnTheFirstTradingDay(t *testing.T) {
	h := newHarness(t, seedOpt{})
	withCash(h)
	if rc := h.trade(execDay, false); rc != 0 || len(h.fb.placed) != 1 {
		t.Fatalf("rc %d %+v: %s", rc, h.fb.placed, h.err)
	}
}
