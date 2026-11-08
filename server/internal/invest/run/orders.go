package run

import (
	"context"
	"fmt"

	"github.com/yuntyu01/MyHome/server/internal/invest"
	"github.com/yuntyu01/MyHome/server/internal/invest/broker"
)

// OrdersOpts는 orders 옵션이다.
type OrdersOpts struct {
	// ClearStale이면 지난 적용월의 남은 주문 기록을 지운다(증권사에 살아 있을 수 있는 주문이 없을 때만).
	ClearStale bool
}

// Orders는 진행 중인 주문 기록을 보여준다. ClearStale이면 지난 적용월 기록을 정리한다.
//
// 지난달 주문 기록이 남아 있으면 trade는 새 주문을 내지 않는다(이중 체결 방지). 매매 창 마지막 날에 낸
// 주문은 체결 확인 실행이 없어 그렇게 남는다. 이 명령이 그 기록을 사람이 보고 치우는 길이다.
//
// 🔴 지우기 전에 증권사에 묻는다. 살아 있을 수 있는 주문이 하나라도 있거나 물어볼 수 없으면 지우지 않는다.
// 기록이 없어진 뒤 그 주문이 체결되면 아무도 모르는 매매가 된다. 체결이 있었는데 장부에 반영되지 않았으면
// 증권사 보유와 장부가 같을 때만 지운다(사람이 adjust 등으로 이미 맞췄다는 뜻이다). 지운 기록은
// invest_state_history에 남는다. 우회 옵션은 없다.
func (a *App) Orders(ctx context.Context, o OrdersOpts) int {
	pos, err := a.Store.LoadPosition(ctx)
	if err != nil {
		return a.fail(ExitFail, "%v", err)
	}
	book, err := a.Store.LoadOrders(ctx)
	if err != nil {
		return a.fail(ExitFail, "%v", err)
	}
	if book == nil {
		a.printf("진행 중인 주문 기록 없음 (적용월 %s)", pos.ApplyYM)
		return ExitOK
	}
	a.printf("주문 기록 %s · 단계 %s · 유형 %d번째", book.YM, book.Stage, book.StyleIdx+1)
	for _, ord := range book.Orders {
		a.printf("  %s", describeOrder(ord))
	}
	stale := book.YM != pos.ApplyYM
	if !stale {
		a.printf("이번 적용월(%s) 기록이다. trade가 이어서 처리한다", pos.ApplyYM)
		if o.ClearStale {
			return a.fail(ExitFail, "이번 적용월 기록은 지우지 않는다. trade가 체결을 확인해 정리한다")
		}
		return ExitOK
	}
	a.printf("⚠ 지난 적용월 기록이다(지금 적용월 %s). 이 기록이 있는 동안 trade는 새 주문을 내지 않는다", pos.ApplyYM)
	if !o.ClearStale {
		a.printf("  증권사에 남은 주문이 없으면: myhome invest orders --clear-stale")
		return ExitOK
	}

	client, err := a.Broker(ctx)
	if err != nil {
		return a.fail(ExitFail, "증권사 설정/연결 실패: %v\n  증권사에 묻지 않고는 지우지 않는다", err)
	}
	filled := 0
	for _, ord := range book.Orders {
		what := describeOrder(ord)
		from, to, err := a.lookupWindow(ord.SubmittedOn)
		if err != nil {
			return a.fail(ExitFail, "%v", err)
		}
		switch {
		case ord.Unknown || ord.OrderID == "":
			refs, err := client.FindOrders(ctx, ord.Symbol, ord.ClientOrderID, from, to)
			if err != nil {
				return a.fail(ExitFail, "지우지 않는다 - 결과를 모르는 주문을 증권사에서 찾지 못했다: %s\n  %v", what, err)
			}
			ref, got := pickUnknown(*book, ord, refs)
			switch {
			case got == lookupAmbiguous:
				return a.fail(ExitFail, "지우지 않는다 - 결과를 모르는 주문이 증권사의 어느 주문인지 확신할 수 없다: %s", what)
			case got == lookupFound && ref.Open:
				return a.fail(ExitFail, "지우지 않는다 - 증권사에 살아 있는 주문이다(주문번호 %s): %s\n  체결되거나 만료된 뒤 다시 실행할 것", ref.OrderID, what)
			case got == lookupFound:
				filled += ref.FilledQty
			}
		case !ord.Filled:
			alive := stillOpen(ctx, client, ord.OrderID)
			if alive == nil {
				return a.fail(ExitFail, "지우지 않는다 - 미체결 조회 실패, 살아 있는지 모른다: %s", what)
			}
			if *alive {
				return a.fail(ExitFail, "지우지 않는다 - 증권사에 살아 있는 주문이다: %s\n  체결되거나 만료된 뒤 다시 실행할 것", what)
			}
			fills, err := client.Fills(ctx, ord.Symbol, from, to)
			if err != nil {
				return a.fail(ExitFail, "지우지 않는다 - 체결 조회 실패: %s\n  %v", what, err)
			}
			for _, f := range fills {
				if sameOrderID(f.OrderID, ord.OrderID) {
					filled += f.Qty
				}
			}
		default:
			filled += ord.Qty // 기록에는 체결됐는데 장부(position)에 반영되기 전에 사이클이 끊겼다
		}
	}
	if filled > 0 {
		// 🔴 장부에 없는 체결이 있었다. 기록만 지우면 그 매매가 장부에서 사라진다
		bal, err := client.Balance(ctx)
		if err != nil {
			return a.fail(ExitFail, "지우지 않는다 - 체결 %d주가 있었는데 잔고를 확인하지 못했다: %v", filled, err)
		}
		if !holdingsMatch(bal, pos.Holdings) {
			return a.fail(ExitFail, "지우지 않는다 - 이 기록의 체결 %d주가 장부에 반영되지 않았다.\n  증권사 %s vs 장부 %s %d주.\n  먼저 장부를 증권사에 맞출 것(앱 밖 매수로 늘어난 것은 `myhome invest adjust --auto`, 그 밖은 사람이 판단). 맞춘 뒤 다시 실행한다",
				filled, holdingsText(bal), dash(pos.Holdings.Symbol), pos.Holdings.Shares)
		}
		a.printf("체결 %d주가 있었지만 증권사 보유와 장부가 같다 - 이미 반영됐다", filled)
	}
	if err := a.Store.ClearOrders(ctx); err != nil {
		return a.fail(ExitFail, "주문 기록 삭제 실패: %v", err)
	}
	a.printf("✓ %s 주문 기록을 지웠다 (이력은 invest_state_history에 남는다). 다음 trade가 %s 매수를 처음부터 진행한다", book.YM, pos.ApplyYM)
	return ExitOK
}

// holdingsMatch는 증권사 보유(이 전략 종목)가 장부와 같은가.
func holdingsMatch(bal broker.Balance, acct invest.Account) bool {
	for _, h := range bal.Holdings {
		if !invest.IsTradable(h.Symbol) || h.Shares <= 0 {
			continue
		}
		if h.Symbol != acct.Symbol {
			return false
		}
	}
	if acct.Symbol == "" {
		return acct.Shares == 0
	}
	return bal.SharesOf(acct.Symbol) == acct.Shares
}

func holdingsText(bal broker.Balance) string {
	s := ""
	for _, h := range bal.Holdings {
		if invest.IsTradable(h.Symbol) && h.Shares > 0 {
			if s != "" {
				s += ", "
			}
			s += fmt.Sprintf("%s %d주", h.Symbol, h.Shares)
		}
	}
	if s == "" {
		return "보유 없음"
	}
	return s
}
