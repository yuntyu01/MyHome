package run

import (
	"errors"
	"strings"
	"testing"

	"github.com/yuntyu01/MyHome/server/internal/dbq"
	"github.com/yuntyu01/MyHome/server/internal/invest"
	"github.com/yuntyu01/MyHome/server/internal/invest/broker"
	"github.com/yuntyu01/MyHome/server/internal/invest/store"
)

// 주문 기록 표(invest_orders, 00024). 뜻은 orders 문서와 같고, 상태가 바뀔 때마다 전이 기록이 남는다.

func (h *harness) events() []string {
	h.t.Helper()
	evs, err := h.st.OrderEvents(h.ctx)
	if err != nil {
		h.t.Fatal(err)
	}
	var out []string
	for _, e := range evs {
		out = append(out, e.ClientOrderID+" "+e.State)
	}
	return out
}

// 결과 모름 → 접수 → 체결, 그리고 묶음이 닫혀도 주문 행과 전이 기록은 남는다.
func TestOrderTableKeepsEveryTransition(t *testing.T) {
	h := newHarness(t, seedOpt{})
	if rc := h.trade(execDay, false); rc != 0 {
		t.Fatalf("rc %d %s", rc, h.err)
	}
	id := h.fb.placed[0].OrderID
	cid := h.fb.placed[0].ClientID
	h.fb.fill(id, h.fb.placed[0].Qty, 64.9, "")
	if rc := h.trade(d(8, 4), false); rc != 0 {
		t.Fatalf("rc %d %s", rc, h.err)
	}
	if h.book() != nil {
		t.Fatal("체결을 반영했는데 주문 묶음이 열려 있다")
	}
	want := []string{cid + " unknown", cid + " open", cid + " filled"}
	if got := h.events(); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("전이 기록 %v, 기대 %v", got, want)
	}
}

// 명시적 거절은 묶음에서 빠지되 지워지지 않는다(dropped). 다음 유형 주문은 새 행이다.
func TestRejectedOrderIsDroppedNotDeleted(t *testing.T) {
	h := newHarness(t, seedOpt{})
	h.fb.rejects[broker.MOC] = true
	if rc := h.trade(execDay, false); rc != 0 {
		t.Fatalf("rc %d %s", rc, h.err)
	}
	b := h.book()
	if b == nil || len(b.Orders) != 1 || b.Orders[0].Style != string(broker.Market) {
		t.Fatalf("묶음 %+v", b)
	}
	got := strings.Join(h.events(), ",")
	if !strings.Contains(got, "-moc unknown") || !strings.Contains(got, "-moc dropped") || !strings.Contains(got, "-market open") {
		t.Fatalf("전이 기록 %s", got)
	}
}

// 결과 모름이었는데 증권사에 없던 주문을 같은 날 같은 유형으로 다시 내면 같은 clientOrderId다 — 같은 행을 다시 쓴다.
func TestResubmittingTheSameKeyReusesTheRow(t *testing.T) {
	h := newHarness(t, seedOpt{})
	h.fb.timeout[broker.MOC] = true // 접수되지 않은 채 끊긴다
	if rc := h.trade(execDay, false); rc != 1 {
		t.Fatalf("결과 모름인데 rc %d", rc)
	}
	h.fb.timeout[broker.MOC] = false
	if rc := h.trade(execDay, false); rc != 0 {
		t.Fatalf("rc %d %s", rc, h.err)
	}
	if len(h.fb.placed) != 1 {
		t.Fatalf("접수 %d건", len(h.fb.placed))
	}
	cid := h.fb.placed[0].ClientID
	want := []string{cid + " unknown", cid + " dropped", cid + " unknown", cid + " open"}
	if got := h.events(); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("전이 기록 %v, 기대 %v", got, want)
	}
	if b := h.book(); b == nil || len(b.Orders) != 1 || b.Orders[0].OrderID == "" {
		t.Fatalf("묶음 %+v", b)
	}
}

// 표와 orders 문서(그림자)가 다르면 읽지 않는다 — trade는 주문하지 않고 실패한다.
func TestOrdersShadowMismatchStopsTrade(t *testing.T) {
	st, q, ctx := newStore(t)
	book := invest.OrderBook{YM: "2026-08", Stage: "buy", Orders: []invest.BookOrder{{
		OrderID: "ODNO7", ClientOrderID: "tq-20260803-TQQQ-buy-6-moc", Symbol: "TQQQ", Side: "buy", Qty: 6, Price: 71.08,
		Style: "moc", SubmittedOn: "2026-08-03", SubmittedAt: "2026-08-03T15:17:00Z",
	}}}
	if err := st.SaveOrders(ctx, book); err != nil {
		t.Fatal(err)
	}
	got, err := st.LoadOrders(ctx)
	if err != nil || got == nil || got.Orders[0] != book.Orders[0] {
		t.Fatalf("왕복 %+v %v", got, err)
	}
	// 문서만 바뀐 상태(손으로 고침 등)
	if err := q.UpsertInvestState(ctx, dbq.UpsertInvestStateParams{Key: "orders", Doc: []byte(`{"ym":"2026-08","stage":"buy","style_idx":0,"orders":[]}`)}); err != nil {
		t.Fatal(err)
	}
	var shadow *store.ShadowError
	if _, err := st.LoadOrders(ctx); !errors.As(err, &shadow) {
		t.Fatalf("어긋났는데 읽었다: %v", err)
	}
}
