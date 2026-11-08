package run

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/yuntyu01/MyHome/server/internal/invest"
	"github.com/yuntyu01/MyHome/server/internal/invest/broker"
	"github.com/yuntyu01/MyHome/server/internal/invest/yahoo"
)

// 자동매매 신뢰성 보강 (2026-11-08). 전부 실제로 일어날 수 있는 실패를 가짜 증권사로 재현한다.

// ── H2. 결과를 모르는 주문 ───────────────────────────────────────────

// 🔴 응답 전에 끊겼는데 증권사는 받았다. 거절로 보고 다음 유형을 내면 같은 돈으로 두 번 산다.
func TestATimedOutOrderThatWasAcceptedIsAdoptedNotDuplicated(t *testing.T) {
	for _, hide := range []bool{false, true} { // 주문 목록에 clientOrderId가 실려 오든 안 오든
		t.Run(fmt.Sprintf("키 숨김=%v", hide), func(t *testing.T) {
			h := newHarness(t, seedOpt{})
			h.fb.timeout[broker.MOC], h.fb.acceptOnTimeout, h.fb.hideClientID = true, true, hide
			if rc := h.trade(execDay, false); rc != 1 || !strings.Contains(h.err.String(), "결과를 모른다") {
				t.Fatalf("끊긴 주문은 멈추고 알려야 한다: rc %d %s", rc, h.err)
			}
			if len(h.fb.attempts) != 1 {
				t.Fatalf("결과를 모르는데 다음 유형을 냈다: %+v", h.fb.attempts)
			}
			b := h.book()
			if b == nil || len(b.Orders) != 1 {
				t.Fatalf("시도한 주문이 기록되지 않았다: %+v", b)
			}
			o, sent := b.Orders[0], h.fb.attempts[0]
			if !o.Unknown || o.OrderID != "" || o.ClientOrderID != sent.ClientID || o.Style != "moc" || o.Qty != sent.Qty ||
				o.Price != sent.Price || o.SubmittedAt == "" || o.SubmittedOn != "2026-08-03" {
				t.Fatalf("결과 모름 기록 %+v (보낸 것 %+v)", o, sent)
			}

			// 다음 실행 — 증권사에서 찾아 이어받는다. 새 주문은 없다
			h.fb.timeout = map[broker.Style]bool{}
			if rc := h.trade(d(8, 4), false); rc != 0 {
				t.Fatalf("rc %d %s", rc, h.err)
			}
			if len(h.fb.attempts) != 1 || len(h.fb.placed) != 1 {
				t.Fatalf("이어받지 않고 새로 냈다: %+v", h.fb.attempts)
			}
			if o := h.book().Orders[0]; o.Unknown || o.OrderID != "ODNO1" {
				t.Fatalf("이어받은 주문 %+v", o)
			}
			if !strings.Contains(h.out.String(), "이어받는다") {
				t.Fatalf("%s", h.out)
			}
			h.fb.fill("ODNO1", h.fb.placed[0].Qty, 64.90, "")
			if rc := h.trade(d(8, 5), false); rc != 0 {
				t.Fatalf("rc %d %s", rc, h.err)
			}
			if p := h.pos(); p.ExecutedYM == nil || p.Holdings.Shares != h.fb.placed[0].Qty {
				t.Fatalf("이어받은 주문의 체결이 반영되지 않았다 %+v", p)
			}
		})
	}
}

// 끊겼고 증권사도 못 받았다 — 확실히 없을 때만 같은 유형부터 다시 낸다(승격하지 않는다).
func TestATimedOutOrderThatNeverArrivedIsPlacedAgainNextRun(t *testing.T) {
	h := newHarness(t, seedOpt{})
	h.fb.timeout[broker.MOC] = true
	if rc := h.trade(execDay, false); rc != 1 || len(h.fb.placed) != 0 {
		t.Fatalf("rc %d %+v", rc, h.fb.placed)
	}
	h.fb.timeout = map[broker.Style]bool{}
	if rc := h.trade(d(8, 4), false); rc != 0 {
		t.Fatalf("rc %d %s", rc, h.err)
	}
	if p := h.fb.placed; len(p) != 1 || p[0].Style != broker.MOC {
		t.Fatalf("없던 주문은 같은 유형으로 다시 내야 한다: %+v", p)
	}
	b := h.book()
	if len(b.Orders) != 1 || b.Orders[0].Unknown || b.Orders[0].OrderID != "ODNO1" || b.StyleIdx != 0 {
		t.Fatalf("%+v", b)
	}
	if !strings.Contains(h.out.String(), "증권사에 없다") {
		t.Fatalf("%s", h.out)
	}
}

