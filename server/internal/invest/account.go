package invest

import (
	"fmt"
	"math"
)

// 계좌 상태기 — 적립·매수·매도·세금 (TQ strategy/account.py).
//
// 모든 연산은 새 Account를 돌려주고 기존 것을 바꾸지 않는다(값 타입 + 맵 복사).
//
// 🔴 세금 시점. 한국 해외주식 양도세는 다음 해 5월에 낸다. 매도 즉시 현금이 빠지지 않으므로
// 실현손익이 생기면 현금을 깎지 않고 미납 세금 부채로 잡고 평가액에서만 뺀다(NetValueKRW).
// 실제 계좌는 5월까지 그 돈을 쓸 수 있으므로 부채 방식이 현실과 맞다.

// 거래 조건.
//
// 🔴 두 요율을 섞지 말 것. BacktestFeeRate는 xlsx 결과를 만든 가정(재현 대조 전용)이고
// FeeRate는 실제 증권사 요율이다. 지금 값이 같아도 합치지 않는다 — 다음에 요율이 바뀔 때
// 백테스트 재현이 같이 틀어진다.
const (
	BacktestFeeRate = 0.001     // 편도 0.10% (수수료 + 호가 스프레드)
	FeeRate         = 0.001     // 토스 실제 요율 (2026-08-06, KIS 0.25%에서 변경)
	SECFeeRate      = 0.0000206 // 미국 SEC 수수료 — 매도에만
	TaxRate         = 0.22      // 양도소득세
	AnnualDeductKRW = 2_500_000 // 기본공제 250만원/년, 손실 이월공제 없음
)

// Account는 계좌 상태다. JSON 모양은 TQ state/position.json의 holdings와 같다.
type Account struct {
	Symbol          string          `json:"symbol"` // 보유 종목. 없으면 ""
	Shares          int             `json:"shares"` // 정수주
	CostKRW         float64         `json:"cost_krw"`
	CashUSD         float64         `json:"cash_usd"`          // 미투자 USD. 다음 달로 이월된다
	RealizedGainKRW map[int]float64 `json:"realized_gain_krw"` // 연도별 통산. 연도별로 독립
	TaxPaidKRW      map[int]float64 `json:"tax_paid_krw"`      // 연도별 실제 납부액
}

