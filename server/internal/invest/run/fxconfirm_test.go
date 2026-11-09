package run

import (
	"context"
	"errors"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/yuntyu01/MyHome/server/internal/invest"
	"github.com/yuntyu01/MyHome/server/internal/invest/broker"
)

// settlingBroker는 결제 내역·환율을 주는 증권사다(토스). 주문 쪽은 fakeBroker 그대로다.
type settlingBroker struct {
	*fakeBroker
	rows  []broker.Settlement
	asked [][2]time.Time
	fx    map[string]float64 // 뉴욕 날짜 → 매매기준율
	fxAt  []time.Time
}

func (s *settlingBroker) Settlements(_ context.Context, sym string, from, to time.Time) ([]broker.Settlement, error) {
	s.asked = append(s.asked, [2]time.Time{from, to})
	var out []broker.Settlement
	for _, r := range s.rows {
		if r.Symbol == sym {
			out = append(out, r)
		}
	}
	return out, nil
}

func (s *settlingBroker) FXAt(_ context.Context, at time.Time) (broker.FXQuote, error) {
	s.fxAt = append(s.fxAt, at)
	v, ok := s.fx[at.In(invest.NewYork).Format("2006-01-02")]
	if !ok {
		return broker.FXQuote{}, errors.New("그 시각 환율 없음 (테스트)")
	}
	return broker.FXQuote{Rate: v + 0.5, Mid: v, At: at}, nil
}

func settling(h *harness) *settlingBroker {
	sb := &settlingBroker{fakeBroker: h.fb}
	h.app.Broker = func(context.Context) (broker.Broker, error) { return sb, nil }
	return sb
}

func buyRow(qty int, px, fx float64, settled bool) broker.Settlement {
	amt := float64(qty) * px
	s := broker.Settlement{Symbol: "TQQQ", Side: broker.Buy, Qty: qty, Amount: amt, Net: amt * 1.001, Settled: settled}
	if settled {
		s.Price, s.FX = px, fx
	}
	return s
}

// tradeOn은 환율을 주지 않는 실제 timer 실행이다(시장 환율로 추정).
func (h *harness) tradeOn(day time.Time) int {
	h.day = day
	return h.app.Trade(h.ctx, TradeOpts{Date: invest.FormatDay(day)})
}

// 자동 매수 → 체결 확인(추정 환율) → 결제 대기 → 결제 뒤 증권사 적용 환율로 확정. 다시 돌려도 그대로다.
func TestTradeConfirmsTheEstimatedRateOnceSettled(t *testing.T) {
	h := newHarness(t, seedOpt{})
	sb := settling(h)
	if rc := h.tradeOn(execDay); rc != 0 {
		t.Fatalf("rc %d %s", rc, h.err)
	}
	o := h.fb.placed[0]
	h.fb.fill(o.OrderID, o.Qty, 64.90, "")
	if rc := h.tradeOn(d(8, 4)); rc != 0 {
		t.Fatalf("rc %d %s", rc, h.err)
	}
	if p := h.pos(); !p.FXEstimated || p.FXRate != 1400 {
		t.Fatalf("체결 확인 때는 시장 환율 추정이어야 한다: %+v", p)
	}

	sb.rows = []broker.Settlement{buyRow(o.Qty, 64.90, 0, false)}
	if rc := h.tradeOn(d(8, 5)); rc != 0 {
		t.Fatalf("rc %d %s", rc, h.err)
	}
	if !h.pos().FXEstimated || !strings.Contains(h.out.String(), "결제 대기") {
		t.Fatalf("미결제인데 확정했다: %s", h.out)
	}

	sb.rows = []broker.Settlement{buyRow(o.Qty, 64.90, 1385.5, true)}
	if rc := h.tradeOn(d(8, 6)); rc != 0 {
		t.Fatalf("rc %d %s", rc, h.err)
	}
	p := h.pos()
	if p.FXEstimated || p.FXRate != 1385.5 || p.Holdings.Shares != o.Qty || p.AmendCount != 1 {
		t.Fatalf("결제 환율로 확정하지 않았다: %+v", p)
	}
	want := float64(o.Qty) * 64.90 * (1 + invest.FeeRate) * 1385.5
	if math.Abs(p.ContributedKRW-want) > 1 {
		t.Fatalf("원금 %.0f, 기대 %.0f", p.ContributedKRW, want)
	}
	if from := sb.asked[0][0]; !from.Equal(time.Date(2026, 8, 1, 0, 0, 0, 0, broker.KST)) {
		t.Fatalf("조회 시작 %s — 적용월 1일부터 봐야 한다", from)
	}
	led, _ := h.st.LoadLedger(h.ctx)
	if len(led) != 1 || led[0].Source != "amend" || !strings.Contains(led[0].Note, "결제 환율") {
		t.Fatalf("원금 내역 %+v", led)
	}
	if rc := h.app.Ledger(h.ctx); rc != 0 {
		t.Fatalf("확정 뒤 원금 장부 불일치: %s", h.out)
	}
	if rc := h.tradeOn(d(8, 7)); rc != 0 || h.pos().AmendCount != 1 {
		t.Fatalf("확정한 달을 또 고쳤다: rc %d, %+v", rc, h.pos())
	}
}

