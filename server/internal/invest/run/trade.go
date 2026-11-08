package run

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/yuntyu01/MyHome/server/internal/invest"
	"github.com/yuntyu01/MyHome/server/internal/invest/broker"
	"github.com/yuntyu01/MyHome/server/internal/invest/store"
)

// 자동 매매 (완전 자동).
//
// 🔴 체결이 가격보다 먼저다 (§12.1.3). 미체결은 곧 한 달 지연이고, 전 구간 지연은 −27%다.
// 반면 매수일이 1일이든 20일이든 성과 차이는 3%다. 그래서 좁은 지정가는 쓰지 않는다.
// MOC → 시장가 → 시장가 대용 지정가 순으로 내려간다.
//   - 증권사가 명시적으로 거절하면(4xx + 오류 코드) 같은 실행에서 즉시 다음 유형
//   - 체결 없이 만료되면 다음 실행에서 다음 유형
//   - 🔴 결과를 모르면(타임아웃·연결 끊김·5xx) 내려가지 않고 멈춘다. 주문은 보내기 전에 결과 모름으로
//     기록해 두고, 다음 실행이 clientOrderId로 증권사 주문 목록을 찾아 이어받거나(있으면) 다시 낸다(확실히 없으면)
//
// 세 유형이 다 막히면 재시도로 풀리는 종류가 아니다 — 멈추고 알린다. 3일 대기 같은 것은 없다.
//
// 🔴 왜 하루에 안 끝내는가. 전환하는 달은 매도가 체결돼야 매수 규모를 안다. 미체결 주문이
// 남아 있으면 새 주문을 절대 내지 않는다 — 이중 체결 방지. 매도 체결을 확인한 실행은 매도대금이
// 주문가능금액에 들어와 있으면 그 자리에서 매수를 낸다(체결된 매도 뒤에만 산다는 규칙은 그대로다).

// SweepBand는 시장가 대용 지정가의 밴드다. 넓은 지정가는 밴드 가격이 아니라 시장 가격에 체결된다 —
// 밴드는 폭주 방어일 뿐이다. TQQQ는 3배라 NDX 3% 갭이 9%가 되므로 갭을 덮고도 남게 잡는다.
const SweepBand = 0.10

// SizingBuffer는 수량을 잡을 때 얹는 안전폭이다. 밴드와 다른 값이다.
// 적게 사면 남은 달러가 다음 달로 이월될 뿐이지만, 많이 사면 통합증거금이 계획에 없던 원화를
// 더 환전해 원금 기록이 어긋난다 — 비대칭이라 보수적으로 잡는다.
const SizingBuffer = 0.05

func limitPrice(close float64, side broker.Side) float64 {
	adj := 1 + SweepBand
	if side == broker.Sell {
		adj = 1 - SweepBand
	}
	return math.Round(close*adj*100) / 100
}

// OrderOpts는 order(주문서 + preflight) 옵션이다.
type OrderOpts struct{ Date string }

// Order는 주문서와 preflight다. 기록용이며 실제 주문은 Trade가 낸다.
func (a *App) Order(ctx context.Context, o OrderOpts) int {
	today, err := a.today(o.Date)
	if err != nil {
		return a.fail(ExitFail, "%v", err)
	}
	pos, prices, devs, rc := a.loadTradeState(ctx)
	if rc != ExitOK {
		return rc
	}
	acct := pos.Holdings
	amount, err := a.Store.ContributionFor(ctx, pos.ApplyYM)
	if err != nil {
		return a.fail(ExitFail, "%v", err)
	}
	// 🔴 이탈이 확정된 달은 그 종목이 이번 달의 목표다
	allowed, deviated, err := allowedSymbol(pos, devs)
	if err != nil {
		return a.fail(ExitFail, "%v", err)
	}
	target := allowed
	if target == "" {
		target = invest.MustTicker(pos.Signal.Target)
	}
	c, err := buildCtx(today, pos, acct, prices.SignalInputs(), sellLegsFor(acct, target), target)
	if err != nil {
		return a.fail(ExitFail, "%v", err)
	}
	fails := invest.Preflight(c)

	a.printf("%s 주문서 (%s)", pos.ApplyYM, invest.FormatDay(today))
	if deviated {
		if allowed != "" {
			a.printf("  ⚠ 이탈 확정 — 신호 %s, 실제 %s", invest.MustTicker(pos.Signal.Target), target)
		} else {
			a.printf("  ⚠ 이탈 확정 — 신호 %s · 미실행으로 기록된 달", invest.MustTicker(pos.Signal.Target))
		}
	}
	a.printf("  적립      %s원", commas(float64(amount)))
	a.printf("  ─ 확인 ──────────────────────")
	a.printf("  □ 증권 계좌에 %s원이 들어와 있는가 (자동이체)", commas(float64(amount)))
	a.printf("    환전은 통합증거금이 주문 시점에 자동 처리 (손댈 것 없음)")
	if acct.Symbol != "" && acct.Symbol != target {
		a.printf("  목표      %s   ← %s 전량 매도 후 교체", target, acct.Symbol)
	} else {
		a.printf("  목표      %s   (추가 매수)", target)
	}
	a.printf("  보유      %s %d주 · 잔여 %s", dash(acct.Symbol), acct.Shares, usd(acct.CashUSD))
	if len(fails) > 0 {
		a.printf("\n✗ preflight 실패 — 자동 실행 중단, 수동 확인 필요")
		for _, x := range fails {
			a.printf("    · %s", x)
		}
		return ExitFail
	}
	a.printf("\n✓ preflight 통과")
	if c.LateDays > 0 {
		a.printf("  ⚠ 실행일(%s)에서 %d일 지연 — 늦은 매수로 기록된다 (미매수보다는 낫다)", invest.FormatDay(c.ExecDay), c.LateDays)
	}
	// TQ와 같은 안내: 손으로 샀으면 체결을 기록한다(자동 매매는 trade가 기록한다)
	a.printf("\n  증권사에서 주문 후:")
	a.printf("    myhome invest execute --fill %s=<체결단가> --fx <적용환율>", target)
	return ExitOK
}

func dash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