func copyYears(m map[int]float64) map[int]float64 {
	out := make(map[int]float64, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// Clone은 맵까지 복사한 사본이다.
func (a Account) Clone() Account {
	a.RealizedGainKRW = copyYears(a.RealizedGainKRW)
	a.TaxPaidKRW = copyYears(a.TaxPaidKRW)
	return a
}

// TaxDueKRW는 그 연도분 양도세. 손실이면 0이고 다음 해로 넘기지 않는다.
func (a Account) TaxDueKRW(year int) float64 {
	return math.Max(0, a.RealizedGainKRW[year]-AnnualDeductKRW) * TaxRate
}

// TaxUnpaidKRW는 그 연도분 미납액.
func (a Account) TaxUnpaidKRW(year int) float64 {
	return math.Max(0, a.TaxDueKRW(year)-a.TaxPaidKRW[year])
}

// TaxLiabilityKRW는 아직 내지 않은 양도세 총액이다.
//
// 🔴 납부한 것은 뺀다. 안 빼면 2027년 5월에 낸 2026년분이 2030년 평가금액에서도 계속 빠진다.
func (a Account) TaxLiabilityKRW() float64 {
	sum := 0.0
	for y := range a.RealizedGainKRW {
		sum += a.TaxUnpaidKRW(y)
	}
	return sum
}

// GrossValueKRW는 세전 평가액 (보유주식 시가 + 현금).
func (a Account) GrossValueKRW(priceUSD, fx float64) float64 {
	return float64(a.Shares)*priceUSD*fx + a.CashUSD*fx
}

// NetValueKRW는 세후 평가액이다. 화면에 보여줄 값은 항상 이것이다.
func (a Account) NetValueKRW(priceUSD, fx float64) float64 {
	return a.GrossValueKRW(priceUSD, fx) - a.TaxLiabilityKRW()
}

// Contribute는 원화를 입금해 USD로 환전한다. fx는 증권사 실제 적용 환율이어야 한다.
func Contribute(a Account, krw, fx float64) (Account, error) {
	if krw < 0 {
		return a, fmt.Errorf("적립액이 음수: %v", krw)
	}
	if fx <= 0 {
		return a, fmt.Errorf("환율 이상치: %v", fx)
	}
	a = a.Clone()
	a.CashUSD += krw / fx
	return a, nil
}

// SellAll은 전량 매도다. 실현손익을 그 연도에 통산하고 현금은 USD로 남긴다.
// 세금은 여기서 현금을 깎지 않는다.
func SellAll(a Account, priceUSD, fx float64, year int, feeRate float64) (Account, error) {
	a = a.Clone()
	if a.Shares == 0 {
		// 팔 것이 없다. 다만 종목 표시는 지운다 — 0주인데 종목이 남아 있으면 다음 달 다른
		// 종목을 살 때 전량 교체 규칙 위반으로 막힌다
		a.Symbol, a.CostKRW = "", 0
		return a, nil
	}
	if priceUSD <= 0 {
		return a, fmt.Errorf("체결단가 이상치: %v", priceUSD)
	}
	proceedsKRW := float64(a.Shares) * priceUSD * fx * (1 - feeRate - SECFeeRate)
	gain := proceedsKRW - a.CostKRW
	a.RealizedGainKRW[year] += gain
	a.CashUSD += proceedsKRW / fx
	a.Symbol, a.Shares, a.CostKRW = "", 0, 0
	return a, nil
}

// BuyMax는 가용 USD로 살 수 있는 정수주 최대 수량을 매수한다.
//
//	shares = floor(가용USD / (단가 × (1 + 수수료율)))
//
// 소수주를 쓰지 않는다. 잔여 현금은 USD로 남아 다음 달로 이월된다.
func BuyMax(a Account, symbol string, priceUSD, fx, feeRate float64) (Account, error) {
	if a.Symbol != "" && a.Symbol != symbol {
		return a, fmt.Errorf("%s 보유 중에 %s 매수 — 전량 교체 규칙 위반", a.Symbol, symbol)
	}
	if priceUSD <= 0 {
		return a, fmt.Errorf("체결단가 이상치: %v", priceUSD)
	}
	a = a.Clone()
	unit := priceUSD * (1 + feeRate)
	qty := int(math.Floor(a.CashUSD / unit))
	if qty <= 0 {
		// 🔴 한 주도 못 샀으면 그 종목을 보유했다고 적지 않는다 — 0주인데 종목만 남으면
		// 다음 달 전환에서 전량 교체 규칙 위반으로 죽는다
		if a.Shares == 0 {
			a.Symbol = ""
		}
		return a, nil
	}
	spent := float64(qty) * unit
	a.Symbol = symbol
	a.Shares += qty
	a.CostKRW += spent * fx
	a.CashUSD -= spent
	return a, nil
}

// BuyExact는 증권사가 실제로 채워 준 수량 그대로 매수를 반영한다.
//
// 🔴 BuyMax로 다시 계산하지 말 것. 주문 수량은 종가 기준 추정이고 체결은 다른 가격에
// 일어난다. 체결 뒤에 다시 계산하면 앱이 자기가 계산한 수량을 적는다 — 실제로 6주를 사고
// 19주로 기록한 적이 있다(§12.1.1.6). 수량은 증권사가 정답이다.
func BuyExact(a Account, symbol string, qty int, priceUSD, fx, feeRate float64) (Account, error) {
	if a.Symbol != "" && a.Symbol != symbol {
		return a, fmt.Errorf("%s 보유 중에 %s 매수 — 전량 교체 규칙 위반", a.Symbol, symbol)
	}
	if qty < 0 {
		return a, fmt.Errorf("체결 수량이 음수: %d", qty)
	}
	a = a.Clone()
	if qty == 0 {
		if a.Shares == 0 {
			a.Symbol = ""
		}
		return a, nil
	}
	if priceUSD <= 0 {
		return a, fmt.Errorf("체결단가 이상치: %v", priceUSD)
	}
	spent := float64(qty) * priceUSD * (1 + feeRate)
	a.Symbol = symbol
	a.Shares += qty
	a.CostKRW += spent * fx
	a.CashUSD -= spent
	return a, nil
}

// MonthlyStep은 월 첫 거래일 1회분이다. 교체 먼저, 적립 나중.
// 반대로 하면 매도 직전 계좌에 그 달 적립금이 섞여 실현손익이 달라진다.
func MonthlyStep(a Account, target string, contributionKRW, fx float64, pricesUSD map[string]float64, year int, feeRate float64) (Account, error) {
	tp, ok := pricesUSD[target]
	if !ok {
		return a, fmt.Errorf("%s 체결단가 없음", target)
	}
	var err error
	if a.Symbol != "" && a.Symbol != target {
		sp, ok := pricesUSD[a.Symbol]
		if !ok {
			return a, fmt.Errorf("매도할 %s의 체결단가 없음", a.Symbol)
		}
		if a, err = SellAll(a, sp, fx, year, feeRate); err != nil {
			return a, err
		}
	}
	if a, err = Contribute(a, contributionKRW, fx); err != nil {
		return a, err
	}
	return BuyMax(a, target, tp, fx, feeRate)
}
