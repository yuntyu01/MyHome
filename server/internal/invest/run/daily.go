package run

import (
	"context"
	"errors"
	"fmt"
	"math"

	"github.com/yuntyu01/MyHome/server/internal/invest"
	"github.com/yuntyu01/MyHome/server/internal/invest/store"
)

// recordDaily는 가격 갱신 뒤 새 거래일마다 매일 기록(invest_daily)을 남긴다(사용자 요청, 2026-11-20).
//
// 신호 칸(SPX·185일선·변동성)은 마스터로 정확히 다시 계산되므로 밀린 날도 채운다. 계좌 칸(원금·금액)은 지금 장부라
// 마스터 마지막 날에만 채운다 — 밀린 날에 지금 보유를 적으면 그날 사실이 아니다.
// 처음 도는 날(표가 비었으면)은 그달 1일부터 신호를 채운다.
//
// 🔴 보여 주기만 한다. 판정·주문은 이 표를 읽지 않는다. 그래서 실패해도 경고만 하고 갱신은 성공이다.
func (a *App) recordDaily(ctx context.Context) {
	if err := a.writeDaily(ctx); err != nil {
		a.warnf("  ⚠ 매일 기록 실패(판정·매매와 무관): %v", err)
	}
}

func (a *App) writeDaily(ctx context.Context) error {
	master, err := a.Store.LoadPrices(ctx)
	if err != nil {
		return err
	}
	last, ok := master.Last()
	if !ok {
		return nil
	}
	lastDaily, has, err := a.Store.LastDailyDay(ctx)
	if err != nil {
		return err
	}
	from := invest.Date(last.Day.Year(), last.Day.Month(), 1)
	if has {
		if !lastDaily.Before(last.Day) {
			return nil // 이미 다 남겼다
		}
		from = lastDaily.AddDate(0, 0, 1)
	}
	sigs, err := invest.DailySignals(master.SignalInputs(), from)
	if err != nil {
		return err
	}

	// 계좌는 마지막 날에만. 옮기기 전(포지션 없음)이면 신호만 남긴다
	pos, err := a.Store.LoadPosition(ctx)
	hasPos := err == nil
	if err != nil && !errors.Is(err, store.ErrNoPosition) {
		return err
	}
	krw, _, hasKRW, err := a.Store.LastBrokerCashKRW(ctx)
	if err != nil {
		return err
	}
	if !hasKRW {
		krw = 0
	}

	added := 0
	var lastRow invest.DailyRow
	for _, s := range sigs {
		t, err := invest.Ticker(s.Pos)
		if err != nil {
			return err
		}
		r := invest.DailyRow{Day: s.Day, SPX: s.SPX, SMA185: s.SMA185, Vol: s.Vol, C1: s.C1, C2: s.C2, Signal: t}
		if hasPos && s.Day.Equal(last.Day) {
			fillAccount(&r, pos, master, krw)
		}
		ok, err := a.Store.AddDaily(ctx, r)
		if err != nil {
			return fmt.Errorf("%s: %w", invest.FormatDay(s.Day), err)
		}
		if ok {
			added++
			lastRow = r
		}
	}
	if added == 0 {
		return nil
	}
	msg := fmt.Sprintf("  매일 기록 +%d일 → %s 신호 %s (S&P500 185일선 %+.1f%% · 변동성 %.1f%%)", added, invest.FormatDay(lastRow.Day),
		lastRow.Signal, (lastRow.SPX/lastRow.SMA185-1)*100, lastRow.Vol*100)
	if lastRow.HasAccount {
		msg += fmt.Sprintf(" · 원금 %s원 · 금액 %s원", won(float64(lastRow.InvestedKRW)), won(float64(lastRow.StockKRW+lastRow.CashKRW)))
	}
	a.printf("%s", msg)
	return nil
}

// fillAccount는 그날 종가·환율로 지금 장부를 평가해 계좌 칸을 채운다. 종가나 환율이 없으면 비워 둔다.
func fillAccount(r *invest.DailyRow, pos invest.Position, master invest.Prices, krw float64) {
	acct := pos.Holdings
	upto := master
	for i := len(master) - 1; i >= 0; i-- {
		if !master[i].Day.After(r.Day) {
			upto = master[:i+1]
			break
		}
	}
	fx, _ := upto.ValuationFX()
	if fx <= 0 {
		return
	}
	price := 0.0
	if acct.Symbol != "" && acct.Shares > 0 {
		row, ok := upto.AsOf(r.Day)
		if !ok {
			return
		}
		price = row.Get(acct.Symbol)
		if math.IsNaN(price) || price <= 0 {
			return
		}
	}
	v := invest.Value(acct, pos.ContributedKRW, price, fx, krw)
	r.HasAccount, r.Symbol, r.Shares, r.CloseUSD, r.FX = true, acct.Symbol, acct.Shares, price, fx
	r.StockKRW, r.CashKRW = int64(math.Round(v.StockKRW)), int64(math.Round(v.CashKRW))
	r.InvestedKRW, r.GainKRW = int64(math.Round(v.InvestedKRW)), int64(math.Round(v.GainKRW))
}