// loadTradeState는 매매 명령이 공통으로 읽는 것과 336행 관문이다.
func (a *App) loadTradeState(ctx context.Context) (invest.Position, invest.Prices, []invest.Deviation, int) {
	pos, err := a.Store.LoadPosition(ctx)
	if err != nil {
		return pos, nil, nil, a.fail(ExitFail, "%v", err)
	}
	prices, err := a.Store.LoadPrices(ctx)
	if err != nil {
		return pos, nil, nil, a.fail(ExitFail, "마스터 읽기 실패: %v", err)
	}
	if err := a.gate(prices.SignalInputs()); err != nil {
		return pos, nil, nil, a.fail(ExitFail, "%v", err)
	}
	devs, err := a.Store.LoadDeviations(ctx)
	if err != nil {
		return pos, nil, nil, a.fail(ExitFail, "%v", err)
	}
	return pos, prices, devs, ExitOK
}

// TradeOpts는 trade 옵션이다.
type TradeOpts struct {
	Date   string
	DryRun bool
	// FX는 증권사 실제 적용 환율이다. 0이면 시장 환율로 추정하고 그 사실을 상태에 남긴다.
	// 통합증거금 환전은 증권사 요율로 처리되고 그 값은 체결 뒤에야 보인다.
	FX float64
}

