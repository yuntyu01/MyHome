package run

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/yuntyu01/MyHome/server/internal/invest"
	"github.com/yuntyu01/MyHome/server/internal/invest/broker"
	"github.com/yuntyu01/MyHome/server/internal/invest/store"
	"github.com/yuntyu01/MyHome/server/internal/invest/yahoo"
	"github.com/yuntyu01/MyHome/server/internal/testdb"
)

// 저장 구조를 바꾸기 전 코드의 결과와 대조하는 골든 테스트(2026-11-20, invest_state 문서 → 정규 테이블).
//
// 운영과 같은 모양의 상태(옮겨 온 position·원금 내역·이력)에서 넉 달을 돈다: 가격 갱신 → 월말 판정 → 매수 →
// 체결 확인 → 환율 확정(amend --auto) → status·reconcile --apply-cash, 전환 달(매도 → 매수, 거절·결과 모름 포함),
// 앱 밖 매수(adjust), 손으로 기록(execute) → 정정(amend), 지난달 주문 기록 정리(orders --clear-stale), 이탈 요청·취소.
// 단계마다 출력·종료 코드와 DB 상태(position·orders·pending 문서, 원금 내역, 이탈, 앱 밖 매수, 문서 변경 이력 …)를
// testdata/cycle_golden.json과 비교한다. 이 파일은 바꾸기 전 코드로 만들었다(-update). 숫자는 전부 합성이다.
//
// 일부러 뺀 필드(TQ 모양 때문에만 남았던 것)는 양쪽에서 지우고 비교한다: position.close_usd, pending.kind.

var updateGolden = flag.Bool("update", false, "testdata/cycle_golden.json을 지금 결과로 다시 쓴다")

const goldenPath = "testdata/cycle_golden.json"

type goldenStep struct {
	Name string          `json:"name"`
	Exit int             `json:"exit"`
	Out  []string        `json:"out"`
	Err  []string        `json:"err"`
	DB   json.RawMessage `json:"db"`
}

type cycleRig struct {
	t    *testing.T
	ctx  context.Context
	pool *pgxpool.Pool
	st   *store.Store
	app  *App
	cb   *cashBroker
	out  *bytes.Buffer
	err  *bytes.Buffer
	now  time.Time
	rows []yrow // 합성 가격(julyLast 다음 거래일부터)
	step []goldenStep
}

// at은 뉴욕 시각이다.
func at(m time.Month, d, hh, mm int) time.Time {
	return time.Date(2026, m, d, hh, mm, 0, 0, invest.NewYork)
}

// julyGolden은 골든을 만들 때 쓴 7월 판정 값이다(소수 끝자리를 줄여 적은 옛 상수 — 골든이 이 값을 담고 있다).
// 이 시나리오는 7월을 다시 판정하지 않으므로 계산값과 끝자리가 달라도 된다.
var julyGolden = invest.Judgment{SPX: 7489.72021484375, SMA185: 7045.09037816723, C1: true, Vol: 0.236368559570371, C2: true, Target: "TQQQ"}