// 🔴 결제 내역 수량이 그 달 체결과 다르면(앱 밖 매수가 섞임) 어느 것이 이번 달 것인지 모른다 — 고치지 않는다.
func TestAQuantityMismatchLeavesTheEstimate(t *testing.T) {
	h := newHarness(t, seedOpt{})
	sb := settling(h)
	h.tradeOn(execDay)
	o := h.fb.placed[0]
	h.fb.fill(o.OrderID, o.Qty, 64.90, "")
	h.tradeOn(d(8, 4))
	sb.rows = []broker.Settlement{buyRow(o.Qty, 64.90, 1385.5, true), buyRow(2, 66, 1390, true)}
	if rc := h.tradeOn(d(8, 6)); rc != 0 {
		t.Fatalf("환율 확정 보류로 매매가 실패하면 안 된다: rc %d", rc)
	}
	if p := h.pos(); !p.FXEstimated || p.AmendCount != 0 {
		t.Fatalf("섞인 결제 내역으로 확정했다: %+v", p)
	}
	if !strings.Contains(h.err.String(), "앱 밖 매매") {
		t.Fatalf("보류 이유를 안 남겼다: %s", h.err)
	}
}

// 🔴 사람이 execute로 적은 달은 자동으로 안 고친다(사람이 적은 원금이 정답일 수 있다). amend --auto로만.
func TestManualMonthsAreConfirmedOnlyWhenAsked(t *testing.T) {
	h := newHarness(t, seedOpt{})
	sb := settling(h)
	if rc := h.app.Execute(h.ctx, ExecuteOpts{Date: "2026-08-03", Fills: map[string]float64{"TQQQ": 64.90}}); rc != 0 {
		t.Fatalf("rc %d %s", rc, h.err)
	}
	shares := h.pos().Holdings.Shares
	sb.rows = []broker.Settlement{buyRow(shares, 64.90, 1390, true)}
	h.tradeOn(d(8, 5))
	if !h.pos().FXEstimated {
		t.Fatal("사람이 기록한 달을 자동으로 고쳤다")
	}
	if rc := h.app.Amend(h.ctx, AmendOpts{Date: "2026-08-05", Auto: true, FX: 1400}); rc != 1 {
		t.Fatal("--auto와 --fx를 같이 받았다")
	}
	if rc := h.app.Amend(h.ctx, AmendOpts{Date: "2026-08-05", Auto: true}); rc != 0 {
		t.Fatalf("rc %d %s", rc, h.err)
	}
	if p := h.pos(); p.FXEstimated || p.FXRate != 1390 || p.Holdings.Shares != shares {
		t.Fatalf("%+v", p)
	}
	if rc := h.app.Amend(h.ctx, AmendOpts{Date: "2026-08-05", Auto: true}); rc != 0 || h.pos().AmendCount != 1 {
		t.Fatalf("이미 확정된 달을 또 고쳤다: rc %d", rc)
	}
}

// 환율은 그날 미국 장 마감 시각의 증권사 매매기준율이다. 못 받은 날은 야후 값으로 채운다.
func TestRefreshTakesTheBrokerMidRate(t *testing.T) {
	a, b := d(8, 3), d(8, 4)
	r := newRefreshRig(t, stageRefresh(a, b))
	sb := settling(r.harness)
	sb.fx = map[string]float64{"2026-08-03": 1388.8}
	if rc := r.refresh(0, 0); rc != 0 {
		t.Fatalf("rc %d %s", rc, r.err)
	}
	got := map[time.Time]float64{}
	for _, row := range r.master() {
		got[row.Day] = row.USDKRWMkt
	}
	if got[a] != 1388.8 {
		t.Fatalf("%s 환율 %v — 증권사 매매기준율이어야 한다", invest.FormatDay(a), got[a])
	}
	if got[b] != 1416 {
		t.Fatalf("%s 환율 %v — 증권사가 못 준 날은 야후 값", invest.FormatDay(b), got[b])
	}
	if len(sb.fxAt) != 2 || !sb.fxAt[0].Equal(time.Date(2026, 8, 3, 16, 0, 0, 0, invest.NewYork)) {
		t.Fatalf("조회 시각 %v — 그날 뉴욕 16:00이어야 한다", sb.fxAt)
	}
}
