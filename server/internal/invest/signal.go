// Package invest는 자동매매(TQ 이식)의 순수 도메인이다. 파일·네트워크·DB I/O가 없다.
//
// 신호(SPX185 + 변동성30), 미국 증시 거래일 달력, 역할→종목 매핑, 계좌 산술, preflight가 여기 있다.
// 산술은 TQ(Python)와 같은 float64다 — 336행 판정 대조와 장부 대조가 비트 단위로 같게 하려고
// (docs/DECISIONS.md). 원 단위 정수 규칙은 가계부 거래에만 적용된다.
package invest

import (
	"errors"
	"fmt"
	"math"
	"time"
)

// 신호 (TQ strategy/signal.py)
//
//	조건 ①  SPX_close > SMA(SPX, 185)
//	조건 ②  realized_vol(NDX, 21) < 0.30
//	① AND ② → 다음 달 RiskOn 100% / 그 외 → 다음 달 RiskOff 100%
//
// 🔴 환율을 조인하지 말 것. 신호는 ^GSPC·^NDX 달러 종가만으로 정의된다. v1 픽스처는 USDKRW
// 결측 행을 드롭했다가 2017-10-09(추석, 미국 증시는 개장)를 잃었고 이후 9개월의 SMA185가
// 최대 1.36포인트 어긋났다. 그래서 신호 입력 타입에는 SPX·NDX 말고는 들어갈 자리가 없다.
//
// 🔴 전체 이력으로 계산할 것. SMA185를 잘린 시리즈에 걸면 앞 8개월이 결측이 되고 1998-11이
// RiskOff로 잘못 판정된다. rolling은 전체 이력에 걸고 구간 필터는 그 뒤에 적용한다.

// 전략 파라미터 — 이것이 단일 출처다. 바꾸면 픽스처 336행이 터진다. 그것이 잠금이다.
const (
	SMAPeriod    = 185
	VolWindow    = 21 // 수익률 개수. 종가는 22개 필요
	VolThreshold = 0.30
	TradingDays  = 252
	RiskOn       = "TQQQ"
	RiskOff      = "QQQ"
	// SignalStart는 판정 로그를 자르는 시작월이다. rolling 계산 이후에 적용된다.
	SignalStart = "1998-08"
)

// SignalInputs는 신호 계산용 SPX·NDX 일간 종가다. 같은 거래일 축을 공유한다.
type SignalInputs struct {
	Days []time.Time // 오름차순, 중복 없음
	SPX  []float64
	NDX  []float64
}

// Validate는 입력이 판정에 쓸 수 있는 상태인지 본다. 조용히 진행하는 경로를 만들지 않는다.
func (in SignalInputs) Validate() error {
	n := len(in.Days)
	if len(in.SPX) != n || len(in.NDX) != n {
		return fmt.Errorf("SPX·NDX 거래일이 다르다 (날짜 %d, SPX %d, NDX %d). 한쪽에만 있는 날짜를 드롭하지 말고 원인을 확인할 것", n, len(in.SPX), len(in.NDX))
	}
	for i := 0; i < n; i++ {
		if i > 0 && !in.Days[i].After(in.Days[i-1]) {
			return fmt.Errorf("인덱스가 오름차순이 아니거나 중복 날짜: %s", FormatDay(in.Days[i]))
		}
		if math.IsNaN(in.SPX[i]) || math.IsNaN(in.NDX[i]) {
			return fmt.Errorf("SPX·NDX 종가에 결측: %s. 드롭하거나 채우지 말고 원천 데이터를 고칠 것", FormatDay(in.Days[i]))
		}
	}
	return nil
}

// SMA는 단순이동평균이다. 전체 이력에 걸어야 한다(구간 자르기는 이 뒤에). 워밍업 구간은 NaN.
func SMA(close []float64, period int) []float64 {
	out := make([]float64, len(close))
	for i := range out {
		if i+1 < period {
			out[i] = math.NaN()
			continue
		}
		sum := 0.0
		for _, v := range close[i+1-period : i+1] {
			sum += v
		}
		out[i] = sum / float64(period)
	}
	return out
}

// PctChange는 일간 수익률이다. 첫 행은 NaN (pandas pct_change(fill_method=None)).
func PctChange(close []float64) []float64 {
	out := make([]float64, len(close))
	for i := range out {
		if i == 0 {
			out[i] = math.NaN()
			continue
		}
		out[i] = close[i]/close[i-1] - 1
	}
	return out
}

// RealizedVol은 연율화 실현변동성이다. window는 수익률 개수다(종가는 window+1개 필요).
//
// ddof=1(표본표준편차) 필수 — ddof=0을 쓰면 값이 달라진다.
func RealizedVol(close []float64, window, tradingDays int) []float64 {
	r := PctChange(close)
	out := make([]float64, len(close))
	for i := range out {
		out[i] = math.NaN()
		if i+1 < window {
			continue
		}
		w := r[i+1-window : i+1]
		ok := true
		mean := 0.0
		for _, v := range w {
			if math.IsNaN(v) {
				ok = false
				break
			}
			mean += v
		}
		if !ok {
			continue
		}
		mean /= float64(window)
		ss := 0.0
		for _, v := range w {
			ss += (v - mean) * (v - mean)
		}
		out[i] = math.Sqrt(ss/float64(window-1)) * math.Sqrt(float64(tradingDays))
	}
	return out
}

// CondTrend는 조건 ①: SPX > SMA185. strict > — 같으면 탈락.
func CondTrend(spx, sma float64) bool { return spx > sma }

