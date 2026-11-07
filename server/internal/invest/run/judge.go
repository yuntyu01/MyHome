package run

import (
	"context"
	"time"

	"github.com/yuntyu01/MyHome/server/internal/invest"
	"github.com/yuntyu01/MyHome/server/internal/invest/store"
)

// JudgeOpts는 judge 옵션이다.
type JudgeOpts struct {
	Date   string
	DryRun bool
}

// Judge는 월말 판정이다. 포지션은 건드리지 않는다.
func (a *App) Judge(ctx context.Context, o JudgeOpts) int {
	today, err := a.today(o.Date)
	if err != nil {
		return a.fail(ExitFail, "%v", err)
	}
	prices, err := a.Store.LoadPrices(ctx)
	if err != nil {
		return a.fail(ExitFail, "마스터 읽기 실패: %v", err)
	}
	in := prices.SignalInputs()
	if err := a.gate(in); err != nil {
		return a.fail(ExitFail, "%v", err)
	}
	pos, err := a.Store.LoadPosition(ctx)
	if err != nil {
		return a.fail(ExitFail, "%v", err)
	}

	day := JudgmentDay(today, in.Days, pos.YM)
	if day.IsZero() {
		a.printf("%s에 판정할 달이 없다. 아무것도 하지 않는다.", invest.FormatDay(today))
		return ExitOK
	}
	if !day.Equal(today) {
		a.printf("⚠ %s 판정을 %s에 만회한다 — 실행이 뉴욕 자정을 넘겨 밀렸다", invest.FormatDay(day), invest.FormatDay(today))
	}

	d, err := invest.LatestDecision(in, invest.SignalStart)
	if err != nil {
		return a.fail(ExitFail, "%v", err)
	}
	if d.Day.Before(day) {
		return a.fail(ExitFail, "데이터 최신일(%s)이 판정일(%s)에 못 미친다. refresh 먼저.", invest.FormatDay(d.Day), invest.FormatDay(day))
	}
	if d.Day.After(day) {
		return a.fail(ExitFail, "다음 달 종가(%s)가 이미 들어왔다 — %s 판정은 만회 창을 넘겼다. 이 데이터로 판정하면 룩어헤드다",
			invest.FormatDay(d.Day), invest.YM(day))
	}

	changed := pos.Signal.Target != "" && pos.Signal.Target != d.Pos
	if o.DryRun {
		a.printf("[dry-run] %s 판정 → %s %s", d.YM, d.ApplyYM, d.Pos)
		return ExitOK
	}

	// 🔴 executed_on은 적용월이 같을 때만 이어받는다. 조건 없이 이월하면 새 적용월이 지난달
	// 체결일을 물려받아 미체결 감지와 미실행 기록이 동시에 무력화된다
	sameApply := pos.ApplyYM == d.ApplyYM
	judged := pos.JudgmentCount
	if pos.YM != d.YM { // 같은 달을 다시 판정해도 늘지 않는다
		judged++
	}
	next := invest.Position{
		Holdings: pos.Holdings, LastRun: invest.FormatDay(d.Day), YM: d.YM,
		Signal: invest.Judgment{SPX: d.SPX, SMA185: d.SMA185, C1: d.C1, Vol: d.Vol, C2: d.C2, Target: d.Pos},
		FXRate: pos.FXRate,
		// 환율을 이어받으면 그 환율이 추정인지도 이어받는다 — 판정 한 번에 추정값이 확정값으로 둔갑하지 않게
		FXEstimated: pos.FXEstimated, ApplyYM: d.ApplyYM, ContributedKRW: pos.ContributedKRW,
		MonthsInvested: pos.MonthsInvested, JudgmentCount: judged, CloseUSD: pos.CloseUSD,
	}
	if sameApply {
		next.ExecutedOn, next.ExecutedYM = pos.ExecutedOn, pos.ExecutedYM
	}
	var missed *invest.Deviation
	err = a.Store.InTx(ctx, func(s *store.Store) error {
		if err := s.SavePosition(ctx, next); err != nil {
			return err
		}
		missed, err = recordMissedMonth(ctx, s, pos, today)
		return err
	})
	if err != nil {
		return a.fail(ExitFail, "저장 실패: %v", err)
	}

	a.printf("%s 판정 (%s)", d.YM, invest.FormatDay(d.Day))
	a.printf("  ① SPX %s vs SMA185 %s  %s", plain2(d.SPX), plain2(d.SMA185), mark(d.C1))
	a.printf("  ② 변동성 %.2f%%                     %s", d.Vol*100, mark(d.C2))
	buy := invest.MustTicker(d.Pos)
	tail := "   (첫 매수)"
	switch {
	case pos.Holdings.Symbol != "" && pos.Holdings.Symbol != buy:
		tail = "   (현재 " + pos.Holdings.Symbol + " · 교체 필요)"
	case pos.Holdings.Symbol != "":
		tail = "   (유지)"
	}
	a.printf("  → %s %s%s", d.ApplyYM, buy, tail)
	if changed {
		a.printf("  ⚠ 신호 변경")
	}
	if missed != nil {
		a.printf("  🔴 %s 매수 없음 — 이탈로 기록했다.", missed.YM)
		a.printf("     사유도 잠금도 남지 않는 이탈이라 자동 기록한다.")
	}
	return ExitOK
}

func mark(ok bool) string {
	if ok {
		return "✓"
	}
	return "✗"
}

// recordMissedMonth는 직전 적용월에 매수가 없었으면 기록한다.
//
// 안 사는 것이 가장 조용한 이탈이다. executed_on만 보면 지난달 체결일이 이월돼 있을 때 그것을 이번 달
// 매수로 착각한다 — 어느 달의 체결인지(executed_ym)까지 본다.
func recordMissedMonth(ctx context.Context, s *store.Store, prev invest.Position, today time.Time) (*invest.Deviation, error) {
	ym := prev.ApplyYM
	if ym == "" || prev.ExecutedThisMonth() {
		return nil, nil
	}
	t, err := time.Parse("2006-01", ym)
	if err != nil {
		return nil, err
	}
	if today.Before(invest.FirstTradingDay(t.Year(), t.Month())) { // 아직 실행일이 안 왔다
		return nil, nil
	}
	tgt, err := invest.Ticker(prev.Signal.Target)
	if err != nil {
		return nil, err
	}
	return s.RecordMissedMonth(ctx, ym, tgt, today)
}
