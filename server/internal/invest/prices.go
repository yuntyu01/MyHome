package invest

import (
	"math"
	"sort"
	"time"
)

// PriceRow는 마스터 1행(일별 원종가)이다. 값이 없으면 NaN.
type PriceRow struct {
	Day       time.Time
	SPX, NDX  float64
	QQQ       float64
	QQQM      float64
	QLD       float64
	TQQQ      float64
	USDKRW    float64 // 하나은행 매매기준율 — 백테스트용, 2026-07-31 동결
	USDKRWMkt float64 // 시장 환율 — 평가 전용
}

// Columns는 refresh가 받는 종목 열 이름과 야후 티커다 (TQ run.py TICKERS).
var Columns = []struct{ Col, Yahoo string }{
	{"SPX", "^GSPC"}, {"NDX", "^NDX"}, {"QQQ", "QQQ"}, {"QQQM", "QQQM"}, {"QLD", "QLD"}, {"TQQQ", "TQQQ"},
}

// FXYahoo는 시장 환율 티커다. 열은 USDKRW_MKT.
const FXYahoo = "KRW=X"

// Get은 열 이름으로 값을 꺼낸다. 모르는 열이면 NaN.
func (r PriceRow) Get(col string) float64 {
	switch col {
	case "SPX":
		return r.SPX
	case "NDX":
		return r.NDX
	case "QQQ":
		return r.QQQ
	case "QQQM":
		return r.QQQM
	case "QLD":
		return r.QLD
	case "TQQQ":
		return r.TQQQ
	case "USDKRW":
		return r.USDKRW
	case "USDKRW_MKT":
		return r.USDKRWMkt
	}
	return math.NaN()
}

// Set은 열 이름으로 값을 넣는다.
func (r *PriceRow) Set(col string, v float64) {
	switch col {
	case "SPX":
		r.SPX = v
	case "NDX":
		r.NDX = v
	case "QQQ":
		r.QQQ = v
	case "QQQM":
		r.QQQM = v
	case "QLD":
		r.QLD = v
	case "TQQQ":
		r.TQQQ = v
	case "USDKRW":
		r.USDKRW = v
	case "USDKRW_MKT":
		r.USDKRWMkt = v
	}
}

// EmptyRow는 전부 결측인 행이다.
func EmptyRow(day time.Time) PriceRow {
	n := math.NaN()
	return PriceRow{Day: day, SPX: n, NDX: n, QQQ: n, QQQM: n, QLD: n, TQQQ: n, USDKRW: n, USDKRWMkt: n}
}

// Prices는 날짜 오름차순 마스터다.
type Prices []PriceRow

// Sort는 날짜순으로 정렬한다.
func (p Prices) Sort() { sort.Slice(p, func(i, j int) bool { return p[i].Day.Before(p[j].Day) }) }

// SignalInputs는 신호용 SPX·NDX만 꺼낸다. 환율·레버리지 종목은 의도적으로 버린다.
func (p Prices) SignalInputs() SignalInputs {
	in := SignalInputs{Days: make([]time.Time, len(p)), SPX: make([]float64, len(p)), NDX: make([]float64, len(p))}
	for i, r := range p {
		in.Days[i], in.SPX[i], in.NDX[i] = r.Day, r.SPX, r.NDX
	}
	return in
}

// AsOf는 day 이전(포함) 마지막 행이다.
func (p Prices) AsOf(day time.Time) (PriceRow, bool) {
	i := sort.Search(len(p), func(i int) bool { return p[i].Day.After(day) })
	if i == 0 {
		return PriceRow{}, false
	}
	return p[i-1], true
}

// Last는 마지막 행이다.
func (p Prices) Last() (PriceRow, bool) {
	if len(p) == 0 {
		return PriceRow{}, false
	}
	return p[len(p)-1], true
}

// ValuationFX는 평가에 쓸 환율과 그 기준일이다.
//
// 🔴 마지막 행만 보면 안 된다. KRW=X는 미국 종가보다 하루 늦게 붙어서 마스터 마지막 행의
// USDKRW_MKT는 갱신 직후 거의 항상 결측이다. 가격은 마지막 행이 정답이지만 환율은 마지막
// 유효값이 정답이다. 구할 수 없으면 (0, zero) — 조용히 1을 넣지 않는다.
func (p Prices) ValuationFX() (float64, time.Time) {
	for _, col := range []string{"USDKRW_MKT", "USDKRW"} {
		for i := len(p) - 1; i >= 0; i-- {
			if v := p[i].Get(col); !math.IsNaN(v) {
				return v, p[i].Day
			}
		}
	}
	return 0, time.Time{}
}

// TradeFX는 (환율, 추정 여부)다. 지정값이 있으면 그것이 정답, 없으면 그날 행의 시장 환율로 추정한다.
func TradeFX(given float64, row PriceRow) (float64, bool) {
	if given > 0 {
		return given, false
	}
	for _, col := range []string{"USDKRW_MKT", "USDKRW"} {
		if v := row.Get(col); !math.IsNaN(v) {
			return v, true
		}
	}
	return 0, true
}
