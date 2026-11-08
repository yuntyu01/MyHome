package run

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yuntyu01/MyHome/server/internal/invest"
	"github.com/yuntyu01/MyHome/server/internal/invest/broker"
	"github.com/yuntyu01/MyHome/server/internal/invest/store"
	"github.com/yuntyu01/MyHome/server/internal/testdb"
)

// 명령 흐름 검증 (TQ tests/test_run.py).
//
// 여기 있는 테스트는 전부 실제로 발생한 결함을 재현하는 방식으로 쓴다 (§5.4).
// 통과 = 테스트가 그 버그를 감지할 능력을 보유. 마스터는 2026-07-31에 고정한다 —
// 날짜가 아니라 코드가 바뀔 때만 깨지게.

var execDay = invest.Date(2026, 8, 3) // 2026-08 첫 거래일

var july = invest.Judgment{SPX: 7489.72021484375, SMA185: 7045.09037816723, C1: true, Vol: 0.236368559570371, C2: true, Target: "TQQQ"}

var (
	pricesOnce sync.Once
	pricesJuly invest.Prices
	pricesErr  error
)

func julyMaster(t *testing.T) invest.Prices {
	t.Helper()
	pricesOnce.Do(func() {
		pricesJuly, pricesErr = readMasterCSV("../testdata/spx_ndx_1985-10_2026-07.csv")
		if pricesErr == nil {
			last := &pricesJuly[len(pricesJuly)-1]
			last.TQQQ, last.QQQM, last.QQQ, last.USDKRWMkt = 64.62, 283.29, 687.99, 1400
		}
	})
	if pricesErr != nil {
		t.Fatal(pricesErr)
	}
	out := make(invest.Prices, len(pricesJuly))
	copy(out, pricesJuly)
	return out
}

// ── 가짜 브로커 ──────────────────────────────────────────────────────

type placed struct {
	OrderID, Symbol string
	Side            broker.Side
	Qty             int
	Price           float64
	Style           broker.Style
	ClientID        string
}

type fakeBroker struct {
	held     map[string]int
	avg      map[string]float64
	rejects  map[broker.Style]bool
	open     []string
	limit    float64 // 0이면 무제한
	spent    float64
	placed   []placed // 접수된 주문
	attempts []placed // Order 호출 전부(거절·끊김 포함)
	filled   map[string][]broker.Fill
	queried  [][2]time.Time
	n        int
	orderErr error
	openErr  error

	// 결과를 모르는 실패: 이 유형은 응답 없이 끊긴다. acceptOnTimeout이면 증권사는 받았다
	timeout         map[broker.Style]bool
	acceptOnTimeout bool
	hideClientID    bool  // 주문 목록에 clientOrderId가 실려 오지 않는다
	findErr         error // 주문 목록 조회 실패
	fundsAbove      int   // 0보다 크면 이 수량을 넘는 매수는 자금 부족으로 거절
	buyableCap      float64
	buyableAsked    []float64
}

// errFakeTimeout은 응답 전에 끊긴 주문이다. 접수 여부를 모른다.
var errFakeTimeout = errors.New("context deadline exceeded (테스트)")

func newFake() *fakeBroker {
	return &fakeBroker{held: map[string]int{}, avg: map[string]float64{}, rejects: map[broker.Style]bool{}, filled: map[string][]broker.Fill{}, timeout: map[broker.Style]bool{}}
}

func (f *fakeBroker) Balance(context.Context) (broker.Balance, error) {
	var hs []broker.Holding
	for s, n := range f.held {
		if n > 0 {
			hs = append(hs, broker.Holding{Symbol: s, Shares: n, AvgPriceUSD: f.avg[s]})
		}
	}
	return broker.Balance{Holdings: hs}, nil
}

// Buyable은 체결한 만큼 줄어든다 — 안 깎이는 가짜를 쓰면 '계좌에 있는 만큼 산다'가 무한 매수로 보인다.
func (f *fakeBroker) Buyable(_ context.Context, sym string, px float64) (broker.Buyable, error) {
	lim := f.limit
	if lim == 0 {
		lim = 1e9
	}
	cash := lim - f.spent
	if cash < 0 {
		cash = 0
	}
	if f.buyableCap > 0 && cash > f.buyableCap { // 매도대금이 아직 주문가능금액에 안 들어온 증권사
		cash = f.buyableCap
	}
	f.buyableAsked = append(f.buyableAsked, px)
	return broker.Buyable{Symbol: sym, PriceUSD: px, CashUSD: cash, MaxQty: int(cash / px)}, nil
}

func (f *fakeBroker) Order(_ context.Context, sym string, side broker.Side, qty int, px float64, style broker.Style, cid string) (broker.OrderResult, error) {
	f.attempts = append(f.attempts, placed{"", sym, side, qty, px, style, cid})
	if f.orderErr != nil {
		return broker.OrderResult{}, f.orderErr
	}
	if f.fundsAbove > 0 && side == broker.Buy && qty > f.fundsAbove {
		return broker.OrderResult{}, errors.New("주문가능금액이 부족합니다 (테스트)")
	}
	if f.rejects[style] {
		return broker.OrderResult{}, fmt.Errorf("%s 거부됨 (테스트)", style)
	}
	// 토스처럼 같은 clientOrderId는 같은 주문이다 — 이전 주문을 그대로 돌려준다
	for _, p := range f.placed {
		if p.ClientID == cid {
			return broker.OrderResult{OrderID: p.OrderID, Symbol: p.Symbol, Side: p.Side, Qty: p.Qty, Price: p.Price}, nil
		}
	}
	if f.timeout[style] {
		if f.acceptOnTimeout {
			f.accept(sym, side, qty, px, style, cid)
		}
		return broker.OrderResult{}, errFakeTimeout
	}
	id := f.accept(sym, side, qty, px, style, cid)
	return broker.OrderResult{OrderID: id, Symbol: sym, Side: side, Qty: qty, Price: px}, nil
}

func (f *fakeBroker) accept(sym string, side broker.Side, qty int, px float64, style broker.Style, cid string) string {
	f.n++
	id := fmt.Sprintf("ODNO%d", f.n)
	f.placed = append(f.placed, placed{id, sym, side, qty, px, style, cid})
	f.open = append(f.open, id)
	return id
}

