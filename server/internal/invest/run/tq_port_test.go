package run

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/yuntyu01/MyHome/server/internal/dbq"
	"github.com/yuntyu01/MyHome/server/internal/invest"
	"github.com/yuntyu01/MyHome/server/internal/invest/broker"
	"github.com/yuntyu01/MyHome/server/internal/invest/store"
	"github.com/yuntyu01/MyHome/server/internal/invest/yahoo"
	"github.com/yuntyu01/MyHome/server/internal/testdb"
)

// TQ tests/test_run.py · test_state.py 중 run_test.go에 아직 없던 것을 옮겼다.
// 하네스(newHarness, fakeBroker)는 run_test.go 것을 그대로 쓴다. 이름은 TQ 테스트 이름을 Go식으로 바꿨다.

const (
	devReason    = "시장이 크게 빠져 3배 레버리지를 들고 있기가 불안하다"
	adjustReason = "여유 자금이 생겨 이번 달만 추가로 매수했다"
)

func ip(n int) *int               { return &n }
func fp(v float64) *float64       { return &v }
func near(a, b, tol float64) bool { return math.Abs(a-b) <= tol }

// update는 저장된 포지션을 고쳐 다시 저장한다 (TQ _seed의 **extra 자리).
func (h *harness) update(f func(p *invest.Position)) {
	h.t.Helper()
	p := h.pos()
	f(&p)
	if err := h.st.SavePosition(h.ctx, p); err != nil {
		h.t.Fatal(err)
	}
}

func (h *harness) devs() []invest.Deviation {
	h.t.Helper()
	d, err := h.st.LoadDeviations(h.ctx)
	if err != nil {
		h.t.Fatal(err)
	}
	return d
}

func (h *harness) ledger() []invest.LedgerEntry {
	h.t.Helper()
	l, err := h.st.LoadLedger(h.ctx)
	if err != nil {
		h.t.Fatal(err)
	}
	return l
}

func (h *harness) deviate(day time.Time, sym string) int {
	h.day = day
	return h.app.Deviate(h.ctx, DeviateOpts{Date: invest.FormatDay(day), Symbol: sym, Reason: devReason})
}

// executeJuly는 TQ _execute() — 2026-08-03 TQQQ=64.62 · 환율 1420.6 → 6주.
func (h *harness) executeJuly() {
	h.t.Helper()
	if rc := h.execute(map[string]float64{"TQQQ": 64.62}, "2026-08-03", nil, nil); rc != 0 {
		h.t.Fatalf("execute rc %d: %s", rc, h.err)
	}
}

func (h *harness) amend(day string, fills map[string]float64, fx float64) int {
	return h.app.Amend(h.ctx, AmendOpts{Date: day, Fills: fills, FX: fx})
}

func (h *harness) adjust(o AdjustOpts) int {
	if o.Date == "" {
		o.Date = "2026-08-05"
	}
	return h.app.Adjust(h.ctx, o)
}

func (h *harness) last() placed { return h.fb.placed[len(h.fb.placed)-1] }

// faultyBroker는 조회 하나를 실패시키는 증권사다. 나머지는 fakeBroker 그대로다.
type faultyBroker struct {
	*fakeBroker
	balanceErr, buyableErr error
}

func (f *faultyBroker) Balance(ctx context.Context) (broker.Balance, error) {
	if f.balanceErr != nil {
		return broker.Balance{}, f.balanceErr
	}
	return f.fakeBroker.Balance(ctx)
}

func (f *faultyBroker) Buyable(ctx context.Context, sym string, px float64) (broker.Buyable, error) {
	if f.buyableErr != nil {
		return broker.Buyable{}, f.buyableErr
	}
	return f.fakeBroker.Buyable(ctx, sym, px)
}

func (h *harness) useBroker(b broker.Broker) {
	h.app.Broker = func(context.Context) (broker.Broker, error) { return b, nil }
}

func julyPos(acct invest.Account) invest.Position {
	return invest.Position{Holdings: acct, LastRun: "2026-07-31", YM: "2026-07", Signal: july, FXRate: 1420.6, ApplyYM: "2026-08", JudgmentCount: 1}
}

func hasLine(fails []string, needle string) bool {
	for _, f := range fails {
		if strings.Contains(f, needle) {
			return true
		}
	}
	return false
}

// ── 1. preflight — 검사가 실제로 검사인가 ─────────────────────────────