// Trade는 자동 매매 한 단계 진행이다. 매일 불러도 안전하다(멱등).
func (a *App) Trade(ctx context.Context, o TradeOpts) int {
	today, err := a.today(o.Date)
	if err != nil {
		return a.fail(ExitFail, "%v", err)
	}
	// 🔴 뉴욕 휴장일에는 주문을 내지 않는다. TQ는 이 검사를 execution.yml 게이트에서 했고,
	//    여기서는 타이머(매월 1~15일)가 휴장일에도 부르므로 명령이 직접 막는다. 실패가 아니라 건너뛰기다
	//    (첫 거래일이 아닌 날 사는 것은 지연이지 위반이 아니다. 다음 거래일 실행이 이어받는다)
	if !invest.IsTradingDay(today) {
		a.printf("%s은 뉴욕 휴장일이다. 주문을 내지 않는다(다음 거래일에 이어서 한다)", invest.FormatDay(today))
		return ExitOK
	}
	pos, prices, devs, rc := a.loadTradeState(ctx)
	if rc != ExitOK {
		return rc
	}
	acct := pos.Holdings
	ym := pos.ApplyYM
	// 🔴 적용월이 이번 달이 아니면 월말 판정이 빠진 것이다. "이미 완료"로 끝내면 지난달 체결 표시가
	// 이번 달을 덮어 판정 누락이 조용히 묻힌다 — 완료 확인보다 먼저 보고 실패로 알린다
	if cur := invest.YM(today); ym != cur {
		return a.fail(ExitFail, "🚨 적용월이 %s다 — 오늘(%s)은 %s. 월말 판정이 빠졌다(마지막 판정 %s, %s).\n  주문하지 않는다. judge 실행 기록을 확인할 것 (만회는 월말 다음 거래일 장 마감 전까지만 된다)",
			ym, invest.FormatDay(today), cur, pos.YM, pos.LastRun)
	}
	if pos.ExecutedThisMonth() {
		a.printf("%s 이미 완료 (%s). 할 일 없음", ym, *pos.ExecutedOn)
		return ExitOK
	}
	target, _, err := allowedSymbol(pos, devs)
	if err != nil {
		return a.fail(ExitFail, "%v", err)
	}
	if target == "" {
		return a.fail(ExitFail, "%s은 미실행 이탈로 기록된 달이다. 자동 매매 대상이 아니다", ym)
	}
	sells := sellLegsFor(acct, target)
	c, err := buildCtx(today, pos, acct, prices.SignalInputs(), sells, target)
	if err != nil {
		return a.fail(ExitFail, "%v", err)
	}
	if fails := invest.Preflight(c); len(fails) > 0 {
		for _, x := range fails {
			a.warnf("    · %s", x)
		}
		return a.fail(ExitFail, "preflight 실패 — 주문을 내지 않는다. 우회 옵션은 없다")
	}

	bookp, err := a.Store.LoadOrders(ctx)
	if err != nil {
		return a.fail(ExitFail, "%v", err)
	}
	book := invest.OrderBook{YM: ym, Stage: "buy", Orders: []invest.BookOrder{}}
	if len(sells) > 0 {
		book.Stage = "sell"
	}
	if bookp != nil && bookp.YM != ym && len(bookp.Orders) == 0 {
		// 주문이 하나도 접수되지 않은 지난달 기록(전 유형 거절 등)은 남은 것이 없다 — 버리고 새로 시작한다
		a.printf("지난 적용월(%s)의 빈 주문 기록을 버린다", bookp.YM)
		bookp = nil
	}
	if bookp != nil {
		book = *bookp
	}
	if book.YM != ym { // 지난달 잔여는 이어받지 않는다
		return a.fail(ExitFail, "진행 중인 주문 기록이 %s 것이다. 새 주문을 내지 않는다.\n  `myhome invest orders`로 보고, 증권사에 남은 주문이 없으면 `myhome invest orders --clear-stale`로 정리할 것", book.YM)
	}

	row, ok := prices.AsOf(today)
	if !ok {
		return a.fail(ExitFail, "%s 이전 가격이 없다", invest.FormatDay(today))
	}
	fx, fxEst := invest.TradeFX(o.FX, row)
	if fx <= 0 {
		return a.fail(ExitFail, "환율을 구할 수 없다. --fx로 넣거나 refresh로 USDKRW_MKT를 채울 것")
	}
	pending := pendingOf(book)

	// 브로커는 실제로 쓸 때만 만든다 — dry-run이 자격증명 없이도 돌아야 무엇을 살지 미리 볼 수 있다
	var client broker.Broker
	if len(pending) > 0 || !o.DryRun {
		client, err = a.Broker(ctx)
		if err != nil {
			return a.fail(ExitFail, "증권사 설정/연결 실패: %v", err)
		}
		if client.Env() == "real" {
			a.warnf("⚠ 실전 계좌로 주문한다 (%s)", client.AccountMasked())
		}
	}

	// ⓪ 결과를 모르는 주문이 있으면 증권사에서 먼저 찾는다. 정해지기 전에는 아무것도 내지 않는다
	if rc, ok := a.resolveUnknown(ctx, client, &book, o.DryRun); !ok {
		return rc
	}
	pending = pendingOf(book)

	// ① 미체결이 있으면 체결부터 확인. 새 주문은 내지 않는다.
	//    단, 주문이 만료됐으면 승격하고, 매도가 끝났으면 매수로 넘어가 아래 주문 단계로 이어진다
	sameRun := false
	if len(pending) > 0 {
		rc, next := a.settle(ctx, client, &book, pending, today, acct, pos, target, row, fx, fxEst, o.DryRun)
		if next == settleStop {
			return rc
		}
		if b, err := a.Store.LoadOrders(ctx); err == nil && b != nil {
			book = *b
		}
		sells = sellLegsFor(acct, target)
		sameRun = next == settleSold
	}

	// ② 매도 단계
	if book.Stage == "sell" && len(sells) > 0 {
		sym := acct.Symbol
		// 🔴 이미 판 만큼을 빼고 비교한다 — 부분 체결 뒤 승격하면 증권사 보유가 그만큼 줄어 있다
		already, _ := avgLegs(legs(book, broker.Sell))
		qty := acct.Shares - already
		if o.DryRun {
			a.printf("%s 매도 주문 — %s %d주 (장부 기준)", ym, sym, qty)
			a.printf("[dry-run] 제출하지 않았다. 실제 실행은 증권사 보유를 다시 묻는다")
			return ExitOK
		}
		// 🔴 장부가 아니라 증권사 실제 보유를 판다. 앱 밖에서 추가 매수하면 장부가 실제보다 적어진다.
		// 그때 장부 수량만 팔면 신호는 위험회피인데 3배 레버리지가 남는다 — 전환이 반쪽만 일어난다
		bal, err := client.Balance(ctx)
		if err != nil {
			return a.fail(ExitFail, "매도 전 잔고 조회 실패: %v", err)
		}
		real := bal.SharesOf(sym)
		if real != qty {
			a.printf("⚠ 남은 장부 %d주 vs 증권사 %d주 — 실제 보유 전량을 판다", qty, real)
			a.printf("   앱 밖 매매가 있었다는 뜻이다. 취득원가·수익률은 어긋난 채 남는다")
			gap := real - qty
			book.LedgerGap = &gap
			qty = real
		}
		if qty <= 0 {
			return a.fail(ExitFail, "증권사에 %s 보유가 없다 (장부 %d주). 확인할 것", sym, acct.Shares)
		}
		close := row.Get(sym)
		if math.IsNaN(close) {
			return a.fail(ExitFail, "%s 종가가 마스터에 없다 — refresh 먼저", sym)
		}
		ref := a.freshPrice(ctx, sym, close, today)
		a.printf("%s 매도 주문 — %s %d주", ym, sym, qty)
		// 매도 레그가 있다는 것은 전환이다 — 실패 비용이 −27%짜리다
		return a.place(ctx, client, &book, sym, broker.Sell, qty, ref, today, true)
	}

	// ③ 매수 단계
	//
	// 🔴 수량은 계좌에 실제로 있는 만큼이다 (§12.1.5). contribution은 사이징에서 빠지고 계획 대비
	// 비교 기준으로만 남는다
	planKRW, err := a.Store.ContributionFor(ctx, ym)
	if err != nil {
		return a.fail(ExitFail, "%v", err)
	}
	// 🔴 매도대금을 반드시 더한다. 전환 달은 매도 체결 뒤에도 계좌가 아직 옛 상태다
	_, proceedsUSD := sold(book)
	// 🔴 이미 채워진 매수분을 뺀다 — 안 빼면 초과 매수가 된다 (실측: 6주 계획에서 1주 체결 후 6주를 또 내 7주)
	doneQty, donePx := avgLegs(legsOf(book, broker.Buy, target))
	spentUSD := float64(doneQty) * donePx * (1 + invest.FeeRate)
	close := row.Get(target)
	if math.IsNaN(close) {
		return a.fail(ExitFail, "%s 종가가 마스터에 없다 — refresh 먼저", target)
	}
	// 🔴 마스터 종가는 금요일·판정일에만 갱신된다. 며칠 사이 5% 넘게 오르면 안전폭이 뚫려 자금 부족이
	// 거듭된다 — 지금 가격을 다시 받아 그 위에 안전폭을 얹는다
	ref := a.freshPrice(ctx, target, close, today)
	est := ref * (1 + SizingBuffer)
	planUSD := acct.CashUSD + float64(planKRW)/fx + proceedsUSD - spentUSD
	planQty := int(math.Floor(planUSD / (est * (1 + invest.FeeRate))))

	var qty int
	if o.DryRun {
		qty = planQty
	} else {
		// 🔴 조회가 실패하면 주문하지 않는다. 얼마나 살 수 있는지 모르는 채로 내지 않는다
		b, err := client.Buyable(ctx, target, est)
		if err != nil {
			return a.fail(ExitFail, "매수가능금액 조회 실패: %v\n  얼마나 살 수 있는지 모르는 채로 주문하지 않는다", err)
		}
		// 매도 체결을 확인한 바로 그 실행에서 사는 것은 매도대금이 주문가능금액에 들어와 있을 때만이다.
		// 안 들어온 채로 계좌 기준으로 사면 매도대금이 한 달 동안 현금으로 논다 — 다음 실행에 맡긴다
		if sameRun && b.CashUSD < proceedsUSD*proceedsSeen {
			a.printf("  매도대금 %s가 아직 주문가능금액(%s)에 다 들어오지 않았다 — 매수는 다음 실행에서 낸다", usd(proceedsUSD), usd(b.CashUSD))
			return ExitOK
		}
		if b.MaxQty <= 0 {
			return a.fail(ExitFail, "증권사가 %s를 0주까지만 허용한다 (주문가능 %s).\n  확인할 것:\n    · %s원 자동이체가 실제로 들어왔는가\n    · 통합증거금(원화주문)이 켜져 있는가 — 신청해야 켜진다",
				target, usd(b.CashUSD), commas(float64(planKRW)))
		}
		qty = b.MaxQty
		a.printf("  증권사 주문가능 %s (최대 %d주)", usd(b.CashUSD), qty)
		if qty != planQty {
			a.printf("  계획 %d주(%s원) vs 계좌 %d주 — 계좌 기준으로 산다", planQty, commas(float64(planKRW)), qty)
		}
	}
	if qty <= 0 {
		return a.fail(ExitFail, "%s 예상단가 %s로 살 수 있는 수량이 0. 계획 %s — 환율(--fx)과 입금을 확인할 것", target, usd(est), usd(planUSD))
	}
	a.printf("%s 매수 주문 — %s %d주", ym, target, qty)
	if o.DryRun {
		a.printf("[dry-run] 제출하지 않았다")
		return ExitOK
	}
	book.Stage = "buy"
	// 전환의 매수 레그는 긴급, 순수 적립 매수는 아니다 — 적립 실패는 잔돈이 이월될 뿐이다
	soldQty, _ := sold(book)
	return a.place(ctx, client, &book, target, broker.Buy, qty, ref, today, soldQty > 0)
}