// IsRejected는 명시적 거절(유형 거부·자금 부족)만 참이다. 끊김은 모름이다.
func (f *fakeBroker) IsRejected(err error) bool {
	return strings.Contains(err.Error(), "거부됨") || f.IsFundsError(err)
}

func (f *fakeBroker) FindOrders(_ context.Context, sym, cid string, _, _ time.Time) ([]broker.OrderRef, error) {
	if f.findErr != nil {
		return nil, f.findErr
	}
	var out []broker.OrderRef
	for _, p := range f.placed {
		byID := !f.hideClientID && cid != "" && p.ClientID == cid
		if !byID && p.Symbol != sym {
			continue
		}
		filled := 0
		for _, x := range f.filled[p.OrderID] {
			filled += x.Qty
		}
		out = append(out, broker.OrderRef{OrderID: p.OrderID, Symbol: p.Symbol, Side: p.Side,
			Open: slices.Contains(f.open, p.OrderID), FilledQty: filled, ByClientID: byID})
	}
	return out, nil
}

func (f *fakeBroker) Fills(_ context.Context, sym string, from, to time.Time) ([]broker.Fill, error) {
	f.queried = append(f.queried, [2]time.Time{from, to})
	var out []broker.Fill
	for _, fs := range f.filled {
		for _, x := range fs {
			if x.Symbol == sym {
				out = append(out, x)
			}
		}
	}
	return out, nil
}

func (f *fakeBroker) OpenOrders(context.Context) ([]map[string]any, error) {
	if f.openErr != nil {
		return nil, f.openErr
	}
	var rows []map[string]any
	for _, o := range f.open {
		rows = append(rows, map[string]any{"any_key": o})
	}
	return rows, nil
}

func (f *fakeBroker) IsFundsError(err error) bool {
	return strings.Contains(err.Error(), "주문가능금액")
}
func (f *fakeBroker) Env() string           { return "test" }
func (f *fakeBroker) AccountMasked() string { return "1234****" }

func (f *fakeBroker) order(id string) placed {
	for _, p := range f.placed {
		if p.OrderID == id {
			return p
		}
	}
	panic("주문 없음 " + id)
}

// fill은 체결을 만든다. asID는 증권사가 다른 폭으로 돌려주는 주문번호다.
func (f *fakeBroker) fill(id string, qty int, px float64, asID string) {
	p := f.order(id)
	if asID == "" {
		asID = id
	}
	f.filled[id] = []broker.Fill{{OrderID: asID, Symbol: p.Symbol, Side: p.Side, Qty: qty, Price: px}}
	sign := 1.0
	if p.Side == broker.Sell {
		sign = -1
	}
	f.spent += sign * float64(qty) * px
	f.removeOpen(id)
}

func (f *fakeBroker) removeOpen(id string) {
	var keep []string
	for _, o := range f.open {
		if o != id {
			keep = append(keep, o)
		}
	}
	f.open = keep
}

// expire는 체결 없이 만료 — 앱은 다음 유형으로 승격해야 한다.
func (f *fakeBroker) expire() { f.open = nil }

func (f *fakeBroker) styles() []broker.Style {
	var s []broker.Style
	for _, p := range f.placed {
		s = append(s, p.Style)
	}
	return s
}

// ── 하네스 ───────────────────────────────────────────────────────────

type harness struct {
	t        *testing.T
	ctx      context.Context
	app      *App
	st       *store.Store
	fb       *fakeBroker
	out, err *bytes.Buffer
	day      time.Time
}

type seedOpt struct {
	acct            invest.Account
	ym, applyYM     string
	executedOn      string
	executedYM      string
	judgmentCount   int
	noBroker        bool
	contributionFor string
}

func newHarness(t *testing.T, o seedOpt) *harness {
	t.Helper()
	pool := testdb.New(t)
	ctx := context.Background()
	st := store.New(pool, "test")
	if _, err := st.CopyPrices(ctx, julyMaster(t)); err != nil {
		t.Fatal(err)
	}
	if err := st.ImportContribution(ctx, invest.Contribution{EffectiveFrom: "2026-01", Amount: 600000, Reason: "테스트 운용 — 소액으로 절차를 먼저 익힌다", RecordedOn: "2026-01-01"}); err != nil {
		t.Fatal(err)
	}
	h := &harness{t: t, ctx: ctx, st: st, fb: newFake(), out: &bytes.Buffer{}, err: &bytes.Buffer{}, day: execDay}
	h.app = &App{Store: st, Out: h.out, Err: h.err, UnsafeDate: true, Sleep: func(time.Duration) {},
		Now: func() time.Time { return time.Date(h.day.Year(), h.day.Month(), h.day.Day(), 15, 17, 0, 0, time.UTC) }}
	if o.noBroker {
		h.app.Broker = func(context.Context) (broker.Broker, error) {
			return nil, errors.New("자격증명 없음 (테스트)")
		}
	} else {
		h.app.Broker = func(context.Context) (broker.Broker, error) { return h.fb, nil }
	}
	h.seed(o)
	return h
}

func (h *harness) seed(o seedOpt) {
	h.t.Helper()
	if o.ym == "" {
		o.ym = "2026-07"
	}
	if o.applyYM == "" {
		o.applyYM = "2026-08"
	}
	if o.judgmentCount == 0 {
		o.judgmentCount = 1
	}
	if o.acct.RealizedGainKRW == nil {
		o.acct.RealizedGainKRW = map[int]float64{}
	}
	p := invest.Position{Holdings: o.acct, LastRun: "2026-07-31", YM: o.ym, Signal: july, FXRate: 1420.6,
		ApplyYM: o.applyYM, JudgmentCount: o.judgmentCount}
	if o.executedOn != "" {
		p.ExecutedOn = invest.StrPtr(o.executedOn)
	}
	if o.executedYM != "" {
		p.ExecutedYM = invest.StrPtr(o.executedYM)
	}
	if err := h.st.SavePosition(h.ctx, p); err != nil {
		h.t.Fatal(err)
	}
}

