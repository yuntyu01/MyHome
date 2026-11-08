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
//   - 주문이 거부되면 같은 실행에서 즉시 다음 유형
//   - 체결 없이 만료되면 다음 실행에서 다음 유형
//
// 세 유형이 다 막히면 재시도로 풀리는 종류가 아니다 — 멈추고 알린다. 3일 대기 같은 것은 없다.
//
// 🔴 왜 하루에 안 끝내는가. 전환하는 달은 매도가 체결돼야 매수 규모를 안다. 미체결 주문이
// 남아 있으면 새 주문을 절대 내지 않는다 — 이중 체결 방지.

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
	//    여기서는 타이머(매월 1~8일)가 휴장일에도 부르므로 명령이 직접 막는다. 실패가 아니라 건너뛰기다
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
	if bookp != nil {
		book = *bookp
	}
	if book.YM != ym { // 지난달 잔여는 이어받지 않는다
		return a.fail(ExitFail, "진행 중인 주문이 %s 것이다. 손으로 정리할 것", book.YM)
	}

	row, ok := prices.AsOf(today)
	if !ok {
		return a.fail(ExitFail, "%s 이전 가격이 없다", invest.FormatDay(today))
	}
	fx, fxEst := invest.TradeFX(o.FX, row)
	if fx <= 0 {
		return a.fail(ExitFail, "환율을 구할 수 없다. --fx로 넣거나 refresh로 USDKRW_MKT를 채울 것")
	}
	var pending []int
	for i, ord := range book.Orders {
		if !ord.Filled {
			pending = append(pending, i)
		}
	}

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

	// ① 미체결이 있으면 체결부터 확인. 새 주문은 내지 않는다.
	//    단, 주문이 만료됐으면 settle이 승격했다고 알리고 아래 주문 단계로 이어진다
	if len(pending) > 0 {
		rc, promoted := a.settle(ctx, client, &book, pending, today, acct, pos, target, row, fx, fxEst, o.DryRun)
		if !promoted {
			return rc
		}
		if b, err := a.Store.LoadOrders(ctx); err == nil && b != nil {
			book = *b
		}
		sells = sellLegsFor(acct, target)
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
		a.printf("%s 매도 주문 — %s %d주", ym, sym, qty)
		// 매도 레그가 있다는 것은 전환이다 — 실패 비용이 −27%짜리다
		return a.place(ctx, client, &book, sym, broker.Sell, qty, close, today, true)
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
	est := close * (1 + SizingBuffer)
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
	return a.place(ctx, client, &book, target, broker.Buy, qty, close, today, soldQty > 0)
}