// proceedsSeen은 매도대금이 주문가능금액에 들어왔다고 볼 비율이다. 수수료 계산 차이만큼 여유를 둔다.
const proceedsSeen = 0.99

// freshQuoteBand를 넘게 마스터 종가와 다른 현재가는 믿지 않는다(야후 오류로 보고 종가를 쓴다).
const freshQuoteBand = 0.30

// freshPrice는 사이징과 지정가 밴드에 쓸 지금 가격이다. 받지 못하면 마스터 종가다.
//
// 🔴 이 값은 마스터에 쓰지 않는다. 장중 시세는 종가가 아니다(§10.0). 수량과 밴드 계산에만 쓴다.
// 토스 클라이언트에는 시세 조회가 없어(문서에서 확인한 엔드포인트만 쓴다) 야후 일봉의 마지막 값을 쓴다.
// 장중이면 그것이 지금 가격이다. 야후가 안 되거나 값이 이상하면 종가로 돌아가고 그렇다고 남긴다.
// 그래도 자금 부족이 나면 place가 수량을 줄여 한 번 더 낸다.
func (a *App) freshPrice(ctx context.Context, symbol string, close float64, today time.Time) float64 {
	if a.Prices == nil {
		return close
	}
	bars, err := a.Prices.DailyCloses(ctx, []string{symbol}, today.AddDate(0, 0, -7))
	if err != nil {
		a.warnf("  ⚠ %s 현재가 조회 실패 - 마스터 종가 %s로 잡는다: %v", symbol, usd(close), err)
		return close
	}
	px := math.NaN()
	for _, b := range bars[symbol] {
		if b.OK() && b.Close > 0 {
			px = b.Close
		}
	}
	if math.IsNaN(px) {
		a.warnf("  ⚠ %s 현재가가 비어 있다 - 마스터 종가 %s로 잡는다", symbol, usd(close))
		return close
	}
	if d := math.Abs(px/close - 1); d > freshQuoteBand {
		a.warnf("  ⚠ %s 현재가 %s가 종가 %s와 %.0f%% 차이 - 믿지 않고 종가로 잡는다", symbol, usd(px), usd(close), d*100)
		return close
	}
	a.printf("  %s 현재가 %s (마스터 종가 %s)", symbol, usd(px), usd(close))
	return px
}

// pendingOf는 아직 끝나지 않은 주문(체결 대기·결과 모름)의 위치다.
func pendingOf(book invest.OrderBook) []int {
	var out []int
	for i, ord := range book.Orders {
		if !ord.Filled {
			out = append(out, i)
		}
	}
	return out
}

// clientOrderID는 주문의 멱등성 키다. (뉴욕 주문일·종목·방향·수량·유형) - TQ와 같은 모양이다.
//
// 같은 날 같은 주문을 다시 보내면 같은 키라 토스가 이전 주문을 돌려준다. 승격하거나 수량을 바꾸면 새 키다.
// 🔴 키는 보내기 전에 주문 기록에 남긴다. 응답을 못 받으면 다음 실행이 이 키로 증권사에서 찾는다.
// 보낸 키와 찾는 키가 같아야 하므로 어댑터가 아니라 여기서 만든다.
func clientOrderID(day time.Time, symbol string, side broker.Side, qty int, style broker.Style) string {
	return fmt.Sprintf("tq-%s-%s-%s-%d-%s", day.Format("20060102"), symbol, side, qty, style)
}

// submitOutcome은 주문 1건을 보낸 결과다.
type submitOutcome int

const (
	submitAccepted submitOutcome = iota // 접수됐다 (주문번호 있음)
	submitRejected                      // 명시적 거절. 접수되지 않은 것이 확실하다
	submitFunds                         // 자금 부족 거절
	submitStop                          // 결과 모름·저장 실패. 멈춘다 (rc가 답이다)
)

// submit은 주문 1건을 기록하고 보낸다.
//
// 🔴 순서가 핵심이다. ① 결과 모름으로 먼저 저장 ② 보낸다 ③ 접수면 주문번호를 채워 저장, 명시적 거절이면
// 기록을 지운다. 응답 전에 끊기면(타임아웃·연결·5xx·형식 이상) 기록은 결과 모름으로 남고 멈춘다.
// 접수됐을 수 있는 주문을 두고 다음 유형을 내면 이중 체결이다.
func (a *App) submit(ctx context.Context, client broker.Broker, book *invest.OrderBook, styleIdx int, symbol string, side broker.Side, qty int, px float64, today time.Time) (int, submitOutcome, error) {
	style := broker.Styles[styleIdx]
	id := clientOrderID(today, symbol, side, qty, style)
	prevIdx := book.StyleIdx
	n := len(book.Orders)
	book.Orders = append(book.Orders, invest.BookOrder{
		ClientOrderID: id, Symbol: symbol, Side: string(side), Qty: qty, Price: px, Style: string(style),
		SubmittedOn: invest.FormatDay(today), SubmittedAt: a.now().UTC().Format(time.RFC3339), Unknown: true,
	})
	book.StyleIdx = styleIdx
	if err := a.Store.SaveOrders(ctx, *book); err != nil {
		book.Orders, book.StyleIdx = book.Orders[:n], prevIdx
		return a.fail(ExitFail, "주문 기록 저장 실패 - 주문을 내지 않았다: %v", err), submitStop, nil
	}
	res, err := client.Order(ctx, symbol, side, qty, px, style, id)
	if err != nil {
		funds := client.IsFundsError(err)
		if !funds && !client.IsRejected(err) {
			return a.fail(ExitFail, "🚨 주문 결과를 모른다 - %s %s %d주 (%s, clientOrderId %s)\n  %v\n  접수됐을 수 있어 다음 유형으로 내려가지 않는다(이중 체결 방지).\n  다음 실행이 이 키로 증권사 주문 목록을 찾아, 있으면 이어받고 확실히 없을 때만 다시 낸다.\n  급하면 증권사 앱에서 이 주문이 들어갔는지 확인할 것",
				symbol, side, qty, style.Label(), id, err), submitStop, err
		}
		// 접수되지 않은 것이 확실하다. 기록을 지운다
		book.Orders, book.StyleIdx = book.Orders[:n], prevIdx
		if serr := a.Store.SaveOrders(ctx, *book); serr != nil {
			return a.fail(ExitFail, "거절된 주문(%s) 기록을 지우지 못했다: %v\n  다음 실행이 증권사에서 확인하고 이어간다", id, serr), submitStop, err
		}
		if funds {
			return ExitOK, submitFunds, err
		}
		return ExitOK, submitRejected, err
	}
	book.Orders[n].OrderID, book.Orders[n].Unknown = res.OrderID, false
	if err := a.Store.SaveOrders(ctx, *book); err != nil {
		// 주문은 이미 접수됐다. 저장된 기록은 결과 모름(clientOrderId)으로 남아 있어 다음 실행이
		// 증권사 주문 목록에서 이 주문을 찾아 이어받는다. 새로 내지 않는다
		return a.fail(ExitFail, "🚨 주문 %s 접수 후 저장 실패: %v\n  기록에는 결과 모름(clientOrderId %s)으로 남아 있다. 다음 실행이 증권사에서 찾아 이어받는다", res.OrderID, err, id), submitStop, nil
	}
	a.printf("  주문번호 %s (%s) - 체결 확인은 다음 실행에서", res.OrderID, style.Label())
	return ExitOK, submitAccepted, nil
}