func (h *harness) pos() invest.Position {
	h.t.Helper()
	p, err := h.st.LoadPosition(h.ctx)
	if err != nil {
		h.t.Fatal(err)
	}
	return p
}

func (h *harness) book() *invest.OrderBook {
	h.t.Helper()
	b, err := h.st.LoadOrders(h.ctx)
	if err != nil {
		h.t.Fatal(err)
	}
	return b
}

func (h *harness) trade(day time.Time, dry bool) int {
	h.day = day
	return h.app.Trade(h.ctx, TradeOpts{Date: invest.FormatDay(day), FX: 1420.6, DryRun: dry})
}

func d(m time.Month, day int) time.Time { return invest.Date(2026, m, day) }

func qqqm3() invest.Account {
	return invest.Account{Symbol: "QQQM", Shares: 3, CostKRW: 1_200_000, RealizedGainKRW: map[int]float64{}}
}

// ── trade ───────────────────────────────────────────────────────────

func TestTradeSubmitsABuyAndWaitsForTheFill(t *testing.T) {
	h := newHarness(t, seedOpt{})
	if rc := h.trade(execDay, false); rc != 0 {
		t.Fatalf("rc %d: %s", rc, h.err)
	}
	if len(h.fb.placed) != 1 {
		t.Fatalf("%+v", h.fb.placed)
	}
	o := h.fb.placed[0]
	if o.Side != broker.Buy || o.Symbol != "TQQQ" || o.Qty <= 0 || o.Price <= 64.62 {
		t.Fatalf("%+v", o)
	}
	b := h.book()
	if b == nil || b.YM != "2026-08" || b.Orders[0].Filled {
		t.Fatalf("%+v", b)
	}
	if h.pos().ExecutedYM != nil {
		t.Fatal("아직 계좌 반영 전이어야 한다")
	}
}

// 🔴 미체결이 남아 있으면 새 주문을 절대 내지 않는다. 자동화에서 가장 비싼 실수다.
func TestTradeNeverDoubleOrdersWhileUnfilled(t *testing.T) {
	h := newHarness(t, seedOpt{})
	h.trade(execDay, false)
	for _, day := range []int{4, 5, 6} {
		if rc := h.trade(d(8, day), false); rc != 0 {
			t.Fatalf("rc %d", rc)
		}
	}
	if len(h.fb.placed) != 1 {
		t.Fatalf("이중 주문: %+v", h.fb.placed)
	}
}

func TestTradeSettlesIntoTheAccountOnceFilled(t *testing.T) {
	h := newHarness(t, seedOpt{})
	h.trade(execDay, false)
	o := h.fb.placed[0]
	h.fb.fill(o.OrderID, o.Qty, 64.90, "")
	if rc := h.trade(d(8, 4), false); rc != 0 {
		t.Fatalf("rc %d: %s", rc, h.err)
	}
	p := h.pos()
	if p.ExecutedYM == nil || *p.ExecutedYM != "2026-08" || p.Holdings.Symbol != "TQQQ" || p.Holdings.Shares != o.Qty || p.MonthsInvested != 1 {
		t.Fatalf("%+v", p)
	}
	if h.book() != nil {
		t.Fatal("주문 기록이 정리되지 않았다")
	}
	led, _ := h.st.LoadLedger(h.ctx)
	if len(led) != 1 || led[0].Source != "trade" || *led[0].Shares != o.Qty {
		t.Fatalf("원금 내역 %+v", led)
	}
	if rc := h.app.Ledger(h.ctx); rc != 0 {
		t.Fatalf("누적적립과 장부가 어긋난다: %s", h.out)
	}
	// 완료 후에는 아무것도 안 낸다
	if rc := h.trade(d(8, 5), false); rc != 0 || len(h.fb.placed) != 1 {
		t.Fatalf("완료 뒤 주문: rc %d %+v", rc, h.fb.placed)
	}
}

// 전환하는 달 — 매도가 체결돼야 매수를 낸다. 매도대금이 매수에 들어가야 한다.
// 매도 체결을 확인한 실행이 같은 자리에서 매수까지 낸다(매도대금이 주문가능금액에 들어와 있으면).
func TestTradeSellsBeforeBuyingAndReinvestsProceeds(t *testing.T) {
	h := newHarness(t, seedOpt{acct: qqqm3()})
	h.fb.held["QQQM"] = 3
	if rc := h.trade(execDay, false); rc != 0 {
		t.Fatalf("rc %d: %s", rc, h.err)
	}
	if p := h.fb.placed; len(p) != 1 || p[0].Side != broker.Sell || p[0].Symbol != "QQQM" || p[0].Price >= 283.29 {
		t.Fatalf("① 매도 제출 %+v", p)
	}
	h.trade(d(8, 4), false) // ② 미체결 — 매수 안 냄
	if len(h.fb.placed) != 1 {
		t.Fatal("매도 미체결인데 매수를 냈다")
	}
	h.fb.fill("ODNO1", 3, 283.00, "")
	if rc := h.trade(d(8, 5), false); rc != 0 { // ③ 매도 체결 확인 → 같은 실행에서 ④ 매수 제출
		t.Fatalf("rc %d: %s", rc, h.err)
	}
	if p := h.fb.placed; len(p) != 2 || p[1].Side != broker.Buy || p[1].Symbol != "TQQQ" || p[1].Style != broker.MOC {
		t.Fatalf("④ 매도 체결을 확인한 실행이 매수를 내지 않았다 %+v", p)
	}
	h.trade(d(8, 6), false) // 매수 미체결 — 새로 내지 않는다
	if len(h.fb.placed) != 2 {
		t.Fatalf("매수 미체결인데 또 냈다 %+v", h.fb.placed)
	}
	ordered := h.fb.placed[1].Qty
	// 매도대금 ≈ $849 + 적립 ≈ $422. 매도대금이 빠지면 6주다
	if ordered < 17 {
		t.Fatalf("매도대금이 빠졌다 — %d주만 주문했다", ordered)
	}
	h.fb.fill("ODNO2", ordered, 64.90, "")
	if rc := h.trade(d(8, 7), false); rc != 0 {
		t.Fatalf("rc %d: %s", rc, h.err)
	}
	p := h.pos()
	if p.Holdings.Symbol != "TQQQ" || p.Holdings.Shares != ordered || p.Holdings.CashUSD >= 200 || p.Holdings.RealizedGainKRW[2026] == 0 {
		t.Fatalf("⑤ 계좌 반영 %+v", p.Holdings)
	}
}

