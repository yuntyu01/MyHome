package invest

import (
	"fmt"
	"sort"
	"time"
)

// MaxSwitches12M은 12개월 전환 상한이다. 실측 최대 4 — 여유 2회.
const MaxSwitches12M = 6

// PreflightCtx는 주문 직전 검증 입력이다. 표본 수와 전환 횟수는 원자료에서 실제로 센 값이다.
type PreflightCtx struct {
	ExecDate       time.Time // 오늘(실행일)
	SignalDate     time.Time
	SPX, Vol       float64
	SMASampleCount int
	VolSampleCount int
	ApplyYM        string
	InApplyMonth   bool
	ExecDay        time.Time // 적용월 첫 거래일
	LateDays       int
	Switches12M    int
	Target         string // 이번 달에 실제로 살 종목 (신호 종목이 아닐 수 있다 — 확정된 이탈)
	Current        string // 보유 종목. 없으면 ""
	SellLegs       []string
}

// Preflight는 주문 직전 결정론적 검증이다. 월간 판정 게이트가 아니다(§6).
//
// 여기 있는 검사는 전부 실측값을 비교해야 한다. 상수를 같은 상수와 비교하거나 항상 0인 식을
// 넣으면 항목만 늘고 검사는 없다 — 과거에 표본 수 2개와 전환 횟수가 그렇게 죽어 있었다.
func Preflight(c PreflightCtx) []string {
	var f []string
	// 신선도는 실행일 기준으로 잰다. 오늘 기준으로 재면 그 달 안에서 늦게 사는 것만으로
	// 신호가 낡았다고 판정해 지연이 곧 미매수가 된다. 판정일→첫 거래일은 실측 1~4일이다
	gap := int(c.ExecDay.Sub(c.SignalDate).Hours() / 24)
	if gap > 5 {
		f = append(f, fmt.Sprintf("신호 데이터 %d일 낡음 — 판정일 %s vs 실행일 %s. 적용월과 판정월이 어긋난다",
			gap, FormatDay(c.SignalDate), FormatDay(c.ExecDay)))
	}
	if !(c.SPX > 1000 && c.SPX < 50000) {
		f = append(f, fmt.Sprintf("SPX 이상치 %v", c.SPX))
	}
	if !(c.Vol > 0 && c.Vol < 3.0) {
		f = append(f, fmt.Sprintf("변동성 이상치 %v", c.Vol))
	}
	if c.SMASampleCount != SMAPeriod {
		f = append(f, fmt.Sprintf("SMA 표본 %d ≠ %d", c.SMASampleCount, SMAPeriod))
	}
	if c.VolSampleCount != VolWindow {
		f = append(f, fmt.Sprintf("변동성 표본 %d ≠ %d", c.VolSampleCount, VolWindow))
	}
	// 시점 — 적용월을 벗어나면 중단. 첫 거래일이 아닌 것은 실패가 아니라 지연이다
	// (늦은 매수는 규칙 위반이나 미매수보다 낫다. 막으면 그 달이 통째로 사라진다)
	if !c.InApplyMonth {
		f = append(f, fmt.Sprintf("실행일이 적용월(%s) 밖", c.ApplyYM))
	}
	if !c.SignalDate.Before(c.ExecDate) {
		f = append(f, "룩어헤드 의심 — 판정일이 실행일 이후")
	}
	if c.Switches12M > MaxSwitches12M {
		f = append(f, fmt.Sprintf("12개월 전환 %d회 — 정상 범위(최대 4) 초과", c.Switches12M))
	}
	// 주문 온전성 — 매도 레그가 실제 보유와 맞는지
	var phantom []string
	for _, s := range c.SellLegs {
		if s != c.Current || c.Current == "" {
			phantom = append(phantom, s)
		}
	}
	if len(phantom) > 0 {
		sort.Strings(phantom)
		f = append(f, fmt.Sprintf("보유하지 않은 종목 매도 시도: %v", phantom))
	}
	if len(c.SellLegs) > 0 && c.Current == c.Target {
		f = append(f, fmt.Sprintf("불필요한 전환 — %s 보유·목표 동일한데 매도 레그 존재", c.Current))
	}
	return f
}