// place는 주문 1건 제출이다. 유형을 내려가며 접수될 때까지 시도한다. ref는 밴드 기준 가격(현재가, 없으면 종가)이다.
//
// 🔴 명시적 거절만 같은 실행에서 즉시 승격한다. 거절은 접수 자체가 안 된 것이라 이중 체결 위험이 없다.
// 결과를 모르는 실패는 승격하지 않고 멈춘다(submit). 세 유형이 다 막히면 재시도로 풀릴 종류가 아니다.
// 멈추고 사람을 부른다.
func (a *App) place(ctx context.Context, client broker.Broker, book *invest.OrderBook, symbol string, side broker.Side, qty int, ref float64, today time.Time, urgent bool) int {
	var tried []string
	shrunk := false
	for i := book.StyleIdx; i < len(broker.Styles); i++ {
		style := broker.Styles[i]
		px := limitPrice(ref, side)
		shown := "(단가 미지정)"
		if style == broker.Sweep {
			shown = "@ " + usd(px)
		}
		a.printf("  %s로 %s %s %d주 %s", style.Label(), symbol, side, qty, shown)
		rc, out, err := a.submit(ctx, client, book, i, symbol, side, qty, px, today)
		switch out {
		case submitAccepted, submitStop:
			return rc
		case submitFunds:
			// 🔴 자금 부족이면 유형을 바꿔도 소용없다. 매수만 한 번, 증권사가 지금 허용하는 수량으로 줄여
			// 같은 유형으로 다시 낸다. 같은 단가로 다시 물으면 같은 수량이 나오므로 안전폭을 한 번 더 얹어 묻는다
			if side == broker.Buy && !shrunk {
				shrunk = true
				b, berr := client.Buyable(ctx, symbol, ref*(1+SizingBuffer)*(1+SizingBuffer))
				if next := min(b.MaxQty, qty-1); berr == nil && next > 0 {
					a.printf("  자금 부족 - 증권사 주문가능 %s 기준으로 %d주 → %d주로 줄여 한 번 더 낸다", usd(b.CashUSD), qty, next)
					qty = next
					i--
					continue
				}
			}
			return a.fail(ExitFail, "자금 부족으로 주문 불가 — %s %s %d주\n  %v\n  주문 유형을 바꿔도 풀리지 않는다. 확인할 것:\n    · 통합증거금(원화주문)이 켜져 있는가 — 신청해야 켜진다\n    · 꺼져 있다면 미리 USD로 환전돼 있는가\n    · 원화가 실제로 입금됐는가",
				symbol, side, qty, err)
		}
		tried = append(tried, fmt.Sprintf("%s: %v", style.Label(), err))
		a.warnf("    거부됨 - 다음 유형으로 내려간다")
	}
	for _, t := range tried {
		a.warnf("    · %s", t)
	}
	kind := "적립"
	tail := "\n  적립 실패는 잔돈이 다음 달로 이월될 뿐이다 (−0.008%)"
	if urgent {
		kind = "🚨 전환"
		tail = "\n  🔴 전환이 막히면 한 달 잘못된 포지션이다. 즉시 손으로 처리할 것"
	}
	return a.fail(ExitFail, "%s 주문이 모든 유형에서 거부됐다 — %s %s %d주.\n  재시도로 풀리는 종류가 아니다. 증권사 앱에서 직접 확인할 것%s", kind, symbol, side, qty, tail)
}

// lookupWindow는 주문을 증권사에 물을 기간(KST 날짜)이다.
//
// 🔴 조회 창을 넓게 잡는다. submitted_on은 뉴욕 날짜이고 증권사 주문일자는 KST다. 미국 장중에
// 낸 주문은 증권사 장부에 다음 날로 찍힌다. 대조는 주문번호로 하므로 남의 주문을 주워오지 않는다
func (a *App) lookupWindow(submittedOn string) (time.Time, time.Time, error) {
	sub, err := invest.ParseDay(submittedOn)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("주문 기록 형식 이상: %v", err)
	}
	end := broker.Today(a.now())
	bt := invest.Date(end.Year(), end.Month(), end.Day())
	if sub.After(bt) {
		bt = sub
	}
	return sub.AddDate(0, 0, -1), bt.AddDate(0, 0, 1), nil
}

// lookup은 결과를 모르는 주문을 증권사 주문 목록에서 가린 결과다.
type lookup int

const (
	lookupAbsent    lookup = iota // 확실히 없다. 접수되지 않았다
	lookupFound                   // 이 주문이다
	lookupAmbiguous               // 확신할 수 없다. 멈춘다
)

