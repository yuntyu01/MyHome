package web

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/yuntyu01/MyHome/server/internal/invest"
	"github.com/yuntyu01/MyHome/server/internal/period"
)

// 투자 첫 화면 맨 위 요약과 이번 달 자동 매수 단계.

func stepOf(t *testing.T, f investFlow, key string) investStep {
	t.Helper()
	for _, s := range f.Steps {
		if s.Key == key {
			return s
		}
	}
	t.Fatalf("단계 %s 없음: %+v", key, f.Steps)
	return investStep{}
}

func decemberPending() invest.Position {
	p := basePosition()
	p.LastRun, p.YM, p.ApplyYM = "2026-11-30", "2026-11", "2026-12"
	p.Signal = invest.Judgment{SPX: 6812.34, SMA185: 6240.11, C1: true, Vol: 0.184, C2: true, Target: "TQQQ"}
	p.ExecutedOn, p.ExecutedYM = invest.StrPtr("2026-11-02"), invest.StrPtr("2026-11")
	return p
}

func TestInvestSummaryShowsTotalAndPrincipal(t *testing.T) {
	now := etNow(2026, 11, 6)
	in := investFlowInputs{KRW: 600_000, HasKRW: true, KRWAt: time.Date(2026, 11, 6, 0, 17, 0, 0, period.KST)}
	s := buildInvestSummary(now, basePosition(), pricesUpTo(invest.Date(2026, 11, 5)), in)
	// 주식 10주 × $50.25 × 1400 = 703,500 · 달러 현금 $12.5 × 1400 = 17,500 · 원화 600,000
	if !s.CanValue || s.StockKRW != 703_500 || s.CashKRW != 617_500 || s.TotalKRW != 1_321_000 || s.InvestedKRW != 600_000 {
		t.Fatalf("%+v", s)
	}
	// 손익은 아직 안 바꾼 원화를 빼고 본다: (703,500 + 17,500 − 600,000) / 600,000
	if s.GainKRW != 121_000 || s.GainPct != "+20.2%" || s.Target != "TQQQ" {
		t.Fatalf("%+v", s)
	}
	if !strings.Contains(s.Basis, "종가") || !strings.Contains(s.Basis, "원화 11/6 00:17 기준") {
		t.Fatalf("basis %q", s.Basis)
	}
}

func TestInvestFlowJudgeLinesAndWaitingForDeposit(t *testing.T) {
	now := etNow(2026, 12, 3)
	f := buildInvestFlow(now, decemberPending(), nil, 600_000, nil, nil, investFlowInputs{DepositDay: 5})
	j := stepOf(t, f, "judge")
	all := strings.Join(j.Lines, "\n")
	if j.State != stepDone || j.Value != "11/30" || !strings.Contains(all, "S&P500 6,812.34 · 185일선 6,240.11 (+9.2%) ✓") ||
		!strings.Contains(all, "21일 변동성 18.4% · 기준 30% 미만 ✓") || !strings.Contains(all, "12월은 TQQQ") {
		t.Fatalf("%+v", j)
	}
	if d := stepOf(t, f, "deposit"); d.State != stepWaiting || d.Value != "5일 예정" {
		t.Fatalf("%+v", d)
	}
	if o := stepOf(t, f, "order"); o.State != stepWaiting || o.Value != "12/7 예정" {
		t.Fatalf("12/5가 토요일이라 12/7(월): %+v", o)
	}
	if f.State != stepWaiting || f.Title != "12월 자동 매수" {
		t.Fatalf("%+v", f)
	}
	// 입금일이 지나면 확인 중(마감 10일 당일까지), 마감이 지나도 안 보이면 문제
	for _, day := range []int{7, 10} {
		if d := stepOf(t, buildInvestFlow(etNow(2026, 12, day), decemberPending(), nil, 600_000, nil, nil, investFlowInputs{DepositDay: 5}), "deposit"); d.State != stepRunning {
			t.Fatalf("12/%d %+v", day, d)
		}
	}
	late := buildInvestFlow(etNow(2026, 12, 11), decemberPending(), nil, 600_000, nil, nil, investFlowInputs{DepositDay: 5})
	if d := stepOf(t, late, "deposit"); d.State != stepFailed || late.State != stepFailed {
		t.Fatalf("%+v / %s", d, late.State)
	}
}

