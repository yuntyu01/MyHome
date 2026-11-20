package run

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/yuntyu01/MyHome/server/internal/invest"
	"github.com/yuntyu01/MyHome/server/internal/invest/broker"
)

// 이번 달 자동 매수 단계(입금 → 환전 → 주문 → 체결)를 증권사에서 본 대로 남긴다(사용자 요청, 2026-11-20).
// 웹·앱 투자 화면이 이 기록으로 단계를 체크한다. 매매 판단에 쓰는 것은 입금뿐이다(입금일 게이트).
// 전부 표시용이라 기록에 실패해도 매매는 그대로 간다.

// observeTimeout은 입금·환전 확인에 쓰는 시간의 상한이다. 넘으면 못 본 것으로 하고 매매를 이어간다.
const observeTimeout = 60 * time.Second

// observed는 이번 적용월에 증권사에서 본 것이다.
type observed struct {
	DepositKRW float64 // 원화 입금 합계(한국 달력으로 적용월 1일부터)
	OK         bool    // 내역을 읽었는가. false면 입금을 못 본 것이 아니라 모르는 것이다
}

// observe는 적용월의 입금·환전과 원화·달러 주문가능 금액을 읽어 남긴다. 읽기 전용이다.
// 증권사가 CashFlows를 구현하지 않았거나 client가 없으면(dry-run) 아무것도 안 한다.
func (a *App) observe(ctx context.Context, client broker.Broker, ym string) observed {
	if client == nil || a.Store == nil {
		return observed{}
	}
	cf, ok := client.(broker.CashFlows)
	if !ok {
		return observed{}
	}
	start, err := time.ParseInLocation("2006-01", ym, broker.KST)
	if err != nil {
		return observed{}
	}
	ctx, cancel := context.WithTimeout(ctx, observeTimeout)
	defer cancel()

	if krw, err := cf.CashKRW(ctx); err != nil {
		a.warnf("⚠ 원화 주문가능 금액 조회 실패(매매와 무관): %v", err)
	} else if bal, err := client.Balance(ctx); err != nil {
		a.warnf("⚠ 달러 주문가능 금액 조회 실패(매매와 무관): %v", err)
	} else if err := a.Store.SaveBrokerCashBoth(ctx, bal.CashUSD, krw, a.now()); err != nil {
		a.warnf("⚠ 주문가능 금액 기록 실패(매매와 무관): %v", err)
	}

	flows, err := cf.CashFlows(ctx, start)
	if err != nil {
		a.warnf("⚠ 입금·환전 내역 조회 실패(매매와 무관): %v", err)
		return observed{}
	}
	known := map[string]bool{}
	if prev, err := a.Store.LoadFlow(ctx, ym); err == nil {
		for _, e := range prev {
			known[e.Step+" "+e.Key] = true
		}
	}
	var out observed
	out.OK = true
	for _, f := range flows {
		switch {
		case f.Kind == broker.FlowDeposit && f.Currency == "KRW":
			out.DepositKRW += f.Amount
			a.addFlow(ctx, known, invest.FlowEvent{YM: ym, Step: invest.StepDeposit,
				Key: fmt.Sprintf("%s:%.0f", f.At.UTC().Format(time.RFC3339), f.Amount), At: f.At, KRW: f.Amount})
		case f.Kind == broker.FlowExchangeIn && f.Currency == "USD":
			// 원화 → 달러 환전. 낸 원화는 같은 시각의 KRW 쪽 줄에 있다
			e := invest.FlowEvent{YM: ym, Step: invest.StepExchange,
				Key: fmt.Sprintf("%s:%.2f", f.At.UTC().Format(time.RFC3339), f.Amount), At: f.At, USD: f.Amount, FX: f.FX,
				KRW: pairedKRW(flows, f.At)}
			if e.KRW == 0 && f.FX > 0 {
				e.KRW = math.Round(f.Amount * f.FX)
			}
			if known[e.Step+" "+e.Key] {
				continue
			}
			// 그 시각의 매매기준율 — 적용 환율이 기준율보다 얼마나 비쌌는지(환전 비용)를 보여 준다
			if src, ok := client.(broker.Settlements); ok {
				if q, err := src.FXAt(ctx, f.At); err == nil {
					e.Mid = q.Mid
				}
			}
			a.addFlow(ctx, known, e)
		}
	}
	return out
}