// 장부보다 많이 들고 있으면 그 전량을 팔고, 판 만큼으로 장부를 만든다.
func TestTradeSellsWhatTheBrokerActuallyHolds(t *testing.T) {
	h := newHarness(t, seedOpt{acct: qqqm3()})
	h.fb.held["QQQM"] = 8 // 앱 밖에서 5주 더 샀다
	h.trade(execDay, false)
	if h.fb.placed[0].Qty != 8 {
		t.Fatalf("실제 보유 전량을 팔아야 한다: %d", h.fb.placed[0].Qty)
	}
	if b := h.book(); b.LedgerGap == nil || *b.LedgerGap != 5 {
		t.Fatalf("ledger_gap %+v", b.LedgerGap)
	}
	h.fb.fill("ODNO1", 8, 283.00, "")
	h.trade(d(8, 5), false)
	h.trade(d(8, 6), false)
	ordered := h.fb.placed[1].Qty
	h.fb.fill("ODNO2", ordered, 64.90, "")
	h.trade(d(8, 7), false)
	if h.pos().Holdings.Shares != ordered {
		t.Fatal("장부가 실제 주문과 다르다")
	}
}

// 🔴 서버 날짜와 증권사 주문일자(KST)가 다르다. 하루짜리 창으로 조회하면 영구 미체결로 굳는다.
func TestTradeLooksForFillsInAWindow(t *testing.T) {
	h := newHarness(t, seedOpt{})
	h.trade(execDay, false)
	h.trade(d(8, 4), false)
	w := h.fb.queried[len(h.fb.queried)-1]
	if !w[0].Before(execDay) || w[1].Before(d(8, 4)) {
		t.Fatalf("조회 창 %s ~ %s", invest.FormatDay(w[0]), invest.FormatDay(w[1]))
	}
}

func TestOrderNumbersMatchEvenWhenZeroPadded(t *testing.T) {
	h := newHarness(t, seedOpt{})
	h.trade(execDay, false)
	o := h.fb.placed[0]
	h.fb.fill(o.OrderID, o.Qty, 64.90, "000000"+o.OrderID)
	h.trade(d(8, 4), false)
	if p := h.pos(); p.ExecutedYM == nil {
		t.Fatal("0-패딩 주문번호를 체결로 인식하지 못했다")
	}
	if !sameOrderID("0000012345", "12345") || sameOrderID("0", "00") {
		t.Fatal("sameOrderID")
	}
}

func TestTradeStyleLadder(t *testing.T) {
	h := newHarness(t, seedOpt{})
	h.trade(execDay, false)
	if s := h.fb.styles(); len(s) != 1 || s[0] != broker.MOC {
		t.Fatalf("MOC가 1순위: %v", s)
	}

	h2 := newHarness(t, seedOpt{})
	h2.fb.rejects[broker.MOC] = true
	if rc := h2.trade(execDay, false); rc != 0 || h2.fb.styles()[0] != broker.Market {
		t.Fatalf("MOC 거부 → 같은 실행에서 시장가: rc %d %v", rc, h2.fb.styles())
	}

	h3 := newHarness(t, seedOpt{})
	h3.fb.rejects[broker.MOC], h3.fb.rejects[broker.Market] = true, true
	if rc := h3.trade(execDay, false); rc != 0 || h3.fb.placed[0].Style != broker.Sweep || h3.fb.placed[0].Price <= 64.62 {
		t.Fatalf("둘 다 거부 → 지정가: %+v", h3.fb.placed)
	}

	h4 := newHarness(t, seedOpt{})
	h4.fb.rejects = map[broker.Style]bool{broker.MOC: true, broker.Market: true, broker.Sweep: true}
	if rc := h4.trade(execDay, false); rc != 1 || !strings.Contains(h4.err.String(), "적립") {
		t.Fatalf("다 거부되면 멈추고 알린다: rc %d %s", rc, h4.err)
	}
	h5 := newHarness(t, seedOpt{acct: qqqm3()})
	h5.fb.held["QQQM"] = 3
	h5.fb.rejects = h4.fb.rejects
	if rc := h5.trade(execDay, false); rc != 1 || !strings.Contains(h5.err.String(), "전환") {
		t.Fatalf("전환 실패는 긴급: %s", h5.err)
	}
}

// 🔴 3일 대기는 없다. MOC가 안 채워졌으면 다음 거래일 시장가다.
func TestTradeEscalatesWhenAnOrderExpiresUnfilled(t *testing.T) {
	h := newHarness(t, seedOpt{})
	h.trade(execDay, false)
	h.fb.expire()
	if rc := h.trade(d(8, 4), false); rc != 0 {
		t.Fatalf("rc %d %s", rc, h.err)
	}
	h.fb.expire()
	h.trade(d(8, 5), false)
	if s := h.fb.styles(); fmt.Sprint(s) != "[moc market sweep]" {
		t.Fatalf("%v", s)
	}
	h.fb.expire()
	if rc := h.trade(d(8, 6), false); rc != 1 || len(h.fb.placed) != 3 {
		t.Fatalf("다 소진 — 긴급 정지: rc %d %d", rc, len(h.fb.placed))
	}
}

// 살아 있는 주문을 두고 새로 내면 이중 체결이다. 미체결 조회가 실패하면 모르는 것이다 — 기다린다.
func TestTradeDoesNotEscalateWhileAliveOrUnknown(t *testing.T) {
	h := newHarness(t, seedOpt{})
	h.trade(execDay, false)
	h.trade(d(8, 4), false)
	if len(h.fb.placed) != 1 {
		t.Fatal("살아 있는 주문을 두고 새로 냈다")
	}
	h.fb.expire()
	h.fb.openErr = errors.New("조회 실패")
	if rc := h.trade(d(8, 5), false); rc != 0 || len(h.fb.placed) != 1 {
		t.Fatalf("모르는데 승격했다: rc %d %d", rc, len(h.fb.placed))
	}
}

