package invest

import (
	"fmt"
	"math"
	"time"
)

// 매일 기록(사용자 요청, 2026-11-20 "매일 기록하고 판단은 기존처럼 달 단위로").
//
// 거래일마다 그날 종가로 본 신호(SPX vs 185일선, NDX 21일 변동성)와 계좌 원금·금액을 남긴다.
// 🔴 보여 주기만 한다. 판정·주문은 이 기록을 읽지 않는다 — 판정은 여전히 달 마지막 거래일 종가 하나다(MonthlySignals).

// DailySignal은 하루 종가로 본 신호다. 판정이 아니다.
type DailySignal struct {
	Day    time.Time
	SPX    float64
	SMA185 float64
	C1     bool
	Vol    float64
	C2     bool
	Pos    string // 역할 라벨 (RiskOn·RiskOff)
}

// DailySignals는 from 이후(포함) 거래일마다의 신호다. in은 전체 이력이어야 한다(rolling은 전체에 건다).
func DailySignals(in SignalInputs, from time.Time) ([]DailySignal, error) {
	if err := in.Validate(); err != nil {
		return nil, err
	}
	sma := SMA(in.SPX, SMAPeriod)
	vol := RealizedVol(in.NDX, VolWindow, TradingDays)
	var out []DailySignal
	for i, d := range in.Days {
		if d.Before(from) {
			continue
		}
		if math.IsNaN(sma[i]) || math.IsNaN(vol[i]) {
			return nil, fmt.Errorf("%s의 SMA185·vol이 비었다. 전체 이력을 넣었는지 확인할 것", FormatDay(d))
		}
		c1, c2 := CondTrend(in.SPX[i], sma[i]), CondVol(vol[i])
		out = append(out, DailySignal{Day: d, SPX: in.SPX[i], SMA185: sma[i], C1: c1, Vol: vol[i], C2: c2, Pos: ResolvePosition(c1, c2)})
	}
	return out, nil
}

// Valuation은 한 시점의 계좌 평가다(세전, 원). 투자 첫 화면 요약과 매일 기록이 같은 식을 쓴다.
type Valuation struct {
	StockKRW    float64 // 주식(종가 × 환율)
	CashKRW     float64 // 달러 현금(원화 환산) + 원화 현금
	InvestedKRW float64 // 주식에 넣은 돈(누적 원금)
	GainKRW     float64 // (주식 + 달러 현금) − 원금. 아직 안 바꾼 원화는 원금에 없어 뺀다
}

// TotalKRW는 증권 총액(주식 + 현금)이다.
func (v Valuation) TotalKRW() float64 { return v.StockKRW + v.CashKRW }

// GainRatio는 수익률이다. 원금이 없으면 ok가 false.
func (v Valuation) GainRatio() (float64, bool) {
	if v.InvestedKRW <= 0 {
		return 0, false
	}
	return v.GainKRW / v.InvestedKRW, true
}

// Value는 계좌를 그 종가(priceUSD, 보유 종목)·환율로 평가한다. krwCash는 증권사 원화 주문가능 금액(모르면 0).
func Value(a Account, contributedKRW, priceUSD, fx, krwCash float64) Valuation {
	stock := 0.0
	if a.Shares > 0 {
		stock = float64(a.Shares) * priceUSD * fx
	}
	usdCash := a.CashUSD * fx
	return Valuation{StockKRW: stock, CashKRW: usdCash + krwCash, InvestedKRW: contributedKRW, GainKRW: stock + usdCash - contributedKRW}
}

// DailyRow는 invest_daily 한 줄이다. 계좌 칸은 HasAccount일 때만 있다
// (밀린 날을 한꺼번에 받으면 그날의 보유를 몰라 마지막 날만 채운다).
type DailyRow struct {
	Day    time.Time
	SPX    float64
	SMA185 float64
	Vol    float64
	C1     bool
	C2     bool
	Signal string // 그날 종가로 판정했다면 살 종목(TQQQ·QQQM)

	HasAccount  bool
	Symbol      string
	Shares      int
	CloseUSD    float64
	FX          float64
	StockKRW    int64
	CashKRW     int64
	InvestedKRW int64
	GainKRW     int64

	RecordedAt time.Time
}
