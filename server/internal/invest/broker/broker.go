// Package broker는 증권사 공통 계약이다 (TQ broker/base.py 이식).
//
// 전략·계좌·알림·웹은 어느 증권사인지 몰라야 한다. 증권사마다 다른 응답 필드명은
// 어댑터 안에서 끝나고, 여기 있는 타입이 국경이다.
//
// 계약은 좁게 유지한다. 메서드가 늘면 새 증권사를 붙이는 비용이 그만큼 는다.
package broker

import (
	"context"
	"time"
)

// Side는 주문 방향이다.
type Side string

const (
	Buy  Side = "buy"
	Sell Side = "sell"
)

// Style은 주문 유형이다. 체결이 가격보다 먼저라 MOC → 시장가 → 시장가 대용 지정가 순으로 내려간다.
type Style string

const (
	MOC    Style = "moc"    // 장마감시장가 — 백테스트가 종가 기준이라 1순위
	Market Style = "market" // 시장가
	Sweep  Style = "sweep"  // 시장가 대용 지정가 (밴드는 호출부가 계산해 넘긴다)
)

// Styles는 승격 순서다.
var Styles = []Style{MOC, Market, Sweep}

// Label은 사람이 읽는 주문 유형 이름이다.
func (s Style) Label() string {
	switch s {
	case MOC:
		return "장마감시장가(MOC)"
	case Market:
		return "시장가"
	case Sweep:
		return "시장가 대용 지정가"
	}
	return string(s)
}

// Holding은 보유 종목 1건이다.
type Holding struct {
	Symbol         string
	Shares         int // 정수주. 소수 수량은 어댑터가 버린다
	AvgPriceUSD    float64
	MarketValueUSD float64
}

// Balance는 보유 종목과 매수가능 USD다.
type Balance struct {
	Holdings  []Holding
	CashUSD   float64
	FetchedAt time.Time
}

// SharesOf는 그 종목의 보유 수량이다.
func (b Balance) SharesOf(symbol string) int {
	n := 0
	for _, h := range b.Holdings {
		if h.Symbol == symbol {
			n += h.Shares
		}
	}
	return n
}

// Buyable은 증권사가 말하는 지금 살 수 있는 한도다. 앱 장부와 다르면 증권사가 정답이다.
//
// 이 값은 거래 자격을 검증하지 않는다. 자금 얘기일 뿐이다.
type Buyable struct {
	Symbol   string
	PriceUSD float64
	CashUSD  float64
	MaxQty   int
}

// OrderResult는 주문 접수 결과다. 체결이 아니라 접수다 — 체결 확인은 Fills가 한다.
type OrderResult struct {
	OrderID     string // 체결 대조의 유일한 열쇠
	Symbol      string
	Side        Side
	Qty         int
	Price       float64 // 실제로 전문에 실어 보낸 단가 (포맷 후 값). 단가 미지정 유형은 0
	SubmittedAt time.Time
}

// OrderRef는 증권사 주문 목록의 한 건이다. 접수 여부를 모르는 주문을 찾을 때 쓴다.
type OrderRef struct {
	OrderID    string
	Symbol     string // 증권사가 비워 주면 ""
	Side       Side   // 읽지 못하면 ""
	Open       bool   // 아직 살아 있는가
	FilledQty  int
	ByClientID bool // 행 어딘가에 찾는 clientOrderId가 있었다
}

// Fill은 체결 1건이다 (주문 단위 집계).
type Fill struct {
	OrderID string
	Symbol  string
	Side    Side
	Qty     int
	Price   float64
}

// Broker는 앱이 증권사에 요구하는 것 전부다. 쓰기는 Order 하나다.
type Broker interface {
	// Balance는 보유 종목과 매수가능 USD. 읽기 전용.
	Balance(ctx context.Context) (Balance, error)
	// Buyable은 그 종목을 그 단가로 지금 몇 주까지 살 수 있는가. 읽기 전용.
	Buyable(ctx context.Context, symbol string, priceUSD float64) (Buyable, error)
	// Order는 주문 제출이다. 쓰기다 — 실제 돈이 움직인다.
	// clientOrderID는 호출부가 정해 주문 기록에 먼저 남긴 멱등성 키다. 같은 키는 같은 주문이다.
	// 에러가 나면 호출부는 IsRejected로 갈라 본다. 명시적 거절만 다음 유형으로 승격한다.
	Order(ctx context.Context, symbol string, side Side, qty int, price float64, style Style, clientOrderID string) (OrderResult, error)
	// IsRejected는 주문이 접수되지 않은 것이 확실한가다(증권사의 명시적 거절, 보내기 전 실패).
	// false면 접수 여부를 모른다 — 타임아웃·연결 끊김·5xx·응답 형식 이상. 그때는 새 주문을 내지 않는다.
	IsRejected(err error) bool
	// FindOrders는 기간(KST 날짜, 양끝 포함)의 끝난 주문과 지금 살아 있는 주문 중 그 종목 것이다. 읽기 전용.
	// 행 어딘가에 clientOrderID가 있으면 종목과 상관없이 넣고 ByClientID를 켠다.
	FindOrders(ctx context.Context, symbol, clientOrderID string, from, to time.Time) ([]OrderRef, error)
	// Fills는 기간(KST 날짜, 양끝 포함)·종목의 체결이다. 넓게 묻고 좁게 거른다 — 주문번호 대조는 호출부가 한다.
	Fills(ctx context.Context, symbol string, from, to time.Time) ([]Fill, error)
	// OpenOrders는 미체결 주문 원본 행이다. 승격 전에 옛 주문이 죽었는지 확인하는 용도다.
	OpenOrders(ctx context.Context) ([]map[string]any, error)
	// IsFundsError는 자금 부족인가. 주문 유형을 바꿔도 안 풀리므로 승격을 멈춰야 한다.
	IsFundsError(err error) bool
	// Env는 실전/모의 구분. 표시와 안전장치에만 쓴다.
	Env() string
	// AccountMasked는 로그용 계좌 표시(앞 4자리 + ****)다.
	AccountMasked() string
}

// KST는 국내 증권사 달력의 시간대다.
var KST = time.FixedZone("KST", 9*60*60)

// Today는 증권사 달력의 오늘(KST 날짜, 자정)이다.
//
// 자동 매매는 미국 장중(UTC 15시대)에 돈다. 그 시각의 한국 날짜는 다음 날이다.
// UTC 날짜를 주문일자로 쓰면 하루 어긋난 키로 체결을 묻게 되고 영구 미체결이 된다.
func Today(now time.Time) time.Time {
	k := now.In(KST)
	return time.Date(k.Year(), k.Month(), k.Day(), 0, 0, 0, 0, KST)
}