// 🔴 자금 부족은 주문 유형을 바꿔도 안 풀린다 — 승격하지 않고 즉시 멈춘다.
func TestTradeStopsImmediatelyOnInsufficientFunds(t *testing.T) {
	h := newHarness(t, seedOpt{})
	h.fb.orderErr = errors.New("주문 실패: 주문가능금액이 부족합니다")
	if rc := h.trade(execDay, false); rc != 1 || len(h.fb.placed) != 0 || !strings.Contains(h.err.String(), "자금 부족") {
		t.Fatalf("rc %d %s", rc, h.err)
	}
}

// 🔴 부분 체결 뒤 승격하면 남은 만큼만 산다. 평균은 수량가중이다.
func TestPartialFillDoesNotDoubleThePurchase(t *testing.T) {
	h := newHarness(t, seedOpt{})
	h.trade(execDay, false)
	planned := h.fb.placed[0].Qty
	h.fb.fill(h.fb.placed[0].OrderID, 1, 60.00, "") // 1주만 체결되고 만료
	if rc := h.trade(d(8, 4), false); rc != 0 {
		t.Fatalf("rc %d %s", rc, h.err)
	}
	if s := h.fb.styles(); fmt.Sprint(s) != "[moc market]" || h.fb.placed[1].Qty >= planned {
		t.Fatalf("계획 수량을 통째로 다시 냈다: %v %+v", s, h.fb.placed)
	}
	n2 := h.fb.placed[1].Qty
	h.fb.fill(h.fb.placed[1].OrderID, n2, 70.00, "")
	if rc := h.trade(d(8, 5), false); rc != 0 {
		t.Fatalf("rc %d %s", rc, h.err)
	}
	p := h.pos().Holdings
	want := (1*60.0 + float64(n2)*70.0) / float64(1+n2)
	got := p.CostKRW / (float64(p.Shares) * 1420.6 * (1 + invest.FeeRate))
	if p.Shares > planned || p.CashUSD < 0 || abs(got-want) > want*1e-6 {
		t.Fatalf("shares %d planned %d cash %v avg %v want %v", p.Shares, planned, p.CashUSD, got, want)
	}
}

func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}

// 매도가 시장가까지 내려갔다고 매수까지 거기서 시작하면 안 된다.
func TestTheBuyLegGetsItsOwnThreeChances(t *testing.T) {
	h := newHarness(t, seedOpt{acct: qqqm3()})
	h.fb.held["QQQM"] = 3
	h.trade(execDay, false)
	h.fb.expire()
	h.trade(d(8, 4), false) // 매도 시장가로 승격
	last := h.fb.placed[len(h.fb.placed)-1]
	h.fb.fill(last.OrderID, 3, 283.00, "")
	h.trade(d(8, 5), false)
	h.trade(d(8, 6), false)
	if l := h.fb.placed[len(h.fb.placed)-1]; l.Side != broker.Buy || l.Style != broker.MOC {
		t.Fatalf("매수가 MOC부터 시작하지 않았다: %+v", l)
	}
}

// 🔴 미리 보려고 친 명령이 그 달 체결을 확정하면 안 된다. 승격도 상태 변경이다.
func TestDryRunNeverCommitsOrPromotes(t *testing.T) {
	h := newHarness(t, seedOpt{})
	h.trade(execDay, false)
	o := h.fb.placed[0]
	h.fb.fill(o.OrderID, o.Qty, 64.62, "")
	if rc := h.trade(d(8, 4), true); rc != 0 {
		t.Fatalf("rc %d", rc)
	}
	p := h.pos()
	if p.ExecutedYM != nil || p.Holdings.Shares != 0 || p.ContributedKRW != 0 {
		t.Fatalf("dry-run이 체결을 확정했다: %+v", p)
	}
	if led, _ := h.st.LoadLedger(h.ctx); len(led) != 0 {
		t.Fatal("dry-run이 원금 내역을 썼다")
	}
	if b := h.book(); b.Orders[0].Filled {
		t.Fatal("dry-run이 주문 상태를 바꿨다")
	}
	if rc := h.trade(d(8, 4), false); rc != 0 || h.pos().ExecutedYM == nil {
		t.Fatal("실제 실행은 반영해야 한다")
	}

	h2 := newHarness(t, seedOpt{})
	h2.trade(execDay, false)
	h2.fb.expire()
	h2.trade(d(8, 4), true)
	if b := h2.book(); b.StyleIdx != 0 || b.Orders[0].Filled || len(h2.fb.placed) != 1 {
		t.Fatalf("dry-run이 승격했다: %+v", b)
	}
}

// dry-run은 자격증명 없이 돌아야 무엇을 살지 미리 볼 수 있다.
func TestDryRunNeedsNoBrokerCredentials(t *testing.T) {
	h := newHarness(t, seedOpt{noBroker: true})
	if rc := h.trade(execDay, true); rc != 0 || !strings.Contains(h.out.String(), "매수 주문 — TQQQ") {
		t.Fatalf("rc %d %s %s", rc, h.out, h.err)
	}
	h2 := newHarness(t, seedOpt{noBroker: true, acct: qqqm3()})
	if rc := h2.trade(execDay, true); rc != 0 || !strings.Contains(h2.out.String(), "매도 주문 — QQQM 3주") {
		t.Fatalf("전환 달 dry-run: rc %d %s %s", rc, h2.out, h2.err)
	}
	h3 := newHarness(t, seedOpt{noBroker: true})
	if rc := h3.trade(execDay, false); rc != 1 {
		t.Fatal("실제 실행은 증권사가 없으면 멈춰야 한다")
	}
}

func TestTradeRefusesLeftoverBookAndMissedMonth(t *testing.T) {
	h := newHarness(t, seedOpt{})
	if err := h.st.SaveOrders(h.ctx, invest.OrderBook{YM: "2026-07", Stage: "buy"}); err != nil {
		t.Fatal(err)
	}
	if rc := h.trade(execDay, false); rc != 1 || len(h.fb.placed) != 0 {
		t.Fatal("지난달 주문 기록을 이어받았다")
	}
	h2 := newHarness(t, seedOpt{})
	if _, err := h2.st.RecordMissedMonth(h2.ctx, "2026-08", "TQQQ", execDay); err != nil {
		t.Fatal(err)
	}
	if rc := h2.trade(execDay, false); rc != 1 {
		t.Fatal("미실행으로 기록된 달에 주문했다")
	}
}