func newCycleRig(t *testing.T) *cycleRig {
	t.Helper()
	pool := testdb.New(t)
	ctx := context.Background()
	r := &cycleRig{t: t, ctx: ctx, pool: pool, out: &bytes.Buffer{}, err: &bytes.Buffer{}}
	r.rows = cyclePrices()

	// 옮겨 오던 날처럼 쓴다 — 이력의 cmd가 운영과 같은 모양이 되게
	seed := store.New(pool, "invest import-tq")
	if _, err := seed.CopyPrices(ctx, julyMaster(t)); err != nil {
		t.Fatal(err)
	}
	if err := seed.ImportContribution(ctx, invest.Contribution{EffectiveFrom: "2026-01", Amount: 600_000, Reason: "합성 데이터 — 첫 적립액(테스트)", RecordedOn: "2026-01-02"}); err != nil {
		t.Fatal(err)
	}
	// 1~7월 원금 내역(TQ에서 옮겨 온 모양: 손으로 기록·자동 매수·앱 밖 매수, 정정으로 덮인 줄 포함)
	ledger := []invest.LedgerEntry{
		{On: "2026-01-02", YM: "2026-01", AmountKRW: 600_000, Source: "execute", Symbol: "TQQQ", Shares: ip(6), Note: "합성"},
		{On: "2026-02-02", YM: "2026-02", AmountKRW: 600_000, Source: "execute", Symbol: "TQQQ", Shares: ip(6)},
		{On: "2026-03-02", YM: "2026-03", AmountKRW: 612_345.67, Source: "trade", Symbol: "TQQQ", Shares: ip(7), Note: "환율 추정"},
		{On: "2026-03-05", YM: "2026-03", AmountKRW: 598_765.43, Source: "amend", Symbol: "TQQQ", Shares: ip(7), Note: "정정 1회차 · 결제 환율 1,380.12"},
		{On: "2026-04-01", YM: "2026-04", AmountKRW: 600_000, Source: "trade", Symbol: "TQQQ", Shares: ip(6)},
		{On: "2026-04-20", YM: "2026-04", AmountKRW: 250_000, Source: "adjust", Symbol: "TQQQ", Shares: ip(2), Note: "앱 밖 매수"},
		{On: "2026-05-01", YM: "2026-05", AmountKRW: 600_000, Source: "trade", Symbol: "TQQQ", Shares: ip(5)},
		{On: "2026-06-01", YM: "2026-06", AmountKRW: 600_000, Source: "trade", Symbol: "TQQQ", Shares: ip(5)},
		{On: "2026-07-01", YM: "2026-07", AmountKRW: 600_000, Source: "trade", Symbol: "TQQQ", Shares: ip(3)},
	}
	for _, e := range ledger {
		if err := seed.ImportLedger(ctx, e); err != nil {
			t.Fatal(err)
		}
	}
	// 3월 자동 매수 줄은 정정으로 덮였다
	if _, err := pool.Exec(ctx, "UPDATE invest_ledger SET superseded_at = now() WHERE on_day = '2026-03-02'"); err != nil {
		t.Fatal(err)
	}
	contributed := 0.0
	for _, e := range ledger {
		if !(e.On == "2026-03-02") {
			contributed += e.AmountKRW
		}
	}
	before := invest.Account{Symbol: "TQQQ", Shares: 37, CostKRW: 3_412_345.125, CashUSD: 431.875, RealizedGainKRW: map[int]float64{}, TaxPaidKRW: map[int]float64{}}
	pos := invest.Position{
		Holdings: invest.Account{Symbol: "TQQQ", Shares: 40, CostKRW: 4_001_234.5, CashUSD: 12.34, RealizedGainKRW: map[int]float64{2025: -12_345.5}, TaxPaidKRW: map[int]float64{}},
		LastRun:  "2026-07-31", YM: "2026-07", Signal: julyGolden, FXRate: 1386.25, ApplyYM: "2026-08",
		ContributedKRW: contributed, MonthsInvested: 7, JudgmentCount: 7,
		CloseUSD:   map[string]float64{"QQQM": 283.29, "TQQQ": 64.62},
		ExecutedOn: invest.StrPtr("2026-07-01"), ExecutedYM: invest.StrPtr("2026-07"), LateDays: ip(0),
		Before: &invest.Snapshot{Holdings: before, ContributedKRW: contributed - 600_000, MonthsInvested: 6},
	}
	if err := seed.Bootstrap(ctx, pos); err != nil {
		t.Fatal(err)
	}
	// 옮긴 뒤 사람이 남은 현금을 증권사에 맞춘 모양(reconcile --apply-cash)
	pos.Holdings.CashUSD = 12.5
	if err := store.New(pool, "invest reconcile").SavePosition(ctx, pos); err != nil {
		t.Fatal(err)
	}

	r.st = store.New(pool, "test")
	r.app = &App{Store: r.st, Out: r.out, Err: r.err, UnsafeDate: true, Sleep: func(time.Duration) {}, Now: func() time.Time { return r.now }}
	r.app.Prices = &fakeYahoo{next: func(int) (map[string][]yahoo.Bar, error) { return r.barsUntil(), nil }}
	r.cb = &cashBroker{settlingBroker: &settlingBroker{fakeBroker: newFake(), fx: map[string]float64{}}}
	r.app.Broker = func(context.Context) (broker.Broker, error) { return r.cb, nil }
	for _, y := range r.rows {
		r.cb.fx[invest.FormatDay(y.day)] = y.krw + 1.25 // 증권사 매매기준율은 야후와 조금 다르다
	}
	r.cb.held["TQQQ"] = 40
	r.cb.cash = 12.5
	return r
}