// pickUnknown은 결과를 모르는 주문 o를 증권사 행에서 고른다.
//
// clientOrderId가 실린 행이 있으면 그것이다. 주문 목록에 키가 실려 오는지는 문서에서 확인하지 못해서,
// 같은 종목·방향인데 우리 기록에 없는 주문도 후보로 본다. 이 계좌는 이 전략만 쓰므로 하나면 그것이다.
// 후보가 하나도 없을 때만 "없다"고 답한다. 둘 이상이거나 주문번호가 비면 모른다.
func pickUnknown(book invest.OrderBook, o invest.BookOrder, refs []broker.OrderRef) (broker.OrderRef, lookup) {
	var exact, cands []broker.OrderRef
	for _, r := range refs {
		if r.ByClientID {
			exact = append(exact, r)
			continue
		}
		if (r.Side != "" && string(r.Side) != o.Side) || (r.Symbol != "" && r.Symbol != o.Symbol) {
			continue
		}
		known := false
		for _, b := range book.Orders {
			if b.OrderID != "" && sameOrderID(b.OrderID, r.OrderID) {
				known = true
			}
		}
		if !known {
			cands = append(cands, r)
		}
	}
	pick := cands
	if len(exact) > 0 {
		pick = exact
	}
	switch {
	case len(pick) == 0:
		return broker.OrderRef{}, lookupAbsent
	case len(pick) == 1 && strings.TrimSpace(pick[0].OrderID) != "":
		return pick[0], lookupFound
	}
	return broker.OrderRef{}, lookupAmbiguous
}

// describeOrder는 주문 기록 한 줄이다.
func describeOrder(o invest.BookOrder) string {
	state := "체결 대기"
	switch {
	case o.Unknown:
		state = "결과 모름"
	case o.Filled && o.Expired:
		state = fmt.Sprintf("만료(%d주 체결)", o.Qty)
	case o.Filled:
		state = fmt.Sprintf("체결 @ $%.4f", o.FillPrice)
	}
	id := o.OrderID
	if id == "" {
		id = "-"
	}
	return fmt.Sprintf("%s %s %d주 (%s, %s 제출) 주문번호 %s · clientOrderId %s · %s",
		o.Symbol, o.Side, o.Qty, broker.Style(o.Style).Label(), o.SubmittedOn, id, dash(o.ClientOrderID), state)
}

// resolveUnknown은 결과를 모르는 주문을 증권사에서 찾아 정한다. ok가 false면 rc로 끝낸다.
//
// 🔴 찾으면 이어받고(그 주문의 체결을 기다린다), 확실히 없을 때만 기록을 지워 같은 유형부터 다시 내게 한다.
// 확실하지 않으면(조회 실패, 후보 여럿) 멈추고 사람을 부른다. 모르는데 새로 내면 이중 체결이다.
func (a *App) resolveUnknown(ctx context.Context, client broker.Broker, book *invest.OrderBook, dryRun bool) (int, bool) {
	keep := make([]invest.BookOrder, 0, len(book.Orders))
	changed := false
	for _, o := range book.Orders {
		if !o.Unknown {
			keep = append(keep, o)
			continue
		}
		what := describeOrder(o)
		from, to, err := a.lookupWindow(o.SubmittedOn)
		if err != nil {
			return a.fail(ExitFail, "%v", err), false
		}
		refs, err := client.FindOrders(ctx, o.Symbol, o.ClientOrderID, from, to)
		if err != nil {
			return a.fail(ExitFail, "🚨 결과를 모르는 주문을 증권사에서 찾지 못했다 - %s\n  %v\n  새 주문을 내지 않는다. 다음 실행이 다시 찾는다. 급하면 증권사 앱에서 확인할 것", what, err), false
		}
		ref, got := pickUnknown(*book, o, refs)
		switch got {
		case lookupFound:
			a.printf("결과를 몰랐던 주문을 증권사에서 찾았다 - %s → 주문번호 %s. 이 주문을 이어받는다", what, ref.OrderID)
			o.OrderID, o.Unknown = ref.OrderID, false
			keep = append(keep, o)
		case lookupAbsent:
			a.printf("결과를 몰랐던 주문은 증권사에 없다 - %s. 접수되지 않았으므로 같은 유형부터 다시 낸다", what)
		default:
			return a.fail(ExitFail, "🚨 결과를 모르는 주문이 증권사의 어느 주문인지 확신할 수 없다 - %s\n  같은 종목 주문 %d건이 보인다. 새 주문을 내지 않는다. 증권사 앱에서 확인할 것", what, len(refs)), false
		}
		changed = true
	}
	if !changed {
		return ExitOK, true
	}
	book.Orders = keep
	if dryRun {
		a.printf("[dry-run] 주문 기록은 저장하지 않았다")
		return ExitOK, true
	}
	if err := a.Store.SaveOrders(ctx, *book); err != nil {
		return a.fail(ExitFail, "주문 기록 저장 실패: %v", err), false
	}
	return ExitOK, true
}

// sameOrderID는 주문번호 대조다. 형식 차이에 깨지지 않게 앞의 0을 떼고 본다.
// 한쪽은 12345, 다른 쪽은 0000012345로 주면 문자열 그대로는 영원히 안 맞는다 — 조용한 영구 미체결.
func sameOrderID(a, b string) bool {
	x := strings.TrimLeft(strings.TrimSpace(a), "0")
	y := strings.TrimLeft(strings.TrimSpace(b), "0")
	return x != "" && x == y
}

func scalarString(v any) (string, bool) {
	switch t := v.(type) {
	case string:
		return t, true
	case json.Number:
		return t.String(), true
	case float64:
		if t == math.Trunc(t) {
			return strconv.FormatFloat(t, 'f', 0, 64), true
		}
	case int:
		return strconv.Itoa(t), true
	case int64:
		return strconv.FormatInt(t, 10), true
	}
	return "", false
}

// stillOpen은 그 주문이 아직 증권사에 살아 있나. 판단 불가면 nil.
//
// 🔴 승격 전에 반드시 묻는다. 만료된 줄 알고 새로 냈는데 옛 주문이 살아 있으면 이중 체결이다.
// 키를 짚지 않고 행의 값 어딘가에 그 주문번호가 있는지 본다 — 없는 키를 지어내는 것보다 낫다.
func stillOpen(ctx context.Context, client broker.Broker, orderID string) *bool {
	rows, err := client.OpenOrders(ctx)
	if err != nil {
		return nil // 모르면 모른다고 답한다. 승격하지 않는다
	}
	yes, no := true, false
	for _, row := range rows {
		for _, v := range row {
			if s, ok := scalarString(v); ok && sameOrderID(s, orderID) {
				return &yes
			}
		}
	}
	return &no
}