// place는 주문 1건 제출이다. 유형을 내려가며 접수될 때까지 시도한다.
//
// 🔴 거부는 같은 실행에서 즉시 승격한다. 거부는 접수 자체가 안 된 것이라 이중 체결 위험이 없다.
// 세 유형이 다 막히면 재시도로 풀릴 종류가 아니다 — 멈추고 사람을 부른다.
func (a *App) place(ctx context.Context, client broker.Broker, book *invest.OrderBook, symbol string, side broker.Side, qty int, close float64, today time.Time, urgent bool) int {
	var tried []string
	for i := book.StyleIdx; i < len(broker.Styles); i++ {
		style := broker.Styles[i]
		px := limitPrice(close, side)
		shown := "(단가 미지정)"
		if style == broker.Sweep {
			shown = "@ " + usd(px)
		}
		a.printf("  %s로 %s %s %d주 %s", style.Label(), symbol, side, qty, shown)
		res, err := client.Order(ctx, symbol, side, qty, px, style)
		if err != nil {
			// 🔴 자금 부족이면 유형을 바꿔도 소용없다. 통합증거금은 신청해야 켜지는 서비스다
			if client.IsFundsError(err) {
				return a.fail(ExitFail, "자금 부족으로 주문 불가 — %s %s %d주\n  %v\n  주문 유형을 바꿔도 풀리지 않는다. 확인할 것:\n    · 통합증거금(원화주문)이 켜져 있는가 — 신청해야 켜진다\n    · 꺼져 있다면 미리 USD로 환전돼 있는가\n    · 원화가 실제로 입금됐는가",
					symbol, side, qty, err)
			}
			tried = append(tried, fmt.Sprintf("%s: %v", style.Label(), err))
			a.warnf("    거부됨 — 다음 유형으로 내려간다")
			continue
		}
		book.StyleIdx = i
		book.Orders = append(book.Orders, invest.BookOrder{
			OrderID: res.OrderID, Symbol: symbol, Side: string(side), Qty: qty, Price: px,
			Style: string(style), SubmittedOn: invest.FormatDay(today),
		})
		if err := a.Store.SaveOrders(ctx, *book); err != nil {
			// 주문은 이미 접수됐다. 저장 실패는 이중 주문으로 이어질 수 있어 크게 알린다.
			// 다음 실행은 같은 clientOrderId로 다시 내므로 증권사가 같은 주문을 돌려준다
			return a.fail(ExitFail, "🚨 주문 %s 접수 후 저장 실패: %v\n  증권사 앱에서 주문을 확인할 것", res.OrderID, err)
		}
		a.printf("  주문번호 %s (%s) — 체결 확인은 다음 실행에서", res.OrderID, style.Label())
		return ExitOK
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

// settle은 미체결 주문을 확인하고 계좌에 반영한다. promoted가 true면 주문이 만료돼 승격했다는
// 뜻이고, 호출부가 이어서 다음 유형으로 새 주문을 낸다.
//
// 🔴 dryRun이면 조회만 하고 아무것도 쓰지 않는다. 미리 보려고 친 명령이 그 달 체결을 확정하면
// 안 된다 — 실제로 --dry-run이 position·ledger를 써서 체결을 확정한 적이 있다.
func (a *App) settle(ctx context.Context, client broker.Broker, book *invest.OrderBook, pending []int, today time.Time,
	acct invest.Account, pos invest.Position, target string, row invest.PriceRow, fx float64, fxEst, dryRun bool) (int, bool) {
	ym := book.YM
	for _, i := range pending {
		o := &book.Orders[i]
		// 🔴 조회 창을 넓게 잡는다. submitted_on은 뉴욕 날짜이고 증권사 주문일자는 KST다. 미국 장중에
		// 낸 주문은 증권사 장부에 다음 날로 찍힌다. 대조는 주문번호로 하므로 남의 주문을 주워오지 않는다
		sub, err := invest.ParseDay(o.SubmittedOn)
		if err != nil {
			return a.fail(ExitFail, "주문 기록 형식 이상: %v", err), false
		}
		end := broker.Today(a.now())
		bt := invest.Date(end.Year(), end.Month(), end.Day())
		if sub.After(bt) {
			bt = sub
		}
		fills, err := client.Fills(ctx, o.Symbol, sub.AddDate(0, 0, -1), bt.AddDate(0, 0, 1))
		if err != nil {
			return a.fail(ExitFail, "체결 조회 실패: %v", err), false
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
				return ExitOK, false
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
				return ExitOK, false
			}
			o.Expired, o.Filled, o.Qty = true, true, got // 채워진 만큼만 인정하고 닫는다
			o.FillPrice = 0
			if got > 0 {
				o.FillPrice = sum / float64(got)
			}
			book.StyleIdx = nxt
			if err := a.Store.SaveOrders(ctx, *book); err != nil {
				return a.fail(ExitFail, "주문 상태 저장 실패: %v", err), false
			}
			a.printf("%s %s 만료 — %d주 중 %d주 체결", ym, style.Label(), asked, got)
			if nxt >= len(broker.Styles) {
				return a.fail(ExitFail, "%s %s 주문이 모든 유형에서 체결되지 않았다.\n  거래정지·시스템 장애 같은 예외 상황이다. 재시도로 풀리지 않는다.\n  증권사 앱에서 직접 처리할 것", o.Symbol, o.Side), false
			}
			a.printf("  → %s로 즉시 승격한다", broker.Styles[nxt].Label())
			return ExitOK, true
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
			return a.fail(ExitFail, "주문 상태 저장 실패: %v", err), false
		}
	}

	if book.Stage == "sell" { // 매도 끝 → 다음 실행에서 매수
		if dryRun {
			a.printf("[dry-run] 저장하지 않았다 — 실제 실행이 매도 완료로 표시하고 매수는 그다음 실행에서 낸다")
			return ExitOK, false
		}
		// 🔴 레그가 바뀌면 유형도 처음부터 다시 시도한다 — 매수도 MOC를 써 볼 기회를 준다
		book.Stage, book.StyleIdx = "buy", 0
		if err := a.Store.SaveOrders(ctx, *book); err != nil {
			return a.fail(ExitFail, "주문 상태 저장 실패: %v", err), false
		}
		a.printf("  매도 완료. 매수는 다음 실행에서 낸다")
		return ExitOK, false
	}

	// 매수까지 끝났다 — 실제 체결값으로 계좌를 만든다 (수량가중 평균)
	bought, buyPx := avgLegs(legsOf(*book, broker.Buy, target))
	if bought <= 0 {
		return a.fail(ExitFail, "%s 체결이 없다. 주문 이력을 확인할 것", target), false
	}
	planKRW, err := a.Store.ContributionFor(ctx, ym)
	if err != nil {
		return a.fail(ExitFail, "%v", err), false
	}
	before := snapshot(acct, pos)

	// 🔴 장부는 받아 적기만 한다. 다시 계산하지 않는다
	soldQty, _ := sold(*book)
	if acct.Symbol != "" && acct.Symbol != target {
		if soldQty <= 0 {
			return a.fail(ExitFail, "%s을 팔아야 하는데 체결된 매도가 없다", acct.Symbol), false
		}
		// 앱 밖 매매로 장부가 실제보다 적었다면 실제로 판 수량이 정답이다
		sq, sellPx := avgLegs(legs(*book, broker.Sell))
		a2 := acct.Clone()
		a2.Shares = sq
		if acct, err = invest.SellAll(a2, sellPx, fx, today.Year(), invest.FeeRate); err != nil {
			return a.fail(ExitFail, "%v", err), false
		}
	}
	// 🔴 원금은 이번에 새로 들어간 돈이다 (§12.1.1.4). 체결액 전부를 적으면 전환 달에 매도대금이
	// 원금으로 두 번 세어진다. 통합증거금은 필요한 만큼만 환전하므로 새로 환전된 몫이 실제 입금액이다
	spentNow := float64(bought) * buyPx * (1 + invest.FeeRate)
	newMoneyKRW := math.Max(0, spentNow-acct.CashUSD) * fx
	stepped, err := invest.Contribute(acct, newMoneyKRW, fx)
	if err != nil {
		return a.fail(ExitFail, "%v", err), false
	}
	newAcct, err := invest.BuyExact(stepped, target, bought, buyPx, fx, invest.FeeRate)
	if err != nil {
		return a.fail(ExitFail, "%v", err), false
	}
	if math.Abs(newMoneyKRW-float64(planKRW)) > float64(planKRW)*0.05 {
		a.printf("  계획 %s원 · 실제 투입 %s원", commas(float64(planKRW)), won(newMoneyKRW))
	}
	if dryRun {
		a.printf("[dry-run] %s %s %d주 · 잔여 %s — 저장하지 않았다", ym, target, newAcct.Shares, usd(newAcct.CashUSD))
		return ExitOK, false
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
		return a.fail(ExitFail, "🚨 체결 반영 저장 실패: %v — 주문 기록은 남아 있다. 다음 실행이 다시 반영한다", err), false
	}
	a.printf("\n%s 자동 매매 완료", ym)
	a.printf("  %s %d주 · 잔여 %s", newAcct.Symbol, newAcct.Shares, usd(newAcct.CashUSD))
	a.printf("  평가 %s원 (세후)", won(newAcct.NetValueKRW(buyPx, fx)))
	if fxEst {
		a.printf("  ⚠ 환율 %s는 시장 환율 추정이다. 증권사 적용값이 확인되면", plain2(fx))
		a.printf("     myhome invest amend --fill %s=<체결단가> --fx <실제환율>", target)
	}
	return ExitOK, false
}
