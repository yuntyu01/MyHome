package run

import (
	"strings"
	"testing"
	"time"

	"github.com/yuntyu01/MyHome/server/internal/dbq"
)

// 전환 달 적립금 추가 매수와 매수 완료 알림(사용자 요청, 2026-11-20).

func (h *harness) alerts() []dbq.InvestAlert {
	h.t.Helper()
	as, err := h.st.AlertsAfter(h.ctx, 0, time.Time{}, 100)
	if err != nil {
		h.t.Fatal(err)
	}
	return as
}

func (h *harness) doneAlerts() []dbq.InvestAlert {
	var out []dbq.InvestAlert
	for _, a := range h.alerts() {
		if a.Kind == "done" {
			out = append(out, a)
		}
	}
	return out
}

// switchWithoutContribution은 QQQM 3주 → TQQQ 전환 달에 적립금이 아직 안 들어온 채 매도·매수까지 끝낸다.
func switchWithoutContribution(t *testing.T) *harness {
	t.Helper()
	h := newHarness(t, seedOpt{acct: qqqm3()})
	h.fb.held["QQQM"] = 3
	h.fb.limit = 1                              // 적립금은 아직 없다(가짜 증권사는 0을 무제한으로 본다)
	if rc := h.trade(execDay, false); rc != 0 { // MOC 매도
		t.Fatalf("매도 rc %d %s", rc, h.err)
	}
	h.fb.fill(h.last().OrderID, 3, 283.00, "") // 매도대금만 주문가능금액에 들어온다(한도 0 − 매도)
	if rc := h.trade(d(8, 4), false); rc != 0 {
		t.Fatalf("매수 rc %d %s", rc, h.err)
	}
	if !strings.Contains(h.out.String(), "매도대금으로 먼저 사고") {
		t.Fatalf("적립금이 없는데 전환 매수를 미뤘거나 알리지 않았다:\n%s\n%s", h.out, h.err)
	}
	buy := h.last()
	h.fb.fill(buy.OrderID, buy.Qty, 64.90, "")
	h.out.Reset()
	if rc := h.trade(d(8, 5), false); rc != 0 {
		t.Fatalf("체결 반영 rc %d %s", rc, h.err)
	}
	p := h.pos()
	if !p.ExecutedThisMonth() || p.TopUpKRW <= 0 || p.Holdings.Symbol != "TQQQ" || p.Holdings.Shares != buy.Qty {
		t.Fatalf("전환 반영 %+v", p)
	}
	if len(h.doneAlerts()) != 0 {
		t.Fatal("적립금이 남았는데 완료 알림을 보냈다")
	}
	return h
}

// 🔴 전환 달은 매도대금으로 먼저 사고, 늦게 들어온 적립금은 같은 달 안에 더 산다. 원금 장부에 정상 기록된다.
func TestSwitchMonthTopsUpTheLateContribution(t *testing.T) {
	h := switchWithoutContribution(t)
	first := h.pos()

	// 아직 안 들어왔다 — 입금 대기(알림 없음)
	h.out.Reset()
	if rc := h.trade(d(8, 6), false); rc != 0 || !strings.Contains(h.out.String(), "전환 달 남은 적립금") || !strings.Contains(h.out.String(), "입금 대기") {
		t.Fatalf("rc %d %s %s", rc, h.out, h.err)
	}
	placed := len(h.fb.placed)
	// 들어왔다 — 남은 적립금만큼 더 산다(계좌에 더 있어도 그만큼만)
	h.fb.limit = 422 + 5000
	if rc := h.trade(d(8, 6), false); rc != 0 || len(h.fb.placed) != placed+1 {
		t.Fatalf("들어왔는데 더 사지 않았다: rc %d %s", rc, h.err)
	}
	top := h.last()
	if top.Symbol != "TQQQ" || top.Qty <= 0 || top.Qty > planQtyAt(64.62*1.05)+1 {
		t.Fatalf("추가 매수 %+v", top)
	}
	h.fb.fill(top.OrderID, top.Qty, 65.10, "")
	if rc := h.trade(d(8, 7), false); rc != 0 {
		t.Fatalf("추가 매수 반영 rc %d %s", rc, h.err)
	}
	p := h.pos()
	if p.TopUpKRW != 0 || p.Holdings.Shares != first.Holdings.Shares+top.Qty || p.MonthsInvested != first.MonthsInvested ||
		*p.ExecutedOn != *first.ExecutedOn || p.ContributedKRW <= first.ContributedKRW || p.Before == nil {
		t.Fatalf("추가 매수 반영 %+v (전환 뒤 %+v)", p, first)
	}
	// 원금 장부: 같은 달 자동매수 두 줄, 둘째는 "적립 추가 매수". 총액과 맞는다
	var rows []string
	for _, e := range h.ledger() {
		if e.YM == "2026-08" {
			rows = append(rows, e.Source+":"+e.Note)
		}
	}
	if len(rows) != 2 || !strings.Contains(rows[1], "적립 추가 매수") {
		t.Fatalf("원금 장부 %v", rows)
	}
	if rc := h.app.Ledger(h.ctx); rc != 0 {
		t.Fatalf("원금 대조 %s", h.out)
	}
	// 체결 장부와 문서가 같다(그림자 대조) — 매도 1 + 매수 2
	if got := sides(h.fills()); got != "opening:opening,trade:sell,trade:buy,trade:buy" {
		t.Fatalf("체결 %s", got)
	}
	// 그 달이 끝났다 — 완료 알림 한 번
	done := h.doneAlerts()
	if len(done) != 1 || *done[0].Ym != "2026-08" || done[0].Title != "8월 매수 완료" {
		t.Fatalf("완료 알림 %+v", done)
	}
	// 본문은 종목·수량·이번 달 원금만 — 수익률·평가액 없음
	if b := done[0].Body; !strings.HasPrefix(b, "TQQQ ") || !strings.Contains(b, "이번 달 원금") || strings.Contains(b, "%") || strings.Contains(b, "평가") {
		t.Fatalf("본문 %q", b)
	}
}