// 모르면 모른다 — 조회가 실패하거나 후보가 여럿이면 새 주문을 내지 않는다.
func TestAnUnknownOrderBlocksNewOrdersUntilItIsSettled(t *testing.T) {
	h := newHarness(t, seedOpt{})
	h.fb.timeout[broker.MOC] = true
	h.trade(execDay, false)
	h.fb.timeout = map[broker.Style]bool{}

	h.fb.findErr = errors.New("조회 실패 (테스트)")
	if rc := h.trade(d(8, 4), false); rc != 1 || len(h.fb.attempts) != 1 {
		t.Fatalf("조회 실패인데 주문했다: rc %d %+v", rc, h.fb.attempts)
	}
	if !h.book().Orders[0].Unknown {
		t.Fatal("결과 모름 표시가 사라졌다")
	}

	// 키가 안 실려 오는 목록에 같은 종목·방향의 모르는 주문이 둘 — 어느 것인지 모른다
	h.fb.findErr = nil
	h.fb.hideClientID = true
	h.fb.accept("TQQQ", broker.Buy, 3, 0, broker.Market, "남의-1")
	h.fb.accept("TQQQ", broker.Buy, 4, 0, broker.Market, "남의-2")
	if rc := h.trade(d(8, 5), false); rc != 1 || len(h.fb.attempts) != 1 || !strings.Contains(h.err.String(), "확신할 수 없다") {
		t.Fatalf("후보가 둘인데 정했다: rc %d %s", rc, h.err)
	}
}

// 🔴 접수는 됐는데 그 뒤 저장이 실패했다. 기록은 결과 모름으로 남아 있고, 다음 실행이 키로 찾는다.
// 예전 주석은 "다음 실행은 같은 clientOrderId로 다시 낸다"였지만 키에 날짜·유형이 있어 다른 키가 됐다.
func TestAnAcceptedOrderWhoseSaveFailedIsFoundByItsKey(t *testing.T) {
	h := newHarness(t, seedOpt{})
	h.trade(execDay, false)
	b := h.book()
	b.Orders[0].OrderID, b.Orders[0].Unknown = "", true // 두 번째 저장이 안 된 상태
	if err := h.st.SaveOrders(h.ctx, *b); err != nil {
		t.Fatal(err)
	}
	if rc := h.trade(d(8, 4), false); rc != 0 || len(h.fb.attempts) != 1 {
		t.Fatalf("rc %d %+v", rc, h.fb.attempts)
	}
	if o := h.book().Orders[0]; o.OrderID != "ODNO1" || o.Unknown {
		t.Fatalf("%+v", o)
	}
}

// 명시적 거절은 예전처럼 같은 실행에서 다음 유형으로 내려간다. 거절된 시도는 기록에 남지 않는다.
func TestAnExplicitRejectionStillEscalatesInTheSameRun(t *testing.T) {
	h := newHarness(t, seedOpt{})
	h.fb.rejects[broker.MOC] = true
	if rc := h.trade(execDay, false); rc != 0 {
		t.Fatalf("rc %d %s", rc, h.err)
	}
	if fmt.Sprint(stylesOf(h.fb.attempts)) != "[moc market]" || len(h.fb.placed) != 1 {
		t.Fatalf("%+v", h.fb.attempts)
	}
	b := h.book()
	if len(b.Orders) != 1 || b.Orders[0].Unknown || b.Orders[0].Style != "market" || b.StyleIdx != 1 {
		t.Fatalf("%+v", b)
	}
	// 보낸 키와 기록한 키가 같다
	if b.Orders[0].ClientOrderID != h.fb.placed[0].ClientID || b.Orders[0].ClientOrderID != "tq-20260803-TQQQ-buy-"+fmt.Sprint(b.Orders[0].Qty)+"-market" {
		t.Fatalf("키 %q / %q", b.Orders[0].ClientOrderID, h.fb.placed[0].ClientID)
	}
}