// cyclePrices는 8~11월 합성 일봉이다. 8월은 오르고, 9월은 SPX가 185일선 아래로 떨어져(→ 10월 QQQ) 10~11월은 횡보한다.
func cyclePrices() []yrow {
	var out []yrow
	i := 0
	for d := invest.Date(2026, 8, 3); !d.After(invest.Date(2026, 11, 30)); d = d.AddDate(0, 0, 1) {
		if !invest.IsTradingDay(d) {
			continue
		}
		i++
		x := float64(i)
		var spx, ndx float64
		switch d.Month() {
		case time.August:
			spx, ndx = 7489.72+6*x, 28274.2+25*x
		case time.September:
			k := x - 21
			spx, ndx = 7615-45*k, 28800-180*k+90*math.Sin(k)
		default:
			k := x - 42
			spx, ndx = 6670+3*math.Sin(k/3), 25000+60*math.Sin(k/2)
		}
		tq := 64.62 * math.Pow(ndx/28274.2, 3)
		out = append(out, yrow{d, round4(spx), round4(ndx), round4(688 * ndx / 28274.2), round4(283.29 * ndx / 28274.2),
			round4(83.67 * math.Pow(ndx/28274.2, 2)), round4(tq), round4(1385 + 7*math.Sin(x/5))})
	}
	return out
}

func round4(v float64) float64 { return math.Round(v*10000) / 10000 }

// barsUntil은 지금(뉴욕) 날짜까지의 일봉이다. julyLast부터 준다 — refresh 앵커 대조가 통과한다.
func (r *cycleRig) barsUntil() map[string][]yahoo.Bar {
	today := invest.TodayET(r.now)
	rows := []yrow{julyLast}
	for _, y := range r.rows {
		if !y.day.After(today) {
			rows = append(rows, y)
		}
	}
	return bars(rows...)
}

func (r *cycleRig) close(sym string, d time.Time) float64 {
	for _, y := range r.rows {
		if y.day.Equal(d) {
			return map[string]float64{"TQQQ": y.tqqq, "QQQM": y.qqqm, "QQQ": y.qqq}[sym]
		}
	}
	r.t.Fatalf("%s 가격 없음", invest.FormatDay(d))
	return 0
}

// do는 명령 하나를 돌리고 출력·DB 상태를 남긴다.
func (r *cycleRig) do(name string, now time.Time, fn func() int) int {
	r.t.Helper()
	r.now = now
	r.out.Reset()
	r.err.Reset()
	rc := fn()
	r.step = append(r.step, goldenStep{Name: name, Exit: rc, Out: lines(r.out.String()), Err: lines(r.err.String()), DB: r.snapshot(false)})
	return rc
}

func lines(s string) []string {
	s = strings.TrimRight(s, "\n")
	if s == "" {
		return []string{}
	}
	return strings.Split(s, "\n")
}

// lastOrder는 가짜 증권사에 마지막으로 접수된 주문이다.
func (r *cycleRig) lastOrder() placed {
	r.t.Helper()
	if len(r.cb.placed) == 0 {
		r.t.Fatal("접수된 주문 없음")
	}
	return r.cb.placed[len(r.cb.placed)-1]
}

// fill은 주문을 체결시키고 증권사 보유·현금을 맞춘다.
func (r *cycleRig) fill(id string, qty int, px float64) {
	p := r.cb.order(id)
	r.cb.fill(id, qty, px, "")
	if p.Side == broker.Buy {
		r.cb.held[p.Symbol] += qty
		r.cb.cash -= float64(qty) * px * (1 + invest.FeeRate)
	} else {
		r.cb.held[p.Symbol] -= qty
		if r.cb.held[p.Symbol] <= 0 {
			delete(r.cb.held, p.Symbol)
		}
		r.cb.cash += float64(qty) * px * (1 - invest.FeeRate - invest.SECFeeRate)
	}
}

func (r *cycleRig) deposit(krw float64, when time.Time, fx float64) {
	r.cb.flows = append(r.cb.flows, broker.CashFlow{Kind: broker.FlowDeposit, Currency: "KRW", Amount: krw, At: when.In(broker.KST)})
	r.cb.krw += krw
	r.cb.cash += krw / fx
	r.cb.limit = r.cb.cash + r.cb.spent
}