// 전환 달 남은 적립금이 마감(10일)까지 안 들어오면 실패로 알린다. 전환 자체는 끝난 상태로 둔다.
func TestSwitchMonthTopUpFailsAtTheDeadline(t *testing.T) {
	h := switchWithoutContribution(t)
	if rc := h.trade(d(8, 10), false); rc != 1 || !strings.Contains(h.err.String(), "전환 달 남은 적립금이 마감(2026-08-10)까지 들어오지 않았다") {
		t.Fatalf("rc %d %s", rc, h.err)
	}
	if p := h.pos(); !p.ExecutedThisMonth() || p.TopUpKRW <= 0 {
		t.Fatalf("%+v", p)
	}
	// 다음 판정이 새 적용월을 열면 남은 추가 매수는 사라진다(그 돈은 다음 달 매수에 쓰인다)
	h.day = d(8, 31)
	if rc := h.judge("2026-07-31"); rc != 0 { // 같은 적용월 다시 판정 — 그대로 둔다
		t.Fatalf("rc %d %s", rc, h.err)
	}
	if h.pos().TopUpKRW <= 0 {
		t.Fatal("같은 적용월을 다시 판정했는데 남은 추가 매수를 지웠다")
	}
}

// 완료 알림은 그 달 한 번뿐이다 — 매매 작업이 다시 돌아도, 직접 다시 남겨도.
func TestDoneAlertIsSentOncePerMonth(t *testing.T) {
	h := newHarness(t, seedOpt{})
	if rc := h.trade(execDay, false); rc != 0 {
		t.Fatalf("rc %d %s", rc, h.err)
	}
	h.fb.fill(h.last().OrderID, h.last().Qty, 64.90, "")
	for _, day := range []int{4, 5, 6} {
		if rc := h.trade(d(8, day), false); rc != 0 {
			t.Fatalf("8/%d rc %d %s", day, rc, h.err)
		}
	}
	if n := len(h.doneAlerts()); n != 1 {
		t.Fatalf("완료 알림 %d건", n)
	}
	if ok, err := h.st.AddDoneAlert(h.ctx, "2026-08", "8월 매수 완료", "다시"); err != nil || ok {
		t.Fatalf("같은 달을 또 남겼다: %v %v", ok, err)
	}
	if ok, err := h.st.AddDoneAlert(h.ctx, "2026-09", "9월 매수 완료", "다음 달"); err != nil || !ok {
		t.Fatalf("다음 달은 남겨야 한다: %v %v", ok, err)
	}
	// 실패 알림은 kind fail로 따로 쌓인다(같은 달 여러 번 가능)
	h.day = d(8, 6)
	if rc := h.app.Alert(h.ctx, AlertOpts{Unit: "myhome-invest-trade.service"}); rc != 0 {
		t.Fatalf("rc %d %s", rc, h.err)
	}
	kinds := map[string]int{}
	for _, a := range h.alerts() {
		kinds[a.Kind]++
	}
	if kinds["done"] != 2 || kinds["fail"] != 1 {
		t.Fatalf("%v", kinds)
	}
}