func stylesOf(ps []placed) []broker.Style {
	var out []broker.Style
	for _, p := range ps {
		out = append(out, p.Style)
	}
	return out
}

// 이미 기록에 있는 우리 주문은 후보가 아니다 — 지난 유형의 만료된 주문을 결과 모름 주문으로 착각하지 않는다.
func TestPickUnknownIgnoresOrdersAlreadyInTheBook(t *testing.T) {
	book := invest.OrderBook{Orders: []invest.BookOrder{
		{OrderID: "0007", Symbol: "TQQQ", Side: "buy", Filled: true, Expired: true},
		{ClientOrderID: "k", Symbol: "TQQQ", Side: "buy", Unknown: true},
	}}
	o := book.Orders[1]
	if _, got := pickUnknown(book, o, []broker.OrderRef{{OrderID: "7", Symbol: "TQQQ", Side: broker.Buy}}); got != lookupAbsent {
		t.Fatalf("기록에 있는 주문을 후보로 봤다: %v", got)
	}
	if _, got := pickUnknown(book, o, []broker.OrderRef{{OrderID: "9", Symbol: "TQQQ", Side: broker.Sell}}); got != lookupAbsent {
		t.Fatalf("반대 방향을 후보로 봤다: %v", got)
	}
	if r, got := pickUnknown(book, o, []broker.OrderRef{{OrderID: "9", Symbol: "TQQQ", Side: broker.Buy}, {OrderID: "10", ByClientID: true}}); got != lookupFound || r.OrderID != "10" {
		t.Fatalf("키가 맞는 행이 우선이다: %v %+v", got, r)
	}
	if _, got := pickUnknown(book, o, []broker.OrderRef{{OrderID: "", Symbol: "TQQQ", Side: broker.Buy}}); got != lookupAmbiguous {
		t.Fatalf("주문번호 없는 후보를 이어받았다: %v", got)
	}
}

// ── M2. 판정이 빠진 달 ───────────────────────────────────────────────

// 🔴 판정이 빠지면 적용월이 지난달로 남는다. 지난달 체결 표시 때문에 "이미 완료"로 끝나면 아무도 모른다.
func TestTradeFailsLoudlyWhenTheMonthWasNeverJudged(t *testing.T) {
	for _, executed := range []bool{true, false} {
		o := seedOpt{ym: "2026-06", applyYM: "2026-07"}
		if executed {
			o.executedOn, o.executedYM = "2026-07-01", "2026-07"
		}
		h := newHarness(t, o)
		rc := h.trade(execDay, false)
		if rc != 1 || len(h.fb.attempts) != 0 || !strings.Contains(h.err.String(), "판정이 빠졌다") {
			t.Fatalf("체결 %v: rc %d %s%s", executed, rc, h.out, h.err)
		}
		if strings.Contains(h.out.String(), "이미 완료") {
			t.Fatalf("완료로 숨겼다: %s", h.out)
		}
	}
}

// ── M3. 매매 창과 남은 주문 기록 ─────────────────────────────────────

// 매도대금이 아직 주문가능금액에 안 들어왔으면 같은 실행에서 사지 않는다 — 다음 실행에 맡긴다.
func TestTheSameRunBuyWaitsForTheSaleProceeds(t *testing.T) {
	h := newHarness(t, seedOpt{acct: qqqm3()})
	h.fb.held["QQQM"] = 3
	h.trade(execDay, false)
	h.fb.fill("ODNO1", 3, 283.00, "")
	h.fb.buyableCap = 400 // 적립금만 보이고 매도대금 $849는 아직
	if rc := h.trade(d(8, 4), false); rc != 0 || len(h.fb.placed) != 1 {
		t.Fatalf("매도대금이 안 들어왔는데 샀다: rc %d %+v", rc, h.fb.placed)
	}
	if b := h.book(); b.Stage != "buy" || !strings.Contains(h.out.String(), "다음 실행") {
		t.Fatalf("%+v %s", b, h.out)
	}
	h.fb.buyableCap = 0
	if rc := h.trade(d(8, 5), false); rc != 0 || len(h.fb.placed) != 2 || h.fb.placed[1].Side != broker.Buy {
		t.Fatalf("다음 실행이 사지 않았다: rc %d %+v", rc, h.fb.placed)
	}
}

