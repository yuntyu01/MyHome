package toss

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/yuntyu01/MyHome/server/internal/invest/broker"
)

// ── 선택 계약: 결제 내역·환율 (broker.Settlements) ─────────────────────
//
// 통합증거금은 주문할 때 필요한 만큼 원화를 환전한다. 그 환율은 주문 응답에도 체결 조회에도 없고,
// 결제가 끝난 거래내역(GET /api/v1/transactions)의 exchangeRate에만 있다(미국 주식은 T+1).
// 그래서 체결 직후에는 시장 환율로 추정해 적고, 결제 뒤 이 값으로 확정한다.

var _ broker.Settlements = (*Client)(nil)

// Settlements는 기간(결제일 기준 KST 날짜)·종목의 미국 주식 매수·매도다. 읽기 전용.
//
// 🔴 미결제 행은 단가·환율이 비어 있다(문서). 0을 진짜 값처럼 돌려주지 않게 Settled=false로 표시한다.
func (c *Client) Settlements(ctx context.Context, symbol string, from, to time.Time) ([]broker.Settlement, error) {
	const where = "GET /api/v1/transactions"
	if from.IsZero() || to.IsZero() {
		return nil, fmt.Errorf("조회 기간이 비었다")
	}
	start, end := ymd(from), ymd(to)
	if end < start {
		return nil, fmt.Errorf("조회 종료일이 시작일보다 앞선다: %s → %s", start, end)
	}
	q := url.Values{"transactionTypes": {"BUY,SELL"}, "from": {start}, "to": {end}, "limit": {"100"}}
	var out []broker.Settlement
	cursor := ""
	for range maxPages {
		if cursor != "" {
			q.Set("cursor", cursor)
		}
		raw, err := c.request(ctx, http.MethodGet, "/api/v1/transactions", reqOpts{query: q})
		if err != nil {
			return nil, err
		}
		var res struct {
			Transactions []map[string]any `json:"transactions"`
			HasNext      any              `json:"hasNext"`
			NextCursor   any              `json:"nextCursor"`
		}
		if err := decode(raw, where, &res); err != nil {
			return nil, err
		}
		for _, row := range res.Transactions {
			s, ok, err := c.settlementOf(row, symbol)
			if err != nil {
				return nil, err
			}
			if ok {
				out = append(out, s)
			}
		}
		if !truthy(res.HasNext) {
			return out, nil
		}
		if cursor = orText(res.NextCursor); cursor == "" {
			return out, nil
		}
	}
	return nil, &Error{Message: fmt.Sprintf("거래내역 %d페이지 초과 — 절단된 결과를 돌려주느니 멈춘다", maxPages)}
}

func (c *Client) settlementOf(row map[string]any, symbol string) (broker.Settlement, bool, error) {
	side, ok := sideIn[text(row["transactionType"])]
	if !ok || text(row["marketCountry"]) != marketUS || truthy(row["isCancellation"]) {
		return broker.Settlement{}, false, nil
	}
	if sym := strings.TrimSpace(text(row["symbol"])); sym != symbol {
		return broker.Settlement{}, false, nil
	}
	day, err := time.ParseInLocation("2006-01-02", text(row["settlementDate"]), broker.KST)
	if err != nil {
		return broker.Settlement{}, false, &Error{Message: "거래내역 결제일 형식 이상 — 규격 변경 의심"}
	}
	qty, err := num(row["quantity"], "거래내역 수량")
	if err != nil {
		return broker.Settlement{}, false, err
	}
	if qty != math.Floor(qty) {
		c.warn(fmt.Sprintf("%s 거래내역 수량 %s — 소수 수량이라 버림 처리", symbol, floatText(qty)))
	}
	s := broker.Settlement{Day: day, Symbol: symbol, Side: side, Qty: int(math.Floor(qty)), Settled: truthy(row["settled"])}
	for _, f := range []struct {
		dst  *float64
		key  string
		what string
	}{{&s.Price, "filledPrice", "거래내역 단가"}, {&s.Amount, "amount", "거래금액"}, {&s.Net, "netAmount", "정산금액"}, {&s.FX, "exchangeRate", "거래 환율"}} {
		if *f.dst, err = numOr0(row[f.key], f.what); err != nil {
			return broker.Settlement{}, false, err
		}
	}
	if !s.Settled {
		s.FX = 0 // 미결제 환율은 없다(문서). 혹시 실려 와도 확정값으로 쓰지 않는다
	}
	return s, true, nil
}

// FXAt은 그 시각의 원/달러 환율이다(at이 0이면 지금). 시세 API라 계좌 헤더를 싣지 않는다. 읽기 전용.
func (c *Client) FXAt(ctx context.Context, at time.Time) (broker.FXQuote, error) {
	const where = "GET /api/v1/exchange-rate"
	q := url.Values{"baseCurrency": {currencyUSD}, "quoteCurrency": {"KRW"}}
	if !at.IsZero() {
		q.Set("dateTime", at.In(broker.KST).Format(time.RFC3339))
	}
	raw, err := c.request(ctx, http.MethodGet, "/api/v1/exchange-rate", reqOpts{query: q, noAccount: true})
	if err != nil {
		return broker.FXQuote{}, err
	}
	var res struct {
		Rate      any `json:"rate"`
		MidRate   any `json:"midRate"`
		ValidFrom any `json:"validFrom"`
	}
	if err := decode(raw, where, &res); err != nil {
		return broker.FXQuote{}, err
	}
	rate, err := num(res.Rate, "환율")
	if err != nil {
		return broker.FXQuote{}, err
	}
	mid, err := num(res.MidRate, "매매기준율")
	if err != nil {
		return broker.FXQuote{}, err
	}
	if rate <= 0 || mid <= 0 {
		return broker.FXQuote{}, &Error{Message: "환율이 0 이하 — 규격 변경 의심"}
	}
	qt := broker.FXQuote{Rate: rate, Mid: mid, At: at}
	if t, err := time.Parse(time.RFC3339, text(res.ValidFrom)); err == nil {
		qt.At = t
	}
	return qt, nil
}
