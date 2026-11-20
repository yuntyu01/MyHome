package invest

import "time"

// 입금일 — 월급이 들어와 자동이체로 증권 계좌에 돈이 들어오는 날(사용자 요청, 2026-11-20 "5일 월급, 5~6일 이체").
//
// TQ는 적용월 첫 거래일에 산다. 그런데 돈이 5~6일에 들어오면 첫 거래일 매수는 남은 달러만으로 끝나고
// 그 달 적립금은 한 달 묵는다. 입금일을 정하면 매수를 그 날 뒤로 미룬다. 늦은 매수는 이탈이 아니라
// 지연이다(TQ §6.2: 막을 것은 지연이 아니라 이탈, 매수일이 1일이든 20일이든 성과 차이는 3%).
//
// 날짜 기준: 입금일은 한국 날짜다. 매매 작업은 미국 장중(UTC 15:17, 한국 다음 날 0:17)에 도는데
// 그때 뉴욕 날짜가 입금일과 같으면 한국에서는 입금일 낮이 이미 지났다. 그래서 뉴욕 날짜와 그대로 견준다.

const (
	// MaxDepositDay보다 늦은 입금일은 받지 않는다. 매매 타이머가 1~24일만 돌고, 판정(25일~)과 겹치지 않게 한다.
	MaxDepositDay = 20
	// DepositGraceDays는 입금일이 지나도 입금이 안 보일 때 더 기다리는 날수다(달력 날짜). 그 뒤에는 있는 돈으로 산다.
	DepositGraceDays = 3
	// DepositSeenRatio는 이번 달 입금이 적립액의 이 비율 이상이면 "들어왔다"로 본다. 다른 용도의 소액 입금으로
	// 월급 이체 전에 사 버리지 않게 한다.
	DepositSeenRatio = 0.5
)

// firstTradingOnOrAfter는 d와 같거나 뒤인 첫 거래일이다(같은 달을 넘어갈 수 있다).
func firstTradingOnOrAfter(d time.Time) time.Time {
	for !IsTradingDay(d) {
		d = d.AddDate(0, 0, 1)
	}
	return d
}

// BuyDay는 적용월에 매수를 시작할 날(뉴욕 날짜)이다. 입금일을 정하지 않았으면(0) 첫 거래일이다.
func BuyDay(year int, month time.Month, depositDay int) time.Time {
	first := FirstTradingDay(year, month)
	if depositDay <= 0 {
		return first
	}
	d := firstTradingOnOrAfter(Date(year, month, min(depositDay, MaxDepositDay)))
	if d.Before(first) {
		return first
	}
	return d
}

// BuyDeadline은 입금이 안 보여도 있는 돈으로 사기 시작하는 날이다. 이 날이 지나도록 못 샀으면 매수가 빠진 것이다.
// 입금일이 없으면 BuyDay와 같다(예전 규칙: 첫 거래일이 지나면 미체결).
func BuyDeadline(year int, month time.Month, depositDay int) time.Time {
	if depositDay <= 0 {
		return BuyDay(year, month, 0)
	}
	return firstTradingOnOrAfter(Date(year, month, min(depositDay, MaxDepositDay)+DepositGraceDays))
}

// BuyDayOf·BuyDeadlineOf는 적용월 "YYYY-MM"으로 부른다.
func BuyDayOf(ym string, depositDay int) (time.Time, bool) {
	t, err := time.Parse("2006-01", ym)
	if err != nil {
		return time.Time{}, false
	}
	return BuyDay(t.Year(), t.Month(), depositDay), true
}

func BuyDeadlineOf(ym string, depositDay int) (time.Time, bool) {
	t, err := time.Parse("2006-01", ym)
	if err != nil {
		return time.Time{}, false
	}
	return BuyDeadline(t.Year(), t.Month(), depositDay), true
}

// DepositSeen은 이번 달 입금 합계가 적립액에 견줘 "들어왔다"고 볼 만한지다. 적립액이 없으면 1원이라도 들어오면 된다.
func DepositSeen(depositedKRW, planKRW float64) bool {
	if planKRW <= 0 {
		return depositedKRW > 0
	}
	return depositedKRW >= planKRW*DepositSeenRatio
}

// 자동 매수 단계 기록의 종류(invest_flow.step).
const (
	StepDeposit  = "deposit"
	StepExchange = "exchange"
	StepOrder    = "order"
	StepFill     = "fill"
)

// FlowEvent는 이번 달 자동 매수 단계에서 증권사에서 본 사실 1건이다(투자 화면 표시용, 입금은 매수 게이트에도 쓴다).
// YM·Step·Key·At은 열, 나머지는 data(jsonb)다.
type FlowEvent struct {
	YM   string    `json:"-"`
	Step string    `json:"-"`
	Key  string    `json:"-"` // 같은 사건을 두 번 적지 않는 키
	At   time.Time `json:"-"` // 사건 시각(입금·환전은 증권사 처리 시각, 주문·체결은 매매 작업이 확인한 시각)

	KRW     float64 `json:"krw,omitempty"`      // 입금액, 환전에 쓴 원화
	USD     float64 `json:"usd,omitempty"`      // 환전으로 받은 달러
	FX      float64 `json:"fx,omitempty"`       // 환전 적용 환율
	Mid     float64 `json:"mid,omitempty"`      // 그 시각 매매기준율(못 받았으면 0)
	Symbol  string  `json:"symbol,omitempty"`   // 주문·체결 종목
	Side    string  `json:"side,omitempty"`     // buy | sell
	Qty     int     `json:"qty,omitempty"`      // 주문·체결 수량
	Price   float64 `json:"price,omitempty"`    // 주문 단가(시장가 대용 지정가) 또는 평균 체결가
	Style   string  `json:"style,omitempty"`    // 주문 유형
	OrderID string  `json:"order_id,omitempty"` // 증권사 주문번호
}