// 늦은 실행은 적용월 안이면 허용된다(§12.1.3) — 창을 15일까지 넓힌 근거다.
func TestTradeStillRunsMidMonth(t *testing.T) {
	h := newHarness(t, seedOpt{})
	if rc := h.trade(d(8, 14), false); rc != 0 || len(h.fb.placed) != 1 {
		t.Fatalf("rc %d %s", rc, h.err)
	}
}

func (h *harness) orders(clear bool) int {
	return h.app.Orders(h.ctx, OrdersOpts{ClearStale: clear})
}

// staleBook은 지난달(7월) 매수 기록이다. 그 주문(ODNO1)은 8일에 냈고 확인되지 않았다.
func (h *harness) staleBook() string {
	h.t.Helper()
	id := h.fb.accept("TQQQ", broker.Buy, 6, 0, broker.MOC, "tq-20260708-TQQQ-buy-6-moc")
	b := invest.OrderBook{YM: "2026-07", Stage: "buy", Orders: []invest.BookOrder{
		{OrderID: id, ClientOrderID: "tq-20260708-TQQQ-buy-6-moc", Symbol: "TQQQ", Side: "buy", Qty: 6, Style: "moc", SubmittedOn: "2026-07-08"},
	}}
	if err := h.st.SaveOrders(h.ctx, b); err != nil {
		h.t.Fatal(err)
	}
	return id
}

// 🔴 지난달 기록은 새 주문을 막는다. 지우는 명령은 증권사에 살아 있을 수 있는 주문이 있으면 거부한다.
func TestClearStaleRefusesWhileAnOrderMayStillBeOpen(t *testing.T) {
	h := newHarness(t, seedOpt{})
	h.staleBook()
	if rc := h.trade(execDay, false); rc != 1 || !strings.Contains(h.err.String(), "--clear-stale") {
		t.Fatalf("지난달 기록이 있는데 진행했다: rc %d %s", rc, h.err)
	}
	if rc := h.orders(false); rc != 0 || !strings.Contains(h.out.String(), "지난 적용월") {
		t.Fatalf("보기: rc %d %s", rc, h.out)
	}
	if rc := h.orders(true); rc != 1 || h.book() == nil || !strings.Contains(h.err.String(), "살아 있는") {
		t.Fatalf("살아 있는 주문을 두고 지웠다: rc %d %s", rc, h.err)
	}
	h.fb.openErr = errors.New("조회 실패")
	h.fb.open = nil
	if rc := h.orders(true); rc != 1 || h.book() == nil {
		t.Fatalf("살아 있는지 모르는데 지웠다: rc %d", rc)
	}
}

// 만료되고 체결도 없으면 지운다. 그 뒤 trade가 이번 달을 처음부터 진행한다.
func TestClearStaleClearsAnExpiredOrderAndTradeResumes(t *testing.T) {
	h := newHarness(t, seedOpt{})
	h.staleBook()
	h.fb.expire()
	if rc := h.orders(true); rc != 0 || h.book() != nil {
		t.Fatalf("rc %d %s", rc, h.err)
	}
	if rc := h.trade(execDay, false); rc != 0 || len(h.fb.placed) != 2 {
		t.Fatalf("정리 뒤 진행하지 않았다: rc %d %s", rc, h.err)
	}
}

