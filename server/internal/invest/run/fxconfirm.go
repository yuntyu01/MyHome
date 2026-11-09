package run

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/yuntyu01/MyHome/server/internal/invest/broker"
)

// 체결 환율 확정.
//
// 통합증거금은 주문 시점에 필요한 만큼 원화를 환전하고, 그 환율은 결제(미국 주식 T+1)가 끝난
// 거래내역에만 실린다. 그래서 trade는 시장 환율로 추정해 적고(fx_estimated), 결제 뒤 여기서
// 증권사 실제 값으로 다시 매긴다. 다시 매기는 길은 amend와 같다 — 수량은 그대로, 단가·환율·원금만.
//
// 🔴 자동으로는 자동 매수(trade)가 남긴 달만 고친다. 사람이 execute로 적은 달은 사람이 적은 원금
// (--paid)이 정답일 수 있다 — 그 달은 `amend --auto`로 사람이 부를 때만 고친다.
// 🔴 결제 내역의 수량이 그 달 체결 수량과 정확히 같을 때만 쓴다. 다르면 앱 밖 매매가 섞인 것이라
// 어느 체결이 이번 달 것인지 고를 수 없다 — 추정을 그대로 두고 알린다.

// confirmFX는 이번 달 체결의 추정 환율을 결제 내역으로 확정한다.
// manual이면 사람이 부른 것(amend --auto)이라 못 하면 실패로 알린다. 아니면(trade·refresh가 부름)
// 못 하는 사정은 경고만 남기고 ExitOK다 — 환율 확정 때문에 매매·갱신이 실패로 끝나면 안 된다.
func (a *App) confirmFX(ctx context.Context, today time.Time, dryRun, manual bool) int {
	skip := func(format string, args ...any) int {
		if manual {
			return a.fail(ExitFail, format, args...)
		}
		a.warnf("  ⚠ 환율 확정 보류 — "+format, args...)
		return ExitOK
	}
	pos, err := a.Store.LoadPosition(ctx)
	if err != nil {
		return skip("%v", err)
	}
	ym := pos.ApplyYM
	if !pos.ExecutedThisMonth() {
		if manual {
			return a.fail(ExitFail, "%s에 확정할 체결 기록이 없다", ym)
		}
		return ExitOK
	}
	if !pos.FXEstimated {
		if manual {
			a.printf("%s 체결 환율은 이미 확정돼 있다 (%s)", ym, plain2(pos.FXRate))
		}
		return ExitOK
	}
	if !manual {
		src, err := a.monthSource(ctx, ym)
		if err != nil {
			return skip("%v", err)
		}
		if src != "trade" {
			return skip("%s는 사람이 기록한 달(%s)이다. 자동으로 바꾸지 않는다 — `myhome invest amend --auto`", ym, src)
		}
	}
	if pos.Before == nil {
		return skip("%s 체결에 복원 지점이 없다", ym)
	}
	if a.Broker == nil {
		return skip("증권사 연결이 없다")
	}
	client, err := a.Broker(ctx)
	if err != nil {
		return skip("증권사 연결 실패: %v", err)
	}
	src, ok := client.(broker.Settlements)
	if !ok {
		if manual {
			return a.fail(ExitFail, "이 증권사 연결은 결제 내역을 주지 않는다 — amend --fill <단가> --fx <환율>로 직접 넣을 것")
		}
		return ExitOK
	}

	target := pos.Holdings.Symbol
	prev := pos.Before.Holdings
	switching := prev.Symbol != "" && prev.Symbol != target
	bought := pos.Holdings.Shares
	if !switching {
		bought -= prev.Shares
	}
	start, err := time.ParseInLocation("2006-01", ym, broker.KST)
	if err != nil {
		return skip("적용월 형식 이상: %q", ym)
	}
	end := broker.Today(a.now())

	buy, waiting, err := settledLeg(ctx, src, target, broker.Buy, start, end)
	if err != nil {
		return skip("결제 내역 조회 실패: %v", err)
	}
	var sell settled
	if switching {
		s, w, err := settledLeg(ctx, src, prev.Symbol, broker.Sell, start, end)
		if err != nil {
			return skip("결제 내역 조회 실패: %v", err)
		}
		sell, waiting = s, waiting || w
	}
	if waiting {
		// 사람이 불렀어도 실패가 아니다 — 기다리면 풀린다. 다음 trade·refresh가 다시 본다
		a.printf("  %s 결제 대기 중 — 결제가 끝나면 환율을 확정한다 (지금은 %s 추정)", ym, plain2(pos.FXRate))
		return ExitOK
	}
	if buy.qty != bought {
		return skip("결제 내역 %s 매수 %d주 vs %s 체결 %d주 — 앱 밖 매매가 섞였다. `amend --fill --fx`로 직접 정정할 것", target, buy.qty, ym, bought)
	}
	if switching && sell.qty != prev.Shares {
		return skip("결제 내역 %s 매도 %d주 vs 장부 %d주 — `amend --fill --fx`로 직접 정정할 것", prev.Symbol, sell.qty, prev.Shares)
	}
	if !buy.ok || (switching && !sell.ok) {
		return skip("결제 내역에 단가·환율이 비어 있다")
	}

	fills := map[string]float64{target: buy.price}
	if switching {
		fills[prev.Symbol] = sell.price
	}
	fx := math.Round(buy.fx*10000) / 10000
	a.printf("%s 결제 내역 — %s %d주 평균 $%.4f · 적용 환율 %s (추정 %s)", ym, target, buy.qty, buy.price, plain2(fx), plain2(pos.FXRate))
	if switching {
		a.printf("  매도 %s %d주 평균 $%.4f (환율은 매수 쪽 하나로 다시 매긴다 — amend와 같은 모형)", prev.Symbol, sell.qty, sell.price)
	}
	return a.amend(ctx, today, fills, fx, dryRun, fmt.Sprintf("결제 환율 %s", plain2(fx)))
}