// 표본 수를 상수에서 옮겨 담으면 자기 자신과의 비교라 무엇도 못 잡는다. 원자료에서 센 값이어야 한다.
func TestSampleCountsAreMeasuredNotRestated(t *testing.T) {
	in := julyMaster(t).SignalInputs()
	pos := julyPos(invest.Account{})
	c, err := buildCtx(execDay, pos, pos.Holdings, in, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if c.SMASampleCount != 185 || c.VolSampleCount != 21 { // = SMAPeriod · VolWindow
		t.Fatalf("표본 %d/%d", c.SMASampleCount, c.VolSampleCount)
	}
	// 원자료가 모자라면 값이 따라 움직여야 한다 — 상수였다면 그대로 185/21이다
	early := pos
	early.LastRun = "1985-10-15"
	c2, err := buildCtx(execDay, early, early.Holdings, in, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if c2.SMASampleCount >= 185 || c2.VolSampleCount >= 21 {
		t.Fatalf("표본 수를 세지 않고 상수를 넣고 있다: %d/%d", c2.SMASampleCount, c2.VolSampleCount)
	}
	f := invest.Preflight(c2)
	if !hasLine(f, "SMA 표본") || !hasLine(f, "변동성 표본") {
		t.Fatalf("%v", f)
	}
}

// `len(...) and 0 or 0`은 어떤 입력에도 0이다 — 빈도 검사가 통째로 죽어 있었다.
func TestSwitchCountIsReal(t *testing.T) {
	all := julyMaster(t)
	pos := julyPos(invest.Account{})
	want := func(in invest.SignalInputs) int {
		log, err := invest.MonthlySignals(in, invest.SignalStart)
		if err != nil {
			t.Fatal(err)
		}
		n := 0
		for i := len(log) - 12; i < len(log); i++ {
			if log[i].Pos != log[i-1].Pos {
				n++
			}
		}
		return n
	}
	// 2020년 말에서 자른 마스터 — 코로나 전환이 들어 있어 0이 아니다
	var cut invest.Prices
	for _, r := range all {
		if !r.Day.After(invest.Date(2020, 12, 31)) {
			cut = append(cut, r)
		}
	}
	nonzero := false
	for _, in := range []invest.SignalInputs{all.SignalInputs(), cut.SignalInputs()} {
		c, err := buildCtx(execDay, pos, pos.Holdings, in, nil, "")
		if err != nil {
			t.Fatal(err)
		}
		w := want(in)
		if c.Switches12M != w {
			t.Fatalf("12개월 전환 %d, 실측 %d", c.Switches12M, w)
		}
		nonzero = nonzero || w > 0
	}
	if !nonzero {
		t.Fatal("전환이 있는 구간으로 검사하지 못했다")
	}
}

// 세 항목이 죽어 있던 자리 — 값을 망가뜨리면 반드시 걸려야 한다. 깨끗한 실데이터는 통과한다.
func TestPreflightFiresOnEachGuardedValue(t *testing.T) {
	in := julyMaster(t).SignalInputs()
	held := invest.Account{Symbol: "TQQQ", Shares: 10, CostKRW: 1e6}
	pos := julyPos(invest.Account{})
	clean, err := buildCtx(execDay, pos, pos.Holdings, in, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if f := invest.Preflight(clean); len(f) != 0 {
		t.Fatalf("깨끗한 맥락이 막혔다: %v", f)
	}
	cases := []struct {
		needle string
		mut    func(c *invest.PreflightCtx)
	}{
		{"SMA 표본", func(c *invest.PreflightCtx) { c.SMASampleCount = 3 }},
		{"변동성 표본", func(c *invest.PreflightCtx) { c.VolSampleCount = 3 }},
		{"12개월 전환", func(c *invest.PreflightCtx) { c.Switches12M = 99 }},
		{"SPX 이상치", func(c *invest.PreflightCtx) { c.SPX = 12 }},
		{"변동성 이상치", func(c *invest.PreflightCtx) { c.Vol = 9.9 }},
		{"룩어헤드", func(c *invest.PreflightCtx) { c.SignalDate = d(8, 20) }},
	}
	for _, k := range cases {
		c := clean
		k.mut(&c)
		if f := invest.Preflight(c); !hasLine(f, k.needle) {
			t.Errorf("%s: %v", k.needle, f)
		}
	}
	// 적용월 밖은 막는다
	out, _ := buildCtx(d(9, 1), pos, pos.Holdings, in, nil, "")
	if f := invest.Preflight(out); !hasLine(f, "적용월") {
		t.Errorf("적용월 밖: %v", f)
	}
	// 보유하지 않은 종목의 매도 레그, 보유=목표인데 매도 레그 (§6 주문 온전성)
	ph, _ := buildCtx(execDay, julyPos(held), held, in, []string{"QLD"}, "")
	if f := invest.Preflight(ph); !hasLine(f, "보유하지 않은 종목") {
		t.Errorf("유령 매도: %v", f)
	}
	pt, _ := buildCtx(execDay, julyPos(held), held, in, []string{"TQQQ"}, "")
	if f := invest.Preflight(pt); !hasLine(f, "불필요한 전환") {
		t.Errorf("불필요한 전환: %v", f)
	}
}

// 🔴 늦은 매수는 규칙 위반이나 미매수보다 낫다. 막을 것은 지연이 아니라 이탈이다.
func TestLateExecutionInsideApplyMonthIsAllowed(t *testing.T) {
	pos := julyPos(invest.Account{})
	late := d(8, 12)
	c, err := buildCtx(late, pos, pos.Holdings, julyMaster(t).SignalInputs(), nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if f := invest.Preflight(c); len(f) != 0 {
		t.Fatalf("늦은 매수를 막았다: %v", f)
	}
	if c.LateDays != daysBetween(execDay, late) || c.LateDays != 9 {
		t.Fatalf("지연 %d일", c.LateDays)
	}
}

// ── 2. judge ─────────────────────────────────────────────────────────

// 같은 적용월을 다시 판정하면 체결 기록은 남아야 한다 (재실행 안전성).
func TestJudgeKeepsExecutionMarkerWhenApplyMonthIsUnchanged(t *testing.T) {
	h := newHarness(t, seedOpt{executedOn: "2026-08-03", executedYM: "2026-08"})
	if rc := h.judge("2026-07-31"); rc != 0 {
		t.Fatalf("rc %d %s", rc, h.err)
	}
	p := h.pos()
	if p.ExecutedOn == nil || *p.ExecutedOn != "2026-08-03" || p.ExecutedYM == nil || *p.ExecutedYM != "2026-08" {
		t.Fatalf("같은 적용월인데 체결 기록이 사라졌다: %+v", p)
	}
}

// 다음 달 첫 장의 종가가 들어온 뒤의 만회는 룩어헤드다 — 판정하지 않고 실패한다.
func TestCatchUpStopsOnceNextMonthHasAClose(t *testing.T) {
	h := newHarness(t, seedOpt{ym: "2026-06", applyYM: "2026-07", executedOn: "2026-07-01", executedYM: "2026-07"})
	row, _ := julyMaster(t).Last()
	row.Day = d(8, 3)
	if _, err := h.st.InsertPrice(h.ctx, row); err != nil {
		t.Fatal(err)
	}
	if rc := h.judge("2026-08-03"); rc != 1 {
		t.Fatalf("룩어헤드 만회가 통과했다: rc %d", rc)
	}
	if h.pos().YM != "2026-06" {
		t.Fatal("실패했는데 판정을 썼다")
	}
}

// 직전 거래일이 월말일 때만 만회한다 — 8/4엔 7월을 다시 열지 않는다.
func TestCatchUpWindowIsASingleSession(t *testing.T) {
	days := julyMaster(t).SignalInputs().Days
	if g := JudgmentDay(d(8, 4), days, "2026-06"); !g.IsZero() {
		t.Fatalf("만회 창이 하루를 넘었다: %s", invest.FormatDay(g))
	}
}

// 🔴 진행 중인 달은 마스터 마지막 행이 곧 그 달의 최댓값이다 — 마스터가 멈춘 금요일은 월말이 아니다.
func TestWhereTheMasterStopsIsNotAMonthEnd(t *testing.T) {
	var fri []time.Time
	for _, day := range julyMaster(t).SignalInputs().Days {
		if !day.After(d(7, 24)) {
			fri = append(fri, day)
		}
	}
	if g := JudgmentDay(d(7, 24), fri, "2026-06"); !g.IsZero() {
		t.Fatal("마스터가 멈춘 7/24를 판정일로 믿었다")
	}
	if g := JudgmentDay(d(7, 31), fri, "2026-06"); !g.Equal(d(7, 31)) {
		t.Fatalf("데이터가 없는 달은 달력이 답해야 한다: %s", invest.FormatDay(g))
	}
}

// 판정은 fx_rate를 이어받는다 — 그 환율이 추정인지도 함께 이어받아야 한다.
func TestJudgingDoesNotTurnAnEstimateIntoAFact(t *testing.T) {
	h := newHarness(t, seedOpt{ym: "2026-06", applyYM: "2026-07", executedOn: "2026-07-01", executedYM: "2026-07"})
	h.update(func(p *invest.Position) { p.FXEstimated = true })
	if rc := h.judge("2026-07-31"); rc != 0 {
		t.Fatalf("rc %d %s", rc, h.err)
	}
	if p := h.pos(); p.YM != "2026-07" || !p.FXEstimated || p.FXRate != 1420.6 {
		t.Fatalf("판정이 추정 표시를 버렸다: %+v", p)
	}
}

// ── 3. execute ───────────────────────────────────────────────────────

// 어느 달의 체결인지 남기고, 수량·원금·환율은 주지 않으면 종전 규칙(계산값·계획 금액·확정 환율)을 쓴다.
func TestExecuteRecordsTheMonthItBelongsTo(t *testing.T) {
	h := newHarness(t, seedOpt{})
	h.executeJuly()
	p := h.pos()
	if p.ExecutedOn == nil || *p.ExecutedOn != "2026-08-03" || p.ExecutedYM == nil || *p.ExecutedYM != "2026-08" {
		t.Fatalf("%+v", p)
	}
	if p.MonthsInvested != 1 || p.ContributedKRW != 600000 || p.Holdings.Shares != 6 {
		t.Fatalf("months %d contributed %v shares %d", p.MonthsInvested, p.ContributedKRW, p.Holdings.Shares)
	}
	// --fx를 넣었으면 확정이다 — 안 꺼지는 경고는 무시된다
	if p.FXEstimated || p.FXRate != 1420.6 {
		t.Fatalf("준 환율이 확정으로 남지 않았다: %v %v", p.FXEstimated, p.FXRate)
	}
}

// 미실행으로 기록된 달은 사후 체결 기록을 받지 않는다 (actual_target이 없다).
func TestExecuteRefusesAfterAMissedMonthWasRecorded(t *testing.T) {
	h := newHarness(t, seedOpt{})
	if _, err := h.st.RecordMissedMonth(h.ctx, "2026-08", "TQQQ", d(8, 31)); err != nil {
		t.Fatal(err)
	}
	if rc := h.execute(map[string]float64{"TQQQ": 64.62}, "2026-08-31", nil, nil); rc != 1 || !strings.Contains(h.err.String(), "미실행 이탈") {
		t.Fatalf("rc %d %s", rc, h.err)
	}
	if h.pos().ExecutedYM != nil {
		t.Fatal("미실행 달에 체결을 기록했다")
	}
}

// 🔴 손으로 산 달은 사람이 정답이다 — 적립액에서 다시 계산하면 증권사가 채워 준 것과 갈린다.
func TestExecuteRecordsTheQuantityAPersonActuallyBought(t *testing.T) {
	h := newHarness(t, seedOpt{})
	if rc := h.execute(map[string]float64{"TQQQ": 64.62}, "2026-08-03", ip(3), nil); rc != 0 {
		t.Fatalf("rc %d %s", rc, h.err)
	}
	p := h.pos().Holdings
	if p.Shares != 3 || p.CashUSD <= 0 {
		t.Fatalf("계산값(6주)이 아니라 실제 수량이어야 하고 남은 돈은 이월된다: %+v", p)
	}
}

// 🔴 원금은 계획 금액이 아니라 실제로 넣은 돈이다 (실측 2026-08: 계획 60만 · 실제 244.7만이 +320%로 떴다).
func TestExecuteRecordsTheMoneyAPersonActuallyPutIn(t *testing.T) {
	h := newHarness(t, seedOpt{})
	if rc := h.execute(map[string]float64{"TQQQ": 64.62}, "2026-08-03", ip(23), fp(2447582)); rc != 0 {
		t.Fatalf("rc %d %s", rc, h.err)
	}
	p := h.pos()
	if !near(p.ContributedKRW, 2447582, 1e-6) || p.Holdings.Shares != 23 {
		t.Fatalf("%+v", p)
	}
	// 🔴 계획 금액으로 환전했으면 23주를 살 돈이 없어 현금이 음수가 된다
	if p.Holdings.CashUSD < 0 {
		t.Fatalf("계획 금액으로 사서 현금이 음수가 됐다: %v", p.Holdings.CashUSD)
	}
	// 원장도 같은 금액을 봐야 한다 — ledger가 총액과 대조한다
	sum := 0.0
	for _, r := range h.ledger() {
		sum += r.AmountKRW
	}
	if !near(sum, 2447582, 1e-6) {
		t.Fatalf("원금 내역 합 %v", sum)
	}
}

// 0원을 넣었다는 기록은 수익률의 분모를 비운다. 음수 수량도 받지 않는다.
func TestExecuteRefusesANonPositivePaidAmountAndANegativeShareCount(t *testing.T) {
	h := newHarness(t, seedOpt{})
	if rc := h.execute(map[string]float64{"TQQQ": 64.62}, "2026-08-03", nil, fp(0)); rc != 1 {
		t.Fatal("0원 투입을 받았다")
	}
	if rc := h.execute(map[string]float64{"TQQQ": 64.62}, "2026-08-03", ip(-1), nil); rc != 1 {
		t.Fatal("음수 수량을 받았다")
	}
	if h.pos().ExecutedYM != nil {
		t.Fatal("거부했는데 달이 잠겼다")
	}
}

// 🔴 0.649를 치면 (실제 $64.90) 장부가 649주가 된다 — ±25% 밖이면 시장이 아니라 손가락이다.
func TestExecuteRejectsAFatFingerFillPrice(t *testing.T) {
	h := newHarness(t, seedOpt{})
	for _, px := range []float64{0.649, 6490} { // 1/100, 100배 오타
		if rc := h.execute(map[string]float64{"TQQQ": px}, "2026-08-03", nil, nil); rc != 1 {
			t.Fatalf("%v 오타를 받았다", px)
		}
	}
	if h.pos().ExecutedYM != nil {
		t.Fatal("오타 거부 뒤 달이 잠겼다")
	}
	h.executeJuly() // 정상가는 통과
	if h.pos().Holdings.Shares != 6 {
		t.Fatalf("shares %d", h.pos().Holdings.Shares)
	}
}

// ── 4. deviate — 확정한 뒤에 실제로 살 수 있는가 ──────────────────────

// 🔴 확정해 놓고 살 수 없으면 그 달을 통째로 잃는다 — 수동 기록 경로도 열려 있어야 한다.
func TestAConfirmedDeviationCanActuallyBeExecuted(t *testing.T) {
	h := newHarness(t, seedOpt{})
	if h.deviate(d(8, 3), "QQQM") != 0 || h.deviate(d(8, 4), "QQQM") != 0 {
		t.Fatalf("이탈 확정 실패: %s", h.err)
	}
	if rc := h.execute(map[string]float64{"QQQM": 283.29}, "2026-08-04", nil, nil); rc != 0 {
		t.Fatalf("확정한 이탈을 기록하지 못했다: rc %d %s", rc, h.err)
	}
	p := h.pos()
	if p.ExecutedYM == nil || *p.ExecutedYM != "2026-08" || p.LateDays == nil || *p.LateDays != 1 {
		t.Fatalf("%+v", p)
	}
}

// 🔴 이탈은 대개 들고 있는 것을 파는 모양이다 — 매도 레그가 '불필요한 전환'으로 막히면 안 된다.
func TestADeviationThatSellsTheSignalSymbolCanBeExecuted(t *testing.T) {
	h := newHarness(t, seedOpt{acct: invest.Account{Symbol: "TQQQ", Shares: 10, CostKRW: 900_000}})
	h.deviate(d(8, 3), "QQQM")
	h.deviate(d(8, 4), "QQQM")
	if rc := h.execute(map[string]float64{"QQQM": 283.29, "TQQQ": 64.90}, "2026-08-04", nil, nil); rc != 0 {
		t.Fatalf("rc %d %s", rc, h.err)
	}
	if p := h.pos(); p.Holdings.Symbol != "QQQM" || p.ExecutedYM == nil || *p.ExecutedYM != "2026-08" {
		t.Fatalf("%+v", p)
	}
}

// 자동 매매 경로도 같은 자리에서 막혀 있었다 — 매도조차 못 냈다.
func TestTradeCanExecuteADeviationThatSellsTheSignalSymbol(t *testing.T) {
	h := newHarness(t, seedOpt{acct: invest.Account{Symbol: "TQQQ", Shares: 10, CostKRW: 900_000}})
	h.fb.held["TQQQ"] = 10
	h.deviate(d(8, 3), "QQQM")
	h.deviate(d(8, 4), "QQQM")
	if rc := h.trade(d(8, 4), false); rc != 0 {
		t.Fatalf("rc %d %s", rc, h.err)
	}
	if p := h.fb.placed; len(p) != 1 || p[0].Symbol != "TQQQ" || p[0].Side != broker.Sell {
		t.Fatalf("%+v", p)
	}
}

// 주문서가 신호 종목을 그리면 trade가 실제로 낼 주문과 갈린다.
func TestTheOrderSheetShowsTheDeviatedSymbol(t *testing.T) {
	h := newHarness(t, seedOpt{acct: invest.Account{Symbol: "TQQQ", Shares: 10, CostKRW: 900_000}})
	h.deviate(d(8, 3), "QQQM")
	h.deviate(d(8, 4), "QQQM")
	h.out.Reset()
	if rc := h.app.Order(h.ctx, OrderOpts{Date: "2026-08-04"}); rc != 0 {
		t.Fatalf("rc %d %s %s", rc, h.out, h.err)
	}
	if out := h.out.String(); !strings.Contains(out, "목표      QQQM") || !strings.Contains(out, "이탈 확정") {
		t.Fatalf("%s", out)
	}
}

// 적용월의 판정일이 이미 지났으면 다음 판정일로 민다 — 아니면 확정 자체가 막힌다.
func TestDeviationLockNeverPrecedesToday(t *testing.T) {
	h := newHarness(t, seedOpt{})
	last := invest.LastTradingDay(2026, 8)
	if h.deviate(last, "QQQM") != 0 || h.deviate(last.AddDate(0, 0, 1), "QQQM") != 0 {
		t.Fatalf("%s", h.err)
	}
	devs := h.devs()
	if len(devs) != 1 {
		t.Fatalf("%+v", devs)
	}
	if lock, _ := invest.ParseDay(devs[0].LockedUntil); !lock.After(last) {
		t.Fatalf("잠금 %s", devs[0].LockedUntil)
	}
}

func TestCancelClearsAPendingRequest(t *testing.T) {
	h := newHarness(t, seedOpt{})
	h.deviate(d(8, 3), "QQQM")
	if p, _ := h.st.LoadPending(h.ctx); p == nil {
		t.Fatal("요청이 접수되지 않았다")
	}
	if rc := h.app.Cancel(h.ctx); rc != 0 {
		t.Fatalf("rc %d", rc)
	}
	if p, _ := h.st.LoadPending(h.ctx); p != nil {
		t.Fatalf("취소했는데 남았다: %+v", p)
	}
}

// ── 5. status ────────────────────────────────────────────────────────

// 체결일만 보면 지난달 매수를 이번 달 것으로 세고 미체결이 영영 안 잡힌다.
func TestStatusIgnoresLastMonthsExecutionMarker(t *testing.T) {
	h := newHarness(t, seedOpt{executedOn: "2026-07-01", executedYM: "2026-07"})
	if rc := h.app.Status(h.ctx, StatusOpts{Date: "2026-08-20"}); rc != 1 {
		t.Fatal("지난달 체결을 이번 달 매수로 셌다")
	}
}

// 🔴 기록만 하고 아무도 안 읽으면 없는 것과 같다 (§12.1.4).
func TestStatusSaysWhenTheFXRateIsStillAnEstimate(t *testing.T) {
	h := newHarness(t, seedOpt{})
	h.update(func(p *invest.Position) { p.FXEstimated = true })
	h.app.Status(h.ctx, StatusOpts{Date: "2026-08-10"})
	if out := h.out.String(); !strings.Contains(out, "추정") || !strings.Contains(out, "amend") {
		t.Fatalf("%s", out)
	}
	h.update(func(p *invest.Position) { p.FXEstimated = false })
	h.out.Reset()
	h.app.Status(h.ctx, StatusOpts{Date: "2026-08-10"})
	if out := h.out.String(); !strings.Contains(out, "확정") {
		t.Fatalf("%s", out)
	}
}

// ── 6. refresh ───────────────────────────────────────────────────────

type fakeYahoo struct {
	calls int
	next  func(call int) (map[string][]yahoo.Bar, error)
}

func (f *fakeYahoo) DailyCloses(context.Context, []string, time.Time) (map[string][]yahoo.Bar, error) {
	f.calls++
	return f.next(f.calls)
}

type yrow struct {
	day                                 time.Time
	spx, ndx, qqq, qqqm, qld, tqqq, krw float64
}

func bars(rows ...yrow) map[string][]yahoo.Bar {
	out := map[string][]yahoo.Bar{}
	for _, r := range rows {
		for sym, v := range map[string]float64{"^GSPC": r.spx, "^NDX": r.ndx, "QQQ": r.qqq, "QQQM": r.qqqm, "QLD": r.qld, "TQQQ": r.tqqq, invest.FXYahoo: r.krw} {
			out[sym] = append(out[sym], yahoo.Bar{Day: r.day, Close: v})
		}
	}
	return out
}

// julyLast는 고정 마스터 마지막 행이다 — 앵커 대조가 통과한다.
var julyLast = yrow{d(7, 31), 7489.72021484375, 28274.19921875, 687.99, 283.29, 83.67, 64.62, 1420.6}

type refreshRig struct {
	*harness
	y     *fakeYahoo
	slept []time.Duration
}

// refreshRig는 마스터 마지막 행(7/31) 뒤를 야후가 채우는 상황이다. 지금은 8/4 장 마감 뒤.
func newRefreshRig(t *testing.T, next func(call int) (map[string][]yahoo.Bar, error)) *refreshRig {
	r := &refreshRig{harness: newHarness(t, seedOpt{}), y: &fakeYahoo{next: next}}
	r.app.Prices = r.y
	r.app.Sleep = func(d time.Duration) { r.slept = append(r.slept, d) }
	r.app.Now = func() time.Time { return time.Date(2026, 8, 5, 12, 0, 0, 0, invest.NewYork) }
	return r
}

func (r *refreshRig) master() invest.Prices {
	r.t.Helper()
	p, err := r.st.LoadPrices(r.ctx)
	if err != nil {
		r.t.Fatal(err)
	}
	return p
}

func (r *refreshRig) refresh(retries int, wait time.Duration) int {
	return r.app.Refresh(r.ctx, RefreshOpts{Retries: retries, Wait: wait})
}

func unchanged(t *testing.T, before, after invest.Prices) {
	t.Helper()
	bl, _ := before.Last()
	al, _ := after.Last()
	if len(after) != len(before) || !al.Day.Equal(bl.Day) {
		t.Fatalf("마스터에 썼다: %d행 → %d행 (%s)", len(before), len(after), invest.FormatDay(al.Day))
	}
}

func complete(t *testing.T, row invest.PriceRow) {
	t.Helper()
	for _, c := range invest.Columns {
		if math.IsNaN(row.Get(c.Col)) {
			t.Fatalf("%s %s 결측", invest.FormatDay(row.Day), c.Col)
		}
	}
}

// holesThenFull은 처음 failTimes번은 ETF 당일 종가가 비고, 그 뒤엔 채워지는 야후다.
func holesThenFull(failTimes int) func(int) (map[string][]yahoo.Bar, error) {
	nan := math.NaN()
	full := yrow{d(8, 3), 7500, 28300, 688.5, 283.5, 83.9, 64.9, 1421}
	late := full
	late.qqq, late.qqqm, late.qld, late.tqqq = nan, nan, nan, nan // 2026-09-26 모양
	return func(call int) (map[string][]yahoo.Bar, error) {
		if call <= failTimes {
			return bars(julyLast, late), nil
		}
		return bars(julyLast, full), nil
	}
}

// 🔴 SPX·NDX만 검사한 탓에 TQQQ가 64거래일 비었다. 쓰지 않고 멈춰야 다음 실행이 같은 날짜를 다시 받는다.
func TestRefreshRefusesToWriteRowsWithMissingTickers(t *testing.T) {
	nan := math.NaN()
	r := newRefreshRig(t, func(int) (map[string][]yahoo.Bar, error) {
		return bars(julyLast,
			yrow{d(8, 3), 7500, 28300, 688.5, 283.5, 83.9, nan, 1421}, // 과거 사고와 같은 모양
			yrow{d(8, 4), 7510, 28350, 689, 283.9, 84.1, nan, 1422}), nil
	})
	before := r.master()
	if rc := r.refresh(0, 0); rc != ExitRetry {
		t.Fatalf("결측이 있는데 rc %d: %s", rc, r.err)
	}
	unchanged(t, before, r.master())
	// 2026-09-26엔 "TQQQ 1일"뿐이라 어느 날인지 추정해야 했다
	if !strings.Contains(r.err.String(), "TQQQ 2일 (2026-08-03, 2026-08-04)") {
		t.Fatalf("%s", r.err)
	}
}

// 🔴 2026-09-26 ETF 종가가 늦게 올라와 주간 갱신이 멈췄다. 재시도는 가드를 끄지 않고 채워질 때까지 기다린다.
func TestRegression202609RefreshWaitsOutALateETFClose(t *testing.T) {
	r := newRefreshRig(t, holesThenFull(2))
	before := len(r.master())
	if rc := r.refresh(3, 900*time.Second); rc != 0 {
		t.Fatalf("rc %d %s", rc, r.err)
	}
	if r.y.calls != 3 || fmt.Sprint(r.slept) != "[15m0s 15m0s]" {
		t.Fatalf("calls %d slept %v", r.y.calls, r.slept)
	}
	after := r.master()
	if len(after) != before+1 {
		t.Fatalf("%d행 → %d행", before, len(after))
	}
	last, _ := after.Last()
	complete(t, last)
}

func TestRefreshGivesUpAfterTheLastRetryWithoutWriting(t *testing.T) {
	r := newRefreshRig(t, holesThenFull(99))
	before := r.master()
	if rc := r.refresh(2, 5*time.Second); rc != ExitRetry {
		t.Fatalf("rc %d", rc)
	}
	// 마지막 뒤엔 기다리지 않는다
	if r.y.calls != 3 || fmt.Sprint(r.slept) != "[5s 5s]" {
		t.Fatalf("calls %d slept %v", r.y.calls, r.slept)
	}
	unchanged(t, before, r.master())
}

// 앵커 오차는 기다려도 안 풀린다 — 재시도로 시간만 태우지 않는다.
func TestRefreshDoesNotRetryAStructuralFailure(t *testing.T) {
	off := julyLast
	off.spx *= 1.01
	r := newRefreshRig(t, func(int) (map[string][]yahoo.Bar, error) {
		return bars(off, yrow{d(8, 3), 7500, 28300, 688.5, 283.5, 83.9, 64.9, 1421}), nil
	})
	if rc := r.refresh(3, 900*time.Second); rc != 1 {
		t.Fatalf("rc %d", rc)
	}
	if r.y.calls != 1 || len(r.slept) != 0 {
		t.Fatalf("calls %d slept %v", r.y.calls, r.slept)
	}
}

func TestRefreshRetriesWhenYahooFails(t *testing.T) {
	full := holesThenFull(0)
	r := newRefreshRig(t, func(call int) (map[string][]yahoo.Bar, error) {
		if call == 1 {
			return nil, fmt.Errorf("%w: rate limited", yahoo.ErrTemporary)
		}
		return full(call)
	})
	if rc := r.refresh(1, 60*time.Second); rc != 0 {
		t.Fatalf("rc %d %s", rc, r.err)
	}
	if fmt.Sprint(r.slept) != "[1m0s]" {
		t.Fatalf("slept %v", r.slept)
	}
}

func TestRefreshWritesWhenEveryTickerIsPresent(t *testing.T) {
	r := newRefreshRig(t, holesThenFull(0))
	before := len(r.master())
	if rc := r.refresh(0, 0); rc != 0 {
		t.Fatalf("rc %d %s", rc, r.err)
	}
	after := r.master()
	if len(after) != before+1 {
		t.Fatalf("%d행 → %d행", before, len(after))
	}
	last, _ := after.Last()
	complete(t, last)
}

// stageRefresh는 7/31 뒤로 a·b 두 거래일을 붙인 야후다.
func stageRefresh(a, b time.Time) func(int) (map[string][]yahoo.Bar, error) {
	return func(int) (map[string][]yahoo.Bar, error) {
		return bars(julyLast,
			yrow{a, 7800, 30100, 733, 301.5, 94.5, 77.2, 1416},
			yrow{b, 7801, 30101, 734, 302.5, 95.5, 78.2, 1416}), nil
	}
}

func days(p invest.Prices) map[time.Time]bool {
	out := map[time.Time]bool{}
	for _, r := range p {
		out[r.Day] = true
	}
	return out
}

// 🔴 2026-08-13 장중 스냅샷이 굳어 주간 갱신이 2주간 죽었다 — 스스로 회복되지 않는다.
func TestRefreshNeverWritesAnUnfinishedSession(t *testing.T) {
	a, b := d(8, 3), d(8, 4)
	r := newRefreshRig(t, stageRefresh(a, b))
	r.app.Now = func() time.Time { return time.Date(2026, 8, 4, 12, 0, 0, 0, invest.NewYork) } // b일 장중
	if rc := r.refresh(0, 0); rc != 0 {
		t.Fatalf("rc %d %s", rc, r.err)
	}
	got := days(r.master())
	if !got[a] {
		t.Fatal("종가가 확정된 날을 빼먹었다")
	}
	if got[b] {
		t.Fatal("장중 시세를 종가로 썼다")
	}
}

// 항상 버리기만 하는 가드면 월말 판정일 종가가 안 들어온다 (§6.1).
func TestRefreshWritesTheSessionOnceItCloses(t *testing.T) {
	a, b := d(8, 3), d(8, 4)
	r := newRefreshRig(t, stageRefresh(a, b))
	r.app.Now = func() time.Time { return time.Date(2026, 8, 4, 16, 30, 0, 0, invest.NewYork) } // b일 마감 후
	if rc := r.refresh(0, 0); rc != 0 {
		t.Fatalf("rc %d %s", rc, r.err)
	}
	if got := days(r.master()); !got[a] || !got[b] {
		t.Fatal("마감했는데도 안 받았다 — 그 달 판정이 유실된다")
	}
}

// ── 7. 우회 경로가 생기지 않았는가 ───────────────────────────────────

// --skip-preflight · --force 류는 만들지 않는다 (claude.md §4.5).
func TestNoBypassFlagsExist(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	files = append(files, filepath.Join("..", "..", "..", "cmd", "myhome", "invest.go"))
	n := 0
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		n++
		src := string(b)
		for _, flag := range []string{"skip-preflight", "force", "no-verify", "yes"} {
			if strings.Contains(src, `"`+flag+`"`) || strings.Contains(src, "--"+flag) {
				t.Errorf("%s: 우회 옵션 %s가 생겼다", f, flag)
			}
		}
	}
	if n < 2 {
		t.Fatalf("검사한 파일이 %d개뿐이다", n)
	}
}

// ── 8. 최초 설정 — 운용 중인 계좌를 덮어쓰지 않는다 ──────────────────

// 운용 중인 계좌를 초기화하는 것은 되돌릴 수 없는 사고다 (TQ init → Go import-tq).
func TestImportNeverOverwritesALiveAccount(t *testing.T) {
	h := newHarness(t, seedOpt{acct: invest.Account{Symbol: "TQQQ", Shares: 205, CostKRW: 28_000_000}})
	before := h.pos()
	dir := t.TempDir()
	if rc := h.app.ImportTQ(h.ctx, ImportOpts{StateDir: dir, MasterCSV: filepath.Join(dir, "master.csv")}); rc != 1 {
		t.Fatalf("rc %d", rc)
	}
	if after := h.pos(); !reflect.DeepEqual(after, before) {
		t.Fatalf("거부했는데 계좌가 바뀌었다: %+v", after)
	}
}

// ── 9. amend ─────────────────────────────────────────────────────────

// 정정은 체결 직전 지점에서 다시 계산한다. 두 번 고쳐도 적립이 겹치면 안 된다.
func TestAmendCorrectsPriceWithoutDoubleCounting(t *testing.T) {
	h := newHarness(t, seedOpt{})
	h.executeJuly()
	first := h.pos()
	if rc := h.amend("2026-08-05", map[string]float64{"TQQQ": 65.90}, 1425.0); rc != 0 {
		t.Fatalf("rc %d %s", rc, h.err)
	}
	if rc := h.amend("2026-08-06", map[string]float64{"TQQQ": 64.90}, 1421.0); rc != 0 {
		t.Fatalf("rc %d %s", rc, h.err)
	}
	p := h.pos()
	// 🔴 원금을 다시 매기되 쌓지 않는다 — 원금은 실제 환전액이라 fx가 바뀌면 같이 바뀐다
	if p.ContributedKRW <= 0 || p.ContributedKRW >= 2*600000 {
		t.Fatalf("정정이 원금을 겹쳐 쌓았다: %v", p.ContributedKRW)
	}
	if p.MonthsInvested != 1 || first.MonthsInvested != 1 || p.AmendCount != 2 {
		t.Fatalf("months %d amend %d", p.MonthsInvested, p.AmendCount)
	}
	if *p.ExecutedOn != *first.ExecutedOn { // 날짜는 그대로
		t.Fatalf("체결일이 바뀌었다: %s", *p.ExecutedOn)
	}
	if p.Holdings.CashUSD == first.Holdings.CashUSD {
		t.Fatal("정정 단가가 반영되지 않았다")
	}
}

func TestAmendWithoutAnExecutionIsRefused(t *testing.T) {
	h := newHarness(t, seedOpt{})
	if rc := h.amend("2026-08-05", map[string]float64{"TQQQ": 64.90}, 1421); rc != 1 {
		t.Fatal("체결 기록 없이 정정했다")
	}
}

// 복원 지점 없이 역산하면 틀린 계좌가 맞는 것처럼 남는다.
func TestAmendRefusesWhenThereIsNoRestorePoint(t *testing.T) {
	h := newHarness(t, seedOpt{acct: invest.Account{Symbol: "TQQQ", Shares: 6, CostKRW: 551_345}, executedOn: "2026-08-03", executedYM: "2026-08"})
	if rc := h.amend("2026-08-05", map[string]float64{"TQQQ": 64.90}, 1421); rc != 1 {
		t.Fatal("복원 지점 없이 정정했다")
	}
}

// 같은 지점에서 몇 번이든 다시 계산할 수 있어야 한다.
func TestAmendPreservesTheRestorePoint(t *testing.T) {
	h := newHarness(t, seedOpt{})
	h.executeJuly()
	snap := h.pos().Before
	if rc := h.amend("2026-08-05", map[string]float64{"TQQQ": 65.90}, 1421); rc != 0 {
		t.Fatalf("rc %d %s", rc, h.err)
	}
	if got := h.pos().Before; !reflect.DeepEqual(got, snap) {
		t.Fatalf("복원 지점이 바뀌었다: %+v → %+v", snap, got)
	}
}

func TestAmendRejectsTheSameTypo(t *testing.T) {
	h := newHarness(t, seedOpt{})
	h.executeJuly()
	if rc := h.amend("2026-08-04", map[string]float64{"TQQQ": 6462}, 1418); rc != 1 {
		t.Fatal("100배 오타로 정정했다")
	}
	if h.pos().Holdings.Shares != 6 {
		t.Fatal("거부했는데 장부가 바뀌었다")
	}
}

// 🔴 amend는 단가·환율만 고친다 — 수량은 증권사가 정답이고 값만 새 단가로 다시 매긴다.
func TestAmendRepricesWithoutChangingTheShareCount(t *testing.T) {
	h := newHarness(t, seedOpt{})
	h.executeJuly()
	before := h.pos().Holdings.Shares
	// 더 싼 단가로 정정 — 재계산이면 수량이 늘어날 상황
	if rc := h.amend("2026-08-04", map[string]float64{"TQQQ": 60.00}, 1418); rc != 0 {
		t.Fatalf("rc %d %s", rc, h.err)
	}
	p := h.pos().Holdings
	want := float64(before) * 60.00 * (1 + invest.FeeRate) * 1418.0
	if p.Shares != before || math.Abs(p.CostKRW-want) > want*1e-9 {
		t.Fatalf("shares %d→%d cost %v want %v", before, p.Shares, p.CostKRW, want)
	}
}

// 전환 달 정정에 매도 단가가 빠지면 크래시가 아니라 안내로 멈춘다.
func TestAmendOnASwitchMonthAsksForBothFills(t *testing.T) {
	h := newHarness(t, seedOpt{acct: qqqm3()})
	h.fb.held["QQQM"] = 3
	h.fb.limit = 450
	h.trade(execDay, false)
	h.fb.fill("ODNO1", 3, 283.00, "")
	h.trade(d(8, 4), false)
	h.trade(d(8, 5), false)
	h.fb.fill(h.last().OrderID, h.last().Qty, 64.90, "")
	if rc := h.trade(d(8, 6), false); rc != 0 || h.pos().Holdings.Symbol != "TQQQ" {
		t.Fatalf("전환 실패: rc %d %s", rc, h.err)
	}
	if rc := h.amend("2026-08-07", map[string]float64{"TQQQ": 64.95}, 1418); rc != 1 || !strings.Contains(h.err.String(), "QQQM") {
		t.Fatalf("매도 단가 없이 정정했다: rc %d %s", rc, h.err)
	}
	n := h.pos().Holdings.Shares
	if rc := h.amend("2026-08-07", map[string]float64{"TQQQ": 64.95, "QQQM": 282.80}, 1418); rc != 0 {
		t.Fatalf("rc %d %s", rc, h.err)
	}
	if h.pos().Holdings.Shares != n {
		t.Fatal("정정이 수량을 바꿨다")
	}
}

// ── 10. tax ──────────────────────────────────────────────────────────

// 손실 연도는 0으로 종료 — 이월공제 없음 (SPEC §2.2).
func TestTaxAppliesDeductionAndDoesNotCarryLosses(t *testing.T) {
	h := newHarness(t, seedOpt{acct: invest.Account{Symbol: "TQQQ", Shares: 6, CostKRW: 551_345,
		RealizedGainKRW: map[int]float64{2026: 1_200_000, 2027: -3_400_000, 2028: 41_000_000}}})
	if rc := h.app.Tax(h.ctx, TaxOpts{}); rc != 0 {
		t.Fatalf("rc %d %s", rc, h.err)
	}
	out := h.out.String()
	if !strings.Contains(out, "8,470,000") || !strings.Contains(out, "이월공제 없음") { // (41,000,000 − 2,500,000) × 0.22
		t.Fatalf("%s", out)
	}
	a := h.pos().Holdings
	if a.TaxDueKRW(2026) != 0 || a.TaxDueKRW(2027) != 0 || !near(a.TaxDueKRW(2028), 8_470_000, 1e-6) || !near(a.TaxLiabilityKRW(), 8_470_000, 1e-6) {
		t.Fatalf("%v %v %v %v", a.TaxDueKRW(2026), a.TaxDueKRW(2027), a.TaxDueKRW(2028), a.TaxLiabilityKRW())
	}
}

func TestTaxIsQuietBeforeTheFirstSale(t *testing.T) {
	h := newHarness(t, seedOpt{})
	if rc := h.app.Tax(h.ctx, TaxOpts{}); rc != 0 || !strings.Contains(h.out.String(), "실현손익 없음") {
		t.Fatalf("rc %d %s", rc, h.out)
	}
}

// tax --paid 2026 — 낸 세금이 평가금액에서 빠지지 않게 한다. 두 번 해도 안전하다.
func TestTaxPaidRecordsThePaymentAndClearsTheLiability(t *testing.T) {
	h := newHarness(t, seedOpt{acct: invest.Account{Symbol: "TQQQ", Shares: 10, CostKRW: 600_000, RealizedGainKRW: map[int]float64{2026: 40_000_000}}})
	if rc := h.app.Tax(h.ctx, TaxOpts{Paid: 2026}); rc != 0 {
		t.Fatalf("rc %d %s", rc, h.err)
	}
	a := h.pos().Holdings
	if a.TaxPaidKRW[2026] <= 0 || a.TaxLiabilityKRW() != 0 {
		t.Fatalf("%+v", a)
	}
	if rc := h.app.Tax(h.ctx, TaxOpts{Paid: 2026}); rc != 0 {
		t.Fatal("두 번째 납부 기록이 실패했다")
	}
}

func TestTaxPaidRefusesAYearWithNothingOwed(t *testing.T) {
	h := newHarness(t, seedOpt{acct: invest.Account{RealizedGainKRW: map[int]float64{2026: -5_000_000}}})
	if rc := h.app.Tax(h.ctx, TaxOpts{Paid: 2026}); rc != 1 {
		t.Fatal("낼 것이 없는 해를 납부로 기록했다")
	}
}

// ── 11. trade ────────────────────────────────────────────────────────

// 🔴 전환 달 — 매도대금은 이미 원금이다. 다시 세면 전환마다 원금이 쌓여 수익률이 0에 수렴한다.
func TestSaleProceedsAreNotCountedAsNewPrincipal(t *testing.T) {
	h := newHarness(t, seedOpt{acct: invest.Account{Symbol: "QQQM", Shares: 30, CostKRW: 9_000_000}, judgmentCount: 15})
	h.update(func(p *invest.Position) { p.ContributedKRW, p.MonthsInvested = 9_000_000, 15 })
	h.fb.held["QQQM"] = 30
	h.fb.limit = 500                                  // 새로 들어온 돈은 이달 적립분뿐
	h.trade(execDay, false)                           // 매도 제출
	h.fb.fill(h.fb.placed[0].OrderID, 30, 283.00, "") // $8,490 회수
	h.trade(d(8, 4), false)                           // 매도 체결 확인
	h.trade(d(8, 5), false)                           // 매수 제출
	buy := h.last()
	if buy.Side != broker.Buy {
		t.Fatalf("%+v", h.fb.placed)
	}
	h.fb.fill(buy.OrderID, buy.Qty, 64.90, "")
	if rc := h.trade(d(8, 6), false); rc != 0 {
		t.Fatalf("rc %d %s", rc, h.err)
	}
	added := h.pos().ContributedKRW - 9_000_000
	if added < 0 || added >= 2_000_000 {
		t.Fatalf("매도대금이 원금으로 다시 세어졌다 (+%.0f원). 새로 환전된 몫만 더해야 한다", added)
	}
}

func TestTradeDryRunPlacesNothing(t *testing.T) {
	h := newHarness(t, seedOpt{})
	if rc := h.trade(execDay, true); rc != 0 {
		t.Fatalf("rc %d %s", rc, h.err)
	}
	if len(h.fb.placed) != 0 || h.book() != nil {
		t.Fatalf("dry-run이 주문했다: %+v", h.fb.placed)
	}
}

// 증권사에 없는데 팔라고 하면 멈춘다 — 앱 밖에서 이미 팔린 것이다.
func TestSwitchStopsWhenTheBrokerHoldsNothing(t *testing.T) {
	// 신호가 위험회피(QQQ)로 바뀐 달, TQQQ를 들고 있다
	h := newHarness(t, seedOpt{acct: invest.Account{Symbol: "TQQQ", Shares: 6, CostKRW: 552_172}})
	h.update(func(p *invest.Position) {
		p.Signal.C2, p.Signal.Target = false, "QQQ"
		p.ContributedKRW, p.MonthsInvested = 600000, 1
	})
	h.fb.held["TQQQ"] = 0
	if rc := h.trade(execDay, false); rc != 1 || len(h.fb.placed) != 0 || !strings.Contains(h.err.String(), "보유가 없다") {
		t.Fatalf("rc %d %+v %s", rc, h.fb.placed, h.err)
	}
	h2 := newHarness(t, seedOpt{acct: qqqm3()})
	h2.fb.held["QQQM"] = 0
	if rc := h2.trade(execDay, false); rc != 1 || len(h2.fb.placed) != 0 {
		t.Fatalf("rc %d %+v", rc, h2.fb.placed)
	}
}

// 🔴 얼마를 들고 있는지 모르는 채로 팔지 않는다 — 장부만 믿고 팔면 3배 레버리지가 남는다.
func TestTradeStopsWhenTheBalanceQueryFailsBeforeSelling(t *testing.T) {
	h := newHarness(t, seedOpt{acct: qqqm3()})
	h.fb.held["QQQM"] = 3
	h.useBroker(&faultyBroker{fakeBroker: h.fb, balanceErr: errors.New("잔고 조회 불가")})
	if rc := h.trade(execDay, false); rc != 1 || len(h.fb.placed) != 0 {
		t.Fatalf("rc %d %+v", rc, h.fb.placed)
	}
}

// 얼마나 살 수 있는지 모르는 채로 주문하지 않는다.
func TestTradeRefusesToOrderWhenItCannotLearnTheLimit(t *testing.T) {
	h := newHarness(t, seedOpt{})
	h.useBroker(&faultyBroker{fakeBroker: h.fb, buyableErr: errors.New("조회 실패")})
	if rc := h.trade(execDay, false); rc != 1 || len(h.fb.placed) != 0 {
		t.Fatalf("rc %d %+v", rc, h.fb.placed)
	}
}

// 수량은 계좌에 실제로 있는 만큼이다 — 장부가 넉넉해도 계좌를 넘지 않는다.
func TestTradeAsksTheBrokerHowMuchItCanActuallyBuy(t *testing.T) {
	h := newHarness(t, seedOpt{})
	h.fb.limit = 200 // 증권사는 $200까지만
	if rc := h.trade(execDay, false); rc != 0 {
		t.Fatalf("rc %d %s", rc, h.err)
	}
	if q := h.fb.placed[0].Qty; float64(q) > math.Floor(200/64.62) {
		t.Fatalf("계좌를 넘겨 주문했다: %d주", q)
	}
	if !strings.Contains(h.out.String(), "주문가능") {
		t.Fatalf("%s", h.out)
	}
}

// 🔴 2026-08-05 규칙 변경 — 계좌에 계획보다 많으면 그만큼 산다. 계획과 다르면 반드시 남긴다.
func TestExtraMoneyInTheAccountActuallyGetsInvested(t *testing.T) {
	h := newHarness(t, seedOpt{})
	h.fb.limit = 2000 // 계획(60만원 ≈ $422)의 약 5배
	if rc := h.trade(execDay, false); rc != 0 {
		t.Fatalf("rc %d %s", rc, h.err)
	}
	if q := h.fb.placed[0].Qty; q <= 6 { // 계획만으로는 6주가 한계
		t.Fatalf("추가 입금이 놀고 있다: %d주", q)
	}
	if !strings.Contains(h.out.String(), "계좌 기준으로 산다") {
		t.Fatalf("%s", h.out)
	}
}

// 적립액은 사이징에서 빠졌다 — 1주도 못 사는 계획이어도 계좌에 돈이 있으면 산다.
func TestAPlanBelowOneShareStillOrders(t *testing.T) {
	h := newHarness(t, seedOpt{})
	if _, err := h.st.SetContribution(h.ctx, 1000, "2026-08", "계획이 사이징을 더 이상 정하지 않는지 본다", d(7, 1), ""); err != nil {
		t.Fatal(err)
	}
	h.fb.limit = 500
	if rc := h.trade(execDay, false); rc != 0 || len(h.fb.placed) == 0 {
		t.Fatalf("계좌에 돈이 있는데 주문을 안 냈다: rc %d %s", rc, h.err)
	}
}

// 🔴 승격하면 만료된 0주 주문이 장부에 남는다 — 체결로 세면 체결단가 0으로 계좌 산술이 죽는다.
func TestSettleSurvivesAStyleChangeMidwayThroughASwitch(t *testing.T) {
	h := newHarness(t, seedOpt{acct: qqqm3()})
	h.fb.held["QQQM"] = 3
	h.trade(execDay, false) // MOC 매도
	h.fb.expire()           // 한 주도 안 채워지고 만료
	if rc := h.trade(d(8, 4), false); rc != 0 || fmt.Sprint(h.fb.styles()) != "[moc market]" {
		t.Fatalf("시장가로 승격하지 않았다: rc %d %v", rc, h.fb.styles())
	}
	h.fb.fill(h.last().OrderID, 3, 283.00, "")
	if rc := h.trade(d(8, 5), false); rc != 0 { // 매도 체결
		t.Fatalf("rc %d %s", rc, h.err)
	}
	if rc := h.trade(d(8, 6), false); rc != 0 { // 매수 제출
		t.Fatalf("rc %d %s", rc, h.err)
	}
	h.fb.fill(h.last().OrderID, h.last().Qty, 64.90, "")
	if rc := h.trade(d(8, 7), false); rc != 0 { // 여기서 죽었다
		t.Fatalf("rc %d %s", rc, h.err)
	}
	a := h.pos().Holdings
	if a.Symbol != "TQQQ" || a.Shares != h.last().Qty {
		t.Fatalf("%+v", a)
	}
	// 매도 실현손익이 0단가로 계산되지 않았다
	if a.RealizedGainKRW[2026] <= -1_200_000 {
		t.Fatalf("실현손익 %v", a.RealizedGainKRW[2026])
	}
}

// 🔴 부분 체결 뒤 승격하면 증권사 보유가 그만큼 줄어 있다 — 우리가 판 것을 '앱 밖 매매'로 오진하면 안 된다.
func TestAPartialSellIsNotMistakenForOutsideTrading(t *testing.T) {
	h := newHarness(t, seedOpt{acct: qqqm3()})
	h.fb.held["QQQM"] = 3
	h.trade(execDay, false)                          // 3주 매도 제출
	h.fb.fill(h.fb.placed[0].OrderID, 2, 283.00, "") // 2주만 체결 후 만료
	h.fb.held["QQQM"] = 1                            // 증권사 보유가 줄었다
	if rc := h.trade(d(8, 4), false); rc != 0 {
		t.Fatalf("rc %d %s", rc, h.err)
	}
	if len(h.fb.placed) != 2 || h.fb.placed[1].Qty != 1 {
		t.Fatalf("남은 1주가 아니라 다시 냈다: %+v", h.fb.placed)
	}
	if strings.Contains(h.out.String(), "앱 밖 매매") {
		t.Fatalf("%s", h.out)
	}
	if b := h.book(); b.LedgerGap != nil {
		t.Fatalf("ledger_gap이 오염됐다: %d", *b.LedgerGap)
	}
}

func TestExpiryMessageShowsWhatWasAskedAndWhatFilled(t *testing.T) {
	h := newHarness(t, seedOpt{})
	h.fb.limit = 450
	h.trade(execDay, false)
	asked := h.fb.placed[0].Qty
	h.fb.fill(h.fb.placed[0].OrderID, 2, 64.00, "")
	h.trade(d(8, 4), false)
	if want := fmt.Sprintf("%d주 중 2주 체결", asked); !strings.Contains(h.out.String(), want) {
		t.Fatalf("%q 없음: %s", want, h.out)
	}
}

// --fx 없이 돌면 시장 환율 추정이다. 그 사실이 상태에 남아야 status가 읽는다.
func TestTradeMarksTheFXAsAnEstimateWhenItWasNotGiven(t *testing.T) {
	run := func(h *harness, day time.Time, fx float64) int {
		h.day = day
		return h.app.Trade(h.ctx, TradeOpts{Date: invest.FormatDay(day), FX: fx})
	}
	h := newHarness(t, seedOpt{})
	if rc := run(h, execDay, 0); rc != 0 {
		t.Fatalf("rc %d %s", rc, h.err)
	}
	h.fb.fill(h.fb.placed[0].OrderID, h.fb.placed[0].Qty, 64.90, "")
	if rc := run(h, d(8, 4), 0); rc != 0 {
		t.Fatalf("rc %d %s", rc, h.err)
	}
	if p := h.pos(); !p.FXEstimated || p.ExecutedYM == nil {
		t.Fatalf("추정 환율인데 확정으로 남았다: %+v", p)
	}
	// --fx를 주면 그것이 정답이다
	h2 := newHarness(t, seedOpt{})
	h2.trade(execDay, false)
	h2.fb.fill(h2.fb.placed[0].OrderID, h2.fb.placed[0].Qty, 64.90, "")
	h2.trade(d(8, 4), false)
	if p := h2.pos(); p.FXEstimated || p.ExecutedYM == nil {
		t.Fatalf("준 환율이 추정으로 남았다: %+v", p)
	}
}

// 체결이 계획을 넘으면 통합증거금이 추가 환전했다는 뜻이다 — 조용히 넘기지 않는다. 수량은 증권사가 정답이다.
func TestSettleWarnsWhenMoreMoneyLeftThanPlanned(t *testing.T) {
	h := newHarness(t, seedOpt{})
	h.fb.limit = 450
	h.trade(execDay, false)
	over := h.fb.placed[0].Qty + 20 // 증권사가 더 채워 준 상황
	h.fb.fill(h.fb.placed[0].OrderID, over, 64.90, "")
	if rc := h.trade(d(8, 4), false); rc != 0 {
		t.Fatalf("rc %d %s", rc, h.err)
	}
	if !strings.Contains(h.out.String(), "실제 투입") {
		t.Fatalf("%s", h.out)
	}
	if h.pos().Holdings.Shares != over {
		t.Fatalf("shares %d want %d", h.pos().Holdings.Shares, over)
	}
}

// ── 12. adjust — 앱 밖 매수를 장부에 반영 ────────────────────────────

// 🔴 증권사는 수량은 알려주지만 원화로 얼마 넣었는지는 모른다. 아는 사람이 직접 적어야 맞는다.
func TestAdjustMakesTheLedgerMatchReality(t *testing.T) {
	h := newHarness(t, seedOpt{})
	h.executeJuly() // 60만원 → 6주
	h.fb.held["TQQQ"] = 21
	if rc := h.adjust(AdjustOpts{Shares: ip(15), Paid: fp(1_400_000), Reason: adjustReason}); rc != 0 {
		t.Fatalf("rc %d %s", rc, h.err)
	}
	p := h.pos()
	if p.Holdings.Shares != 21 || p.ContributedKRW != 2_000_000 || p.Holdings.CostKRW <= 1_400_000 {
		t.Fatalf("%+v", p)
	}
}

// 🔴 사고 재현 — adjust가 계좌를 손으로 재구성하며 tax_paid_krw를 빠뜨렸다 (§12.1.1.7).
func TestAdjustKeepsTaxPaymentRecords(t *testing.T) {
	h := newHarness(t, seedOpt{acct: invest.Account{Symbol: "TQQQ", Shares: 6, CostKRW: 552_172,
		RealizedGainKRW: map[int]float64{2025: 3_000_000}, TaxPaidKRW: map[int]float64{2025: 110_000}}})
	h.fb.held["TQQQ"] = 21
	if rc := h.adjust(AdjustOpts{Shares: ip(15), Paid: fp(1_400_000), Reason: adjustReason}); rc != 0 {
		t.Fatalf("rc %d %s", rc, h.err)
	}
	a := h.pos().Holdings
	if a.Shares != 21 || !reflect.DeepEqual(a.TaxPaidKRW, map[int]float64{2025: 110_000}) || !reflect.DeepEqual(a.RealizedGainKRW, map[int]float64{2025: 3_000_000}) {
		t.Fatalf("%+v", a)
	}
}

// 수량이 안 맞으면 멈춘다 — 뭔가 더 있었다는 뜻이다.
func TestAdjustRefusesWhenTheBrokerDisagrees(t *testing.T) {
	h := newHarness(t, seedOpt{})
	h.executeJuly()
	h.fb.held["TQQQ"] = 30 // 21이 아니라 30
	if rc := h.adjust(AdjustOpts{Shares: ip(15), Paid: fp(1_400_000), Reason: adjustReason}); rc != 1 {
		t.Fatal("증권사와 안 맞는데 반영했다")
	}
	if h.pos().Holdings.Shares != 6 {
		t.Fatal("거부했는데 장부를 건드렸다")
	}
}

func TestAdjustRequiresAReason(t *testing.T) {
	h := newHarness(t, seedOpt{})
	h.executeJuly()
	h.fb.held["TQQQ"] = 21
	if rc := h.adjust(AdjustOpts{Shares: ip(15), Paid: fp(1_400_000), Reason: "추가"}); rc != 1 {
		t.Fatal("사유 없이 반영했다")
	}
	if adj, _ := h.st.LoadAdjustments(h.ctx); len(adj) != 0 {
		t.Fatalf("%+v", adj)
	}
	if h.pos().Holdings.Shares != 6 {
		t.Fatal("거부했는데 장부를 건드렸다")
	}
}

// 보유 중인 것과 다른 종목은 전량 교체 규칙 위반 — deviate의 일이다.
func TestAdjustRefusesADifferentSymbol(t *testing.T) {
	h := newHarness(t, seedOpt{})
	h.executeJuly()
	h.fb.held["TQQQ"] = 21
	if rc := h.adjust(AdjustOpts{Shares: ip(3), Paid: fp(300_000), Symbol: "QQQM", Reason: "다른 종목을 넣어보려 한다 충분히 긴 사유"}); rc != 1 {
		t.Fatal("다른 종목을 넣었다")
	}
}

// 이탈이 아니므로 deviations에 섞지 않고 따로 남긴다.
func TestAdjustIsRecordedForTheAuditTrail(t *testing.T) {
	h := newHarness(t, seedOpt{})
	h.executeJuly()
	h.fb.held["TQQQ"] = 21
	h.adjust(AdjustOpts{Shares: ip(15), Paid: fp(1_400_000), Reason: adjustReason})
	adj, _ := h.st.LoadAdjustments(h.ctx)
	if len(adj) != 1 || adj[0].Shares != 15 || adj[0].PaidKRW != 1_400_000 {
		t.Fatalf("%+v", adj)
	}
	if len(h.devs()) != 0 {
		t.Fatal("앱 밖 매수를 이탈로 셌다")
	}
}

// 🔴 조회로 채울 수 있는 것은 채운다 — 손으로 넣을 값은 매수 시점 환율 하나로 줄인다.
func TestAdjustAutoReadsQuantityAndCostFromTheBroker(t *testing.T) {
	h := newHarness(t, seedOpt{})
	h.executeJuly() // 6주
	h.fb.held["TQQQ"], h.fb.avg["TQQQ"] = 21, 64.80
	if rc := h.adjust(AdjustOpts{Auto: true, FX: 1420.6, Reason: adjustReason}); rc != 0 {
		t.Fatalf("rc %d %s", rc, h.err)
	}
	p := h.pos()
	// 추가 15주 × $64.80 × 1420.6 이 누적적립·취득원가에 더해진다
	if p.Holdings.Shares != 21 || !near(p.ContributedKRW, 600_000+15*64.80*1420.6, 1) {
		t.Fatalf("%+v", p)
	}
}

func TestAdjustAutoStopsWhenNothingWasAdded(t *testing.T) {
	h := newHarness(t, seedOpt{})
	h.executeJuly()
	h.fb.held["TQQQ"], h.fb.avg["TQQQ"] = 6, 64.62 // 늘어난 것 없음
	if rc := h.adjust(AdjustOpts{Auto: true, Reason: "늘어난 것이 없는데 넣어보려 한다"}); rc != 1 {
		t.Fatal("늘어난 것이 없는데 반영했다")
	}
}

// 평단 0은 '응답에 그 필드가 없었다'는 뜻이다. 원금 0원을 조용히 기록하면 안 된다.
func TestAdjustAutoRefusesAZeroAveragePrice(t *testing.T) {
	h := newHarness(t, seedOpt{})
	h.executeJuly()
	h.fb.held["TQQQ"], h.fb.avg["TQQQ"] = 21, 0
	// 못 읽었으면 못 읽었다고 말하고 멈춘다 — 직접 넣을 방법을 안내한다
	if rc := h.adjust(AdjustOpts{Auto: true, Reason: "평단을 못 읽는 상황에서 자동으로 채우려 한다"}); rc != 1 || !strings.Contains(h.err.String(), "매입평균가") {
		t.Fatalf("평단 0으로 반영했다: rc %d %s", rc, h.err)
	}
	if led := h.ledger(); led[len(led)-1].Source != "execute" {
		t.Fatalf("원금 내역이 늘었다: %+v", led)
	}
}

// ── ledger — 원금 내역 ───────────────────────────────────────────────

// 총액만으로는 '언제 얼마'가 복원되지 않는다. 사건 자체를 남기고 내역 합 == 누적적립이어야 한다.
func TestLedgerRecordsEveryWonThatWentIn(t *testing.T) {
	h := newHarness(t, seedOpt{})
	h.executeJuly()
	h.fb.held["TQQQ"], h.fb.avg["TQQQ"] = 21, 64.80
	if rc := h.adjust(AdjustOpts{Auto: true, FX: 1420.6, Reason: adjustReason}); rc != 0 {
		t.Fatalf("rc %d %s", rc, h.err)
	}
	rows := h.ledger()
	if len(rows) != 2 || rows[0].Source != "execute" || rows[1].Source != "adjust" || rows[0].AmountKRW != 600_000 || *rows[1].Shares != 15 {
		t.Fatalf("%+v", rows)
	}
	sum := rows[0].AmountKRW + rows[1].AmountKRW
	if !near(sum, h.pos().ContributedKRW, 1) {
		t.Fatalf("내역 합 %v vs 누적적립 %v — 원금이 소리 없이 움직였다", sum, h.pos().ContributedKRW)
	}
}

func TestLedgerIsQuietWhenNothingHasGoneInYet(t *testing.T) {
	h := newHarness(t, seedOpt{})
	if rc := h.app.Ledger(h.ctx); rc != 0 {
		t.Fatalf("rc %d %s", rc, h.out)
	}
}

// ── 상태 영속화 (TQ tests/test_state.py) ─────────────────────────────

func newStore(t *testing.T) (*store.Store, *dbq.Queries, context.Context) {
	t.Helper()
	pool := testdb.New(t)
	return store.New(pool, "test"), dbq.New(pool), context.Background()
}

// Account 왕복에서 값이 하나도 안 변해야 한다. 연도 키는 저장 때 문자열, 읽으면 정수다.
func TestPositionRoundTripIsLossless(t *testing.T) {
	st, q, ctx := newStore(t)
	acct := invest.Account{Symbol: "TQQQ", Shares: 1340, CostKRW: 123_456_789.5, CashUSD: 12.4,
		RealizedGainKRW: map[int]float64{2025: -3_000_000, 2026: 8_421_550.25}, TaxPaidKRW: map[int]float64{}}
	p := invest.Position{Holdings: acct, LastRun: "2026-08-31", YM: "2026-08", Signal: invest.Judgment{SPX: 7489.72, Target: "TQQQ"}, FXRate: 1441.1, ApplyYM: "2026-09"}
	if err := st.SavePosition(ctx, p); err != nil {
		t.Fatal(err)
	}
	got, err := st.LoadPosition(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.Holdings, acct) || got.YM != "2026-08" || got.FXRate != 1441.1 || got.Signal.SPX != 7489.72 {
		t.Fatalf("%+v", got)
	}
	row, err := q.GetInvestState(ctx, "position")
	if err != nil {
		t.Fatal(err)
	}
	var raw struct {
		Holdings struct {
			Gain map[string]float64 `json:"realized_gain_krw"`
		} `json:"holdings"`
	}
	if err := json.Unmarshal(row.Doc, &raw); err != nil {
		t.Fatal(err)
	}
	if _, ok := raw.Holdings.Gain["2025"]; !ok || len(raw.Holdings.Gain) != 2 {
		t.Fatalf("저장된 연도 키 %v", raw.Holdings.Gain)
	}
}

// 소수 주식·파손된 상태는 조용히 읽지 않는다. 없으면 없다고 말한다.
func TestStoreRejectsCorruptAndFractionalPositions(t *testing.T) {
	st, q, ctx := newStore(t)
	if _, err := st.LoadPosition(ctx); !errors.Is(err, store.ErrNoPosition) {
		t.Fatalf("없는 상태: %v", err)
	}
	p := julyPos(invest.Account{Symbol: "TQQQ", Shares: 1340})
	if err := st.SavePosition(ctx, p); err != nil {
		t.Fatal(err)
	}
	row, _ := q.GetInvestState(ctx, "position")
	var doc map[string]any
	if err := json.Unmarshal(row.Doc, &doc); err != nil {
		t.Fatal(err)
	}
	doc["holdings"].(map[string]any)["shares"] = 1340.5
	frac, _ := json.Marshal(doc)
	for name, b := range map[string][]byte{"소수 주식": frac, "파손": []byte(`"{not json"`)} {
		if err := q.UpsertInvestState(ctx, dbq.UpsertInvestStateParams{Key: "position", Doc: b}); err != nil {
			t.Fatal(err)
		}
		if _, err := st.LoadPosition(ctx); err == nil || !strings.Contains(err.Error(), "파손") {
			t.Errorf("%s 상태를 읽었다: %v", name, err)
		}
	}
}

// 🔴 사유 200자 강제를 없앴다 (§6.3). 다만 비워 둔 사유는 6개월 뒤 다시 보여줄 것도 없다.
func TestDeviationReasonIsOptionalAndOnlyAWrittenOneResurfaces(t *testing.T) {
	h := newHarness(t, seedOpt{})
	rec, err := h.st.AppendDeviation(h.ctx, "2026-02", invest.Date(2026, 2, 2), "QQQ", "TQQQ", "", invest.Date(2026, 2, 27))
	if err != nil || rec.Reason != "" || len(h.devs()) != 1 {
		t.Fatalf("사유 없는 이탈을 기록하지 못했다: %v %+v", err, rec)
	}
	h.app.Why(h.ctx, "2026-09-01")
	if strings.Contains(h.out.String(), "그때 이렇게 적으셨습니다") {
		t.Fatal("빈 사유를 다시 보여줬다")
	}

	h2 := newHarness(t, seedOpt{})
	if _, err := h2.st.AppendDeviation(h2.ctx, "2026-02", invest.Date(2026, 2, 2), "QQQ", "TQQQ", "이번만은 다르다고 느꼈다", invest.Date(2026, 2, 27)); err != nil {
		t.Fatal(err)
	}
	h2.app.Why(h2.ctx, "2026-09-01")
	if !strings.Contains(h2.out.String(), "이번만은 다르다고 느꼈다") {
		t.Fatalf("써 둔 사유가 돌아오지 않았다: %s", h2.out)
	}
}

// 사유는 6개월 뒤 돌아온다. 이탈 비용은 시점마다 달라 기록에 박지 않는다.
func TestDeviationResurfacesAfterSixMonthsAndFreezesNoCost(t *testing.T) {
	h := newHarness(t, seedOpt{})
	reason := "나스닥이 연일 신고가라 이번만은 TQQQ를 유지한다"
	rec, err := h.st.AppendDeviation(h.ctx, "2026-09", invest.Date(2026, 9, 1), "QQQ", "TQQQ", "  "+reason+"\n", invest.Date(2026, 9, 30))
	if err != nil {
		t.Fatal(err)
	}
	if rec.ResurfaceOn != "2027-03-01" || rec.Reason != reason {
		t.Fatalf("%+v", rec)
	}
	b, _ := json.Marshal(rec)
	if strings.Contains(string(b), "cost") {
		t.Fatalf("비용이 기록에 박혔다: %s", b)
	}
	h.app.Why(h.ctx, "2027-02-28")
	if strings.Contains(h.out.String(), reason) {
		t.Fatal("6개월 전에 보여줬다")
	}
	h.out.Reset()
	h.app.Why(h.ctx, "2027-03-01")
	if !strings.Contains(h.out.String(), reason) {
		t.Fatalf("6개월 뒤에 안 보여줬다: %s", h.out)
	}
}

func TestAppendDeviationRejectsNonDeviationAndBadLock(t *testing.T) {
	st, _, ctx := newStore(t)
	if _, err := st.AppendDeviation(ctx, "2026-09", invest.Date(2026, 9, 1), "TQQQ", "TQQQ", "", invest.Date(2026, 9, 30)); err == nil || !strings.Contains(err.Error(), "이탈이 아니다") {
		t.Fatalf("%v", err)
	}
	if _, err := st.AppendDeviation(ctx, "2026-09", invest.Date(2026, 9, 1), "QQQ", "TQQQ", "", invest.Date(2026, 8, 1)); err == nil || !strings.Contains(err.Error(), "잠금 해제일") {
		t.Fatalf("%v", err)
	}
}

const contribReason = "이직으로 소득이 줄어 당분간 적립액을 낮춘다"

func contribStore(t *testing.T) (*store.Store, context.Context) {
	st, _, ctx := newStore(t)
	if err := st.ImportContribution(ctx, invest.Contribution{EffectiveFrom: "2026-01", Amount: 1_500_000, Reason: "운용 시작", RecordedOn: "2026-01-01"}); err != nil {
		t.Fatal(err)
	}
	return st, ctx
}

func TestContributionAppliesFromEffectiveMonth(t *testing.T) {
	st, ctx := contribStore(t)
	if _, err := st.SetContribution(ctx, 1_000_000, "2026-09", contribReason, invest.Date(2026, 7, 15), ""); err != nil {
		t.Fatal(err)
	}
	for ym, want := range map[string]int64{"2026-08": 1_500_000, "2026-09": 1_000_000, "2027-05": 1_000_000} {
		if got, err := st.ContributionFor(ctx, ym); err != nil || got != want {
			t.Errorf("%s %d (%v), 기대 %d", ym, got, err, want)
		}
	}
}

// 이번 달·지난달은 못 바꾼다. 신호를 아는 달을 바꾸는 것은 마켓 타이밍이다.
func TestContributionChangeIsGuarded(t *testing.T) {
	st, ctx := contribStore(t)
	for _, ym := range []string{"2026-07", "2026-06"} {
		if _, err := st.SetContribution(ctx, 1_000_000, ym, contribReason, invest.Date(2026, 7, 15), ""); err == nil || !strings.Contains(err.Error(), "다음 달") {
			t.Errorf("%s: %v", ym, err)
		}
	}
	if _, err := st.SetContribution(ctx, 500_000, "2026-08", contribReason, invest.Date(2026, 7, 31), "2026-08"); err == nil || !strings.Contains(err.Error(), "마켓 타이밍") {
		t.Errorf("신호를 아는 달: %v", err)
	}
	if rec, err := st.SetContribution(ctx, 500_000, "2026-09", contribReason, invest.Date(2026, 7, 31), "2026-08"); err != nil || rec.EffectiveFrom != "2026-09" {
		t.Errorf("그 다음 달부터는 허용된다: %v", err)
	}
	if _, err := st.SetContribution(ctx, 1_000_000, "2026-10", "줄임", invest.Date(2026, 7, 15), ""); err == nil || !strings.Contains(err.Error(), "20자 이상") {
		t.Errorf("짧은 사유: %v", err)
	}
	if _, err := st.SetContribution(ctx, 0, "2026-10", contribReason, invest.Date(2026, 7, 15), ""); err == nil || !strings.Contains(err.Error(), "0보다") {
		t.Errorf("0원: %v", err)
	}
	if _, err := st.SetContribution(ctx, 2_000_000, "2026-09", contribReason, invest.Date(2026, 7, 20), ""); err == nil || !strings.Contains(err.Error(), "이미 변경 기록") {
		t.Errorf("중복: %v", err)
	}
}

func TestMissingInitialContributionFailsLoudly(t *testing.T) {
	st, _, ctx := newStore(t)
	if _, err := st.ContributionFor(ctx, "2026-08"); err == nil || !strings.Contains(err.Error(), "적용할 적립액이 없다") {
		t.Fatalf("%v", err)
	}
}

// 안 사는 것은 아무것도 입력하지 않는 행위라 사유가 없다. 준수 카운트에 들어가고 두 번 세지 않는다.
func TestMissedMonthIsRecordedOnceWithoutAReason(t *testing.T) {
	st, _, ctx := newStore(t)
	rec, err := st.RecordMissedMonth(ctx, "2026-08", "TQQQ", invest.Date(2026, 8, 31))
	if err != nil || rec == nil || rec.Kind != "missed" || rec.ActualTarget != nil || !strings.Contains(rec.Reason, "사유 없음") {
		t.Fatalf("%v %+v", err, rec)
	}
	again, err := st.RecordMissedMonth(ctx, "2026-08", "TQQQ", invest.Date(2026, 9, 30))
	if err != nil || again != nil {
		t.Fatalf("두 번 기록했다: %v %+v", err, again)
	}
	if _, err := st.AppendDeviation(ctx, "2026-09", invest.Date(2026, 9, 1), "QQQ", "TQQQ", "", invest.Date(2026, 9, 30)); err != nil {
		t.Fatal(err)
	}
	devs, _ := st.LoadDeviations(ctx)
	if len(devs) != 2 || devs[0].ActualTarget != nil {
		t.Fatalf("%+v", devs)
	}
}