func TestInvestFlowAfterDepositExchangeOrderFill(t *testing.T) {
	at := func(d, h, m int) time.Time { return time.Date(2026, 12, d, h, m, 0, 0, period.KST) }
	ev := []invest.FlowEvent{
		{Step: invest.StepDeposit, At: at(5, 14, 2), KRW: 600_000},
		{Step: invest.StepOrder, At: at(8, 0, 17), Symbol: "TQQQ", Side: "buy", Qty: 9, Price: 71.2, Style: "시장가 대용 지정가(±10%)", OrderID: "77"},
		{Step: invest.StepFill, At: at(9, 0, 17), Symbol: "TQQQ", Side: "buy", Qty: 9, Price: 67.8, OrderID: "77"},
		{Step: invest.StepExchange, At: at(9, 6, 30), KRW: 599_000, USD: 413.1, FX: 1450.01, Mid: 1448.56},
	}
	p := decemberPending()
	// 주문 뒤, 체결 반영 전
	mid := buildInvestFlow(etNow(2026, 12, 8), p, nil, 600_000, nil, nil, investFlowInputs{DepositDay: 5, Events: ev[:2]})
	if d := stepOf(t, mid, "deposit"); d.State != stepDone || d.Value != "12/5 14:02" {
		t.Fatalf("%+v", d)
	}
	if x := stepOf(t, mid, "exchange"); x.State != stepRunning {
		t.Fatalf("%+v", x)
	}
	if o := stepOf(t, mid, "order"); o.State != stepDone || !strings.Contains(o.Lines[0], "TQQQ 매수 9주") || !strings.Contains(o.Lines[0], "@ $71.20") {
		t.Fatalf("%+v", o)
	}
	if fl := stepOf(t, mid, "fill"); fl.State != stepRunning || mid.State != stepRunning {
		t.Fatalf("%+v / %s", fl, mid.State)
	}
	// 체결 반영 뒤
	p.ExecutedOn, p.ExecutedYM = invest.StrPtr("2026-12-08"), invest.StrPtr("2026-12")
	shares := 9
	led := []invest.LedgerEntry{{On: "2026-12-08", YM: "2026-12", AmountKRW: 599_000, Source: "trade", Symbol: "TQQQ", Shares: &shares}}
	done := buildInvestFlow(etNow(2026, 12, 9), p, led, 600_000, nil, nil, investFlowInputs{DepositDay: 5, Events: ev})
	x := stepOf(t, done, "exchange")
	if x.State != stepDone || x.Value != "12/9 06:30" || !strings.Contains(strings.Join(x.Lines, "\n"), "기준율보다 +1.45원 (+0.10%)") {
		t.Fatalf("%+v", x)
	}
	if fl := stepOf(t, done, "fill"); fl.State != stepDone || fl.Value != "12/9 00:17" || !strings.Contains(strings.Join(fl.Lines, "\n"), "TQQQ +9주 · 원금 +599,000원") {
		t.Fatalf("%+v", fl)
	}
	if done.State != stepDone || done.Value != "완료" {
		t.Fatalf("%s %s", done.State, done.Value)
	}
	// 앱이 읽는 이름 그대로
	b, _ := json.Marshal(done)
	for _, k := range []string{`"steps"`, `"state"`, `"lines"`, `"value"`, `"title"`} {
		if !strings.Contains(string(b), k) {
			t.Fatalf("%s 없음: %s", k, b)
		}
	}
}

// 기록을 시작하기 전 달(11월, 토스 앱에서 직접 산 달)도 깨지지 않게 보인다.
func TestInvestFlowForAManualMonthWithoutRecords(t *testing.T) {
	now := etNow(2026, 11, 20)
	shares := 12
	led := []invest.LedgerEntry{{On: "2026-11-09", YM: "2026-11", AmountKRW: 1_017_316, Source: "execute", Symbol: "TQQQ", Shares: &shares}}
	f := buildInvestFlow(now, basePosition(), led, 600_000, nil, nil, investFlowInputs{})
	want := map[string]string{"judge": stepDone, "deposit": stepSkipped, "exchange": stepDone, "order": stepDone, "fill": stepDone}
	for k, st := range want {
		if s := stepOf(t, f, k); s.State != st {
			t.Fatalf("%s: %+v", k, s)
		}
	}
	if f.State != stepDone {
		t.Fatalf("%+v", f)
	}
}