// 🔴 확인되지 않은 체결이 있었다 — 기록만 지우면 그 매수가 장부에서 사라진다. 장부가 증권사와 같아진 뒤에만 지운다.
func TestClearStaleRefusesToLoseAnUnrecordedFill(t *testing.T) {
	h := newHarness(t, seedOpt{})
	id := h.staleBook()
	h.fb.fill(id, 6, 64.0, "")
	h.fb.held["TQQQ"] = 6
	if rc := h.orders(true); rc != 1 || h.book() == nil || !strings.Contains(h.err.String(), "반영되지 않았다") {
		t.Fatalf("장부에 없는 체결을 두고 지웠다: rc %d %s", rc, h.err)
	}
	// 사람이 장부를 맞췄다(adjust 등)
	h.update(func(p *invest.Position) { p.Holdings.Symbol, p.Holdings.Shares = "TQQQ", 6 })
	if rc := h.orders(true); rc != 0 || h.book() != nil {
		t.Fatalf("장부가 맞는데 지우지 않았다: rc %d %s", rc, h.err)
	}
}

// 이번 달 기록은 지우지 않는다 — trade가 처리한다.
func TestClearStaleNeverClearsTheCurrentMonth(t *testing.T) {
	h := newHarness(t, seedOpt{})
	h.trade(execDay, false)
	h.fb.expire()
	if rc := h.orders(true); rc != 1 || h.book() == nil {
		t.Fatalf("rc %d", rc)
	}
}

// ── M5. 사이징 가격 ──────────────────────────────────────────────────

// quoteYahoo는 TQQQ 현재가를 주는 야후다.
func quoteYahoo(px float64, err error) *fakeYahoo {
	return &fakeYahoo{next: func(int) (map[string][]yahoo.Bar, error) {
		if err != nil {
			return nil, err
		}
		return map[string][]yahoo.Bar{"TQQQ": {{Day: d(7, 31), Close: 64.62}, {Day: d(8, 3), Close: px}}}, nil
	}}
}

// 🔴 마스터 종가는 금요일·판정일에만 갱신된다. 그 뒤 5% 넘게 오르면 종가×1.05로 잡은 수량이 계좌를 넘는다.
func TestSizingUsesAFreshQuoteNotTheStaleClose(t *testing.T) {
	h := newHarness(t, seedOpt{})
	h.app.Prices = quoteYahoo(75, nil) // 종가 64.62에서 16% 올랐다
	h.fb.limit = 1000
	if rc := h.trade(execDay, false); rc != 0 {
		t.Fatalf("rc %d %s", rc, h.err)
	}
	est := 75 * (1 + SizingBuffer)
	if q := h.fb.placed[0].Qty; q != int(1000/est) {
		t.Fatalf("현재가로 잡지 않았다: %d주 (현재가 기준 %d주)", q, int(1000/est))
	}
	if !strings.Contains(h.out.String(), "현재가 $75.00") {
		t.Fatalf("%s", h.out)
	}
}

// 야후가 안 되거나 값이 이상하면 종가로 잡는다 — 그것 때문에 그 달을 잃지 않는다.
func TestSizingFallsBackToTheCloseWhenTheQuoteIsUnusable(t *testing.T) {
	for _, y := range []*fakeYahoo{quoteYahoo(0, fmt.Errorf("%w: 테스트", yahoo.ErrTemporary)), quoteYahoo(6.46, nil), quoteYahoo(math.NaN(), nil)} {
		h := newHarness(t, seedOpt{})
		h.app.Prices = y
		h.fb.limit = 1000
		if rc := h.trade(execDay, false); rc != 0 {
			t.Fatalf("rc %d %s", rc, h.err)
		}
		if q := h.fb.placed[0].Qty; q != int(1000/closeEst()) {
			t.Fatalf("종가로 돌아가지 않았다: %d주 %s", q, h.err)
		}
	}
}

// closeEst는 마스터 종가(7/31 TQQQ)에 안전폭을 얹은 예상단가다.
func closeEst() float64 { c := 64.62; return c * (1 + SizingBuffer) }