// legs는 그 방향으로 실제로 채워진 주문들이다.
//
// 🔴 수량 0을 걸러 내는 것이 핵심이다. 승격할 때 만료된 주문을 filled로 닫는데, 하나도 안
// 채워졌으면 수량 0 · 체결가 0이다. 그것이 평균에 섞이면 체결단가 0이 되어 계좌 산술이 죽는다.
func legs(book invest.OrderBook, side broker.Side) []invest.BookOrder {
	var out []invest.BookOrder
	for _, o := range book.Orders {
		if o.Side == string(side) && o.Filled && o.Qty > 0 {
			out = append(out, o)
		}
	}
	return out
}

func legsOf(book invest.OrderBook, side broker.Side, symbol string) []invest.BookOrder {
	var out []invest.BookOrder
	for _, o := range legs(book, side) {
		if o.Symbol == symbol {
			out = append(out, o)
		}
	}
	return out
}

// avgLegs는 (총수량, 수량가중 평균 체결가)다. 유형을 갈아타며 나눠 체결됐을 수 있다.
func avgLegs(ls []invest.BookOrder) (int, float64) {
	qty, sum := 0, 0.0
	for _, o := range ls {
		qty += o.Qty
		sum += float64(o.Qty) * o.FillPrice
	}
	if qty <= 0 {
		return 0, 0
	}
	return qty, sum / float64(qty)
}

// sold는 이번 사이클에 실제로 체결된 매도의 (수량, 수수료 뺀 USD 대금)이다.
//
// 전환하는 달은 매도와 매수가 다른 날에 일어난다. 매도가 체결돼도 계좌는 아직 갱신되지 않으므로
// 그대로 매수 수량을 계산하면 매도대금이 통째로 빠진 채 적립금만으로 사게 된다.
func sold(book invest.OrderBook) (int, float64) {
	qty, px := avgLegs(legs(book, broker.Sell))
	return qty, float64(qty) * px * (1 - invest.FeeRate - invest.SECFeeRate)
}

// settleNext는 settle 뒤에 할 일이다.
type settleNext int

const (
	settleStop     settleNext = iota // 여기서 끝낸다
	settlePromoted                   // 만료돼 승격했다. 다음 유형으로 낸다
	settleSold                       // 매도가 끝났다. 같은 실행에서 매수로 이어간다
)