func TestTradeStopsWhenPreflightFails(t *testing.T) {
	h := newHarness(t, seedOpt{})
	if rc := h.trade(d(9, 1), false); rc != 1 || len(h.fb.placed) != 0 {
		t.Fatal("적용월 밖에서 주문했다")
	}
}

func TestTradeStopsWhenTheBrokerAllowsNothing(t *testing.T) {
	h := newHarness(t, seedOpt{})
	h.fb.spent = 1e9 // 한도 0
	if rc := h.trade(execDay, false); rc != 1 || !strings.Contains(h.err.String(), "0주") {
		t.Fatalf("rc %d %s", rc, h.err)
	}
}

// 계좌에 계획보다 많은 돈이 있으면 계좌 기준으로 산다 (§12.1.5).
func TestThePlanNoLongerCapsTheOrder(t *testing.T) {
	h := newHarness(t, seedOpt{})
	h.fb.limit = 3000
	h.trade(execDay, false)
	lim, est := 3000.0, 64.62*(1+SizingBuffer)
	if q := h.fb.placed[0].Qty; q != int(lim/est) {
		t.Fatalf("계좌 기준 수량이 아니다: %d", q)
	}
}

// ── judge ───────────────────────────────────────────────────────────

func (h *harness) judge(date string) int {
	return h.app.Judge(h.ctx, JudgeOpts{Date: date})
}

func TestJudgeExecutionMarkerAndCount(t *testing.T) {
	// 새 적용월이면 지난 체결 표시를 버린다 — 이월하면 미체결 감지가 죽는다
	h := newHarness(t, seedOpt{ym: "2026-06", applyYM: "2026-07", executedOn: "2026-07-01", executedYM: "2026-07"})
	if rc := h.judge("2026-07-31"); rc != 0 {
		t.Fatalf("rc %d %s", rc, h.err)
	}
	p := h.pos()
	if p.ApplyYM != "2026-08" || p.ExecutedYM != nil || p.ExecutedOn != nil || p.JudgmentCount != 2 {
		t.Fatalf("%+v", p)
	}
	// 같은 달을 다시 판정해도 횟수는 늘지 않는다
	h.judge("2026-07-31")
	if h.pos().JudgmentCount != 2 {
		t.Fatal("재실행이 판정 횟수를 늘렸다")
	}
	if devs, _ := h.st.LoadDeviations(h.ctx); len(devs) != 0 {
		t.Fatalf("체결한 달을 미실행으로 기록했다: %+v", devs)
	}
}

func TestJudgeRecordsAMissedMonth(t *testing.T) {
	h := newHarness(t, seedOpt{ym: "2026-06", applyYM: "2026-07"})
	h.judge("2026-07-31")
	devs, _ := h.st.LoadDeviations(h.ctx)
	if len(devs) != 1 || devs[0].YM != "2026-07" || devs[0].Kind != "missed" {
		t.Fatalf("%+v", devs)
	}
}

func TestJudgeIsSilentOnANonJudgmentDay(t *testing.T) {
	h := newHarness(t, seedOpt{})
	before := h.pos()
	h.judge("2026-07-30")
	if h.pos().YM != before.YM {
		t.Fatal("판정일이 아닌데 바꿨다")
	}
}

// 🔴 뉴욕 자정을 넘기면 '오늘'이 토요일이 된다 — 그 달 판정이 통째로 유실되던 자리.
func TestJudgeCatchesUpAfterNewYorkMidnight(t *testing.T) {
	for _, day := range []string{"2026-08-01", "2026-08-03"} {
		h := newHarness(t, seedOpt{ym: "2026-06", applyYM: "2026-07", executedOn: "2026-07-01", executedYM: "2026-07"})
		if rc := h.judge(day); rc != 0 {
			t.Fatalf("%s rc %d %s", day, rc, h.err)
		}
		p := h.pos()
		if p.YM != "2026-07" || p.LastRun != "2026-07-31" || p.ApplyYM != "2026-08" || p.JudgmentCount != 2 {
			t.Fatalf("%s %+v", day, p)
		}
	}
}

// 🔴 판정한 달을 다음 달 첫 거래일에 다시 판정하면 막 시작한 적용월이 이탈로 남는다.
func TestCatchUpNeverRejudgesAJudgedMonth(t *testing.T) {
	h := newHarness(t, seedOpt{})
	h.judge("2026-08-03")
	if devs, _ := h.st.LoadDeviations(h.ctx); len(devs) != 0 || h.pos().JudgmentCount != 1 {
		t.Fatalf("판정한 달을 다시 판정했다: %+v", devs)
	}
}

func TestJudgmentDayGate(t *testing.T) {
	days := []time.Time{d(7, 29), d(7, 30), d(7, 31)}
	if g := JudgmentDay(d(7, 31), days, "2026-06"); !g.Equal(d(7, 31)) {
		t.Fatal("월말 당일")
	}
	if g := JudgmentDay(d(8, 3), days, "2026-06"); !g.Equal(d(7, 31)) {
		t.Fatal("다음 거래일 만회")
	}
	if g := JudgmentDay(d(8, 3), days, "2026-07"); !g.IsZero() {
		t.Fatal("이미 판정한 달")
	}
	// 진행 중인 달 — 마스터가 멈춘 날을 월말로 믿으면 안 된다
	mid := []time.Time{invest.Date(2026, 10, 22), invest.Date(2026, 10, 23)}
	if isLastTradingDay(invest.Date(2026, 10, 23), mid) {
		t.Fatal("마스터가 멈춘 금요일을 월말로 믿었다")
	}
}

// ── execute · amend · ledger ────────────────────────────────────────

func (h *harness) execute(fills map[string]float64, day string, shares *int, paid *float64) int {
	return h.app.Execute(h.ctx, ExecuteOpts{Date: day, Fills: fills, FX: 1420.6, Shares: shares, Paid: paid})
}

