package toss

import (
	"testing"
	"time"

	"github.com/yuntyu01/MyHome/server/internal/invest/broker"
)

// 입출금·환전 응답 모양은 openapi.json(2026-11-20 확인)의 Transaction 그대로다. 값은 지어낸 것이다.

func cashRow(typ, currency, amount, fx, at string, cancel bool) map[string]any {
	n := func(s string) any {
		if s == "" {
			return nil
		}
		return s
	}
	country := "KR"
	if currency == "USD" {
		country = "US"
	}
	return map[string]any{
		"transactionType": typ, "marketCountry": country, "currency": currency, "symbol": nil, "assetClass": nil,
		"isinCode": nil, "quantity": nil, "filledPrice": nil, "amount": amount, "commission": "0", "tax": "0",
		"netAmount": amount, "exchangeRate": n(fx), "settlementDate": at[:10], "settled": true,
		"transactionTime": n(at), "isCancellation": cancel,
	}
}

func TestCashFlowsReadDepositsAndBothExchangeLegs(t *testing.T) {
	fx := client(t)
	fx.routeFn("GET", "/api/v1/transactions", func(c seen) (int, any) {
		if c.Query.Get("cursor") == "" {
			return 200, txPage("NEXT",
				cashRow("DEPOSIT", "KRW", "600000", "", "2026-12-05T14:02:11+09:00", false),
				cashRow("DEPOSIT", "KRW", "10000", "", "2026-12-05T15:00:00+09:00", true), // 취소
			)
		}
		return 200, txPage(nil,
			cashRow("EXCHANGE_OUT", "KRW", "599000", "", "2026-12-08T00:18:02+09:00", false),
			cashRow("EXCHANGE_IN", "USD", "413.10", "1450.01", "2026-12-08T00:18:02+09:00", false),
		)
	})
	got, err := fx.c.CashFlows(ctx, day(2026, 12, 1))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("행 = %+v", got)
	}
	dep := got[0]
	if dep.Kind != broker.FlowDeposit || dep.Currency != "KRW" || dep.Amount != 600000 ||
		!dep.At.Equal(time.Date(2026, 12, 5, 14, 2, 11, 0, broker.KST)) {
		t.Fatalf("입금 = %+v", dep)
	}
	if out := got[1]; out.Kind != broker.FlowExchangeOut || out.Amount != 599000 || out.FX != 0 {
		t.Fatalf("환전(원화) = %+v", out)
	}
	if in := got[2]; in.Kind != broker.FlowExchangeIn || in.Currency != "USD" || in.Amount != 413.10 || in.FX != 1450.01 {
		t.Fatalf("환전(달러) = %+v", in)
	}
	q := fx.callsTo("GET", "/api/v1/transactions")[0].Query
	if q.Get("transactionTypes") != "DEPOSIT,EXCHANGE_IN,EXCHANGE_OUT" || q.Get("from") != "2026-12-01" || q.Get("to") != "" {
		t.Fatalf("조회 조건 = %v", q)
	}
}

func TestCashKRWAsksForWon(t *testing.T) {
	fx := client(t)
	fx.route("GET", "/api/v1/buying-power", 200, ok(map[string]any{"currency": "KRW", "cashBuyingPower": "600000"}))
	got, err := fx.c.CashKRW(ctx)
	if err != nil || got != 600000 {
		t.Fatalf("원화 = %v %v", got, err)
	}
	if c := fx.callsTo("GET", "/api/v1/buying-power")[0]; c.Query.Get("currency") != "KRW" || c.Header.Get(AccountHeader) != "42" {
		t.Fatalf("조회 = %v", c.Query)
	}
}

// 🔴 TQQQ·QQQM 말고는 증권사로 나가지 않는다. 나가지 않았다는 것이 확실해야 한다(IsRejected).
func TestOrderRefusesAnyOtherSymbol(t *testing.T) {
	fx := client(t)
	for _, sym := range []string{"AAPL", "tqqq", "TQQQ ", "", "QLD"} {
		_, err := fx.c.Order(ctx, sym, broker.Buy, 1, 70, broker.Sweep, "tq-test")
		if err == nil || !fx.c.IsRejected(err) {
			t.Fatalf("%q: %v", sym, err)
		}
	}
	if n := len(fx.callsTo("POST", "/api/v1/orders")); n != 0 {
		t.Fatalf("주문이 나갔다: %d", n)
	}
}