// settle은 미체결 주문을 확인하고 계좌에 반영한다. 다음 할 일을 함께 돌려준다.
//   - settlePromoted: 주문이 만료돼 승격했다. 호출부가 이어서 다음 유형으로 새 주문을 낸다
//   - settleSold: 매도가 다 끝났다. 호출부가 같은 실행에서 매수로 이어간다(매도대금이 들어와 있을 때만)
//   - settleStop: 여기서 끝낸다(rc가 답이다)
//
// 🔴 dryRun이면 조회만 하고 아무것도 쓰지 않는다. 미리 보려고 친 명령이 그 달 체결을 확정하면
// 안 된다 — 실제로 --dry-run이 position·ledger를 써서 체결을 확정한 적이 있다.
func (a *App) settle(ctx context.Context, client broker.Broker, book *invest.OrderBook, pending []int, today time.Time,
	acct invest.Account, pos invest.Position, target string, row invest.PriceRow, fx float64, fxEst, dryRun bool) (int, settleNext) {
	ym := book.YM
	for _, i := range pending {
		o := &book.Orders[i]
		from, to, err := a.lookupWindow(o.SubmittedOn)
		if err != nil {
			return a.fail(ExitFail, "%v", err), settleStop
		}
		fills, err := client.Fills(ctx, o.Symbol, from, to)
		if err != nil {
			return a.fail(ExitFail, "체결 조회 실패: %v", err), settleStop
		}
		got, sum := 0, 0.0
		for _, f := range fills {
			if sameOrderID(f.OrderID, o.OrderID) {
				got += f.Qty
				sum += float64(f.Qty) * f.Price
			}
		}
		style := broker.Style(o.Style)
		if got < o.Qty {
			// 🔴 아직 살아 있으면 기다린다. 죽었으면 다음 유형으로 승격한다
			alive := stillOpen(ctx, client, o.OrderID)
			if alive == nil || *alive {
				why := "아직 체결 대기 중"
				if alive == nil {
					why = "미체결 조회 실패 — 승격 보류"
				}
				a.printf("%s 미체결 — %s %s %d/%d주 (%s, %s 제출) · %s", ym, o.Symbol, o.Side, got, o.Qty, style.Label(), o.SubmittedOn, why)
				a.printf("  오늘은 새 주문을 내지 않는다")
				return ExitOK, settleStop
			}
			asked := o.Qty
			nxt := book.StyleIdx + 1
			if dryRun {
				a.printf("%s %s 만료 — %d주 중 %d주 체결", ym, style.Label(), asked, got)
				if nxt < len(broker.Styles) {
					a.printf("[dry-run] 승격하지 않았다 — 실제 실행은 %s로 다시 낸다", broker.Styles[nxt].Label())
				} else {
					a.printf("[dry-run] 승격하지 않았다 — 실제 실행은 모든 유형 소진으로 멈추고 알린다")
				}
				return ExitOK, settleStop
			}
			o.Expired, o.Filled, o.Qty = true, true, got // 채워진 만큼만 인정하고 닫는다
			o.FillPrice = 0
			if got > 0 {
				o.FillPrice = sum / float64(got)
			}
			book.StyleIdx = nxt
			if err := a.Store.SaveOrders(ctx, *book); err != nil {
				return a.fail(ExitFail, "주문 상태 저장 실패: %v", err), settleStop
			}
			a.printf("%s %s 만료 — %d주 중 %d주 체결", ym, style.Label(), asked, got)
			if nxt >= len(broker.Styles) {
				return a.fail(ExitFail, "%s %s 주문이 모든 유형에서 체결되지 않았다.\n  거래정지·시스템 장애 같은 예외 상황이다. 재시도로 풀리지 않는다.\n  증권사 앱에서 직접 처리할 것", o.Symbol, o.Side), settleStop
			}
			a.printf("  → %s로 즉시 승격한다", broker.Styles[nxt].Label())
			return ExitOK, settlePromoted
		}
		// 🔴 실제 체결 수량을 적는다. 증권사가 주문보다 많이 채워 주면 그것이 사실이다
		if got != o.Qty {
			a.printf("  ⚠ 주문 %d주 · 체결 %d주 — 체결 수량으로 기록한다", o.Qty, got)
		}
		o.Filled, o.Qty, o.FillPrice = true, got, sum/float64(got)
		a.printf("  체결 확인 — %s %s %d주 @ $%.4f", o.Symbol, o.Side, got, o.FillPrice)
	}
	if !dryRun {
		if err := a.Store.SaveOrders(ctx, *book); err != nil {
			return a.fail(ExitFail, "주문 상태 저장 실패: %v", err), settleStop
		}
	}

	if book.Stage == "sell" { // 매도 끝 → 매수
		if dryRun {
			a.printf("[dry-run] 저장하지 않았다 - 실제 실행이 매도 완료로 표시하고, 매도대금이 주문가능금액에 들어와 있으면 같은 실행에서 매수를 낸다")
			return ExitOK, settleStop
		}
		// 🔴 레그가 바뀌면 유형도 처음부터 다시 시도한다 — 매수도 MOC를 써 볼 기회를 준다
		book.Stage, book.StyleIdx = "buy", 0
		if err := a.Store.SaveOrders(ctx, *book); err != nil {
			return a.fail(ExitFail, "주문 상태 저장 실패: %v", err), settleStop
		}
		// 🔴 매도 체결을 확인했으니 매수 규모를 안다. 다음 실행을 기다리면 하루가 더 걸리고, 8일에 낸 매도는
		// 확인조차 못 한 채 창이 닫혔다. 매수는 체결된 매도 뒤에만 낸다는 규칙은 그대로다
		a.printf("  매도 완료. 이어서 매수한다")
		return ExitOK, settleSold
	}

	// 매수까지 끝났다 — 실제 체결값으로 계좌를 만든다 (수량가중 평균)
	bought, buyPx := avgLegs(legsOf(*book, broker.Buy, target))
	if bought <= 0 {
		return a.fail(ExitFail, "%s 체결이 없다. 주문 이력을 확인할 것", target), settleStop
	}
	planKRW, err := a.Store.ContributionFor(ctx, ym)
	if err != nil {
		return a.fail(ExitFail, "%v", err), settleStop
	}
	before := snapshot(acct, pos)

	// 🔴 장부는 받아 적기만 한다. 다시 계산하지 않는다
	soldQty, _ := sold(*book)
	if acct.Symbol != "" && acct.Symbol != target {
		if soldQty <= 0 {
			return a.fail(ExitFail, "%s을 팔아야 하는데 체결된 매도가 없다", acct.Symbol), settleStop
		}
		// 앱 밖 매매로 장부가 실제보다 적었다면 실제로 판 수량이 정답이다
		sq, sellPx := avgLegs(legs(*book, broker.Sell))
		a2 := acct.Clone()
		a2.Shares = sq
		if acct, err = invest.SellAll(a2, sellPx, fx, today.Year(), invest.FeeRate); err != nil {
			return a.fail(ExitFail, "%v", err), settleStop
		}
	}
	// 🔴 원금은 이번에 새로 들어간 돈이다 (§12.1.1.4). 체결액 전부를 적으면 전환 달에 매도대금이
	// 원금으로 두 번 세어진다. 통합증거금은 필요한 만큼만 환전하므로 새로 환전된 몫이 실제 입금액이다
	spentNow := float64(bought) * buyPx * (1 + invest.FeeRate)
	newMoneyKRW := math.Max(0, spentNow-acct.CashUSD) * fx
	stepped, err := invest.Contribute(acct, newMoneyKRW, fx)
	if err != nil {
		return a.fail(ExitFail, "%v", err), settleStop
	}
	newAcct, err := invest.BuyExact(stepped, target, bought, buyPx, fx, invest.FeeRate)
	if err != nil {
		return a.fail(ExitFail, "%v", err), settleStop
	}
	if math.Abs(newMoneyKRW-float64(planKRW)) > float64(planKRW)*0.05 {
		a.printf("  계획 %s원 · 실제 투입 %s원", commas(float64(planKRW)), won(newMoneyKRW))
	}
	if dryRun {
		a.printf("[dry-run] %s %s %d주 · 잔여 %s — 저장하지 않았다", ym, target, newAcct.Shares, usd(newAcct.CashUSD))
		return ExitOK, settleStop
	}
	zero := 0
	next := invest.Position{
		Holdings: newAcct, LastRun: pos.LastRun, YM: pos.YM, Signal: pos.Signal, FXRate: fx, ApplyYM: ym,
		ContributedKRW: pos.ContributedKRW + newMoneyKRW, MonthsInvested: pos.MonthsInvested + 1,
		JudgmentCount: pos.JudgmentCount, CloseUSD: closesOf(row),
		ExecutedOn: invest.StrPtr(invest.FormatDay(today)), ExecutedYM: invest.StrPtr(ym),
		LateDays: &zero, Before: before, FXEstimated: fxEst,
	}
	shares := newAcct.Shares - stepped.Shares
	note := ""
	if fxEst {
		note = "환율 추정"
	}
	err = a.Store.InTx(ctx, func(s *store.Store) error {
		if err := s.SavePosition(ctx, next); err != nil {
			return err
		}
		if err := s.AppendLedger(ctx, invest.LedgerEntry{On: invest.FormatDay(today), YM: ym, AmountKRW: newMoneyKRW, Source: "trade", Symbol: target, Shares: &shares, Note: note}); err != nil {
			return err
		}
		return s.ClearOrders(ctx)
	})
	if err != nil {
		return a.fail(ExitFail, "🚨 체결 반영 저장 실패: %v — 주문 기록은 남아 있다. 다음 실행이 다시 반영한다", err), settleStop
	}
	a.printf("\n%s 자동 매매 완료", ym)
	a.printf("  %s %d주 · 잔여 %s", newAcct.Symbol, newAcct.Shares, usd(newAcct.CashUSD))
	a.printf("  평가 %s원 (세후)", won(newAcct.NetValueKRW(buyPx, fx)))
	if fxEst {
		a.printf("  ⚠ 환율 %s는 시장 환율 추정이다. 증권사 적용값이 확인되면", plain2(fx))
		a.printf("     myhome invest amend --fill %s=<체결단가> --fx <실제환율>", target)
	}
	return ExitOK, settleStop
}