func TestCycleGolden(t *testing.T) {
	r := newCycleRig(t)
	ctx := r.ctx

	// ── 8월: 적립 매수, 체결 확인, 결제 뒤 환율 확정 ──────────────────────
	r.deposit(600_000, at(8, 3, 9, 30), 1385)
	r.do("8/3 trade (매수 주문)", at(8, 3, 11, 17), func() int { return r.app.Trade(ctx, TradeOpts{}) })
	aug := r.lastOrder()
	r.fill(aug.OrderID, aug.Qty, 65.4321)
	r.do("8/4 trade (체결 반영)", at(8, 4, 11, 17), func() int { return r.app.Trade(ctx, TradeOpts{}) })
	r.cb.rows = append(r.cb.rows, broker.Settlement{Day: invest.Date(2026, 8, 5), Symbol: "TQQQ", Side: broker.Buy, Qty: aug.Qty,
		Price: 65.4321, Amount: 65.4321 * float64(aug.Qty), Net: 65.4321 * float64(aug.Qty) * 1.001, FX: 1388.4, Settled: true})
	r.do("8/5 trade (결제 환율 확정)", at(8, 5, 11, 17), func() int { return r.app.Trade(ctx, TradeOpts{}) })
	r.do("8/10 status", at(8, 10, 22, 23), func() int { return r.app.Status(ctx, StatusOpts{}) })
	r.cb.cash += 3.21 // 배당
	r.do("8/10 reconcile --apply-cash", at(8, 10, 22, 30), func() int { return r.app.Reconcile(ctx, ReconcileOpts{ApplyCash: true}) })
	r.do("8/10 ledger", at(8, 10, 22, 31), func() int { return r.app.Ledger(ctx) })

	// ── 8월 말: 갱신 → 판정(유지) ─────────────────────────────────────────
	r.do("8/31 refresh", at(8, 31, 18, 36), func() int { return r.app.Refresh(ctx, RefreshOpts{}) })
	r.do("8/31 judge", at(8, 31, 18, 37), func() int { return r.app.Judge(ctx, JudgeOpts{}) })

	// ── 9월: 첫 주문은 결과 모름(접수는 됨) → 다음 실행이 찾아 이어받는다 ───────
	r.deposit(600_000, at(9, 1, 9, 30), 1390)
	r.cb.timeout[broker.MOC], r.cb.acceptOnTimeout = true, true
	r.do("9/1 trade (결과 모름)", at(9, 1, 11, 17), func() int { return r.app.Trade(ctx, TradeOpts{}) })
	r.cb.timeout[broker.MOC] = false
	r.do("9/2 trade (주문 찾음)", at(9, 2, 11, 17), func() int { return r.app.Trade(ctx, TradeOpts{}) })
	sep := r.lastOrder()
	r.fill(sep.OrderID, sep.Qty, r.close("TQQQ", invest.Date(2026, 9, 1)))
	r.do("9/3 trade (체결 반영)", at(9, 3, 11, 17), func() int { return r.app.Trade(ctx, TradeOpts{}) })
	r.do("9/30 refresh", at(9, 30, 18, 36), func() int { return r.app.Refresh(ctx, RefreshOpts{}) })
	r.do("9/30 judge (전환)", at(9, 30, 18, 37), func() int { return r.app.Judge(ctx, JudgeOpts{}) })

	// ── 10월: 전환 — 매도 → 매수(첫 유형 거절 → 다음 유형) ────────────────────
	r.deposit(600_000, at(10, 1, 9, 30), 1392)
	r.do("10/1 trade (매도 주문)", at(10, 1, 11, 17), func() int { return r.app.Trade(ctx, TradeOpts{}) })
	sell := r.lastOrder()
	r.fill(sell.OrderID, sell.Qty, r.close("TQQQ", invest.Date(2026, 10, 1)))
	r.cb.limit = r.cb.cash + r.cb.spent
	r.cb.rejects[broker.MOC] = true
	r.do("10/2 trade (매도 체결 → 매수, MOC 거절)", at(10, 2, 11, 17), func() int { return r.app.Trade(ctx, TradeOpts{FX: 1391.5}) })
	r.cb.rejects[broker.MOC] = false
	buy := r.lastOrder()
	r.fill(buy.OrderID, buy.Qty, r.close("QQQM", invest.Date(2026, 10, 2)))
	r.do("10/5 trade (전환 완료)", at(10, 5, 11, 17), func() int { return r.app.Trade(ctx, TradeOpts{FX: 1391.5}) })
	r.do("10/5 tax", at(10, 5, 12, 0), func() int { return r.app.Tax(ctx, TaxOpts{}) })

	// ── 10월 중순: 앱 밖 매수, 이탈 요청 → 취소 ─────────────────────────────
	r.cb.held["QQQM"] += 2
	r.do("10/15 adjust", at(10, 15, 12, 0), func() int {
		return r.app.Adjust(ctx, AdjustOpts{Shares: ip(2), Paid: fp(700_000), Reason: "합성 데이터 — 증권사 앱에서 따로 산 두 주(테스트)"})
	})
	r.do("10/16 deviate (요청)", at(10, 16, 9, 0), func() int {
		return r.app.Deviate(ctx, DeviateOpts{Symbol: "TQQQ", Reason: "합성 — 반등이 보여서"})
	})
	r.do("10/16 cancel", at(10, 16, 10, 0), func() int { return r.app.Cancel(ctx) })

	// ── 10월 말 → 11월: 지난달 주문 기록이 남은 채(만료) → 정리, 손으로 기록 → 정정 ──
	r.do("10/30 refresh", at(10, 30, 18, 36), func() int { return r.app.Refresh(ctx, RefreshOpts{}) })
	r.do("10/30 judge (유지)", at(10, 30, 18, 37), func() int { return r.app.Judge(ctx, JudgeOpts{}) })
	stale := invest.OrderBook{YM: "2026-10", Stage: "buy", StyleIdx: 1, Orders: []invest.BookOrder{{
		OrderID: "ODNO-OLD", ClientOrderID: "tq-20261030-QQQM-buy-1-MOC", Symbol: "QQQM", Side: "buy", Qty: 1, Price: 250.1, Style: "MOC",
		SubmittedOn: "2026-10-30", SubmittedAt: "2026-10-30T15:17:00Z", Filled: true, Expired: true,
	}}}
	if err := r.st.SaveOrders(ctx, stale); err != nil {
		t.Fatal(err)
	}
	r.do("11/2 trade (지난달 기록에 막힘)", at(11, 2, 11, 17), func() int { return r.app.Trade(ctx, TradeOpts{}) })
	r.do("11/2 orders", at(11, 2, 12, 0), func() int { return r.app.Orders(ctx, OrdersOpts{}) })
	r.do("11/2 orders --clear-stale", at(11, 2, 12, 1), func() int { return r.app.Orders(ctx, OrdersOpts{ClearStale: true}) })
	qqqm := r.close("QQQM", invest.Date(2026, 11, 2))
	r.cb.held["QQQM"] += 3
	r.do("11/3 execute", at(11, 3, 12, 0), func() int {
		return r.app.Execute(ctx, ExecuteOpts{Fills: map[string]float64{"QQQM": qqqm}, FX: 1393.25, Shares: ip(3), Paid: fp(601_234)})
	})
	r.do("11/4 amend", at(11, 4, 12, 0), func() int {
		return r.app.Amend(ctx, AmendOpts{Fills: map[string]float64{"QQQM": qqqm * 1.002}, FX: 1394.75})
	})
	r.do("11/4 ledger", at(11, 4, 12, 1), func() int { return r.app.Ledger(ctx) })
	r.do("11/30 refresh", at(11, 30, 18, 36), func() int { return r.app.Refresh(ctx, RefreshOpts{}) })
	r.do("11/30 judge", at(11, 30, 18, 37), func() int { return r.app.Judge(ctx, JudgeOpts{}) })
	r.do("12/1 status", at(12, 1, 9, 0), func() int { return r.app.Status(ctx, StatusOpts{}) })

	// 마지막 단계에는 문서 변경 이력 전체와 매일 기록까지 싣는다(단계마다 싣기엔 커서)
	r.step[len(r.step)-1].DB = r.snapshot(true)
	got, err := json.MarshalIndent(r.step, "", " ")
	if err != nil {
		t.Fatal(err)
	}
	got = append(got, '\n')
	if *updateGolden {
		if err := os.MkdirAll(filepath.Dir(goldenPath), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(goldenPath, got, 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("%s 다시 씀 (%d단계)", goldenPath, len(r.step))
		return
	}
	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("%v — 처음이면 -update로 만든다", err)
	}
	if !bytes.Equal(got, want) {
		var ws []goldenStep
		_ = json.Unmarshal(want, &ws)
		for i := range r.step {
			if i >= len(ws) {
				t.Fatalf("단계 %d(%s)가 골든에 없다", i, r.step[i].Name)
			}
			g, _ := json.MarshalIndent(r.step[i], "", " ")
			w, _ := json.MarshalIndent(ws[i], "", " ")
			if !bytes.Equal(g, w) {
				t.Fatalf("단계 %d %q가 바꾸기 전 코드와 다르다\n--- 지금\n%s\n--- 골든\n%s", i, r.step[i].Name, g, w)
			}
		}
		t.Fatalf("단계 수가 다르다: 지금 %d, 골든 %d", len(r.step), len(ws))
	}
}

// snapshot은 비교할 DB 상태다. 시각(created_at 등)은 빼고, 숫자는 비트 그대로(%g -1) 적는다.
// full이면 매일 기록과 문서 변경 이력 전체도 싣는다.
func (r *cycleRig) snapshot(full bool) json.RawMessage {
	r.t.Helper()
	ctx := r.ctx
	doc := func(key string) any {
		var raw []byte
		err := r.pool.QueryRow(ctx, "SELECT doc FROM invest_state WHERE key = $1", key).Scan(&raw)
		if err != nil {
			return nil
		}
		return canonDoc(key, raw)
	}
	s := map[string]any{"position": doc("position"), "orders": doc("orders"), "pending": doc("pending")}
	s["ledger"] = r.rowsOf(`SELECT on_day::text, ym, amount_krw, source, coalesce(symbol,''), coalesce(shares::text,''), note, superseded_at IS NOT NULL
		FROM invest_ledger ORDER BY id`)
	s["deviations"] = r.rowsOf(`SELECT coalesce(kind,''), ym, occurred_on::text, signal_target, coalesce(actual_target,''), reason, locked_until::text, resurface_on::text
		FROM invest_deviations ORDER BY id`)
	s["adjustments"] = r.rowsOf(`SELECT on_day::text, symbol, shares, paid_krw, reason FROM invest_adjustments ORDER BY id`)
	s["prices"] = r.rowsOf(`SELECT count(*), max(day)::text, count(usdkrw_mkt) FROM invest_prices`)
	if full {
		s["daily"] = r.rowsOf(`SELECT day::text, spx, sma185, vol, signal, coalesce(symbol,''), coalesce(shares,0), coalesce(stock_krw,0), coalesce(cash_krw,0), coalesce(invested_krw,0), coalesce(gain_krw,0)
		FROM invest_daily ORDER BY day`)
	}
	s["flow"] = r.rowsOf(`SELECT ym, step, key, data::text FROM invest_flow ORDER BY id`)
	s["broker_cash"] = r.rowsOf(`SELECT cash_usd, coalesce(cash_krw, -1) FROM invest_broker_cash ORDER BY id`)
	s["history_count"] = r.rowsOf(`SELECT key, count(*) FROM invest_state_history GROUP BY key ORDER BY key`)
	if !full {
		b, err := json.Marshal(s)
		if err != nil {
			r.t.Fatal(err)
		}
		return b
	}
	// 문서 변경 이력 — 바꾼 뒤에도 그림자 문서가 같은 순서·같은 값으로 쓰여야 한다
	var hist []any
	rows, err := r.pool.Query(ctx, "SELECT key, cmd, doc FROM invest_state_history ORDER BY id")
	if err != nil {
		r.t.Fatal(err)
	}
	for rows.Next() {
		var key, cmd string
		var raw []byte
		if err := rows.Scan(&key, &cmd, &raw); err != nil {
			r.t.Fatal(err)
		}
		var d any
		if raw != nil {
			d = canonDoc(key, raw)
		}
		hist = append(hist, []any{key, cmd, d})
	}
	rows.Close()
	s["history"] = hist
	b, err := json.Marshal(s)
	if err != nil {
		r.t.Fatal(err)
	}
	return b
}

// canonDoc은 문서를 키 정렬된 값으로 읽는다. 일부러 뺀 필드는 지운다.
func canonDoc(key string, raw []byte) any {
	var v map[string]any
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err := dec.Decode(&v); err != nil {
		return string(raw)
	}
	switch key {
	case "position":
		delete(v, "close_usd")
	case "pending":
		delete(v, "kind")
	}
	return v
}

// rowsOf는 질의 결과를 문자열 행으로 적는다. float64는 왕복 가능한 가장 짧은 표기다.
func (r *cycleRig) rowsOf(q string) [][]string {
	r.t.Helper()
	rows, err := r.pool.Query(r.ctx, q)
	if err != nil {
		r.t.Fatalf("%v\n%s", err, q)
	}
	defer rows.Close()
	out := [][]string{}
	for rows.Next() {
		vals, err := rows.Values()
		if err != nil {
			r.t.Fatal(err)
		}
		row := make([]string, len(vals))
		for i, v := range vals {
			switch x := v.(type) {
			case float64:
				row[i] = strconv.FormatFloat(x, 'g', -1, 64)
			case nil:
				row[i] = "∅"
			default:
				row[i] = fmt.Sprint(x)
			}
		}
		out = append(out, row)
	}
	return out
}