// 🔴 자금 부족이면 유형을 바꿔도 안 풀린다. 매수는 한 번, 증권사가 허용하는 만큼으로 줄여 다시 낸다.
func TestAFundsRejectionRetriesOnceWithFewerShares(t *testing.T) {
	h := newHarness(t, seedOpt{})
	h.fb.limit = 1000
	first := int(1000 / closeEst())
	h.fb.fundsAbove = first - 1
	if rc := h.trade(execDay, false); rc != 0 {
		t.Fatalf("rc %d %s", rc, h.err)
	}
	if len(h.fb.attempts) != 2 || h.fb.attempts[0].Qty != first || h.fb.attempts[1].Style != broker.MOC {
		t.Fatalf("%+v", h.fb.attempts)
	}
	if p := h.fb.placed; len(p) != 1 || p[0].Qty >= first || p[0].Qty <= 0 {
		t.Fatalf("줄인 수량으로 내지 않았다: %+v", p)
	}
	if b := h.book(); len(b.Orders) != 1 || b.Orders[0].Qty != h.fb.placed[0].Qty {
		t.Fatalf("%+v", b)
	}
	// 줄여도 안 되면 한 번만 더 해 보고 멈춘다
	h2 := newHarness(t, seedOpt{})
	h2.fb.limit = 1000
	h2.fb.fundsAbove = 1
	if rc := h2.trade(execDay, false); rc != 1 || len(h2.fb.attempts) != 2 || len(h2.fb.placed) != 0 || !strings.Contains(h2.err.String(), "자금 부족") {
		t.Fatalf("rc %d %+v %s", rc, h2.fb.attempts, h2.err)
	}
}

// ── L2. 재시도 대기 중 잠금 ──────────────────────────────────────────

// refresh는 기다리는 동안 잠금을 놓는다(Pause). 쥐고 기다리면 겹친 judge가 75로 끝난다.
func TestRefreshReleasesTheLockWhileWaiting(t *testing.T) {
	r := newRefreshRig(t, holesThenFull(2))
	var paused []time.Duration
	r.app.Pause = func(_ context.Context, d time.Duration) error { paused = append(paused, d); return nil }
	if rc := r.refresh(3, 15*time.Minute); rc != 0 {
		t.Fatalf("rc %d %s", rc, r.err)
	}
	if fmt.Sprint(paused) != "[15m0s 15m0s]" || len(r.slept) != 0 {
		t.Fatalf("잠금을 놓지 않고 잤다: paused %v slept %v", paused, r.slept)
	}
	// 다시 못 잡으면 쓰지 않고 75로 끝난다
	r2 := newRefreshRig(t, holesThenFull(2))
	r2.app.Pause = func(context.Context, time.Duration) error { return errors.New("잠금 대기 초과") }
	before := len(r2.master())
	if rc := r2.refresh(3, time.Minute); rc != ExitRetry || len(r2.master()) != before || r2.y.calls != 1 {
		t.Fatalf("rc %d calls %d", rc, r2.y.calls)
	}
}

// ── M1. 평일 월말의 만회 ─────────────────────────────────────────────

// augRow는 8월 합성 행이다. NDX·TQQQ를 조금씩 흔들어 변동성이 0이 되지 않게 한다(preflight 이상치 검사).
func augRow(day time.Time, i int) yrow {
	f := 1 + 0.004*float64(i%3-1)
	return yrow{day, 7489.72021484375, 28274.19921875 * f, 687.99, 283.29, 83.67, 64.62 * f, 1400}
}

func (h *harness) appendRows(rows ...yrow) {
	h.t.Helper()
	for _, r := range rows {
		p := invest.PriceRow{Day: r.day, SPX: r.spx, NDX: r.ndx, QQQ: r.qqq, QQQM: r.qqqm, QLD: r.qld, TQQQ: r.tqqq, USDKRW: math.NaN(), USDKRWMkt: r.krw}
		if _, err := h.st.InsertPrice(h.ctx, p); err != nil {
			h.t.Fatal(err)
		}
	}
}