// settled는 한 방향 결제 내역의 합이다.
type settled struct {
	qty   int
	price float64 // 수량가중 평균 단가
	fx    float64 // 정산금액가중 평균 환율
	ok    bool    // 단가·환율이 다 있다
}

// settledLeg는 그 종목·방향의 결제 내역 합이다. 하나라도 미결제면 waiting이다.
func settledLeg(ctx context.Context, src broker.Settlements, symbol string, side broker.Side, from, to time.Time) (settled, bool, error) {
	rows, err := src.Settlements(ctx, symbol, from, to)
	if err != nil {
		return settled{}, false, err
	}
	var s settled
	var amount, net, krw float64
	s.ok = true
	waiting := false
	for _, r := range rows {
		if r.Side != side || r.Symbol != symbol {
			continue
		}
		if !r.Settled {
			waiting = true
			continue
		}
		s.qty += r.Qty
		amt := r.Amount
		if amt <= 0 {
			amt = r.Price * float64(r.Qty)
		}
		amount += amt
		n := r.Net
		if n <= 0 {
			n = amt
		}
		net += n
		krw += n * r.FX
		if amt <= 0 || r.FX <= 0 {
			s.ok = false
		}
	}
	if s.qty > 0 {
		s.price = amount / float64(s.qty)
	}
	if net > 0 {
		s.fx = krw / net
	}
	if s.qty == 0 {
		s.ok = false
	}
	return s, waiting, nil
}

// monthSource는 그 달 원금 내역의 출처다(trade·execute·adjust·amend). 덮인 행은 빠진 목록에서 마지막 것.
func (a *App) monthSource(ctx context.Context, ym string) (string, error) {
	rows, err := a.Store.LoadLedger(ctx)
	if err != nil {
		return "", err
	}
	src := ""
	for _, r := range rows {
		if r.YM == ym && r.Source != "adjust" {
			src = r.Source
		}
	}
	if src == "" {
		return "", fmt.Errorf("%s 원금 내역이 없다", ym)
	}
	return src, nil
}

// confirmFXQuietly는 trade·refresh 끝에 부르는 자동 확정이다. 결과 코드는 버린다(경고만).
func (a *App) confirmFXQuietly(ctx context.Context, today time.Time) {
	_ = a.confirmFX(ctx, today, false, false)
}