func TestExecuteIsIdempotentAndGuarded(t *testing.T) {
	h := newHarness(t, seedOpt{})
	if rc := h.execute(map[string]float64{"TQQQ": 64.90}, "2026-08-03", nil, nil); rc != 0 {
		t.Fatalf("rc %d %s", rc, h.err)
	}
	p := h.pos()
	if p.ExecutedYM == nil || *p.ExecutedYM != "2026-08" || p.Before == nil {
		t.Fatalf("%+v", p)
	}
	if rc := h.execute(map[string]float64{"TQQQ": 64.90}, "2026-08-04", nil, nil); rc != 1 {
		t.Fatal("두 번째 기록을 받았다 (5회 실행 시 21주·300만원 계상 사고)")
	}
	if h.pos().Holdings.Shares != p.Holdings.Shares {
		t.Fatal("중복 실행이 수량을 불렸다")
	}

	h2 := newHarness(t, seedOpt{})
	if rc := h2.execute(map[string]float64{"QQQM": 283}, "2026-08-03", nil, nil); rc != 1 {
		t.Fatal("신호가 고르지 않은 종목을 받았다")
	}
	if rc := h2.execute(map[string]float64{"TQQQ": 0.649}, "2026-08-03", nil, nil); rc != 1 || !strings.Contains(h2.err.String(), "오타") {
		t.Fatal("0.649 오타를 막지 못했다 (649주 사고)")
	}
	if rc := h2.execute(map[string]float64{"TQQQ": 64.9}, "2026-09-01", nil, nil); rc != 1 {
		t.Fatal("적용월 밖 기록을 받았다")
	}
}

func TestExecuteRecordsWhatAPersonActuallyDid(t *testing.T) {
	h := newHarness(t, seedOpt{})
	six, paid := 6, 600_000.0
	if rc := h.execute(map[string]float64{"TQQQ": 64.90}, "2026-08-05", &six, &paid); rc != 0 {
		t.Fatalf("rc %d %s", rc, h.err)
	}
	p := h.pos()
	if p.Holdings.Shares != 6 || p.ContributedKRW != paid || p.LateDays == nil || *p.LateDays != 2 {
		t.Fatalf("%+v", p)
	}
}

// 정정은 그 달을 다시 계산한다 — 두 번 세지 않고, 두 번째 정정이 첫 정정 위에 더해지지 않는다.
func TestAmendReplacesTheMonthAndClearsTheEstimate(t *testing.T) {
	h := newHarness(t, seedOpt{})
	h.app.Execute(h.ctx, ExecuteOpts{Date: "2026-08-03", Fills: map[string]float64{"TQQQ": 64.90}}) // 환율 추정
	if !h.pos().FXEstimated {
		t.Fatal("환율을 안 줬으면 추정으로 남아야 한다")
	}
	shares := h.pos().Holdings.Shares
	for i, px := range []float64{65.10, 65.30} {
		if rc := h.app.Amend(h.ctx, AmendOpts{Date: "2026-08-04", Fills: map[string]float64{"TQQQ": px}, FX: 1431.2}); rc != 0 {
			t.Fatalf("정정 %d rc %d %s", i+1, rc, h.err)
		}
	}
	p := h.pos()
	if p.Holdings.Shares != shares || p.FXEstimated || p.AmendCount != 2 {
		t.Fatalf("정정이 수량을 바꿨거나 추정 표시가 남았다: %+v", p)
	}
	led, _ := h.st.LoadLedger(h.ctx)
	if len(led) != 1 || led[0].Source != "amend" {
		t.Fatalf("정정이 그 달을 덮지 않았다: %+v", led)
	}
	if rc := h.app.Ledger(h.ctx); rc != 0 {
		t.Fatalf("정정 뒤 원금 장부 불일치 (−694,510원 사고): %s", h.out)
	}
	if rc := h.app.Amend(h.ctx, AmendOpts{Date: "2026-08-04", Fills: map[string]float64{"QQQM": 283}, FX: 1431}); rc != 1 {
		t.Fatal("정정으로 종목을 바꿨다")
	}
}

func TestLedgerFailsWhenTheTotalDrifted(t *testing.T) {
	h := newHarness(t, seedOpt{})
	h.execute(map[string]float64{"TQQQ": 64.90}, "2026-08-03", nil, nil)
	p := h.pos()
	p.ContributedKRW += 5000
	if err := h.st.SavePosition(h.ctx, p); err != nil {
		t.Fatal(err)
	}
	if rc := h.app.Ledger(h.ctx); rc != 1 {
		t.Fatal("총액이 내역 없이 바뀐 것을 못 잡았다")
	}
}

// ── deviate · status ────────────────────────────────────────────────

func TestDeviationNeedsTwoRunsADayApart(t *testing.T) {
	h := newHarness(t, seedOpt{})
	run := func(day string) int {
		return h.app.Deviate(h.ctx, DeviateOpts{Date: day, Symbol: "QQQM", Reason: "불안해서"})
	}
	if rc := run("2026-08-03"); rc != 0 {
		t.Fatalf("rc %d %s", rc, h.err)
	}
	if devs, _ := h.st.LoadDeviations(h.ctx); len(devs) != 0 {
		t.Fatal("1차에 확정했다")
	}
	if rc := run("2026-08-03"); rc != 1 {
		t.Fatal("냉각 중인데 확정했다")
	}
	if rc := run("2026-08-04"); rc != 0 {
		t.Fatalf("24시간 뒤 확정 실패: %s", h.err)
	}
	devs, _ := h.st.LoadDeviations(h.ctx)
	if len(devs) != 1 || *devs[0].ActualTarget != "QQQM" || devs[0].LockedUntil != "2026-08-31" {
		t.Fatalf("%+v", devs)
	}
	// 확정한 이탈은 실제로 살 수 있어야 한다 (첫 거래일 검사에 막히던 사고)
	if rc := h.trade(d(8, 5), false); rc != 0 || h.fb.placed[0].Symbol != "QQQM" {
		t.Fatalf("이탈 종목을 사지 못했다: rc %d %s %+v", rc, h.err, h.fb.placed)
	}
}