// pairedKRW는 같은 환전의 원화 쪽 금액이다(처리 시각이 같은 EXCHANGE_OUT). 없으면 0.
func pairedKRW(flows []broker.CashFlow, at time.Time) float64 {
	best, gap := 0.0, time.Minute
	for _, f := range flows {
		if f.Kind != broker.FlowExchangeOut || f.Currency != "KRW" {
			continue
		}
		d := f.At.Sub(at)
		if d < 0 {
			d = -d
		}
		if d <= gap {
			best, gap = f.Amount, d
		}
	}
	return best
}

// addFlow는 사건 하나를 남긴다. 이미 있으면 건너뛴다. 실패해도 매매와 무관하다.
func (a *App) addFlow(ctx context.Context, known map[string]bool, e invest.FlowEvent) {
	if a.Store == nil {
		return
	}
	if known != nil {
		if known[e.Step+" "+e.Key] {
			return
		}
		known[e.Step+" "+e.Key] = true
	}
	if _, err := a.Store.AddFlow(ctx, e); err != nil {
		a.warnf("⚠ 매수 단계 기록 실패(매매와 무관): %v", err)
	}
}

// waitForDeposit은 입금일 게이트다. 이번 달 주문을 아직 하나도 안 냈을 때만 부른다(낸 주문은 끝까지 따라간다).
// true면 오늘은 사지 않고 기다린다.
//
// 입금일을 정하지 않았으면 예전처럼 바로 산다. 정했으면 이번 달 입금이 적립액의 절반 이상 보일 때 사고,
// 입금일 + DepositGraceDays가 지나도 안 보이면 있는 돈으로 산다(못 사는 것보다 낫다). 입금이 일찍 들어오면 일찍 산다.
func (a *App) waitForDeposit(ctx context.Context, today time.Time, ym string, seen observed, dryRun bool) bool {
	dd, err := a.Store.DepositDay(ctx)
	if err != nil {
		a.warnf("⚠ 입금일을 읽지 못했다 — 기다리지 않고 산다: %v", err)
		return false
	}
	if dd == 0 {
		return false
	}
	plan, err := a.Store.ContributionFor(ctx, ym)
	if err != nil {
		plan = 0
	}
	if invest.DepositSeen(seen.DepositKRW, float64(plan)) {
		a.printf("  입금 확인 — 이번 달 %s원 (입금일 %d일)", won(seen.DepositKRW), dd)
		return false
	}
	deadline, ok := invest.BuyDeadlineOf(ym, dd)
	if !ok {
		return false
	}
	if today.Before(deadline) {
		if dryRun {
			a.printf("[dry-run] 입금일 %d일 — 실제 실행은 입금이 보이거나 %s가 될 때까지 기다린다", dd, invest.FormatDay(deadline))
			return false
		}
		what := "이번 달 입금 " + won(seen.DepositKRW) + "원"
		if !seen.OK {
			what = "입금 내역을 읽지 못함"
		}
		a.printf("%s 입금 기다림 — 입금일 %d일 · %s. %s까지 안 보이면 있는 돈으로 산다", ym, dd, what, invest.FormatDay(deadline))
		return true
	}
	a.warnf("⚠ 입금이 안 보인다 — 입금일 %d일에서 %d일이 지났다. 있는 돈으로 산다", dd, invest.DepositGraceDays)
	return false
}

// noteOrder는 증권사가 받은 주문을 남긴다(구매 신청 단계).
func (a *App) noteOrder(ctx context.Context, ym string, o invest.BookOrder) {
	if o.OrderID == "" {
		return
	}
	a.addFlow(ctx, nil, invest.FlowEvent{YM: ym, Step: invest.StepOrder, Key: o.OrderID, At: a.now(),
		Symbol: o.Symbol, Side: o.Side, Qty: o.Qty, Price: o.Price, Style: broker.Style(o.Style).Label(), OrderID: o.OrderID})
}

// noteFill은 확인한 체결을 남긴다(구매 성공 단계). 시각은 체결 시각이 아니라 매매 작업이 확인한 시각이다.
func (a *App) noteFill(ctx context.Context, ym string, o invest.BookOrder) {
	if o.OrderID == "" || o.Qty <= 0 {
		return
	}
	a.addFlow(ctx, nil, invest.FlowEvent{YM: ym, Step: invest.StepFill, Key: o.OrderID, At: a.now(),
		Symbol: o.Symbol, Side: o.Side, Qty: o.Qty, Price: o.FillPrice, Style: broker.Style(o.Style).Label(), OrderID: o.OrderID})
}
