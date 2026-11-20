package run

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/yuntyu01/MyHome/server/internal/dbq"
	"github.com/yuntyu01/MyHome/server/internal/invest"
	"github.com/yuntyu01/MyHome/server/internal/invest/store"
)

// 체결 장부(invest_fills, 00025)와 position 문서 그림자 대조.

func (h *harness) fills() []invest.Fill {
	h.t.Helper()
	fs, err := h.st.LoadFills(h.ctx)
	if err != nil {
		h.t.Fatal(err)
	}
	return fs
}

func sides(fs []invest.Fill) string {
	var out []string
	for _, f := range fs {
		out = append(out, f.Source+":"+f.Side)
	}
	return strings.Join(out, ",")
}

// 손으로 기록 → 정정: 정정은 그 달 체결을 덮고(지우지 않고) 다시 쌓는다. 다시 쌓은 보유는 문서와 같다.
func TestAmendSupersedesTheMonthsFills(t *testing.T) {
	h := newHarness(t, seedOpt{acct: qqqm3()})
	if rc := h.app.Execute(h.ctx, ExecuteOpts{Date: "2026-08-03", Fills: map[string]float64{"TQQQ": 64.90, "QQQM": 283.29}, FX: 1420.6}); rc != 0 {
		t.Fatalf("execute rc %d %s", rc, h.err)
	}
	if got := sides(h.fills()); got != "opening:opening,execute:sell,execute:buy" {
		t.Fatalf("execute 뒤 체결 %s", got)
	}
	h.day = d(8, 5)
	if rc := h.app.Amend(h.ctx, AmendOpts{Date: "2026-08-05", Fills: map[string]float64{"TQQQ": 65.10, "QQQM": 283.0}, FX: 1418.2}); rc != 0 {
		t.Fatalf("amend rc %d %s", rc, h.err)
	}
	fs := h.fills()
	if got := sides(fs); got != "opening:opening,amend:sell,amend:buy" {
		t.Fatalf("amend 뒤 살아 있는 체결 %s", got)
	}
	// 정정 줄의 날짜는 체결일(실현손익 연도), 덮인 줄은 남아 있다
	if invest.FormatDay(fs[1].Day) != "2026-08-03" {
		t.Fatalf("정정 매도 날짜 %s", invest.FormatDay(fs[1].Day))
	}
	if _, err := h.st.LoadPosition(h.ctx); err != nil {
		t.Fatalf("정정 뒤 대조: %v", err)
	}
}

// 체결 장부가 생기기 전에 끝난 달을 정정하면 덮을 줄이 없다 — 정정한 계좌를 여는 잔고로 다시 놓는다.
func TestAmendingAMonthBeforeTheFillsTableRebases(t *testing.T) {
	h := newHarness(t, seedOpt{acct: qqqm3()})
	if rc := h.app.Execute(h.ctx, ExecuteOpts{Date: "2026-08-03", Fills: map[string]float64{"TQQQ": 64.90, "QQQM": 283.29}, FX: 1420.6}); rc != 0 {
		t.Fatalf("execute rc %d %s", rc, h.err)
	}
	// 마이그레이션이 이 달 체결 뒤에 일어난 것처럼: 그 달 체결을 지우고 지금 계좌를 여는 잔고로
	p := h.pos()
	h.supersedeAll()
	if err := h.st.Rebase(h.ctx, p.Holdings, d(8, 4), p.ApplyYM); err != nil {
		t.Fatal(err)
	}
	if rc := h.app.Amend(h.ctx, AmendOpts{Date: "2026-08-05", Fills: map[string]float64{"TQQQ": 65.10, "QQQM": 283.0}, FX: 1418.2}); rc != 0 {
		t.Fatalf("amend rc %d %s", rc, h.err)
	}
	// 전환 달이라 실현손익 줄이 따라온다(여는 잔고 → 정정한 잔고)
	if got := sides(h.fills()); got != "opening:opening,opening:realized,amend:opening,amend:realized" {
		t.Fatalf("체결 %s", got)
	}
}

// 문서와 체결이 어긋나면(손으로 고친 문서 등) 어떤 명령도 돌지 않고 실패로 알린다.
func TestPositionShadowMismatchStopsCommands(t *testing.T) {
	h := newHarness(t, seedOpt{acct: qqqm3()})
	p := h.pos()
	p.Holdings.Shares = 4 // 체결은 3주
	doc := mustJSON(t, p)
	if err := dbq.New(h.pg).UpsertInvestState(h.ctx, dbq.UpsertInvestStateParams{Key: "position", Doc: doc}); err != nil {
		t.Fatal(err)
	}
	var shadow *store.ShadowError
	if _, err := h.st.LoadPosition(h.ctx); !errors.As(err, &shadow) || !strings.Contains(err.Error(), "수량 체결 3 · 문서 4") {
		t.Fatalf("어긋났는데 읽었다: %v", err)
	}
	for name, run := range map[string]func() int{
		"status": func() int { return h.app.Status(h.ctx, StatusOpts{Date: "2026-08-10"}) },
		"trade":  func() int { return h.trade(execDay, false) },
		"judge":  func() int { return h.app.Judge(h.ctx, JudgeOpts{Date: "2026-08-31"}) },
	} {
		h.err.Reset()
		if rc := run(); rc != 1 || !strings.Contains(h.err.String(), "그림자 대조") {
			t.Errorf("%s: rc %d %s", name, rc, h.err)
		}
	}
	if len(h.fb.placed) != 0 {
		t.Fatal("어긋난 채로 주문했다")
	}
	// 저장도 막는다 — 체결 없이 보유를 바꾼 문서는 쓰이지 않는다
	if err := h.st.SavePosition(h.ctx, p); !errors.As(err, &shadow) {
		t.Fatalf("어긋난 문서를 저장했다: %v", err)
	}
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// supersedeAll은 체결 장부를 전부 덮는다(체결 장부가 생기기 전 상태 흉내).
func (h *harness) supersedeAll() {
	h.t.Helper()
	if _, err := h.pg.Exec(h.ctx, "UPDATE invest_fills SET superseded_at = now() WHERE superseded_at IS NULL"); err != nil {
		h.t.Fatal(err)
	}
}
