package toss

import (
	"testing"
	"time"

	"github.com/yuntyu01/MyHome/server/internal/invest/broker"
)

// 거래내역·환율 응답 모양은 openapi.json(2026-11-09 확인)의 Transaction·ExchangeRateResponse 그대로다.
// 값은 지어낸 것이다(실제 계좌 아님).

func txRow(typ, country, sym, qty, px, amount, net, fx, day string, settled bool) map[string]any {
	n := func(s string) any {
		if s == "" {
			return nil
		}
		return s
	}
	return map[string]any{
		"transactionType": typ, "marketCountry": country, "currency": "USD", "symbol": n(sym), "assetClass": "STOCK",
		"isinCode": nil, "quantity": n(qty), "filledPrice": n(px), "amount": n(amount), "commission": "0.70", "tax": "0",
		"netAmount": n(net), "exchangeRate": n(fx), "settlementDate": day, "settled": settled,
		"transactionTime": nil, "isCancellation": false,
	}
}

func txPage(cursor any, rows ...any) map[string]any {
	if rows == nil {
		rows = []any{}
	}
	return ok(map[string]any{"transactions": rows, "nextCursor": cursor, "hasNext": cursor != nil})
}

func TestSettlementsReadTheAppliedRate(t *testing.T) {
	fx := client(t)
	fx.routeFn("GET", "/api/v1/transactions", func(c seen) (int, any) {
		if c.Query.Get("cursor") == "" {
			return 200, txPage("NEXT",
				txRow("BUY", "US", "TQQQ", "10", "70.12", "701.2", "701.9", "1352.3", "2026-11-04", true),
				txRow("BUY", "US", "QLD", "3", "90.1", "270.3", "270.57", "1352.3", "2026-11-04", true), // 다른 종목
				txRow("SELL", "KR", "TQQQ", "1", "1", "1", "1", "", "2026-11-04", true),                 // 국내
			)
		}
		return 200, txPage(nil, txRow("BUY", "US", "TQQQ", "5", "", "300", "300.3", "1500", "2026-11-05", false)) // 미결제
	})
	got, err := fx.c.Settlements(ctx, "TQQQ", day(2026, 11, 1), day(2026, 11, 9))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("행 = %+v", got)
	}
	a := got[0]
	if a.Side != broker.Buy || a.Qty != 10 || a.Price != 70.12 || a.Net != 701.9 || a.FX != 1352.3 || !a.Settled ||
		!a.Day.Equal(day(2026, 11, 4)) {
		t.Fatalf("결제 행 = %+v", a)
	}
	if b := got[1]; b.Settled || b.FX != 0 {
		t.Fatalf("미결제 행의 환율을 확정값으로 넘겼다: %+v", b)
	}
	calls := fx.callsTo("GET", "/api/v1/transactions")
	if len(calls) != 2 {
		t.Fatalf("페이지를 끝까지 넘기지 않았다: %d", len(calls))
	}
	q := calls[0].Query
	if q.Get("transactionTypes") != "BUY,SELL" || q.Get("from") != "2026-11-01" || q.Get("to") != "2026-11-09" || q.Get("limit") != "100" {
		t.Fatalf("조회 조건 = %v", q)
	}
	if calls[0].Header.Get(AccountHeader) != "42" {
		t.Fatal("거래내역은 계좌 헤더가 필요하다")
	}
}

func TestSettlementsRejectAMalformedDate(t *testing.T) {
	fx := client(t)
	fx.route("GET", "/api/v1/transactions", 200, txPage(nil, txRow("BUY", "US", "TQQQ", "1", "1", "1", "1", "1", "11/04", true)))
	_, err := fx.c.Settlements(ctx, "TQQQ", day(2026, 11, 1), day(2026, 11, 9))
	mustContain(t, err, "결제일")
}

func TestFXAtAsksForThatMomentWithoutTheAccount(t *testing.T) {
	fx := client(t)
	fx.route("GET", "/api/v1/exchange-rate", 200, ok(map[string]any{
		"baseCurrency": "USD", "quoteCurrency": "KRW", "rate": "1351.6", "midRate": "1351.4", "basisPoint": "1",
		"rateChangeType": "UP", "validFrom": "2026-11-03T10:00:00.000+09:00", "validUntil": "2026-11-03T10:01:00.000+09:00",
	}))
	at := time.Date(2026, 11, 3, 1, 0, 30, 0, time.UTC)
	q, err := fx.c.FXAt(ctx, at)
	if err != nil {
		t.Fatal(err)
	}
	if q.Rate != 1351.6 || q.Mid != 1351.4 || q.At.IsZero() {
		t.Fatalf("환율 = %+v", q)
	}
	c := fx.callsTo("GET", "/api/v1/exchange-rate")[0]
	if c.Query.Get("dateTime") != "2026-11-03T10:00:30+09:00" || c.Query.Get("baseCurrency") != "USD" || c.Query.Get("quoteCurrency") != "KRW" {
		t.Fatalf("조회 조건 = %v", c.Query)
	}
	if c.Header.Get(AccountHeader) != "" {
		t.Fatal("시세 API에 계좌 헤더를 실었다")
	}
}

func TestFXAtRefusesAnEmptyRate(t *testing.T) {
	fx := client(t)
	fx.route("GET", "/api/v1/exchange-rate", 200, ok(map[string]any{"rate": nil, "midRate": "1351.4"}))
	_, err := fx.c.FXAt(ctx, time.Time{})
	mustContain(t, err, "환율")
	if got := fx.callsTo("GET", "/api/v1/exchange-rate")[0].Query.Get("dateTime"); got != "" {
		t.Fatalf("시각을 안 줬으면 dateTime을 싣지 않는다: %q", got)
	}
}