func TestStatusDeadman(t *testing.T) {
	h := newHarness(t, seedOpt{})
	if rc := h.app.Status(h.ctx, StatusOpts{Date: "2026-08-03"}); rc != 0 {
		t.Fatal("실행일 당일은 오경보다")
	}
	if rc := h.app.Status(h.ctx, StatusOpts{Date: "2026-08-05"}); rc != 1 {
		t.Fatal("매수 없이 지난 실행일을 못 잡았다")
	}
	h.execute(map[string]float64{"TQQQ": 64.90}, "2026-08-04", nil, nil)
	if rc := h.app.Status(h.ctx, StatusOpts{Date: "2026-08-05"}); rc != 0 {
		t.Fatalf("이번 달 매수를 인정하지 않았다: %s", h.err)
	}
	if rc := h.app.Status(h.ctx, StatusOpts{Date: "2026-09-10"}); rc != 1 {
		t.Fatal("35일 넘은 판정을 못 잡았다")
	}
}

// 운영에서 --date로 날짜를 바꿔 preflight를 통과시킬 수 없어야 한다.
func TestDateNeedsTheUnsafeSwitch(t *testing.T) {
	h := newHarness(t, seedOpt{})
	h.app.UnsafeDate = false
	if rc := h.trade(execDay, true); rc != 1 {
		t.Fatal("--date 우회가 열려 있다")
	}
}

// 실패 알림은 실패한 실행의 출력을 싣고 DB에 남는다 — 앱 알림만 보고 어디를 볼지 알아야 한다.
func TestAlertCarriesTheFailedRunOutput(t *testing.T) {
	h := newHarness(t, seedOpt{})
	id, err := h.st.StartRun(h.ctx, "trade", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := h.st.FinishRun(h.ctx, id, 1, "✗ 증권사가 TQQQ를 0주까지만 허용한다"); err != nil {
		t.Fatal(err)
	}
	h.app.Now = time.Now
	if rc := h.app.Alert(h.ctx, AlertOpts{Unit: "myhome-invest-trade.service"}); rc != 0 {
		t.Fatalf("rc %d %s", rc, h.err)
	}
	// 실행 기록에 없는 유닛(DB에 닿기 전에 죽음)도 알림은 남긴다
	if rc := h.app.Alert(h.ctx, AlertOpts{Unit: "myhome-invest-status.service"}); rc != 0 {
		t.Fatalf("rc %d %s", rc, h.err)
	}
	got, err := h.st.AlertsAfter(h.ctx, 0, time.Now().Add(-time.Hour), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("알림 %d건", len(got))
	}
	a := got[0]
	if !strings.Contains(a.Title, "주문") || !strings.Contains(a.Title, "exit 1") || !strings.Contains(a.Body, "0주까지만") || a.RunID == nil || *a.RunID != id {
		t.Fatalf("%+v", a)
	}
	if got[1].RunID != nil || !strings.Contains(got[1].Body, "journalctl -u myhome-invest-status.service") {
		t.Fatalf("%+v", got[1])
	}
	// 앱은 마지막으로 본 id 다음만 받는다
	if rest, _ := h.st.AlertsAfter(h.ctx, got[0].ID, time.Now().Add(-time.Hour), 10); len(rest) != 1 || rest[0].ID != got[1].ID {
		t.Fatalf("after: %+v", rest)
	}
}

// 매매가 아닌 유닛(백업)의 실패는 매매 실행 기록을 끌어오지 않는다
func TestAlertForBackupUnit(t *testing.T) {
	h := newHarness(t, seedOpt{})
	id, _ := h.st.StartRun(h.ctx, "trade", "")
	_ = h.st.FinishRun(h.ctx, id, 1, "✗ 매매 실패")
	h.app.Now = time.Now
	if rc := h.app.Alert(h.ctx, AlertOpts{Unit: "myhome-backup.service"}); rc != 0 {
		t.Fatalf("rc %d %s", rc, h.err)
	}
	got, _ := h.st.AlertsAfter(h.ctx, 0, time.Now().Add(-time.Hour), 10)
	if len(got) != 1 || got[0].Title != "서버 백업 실패" || got[0].RunID != nil || strings.Contains(got[0].Body, "매매 실패") {
		t.Fatalf("%+v", got)
	}
}

func TestLastLinesKeepsTheEnd(t *testing.T) {
	long := strings.Repeat("가나다라마\n", 400) + "✗ 마지막 이유"
	got := lastLines(long, 100)
	if !strings.HasPrefix(got, "…\n") || !strings.HasSuffix(got, "✗ 마지막 이유") || len([]rune(got)) > 103 {
		t.Fatalf("%q", got)
	}
	if lastLines(" 짧다 ", 100) != "짧다" {
		t.Fatal("짧은 출력은 그대로")
	}
}

// 🔴 뉴욕 휴장일(주말·공휴일)에는 주문을 내지 않고 성공으로 끝난다 — 타이머가 매월 1~8일 매일 부르므로.
// 다음 거래일 실행이 이어받는다(TQ execution.yml 게이트와 같은 동작).
func TestTradeSkipsNewYorkHolidays(t *testing.T) {
	h := newHarness(t, seedOpt{})
	for _, day := range []time.Time{d(8, 1), d(8, 2), invest.Date(2026, 9, 7)} { // 토, 일, 노동절
		if rc := h.trade(day, false); rc != 0 {
			t.Fatalf("%s rc %d: %s", invest.FormatDay(day), rc, h.err)
		}
		if !strings.Contains(h.out.String(), "휴장일") {
			t.Fatalf("%s: 건너뛴 이유가 없다: %s", invest.FormatDay(day), h.out)
		}
	}
	if len(h.fb.placed) != 0 {
		t.Fatalf("휴장일에 주문했다: %+v", h.fb.placed)
	}
	// 첫 거래일에는 정상으로 주문한다
	if rc := h.trade(execDay, false); rc != 0 || len(h.fb.placed) != 1 {
		t.Fatalf("rc %d placed %d: %s", rc, len(h.fb.placed), h.err)
	}
}