// CondVol은 조건 ②: 실현변동성 < 임계. strict < — 같으면 탈락.
func CondVol(vol float64) bool { return vol < VolThreshold }

// ResolvePosition은 ① AND ② → RiskOn, 그 외 → RiskOff.
func ResolvePosition(c1, c2 bool) string {
	if c1 && c2 {
		return RiskOn
	}
	return RiskOff
}

// NextYM은 다음 달 YYYY-MM.
func NextYM(ym string) string {
	t, err := time.Parse("2006-01", ym)
	if err != nil {
		panic(fmt.Sprintf("YYYY-MM 형식이 아니다: %q", ym))
	}
	return YM(t.AddDate(0, 1, 0))
}

// SignalRow는 월말 판정 1건이다. ApplyYM이 이 신호가 적용될 달이다 (룩어헤드 방지).
type SignalRow struct {
	YM      string    // 판정월
	Day     time.Time // 판정일 (월 마지막 실제 거래일)
	SPX     float64
	SMA185  float64
	C1      bool
	Vol     float64
	C2      bool
	Pos     string // 역할 라벨 (RiskOn·RiskOff)
	ApplyYM string // 적용월
}

// MonthlySignals는 월 마지막 거래일 종가로 판정한 전체 신호 로그다.
//
// in은 전체 이력이어야 한다(자르지 말 것). start(YYYY-MM)는 rolling 계산 이후에 적용된다.
func MonthlySignals(in SignalInputs, start string) ([]SignalRow, error) {
	if err := in.Validate(); err != nil {
		return nil, err
	}
	sma := SMA(in.SPX, SMAPeriod)
	vol := RealizedVol(in.NDX, VolWindow, TradingDays)

	var startDay time.Time
	if start != "" {
		t, err := time.Parse("2006-01", start)
		if err != nil {
			return nil, fmt.Errorf("start는 YYYY-MM: %q", start)
		}
		startDay = t
	}
	var out []SignalRow
	var bad []string
	n := len(in.Days)
	for i := 0; i < n; i++ {
		// 월 마지막 거래일 (달력 말일이 아니라 실제 데이터에 있는 마지막 행)
		if i+1 < n && in.Days[i+1].Year() == in.Days[i].Year() && in.Days[i+1].Month() == in.Days[i].Month() {
			continue
		}
		d := in.Days[i]
		if !startDay.IsZero() && d.Before(startDay) {
			continue
		}
		if math.IsNaN(sma[i]) || math.IsNaN(vol[i]) {
			bad = append(bad, FormatDay(d))
			continue
		}
		c1, c2 := CondTrend(in.SPX[i], sma[i]), CondVol(vol[i])
		ym := YM(d)
		out = append(out, SignalRow{
			YM: ym, Day: d, SPX: in.SPX[i], SMA185: sma[i], C1: c1,
			Vol: vol[i], C2: c2, Pos: ResolvePosition(c1, c2), ApplyYM: NextYM(ym),
		})
	}
	if len(bad) > 0 {
		if len(bad) > 5 {
			bad = bad[:5]
		}
		return nil, fmt.Errorf("SMA185·vol에 결측이 있다: %v. 전체 이력을 넣었는지 확인할 것 (잘린 시리즈에 rolling을 걸면 워밍업 구간이 빈다)", bad)
	}
	return out, nil
}

// LatestDecision은 가장 최근 월말 판정 1건이다.
//
// 호출부는 이 결과를 그대로 실행하지 말고 preflight를 통과시킬 것. ApplyYM이 실행 대상 달이다.
func LatestDecision(in SignalInputs, start string) (SignalRow, error) {
	log, err := MonthlySignals(in, start)
	if err != nil {
		return SignalRow{}, err
	}
	if len(log) == 0 {
		return SignalRow{}, errors.New("판정 가능한 월이 없다")
	}
	return log[len(log)-1], nil
}

// CompletedSignals는 진행 중인 달을 떨군 신호 로그다.
//
// 주간 갱신 뒤에는 마지막 행이 미완결 월이다. 그 행은 판정이 아니므로 검산·통계·전환
// 횟수에 세지 않는다. 판정 자체는 judge의 월말 게이트가 지킨다.
func CompletedSignals(log []SignalRow) []SignalRow {
	if len(log) == 0 {
		return log
	}
	last := log[len(log)-1]
	if !last.Day.Equal(LastTradingDay(last.Day.Year(), last.Day.Month())) {
		return log[:len(log)-1]
	}
	return log
}

// Switches12M은 최근 12개 판정 중 직전 판정과 포지션이 달라진 횟수다.
// 로그 첫 행은 직전이 없으므로 전환으로 센다 (pandas shift 비교와 같다).
func Switches12M(log []SignalRow) int {
	n := 0
	from := len(log) - 12
	if from < 0 {
		from = 0
	}
	for i := from; i < len(log); i++ {
		if i == 0 || log[i].Pos != log[i-1].Pos {
			n++
		}
	}
	return n
}

// SampleCounts는 판정일 sd까지의 SMA 표본 수와 변동성 수익률 표본 수를 원자료에서 실제로 센다.
func SampleCounts(in SignalInputs, sd time.Time) (smaN, volN int) {
	k := 0
	for k < len(in.Days) && !in.Days[k].After(sd) {
		k++
	}
	smaN = k
	if smaN > SMAPeriod {
		smaN = SMAPeriod
	}
	volN = k - 1
	if volN < 0 {
		volN = 0
	}
	if volN > VolWindow {
		volN = VolWindow
	}
	return smaN, volN
}