// 🔴 2026-08-31은 월요일이다(11-30도 월요일). 그날 저녁 판정이 야후 지연으로 빠지면, 예전 일정의 만회는
// 다음 날(1일) 22:37 UTC - 1일 장 마감 뒤라 1일 종가가 먼저 들어와 룩어헤드로 막혔다. 장 시작 전(11:37 UTC)
// 실행이 그 사이를 메운다. 월말 당일 아침의 같은 실행은 아직 때가 아니므로 실패하지 않는다.
func TestAWeekdayMonthEndIsCaughtUpBeforeTheNextOpen(t *testing.T) {
	h := newHarness(t, seedOpt{executedOn: "2026-08-03", executedYM: "2026-08"})
	var aug []yrow
	for day := d(8, 3); !day.After(d(8, 28)); day = day.AddDate(0, 0, 1) {
		if invest.IsTradingDay(day) {
			aug = append(aug, augRow(day, len(aug)))
		}
	}
	h.appendRows(aug...)
	at := func(ts time.Time) { h.app.Now = func() time.Time { return ts } }

	// ① 8/31(월) 장 시작 전 - 월말 당일 아침. 종가가 아직 없으니 아무것도 하지 않고 0으로 끝난다
	at(time.Date(2026, 8, 31, 11, 37, 0, 0, time.UTC))
	if rc := h.app.Judge(h.ctx, JudgeOpts{}); rc != 0 || h.pos().YM != "2026-07" || !strings.Contains(h.out.String(), "장 마감 전") {
		t.Fatalf("월말 당일 아침 실행: rc %d %s%s", rc, h.out, h.err)
	}

	// ② 8/31 저녁 판정은 빠졌다(마스터는 8/28까지). 9/1(화) 장 시작 전에 갱신 → 판정
	at(time.Date(2026, 9, 1, 11, 37, 0, 0, time.UTC))
	aug31, sep1 := augRow(d(8, 31), len(aug)), augRow(d(9, 1), len(aug)+1) // 9/1 값은 장 시작 전 바 - 버려야 한다
	h.app.Prices = &fakeYahoo{next: func(int) (map[string][]yahoo.Bar, error) { return bars(aug[len(aug)-1], aug31, sep1), nil }}
	if rc := h.app.Refresh(h.ctx, RefreshOpts{}); rc != 0 {
		t.Fatalf("refresh rc %d %s", rc, h.err)
	}
	if rc := h.app.Judge(h.ctx, JudgeOpts{}); rc != 0 {
		t.Fatalf("만회 판정 rc %d %s", rc, h.err)
	}
	p := h.pos()
	if p.YM != "2026-08" || p.LastRun != "2026-08-31" || p.ApplyYM != "2026-09" || p.JudgmentCount != 2 {
		t.Fatalf("8월 판정이 아니다: %+v", p)
	}
	if devs, _ := h.st.LoadDeviations(h.ctx); len(devs) != 0 {
		t.Fatalf("체결한 달을 미실행으로 기록했다: %+v", devs)
	}

	// ③ 같은 날 장중 매매는 9월 적용월로 진행한다(판정이 빠졌다면 trade가 실패로 알린다)
	if rc := h.trade(d(9, 1), false); rc != 0 || len(h.fb.placed) != 1 {
		t.Fatalf("9월 매수 rc %d %s", rc, h.err)
	}
}

// 주문 기록은 이제 보내기 전에 저장된다. 전 유형이 거절된 달은 주문 없는 기록만 남는데,
// 남은 주문이 없으니 다음 달을 막지 않는다.
func TestAnEmptyBookFromLastMonthDoesNotBlock(t *testing.T) {
	h := newHarness(t, seedOpt{})
	if err := h.st.SaveOrders(h.ctx, invest.OrderBook{YM: "2026-07", Stage: "buy", StyleIdx: 2}); err != nil {
		t.Fatal(err)
	}
	if rc := h.trade(execDay, false); rc != 0 || len(h.fb.placed) != 1 || h.fb.placed[0].Style != broker.MOC {
		t.Fatalf("빈 기록이 막았다: rc %d %s %+v", rc, h.err, h.fb.placed)
	}
	if b := h.book(); b.YM != "2026-08" {
		t.Fatalf("%+v", b)
	}
}
