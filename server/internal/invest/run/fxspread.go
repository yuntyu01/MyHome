package run

import (
	"context"
	"math"
	"time"

	"github.com/yuntyu01/MyHome/server/internal/invest"
	"github.com/yuntyu01/MyHome/server/internal/invest/broker"
)

// 실제 환전 스프레드 (사용자 요청, 2026-11-20 "지금까지 체결의 실제 환전 스프레드(거래내역 환율 vs usdkrw_mkt)").
//
// 토스 Open API에는 환전 주문이 없다(v1.2.28). 통합증거금이 주문·결제 때 알아서 바꾸고, 그 환율은 결제가 끝난
// 거래내역에만 실린다. 이 명령은 읽기만 해서 두 가지를 견준다:
//   - 결제 내역(체결): 적용 환율 vs 그 체결 거래일의 usdkrw_mkt(뉴욕 16:00 매매기준율, 가격 마스터)
//   - 환전 거래(EXCHANGE_IN): 적용 환율 vs 그 시각 토스 매매기준율(같은 시각이라 더 정확하다)
// 차이가 곧 환전 비용이다(살 때는 +, 팔 때는 −가 정상). 원금 장부·계좌는 건드리지 않는다.

// FXSpreadOpts는 fx-spread 옵션이다.
type FXSpreadOpts struct {
	From string // YYYY-MM-DD(한국 날짜). 비우면 원금 내역의 첫 날
}

type spreadSum struct {
	n          int
	usd, wDiff float64 // 금액 가중 합
}

func (s *spreadSum) add(usdAmt, diff float64) {
	s.n++
	s.usd += usdAmt
	s.wDiff += usdAmt * diff
}

func (s spreadSum) avg() float64 {
	if s.usd <= 0 {
		return math.NaN()
	}
	return s.wDiff / s.usd
}

// FXSpread는 지금까지 체결·환전의 적용 환율이 기준율과 얼마나 달랐는지 보여 준다. 읽기 전용이다.
func (a *App) FXSpread(ctx context.Context, o FXSpreadOpts) int {
	from, err := a.spreadFrom(ctx, o.From)
	if err != nil {
		return a.fail(ExitFail, "%v", err)
	}
	prices, err := a.Store.LoadPrices(ctx)
	if err != nil {
		return a.fail(ExitFail, "마스터 읽기 실패: %v", err)
	}
	client, err := a.Broker(ctx)
	if err != nil {
		return a.fail(ExitFail, "증권사 연결 실패: %v", err)
	}
	to := broker.Today(a.now())
	a.printf("환전 스프레드 %s ~ %s (적용 환율 ÷ 기준율 − 1, 살 때 +가 비용)", invest.FormatDay(from), invest.FormatDay(to))

	if src, ok := client.(broker.Settlements); ok {
		a.printf("\n결제 내역 — 기준은 그 체결 거래일의 usdkrw_mkt(뉴욕 16:00)")
		var buy, sell spreadSum
		for _, sym := range invest.Tickers {
			rows, err := src.Settlements(ctx, sym, from, to)
			if err != nil {
				return a.fail(ExitFail, "%s 결제 내역 조회 실패: %v", sym, err)
			}
			for _, r := range rows {
				if !r.Settled || r.FX <= 0 {
					continue
				}
				day, mid, ok := midBefore(prices, r.Day)
				if !ok {
					a.printf("  %s %s %s %d주 · 적용 %s · 기준율 없음", invest.FormatDay(r.Day), r.Symbol, sideWordKR(r.Side), r.Qty, plain2(r.FX))
					continue
				}
				diff := r.FX/mid - 1
				amt := r.Net
				if amt <= 0 {
					amt = r.Amount
				}
				a.printf("  결제 %s(거래 %s) %s %s %d주 · 적용 %s · 기준 %s · %+.2f%%", invest.FormatDay(r.Day), invest.FormatDay(day),
					r.Symbol, sideWordKR(r.Side), r.Qty, plain2(r.FX), plain2(mid), diff*100)
				if r.Side == broker.Sell {
					sell.add(amt, diff)
				} else {
					buy.add(amt, diff)
				}
			}
		}
		a.printSpread("매수", buy)
		a.printSpread("매도", sell)
	} else {
		a.printf("\n결제 내역 — 이 증권사 연결은 결제 내역을 주지 않는다")
	}

	if cf, ok := client.(broker.CashFlows); ok {
		a.printf("\n환전 거래(원 → 달러) — 기준은 그 시각 토스 매매기준율")
		flows, err := cf.CashFlows(ctx, from)
		if err != nil {
			return a.fail(ExitFail, "환전 내역 조회 실패: %v", err)
		}
		src, _ := client.(broker.Settlements)
		var ex spreadSum
		for _, f := range flows {
			if f.Kind != broker.FlowExchangeIn || f.Currency != "USD" || f.FX <= 0 {
				continue
			}
			if src == nil {
				continue
			}
			q, err := src.FXAt(ctx, f.At)
			if err != nil || q.Mid <= 0 {
				a.printf("  %s $%s · 적용 %s · 그 시각 기준율 못 받음", f.At.In(broker.KST).Format("2006-01-02 15:04"), plain2(f.Amount), plain2(f.FX))
				continue
			}
			diff := f.FX/q.Mid - 1
			a.printf("  %s $%s · 적용 %s · 기준 %s · %+.2f%%", f.At.In(broker.KST).Format("2006-01-02 15:04"), plain2(f.Amount), plain2(f.FX), plain2(q.Mid), diff*100)
			ex.add(f.Amount, diff)
		}
		a.printSpread("환전", ex)
	}
	return ExitOK
}

func (a *App) printSpread(what string, s spreadSum) {
	if s.n == 0 {
		a.printf("  %s 없음", what)
		return
	}
	a.printf("  %s %d건 · 금액 가중 평균 %+.2f%% ($%s)", what, s.n, s.avg()*100, plain2(s.usd))
}

// spreadFrom은 조회 시작일(한국 날짜)이다. 비우면 원금 내역의 첫 날.
func (a *App) spreadFrom(ctx context.Context, from string) (time.Time, error) {
	if from != "" {
		d, err := invest.ParseDay(from)
		if err != nil {
			return time.Time{}, err
		}
		return time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, broker.KST), nil
	}
	rows, err := a.Store.LoadLedger(ctx)
	if err != nil {
		return time.Time{}, err
	}
	first := a.now().In(broker.KST).AddDate(-1, 0, 0)
	for _, r := range rows {
		if d, err := invest.ParseDay(r.On); err == nil && d.Before(first) {
			first = d
		}
	}
	return time.Date(first.Year(), first.Month(), first.Day(), 0, 0, 0, 0, broker.KST), nil
}

// midBefore는 결제일(한국 날짜) 앞의 마지막 거래일과 그날 usdkrw_mkt다. 미국 주식은 T+1 결제라 그 앞 거래일이 체결일이다.
func midBefore(prices invest.Prices, settled time.Time) (time.Time, float64, bool) {
	key := settled.Format("2006-01-02")
	for i := len(prices) - 1; i >= 0; i-- {
		if invest.FormatDay(prices[i].Day) >= key {
			continue
		}
		if v := prices[i].USDKRWMkt; !math.IsNaN(v) && v > 0 {
			return prices[i].Day, v, true
		}
		return prices[i].Day, 0, false
	}
	return time.Time{}, 0, false
}

func sideWordKR(s broker.Side) string {
	if s == broker.Sell {
		return "매도"
	}
	return "매수"
}
