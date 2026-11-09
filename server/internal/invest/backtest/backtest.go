// Package backtest는 이 전략의 고정 백테스트 표다 (TQ 뷰어 「백테스트 결과」 이식).
//
// 원천은 TQ docs/전략_SPX185_백테스트결과_v2.xlsx 하나다(backtest.json, 수수료 0.10% 시트).
// 🔴 새로 계산하지 않는다. 앱 안 백테스트·파라미터 튜닝은 금지다(TQ claude.md §2) — 이것은 고정된 과거 표다.
// 그래도 비교표라서 TQ와 같은 세 가지를 지킨다.
//
//	① 하위 화면에 둔다 - 여는 것은 의식적 행위
//	② 최악·하위10%를 앞에 둔다 - 이 전략의 근거가 수익이 아니라 하방이다
//	③ 읽는 법(Notes)을 표와 같은 화면에 둔다 - 평균만 보고 판단하는 것을 막는다
//
// 왜 있는가: 힘든 구간에서 "예상 범위 안이다"를 확인하는 근거다. 장기 보유는 이 숫자를 믿는 데서 나온다.
package backtest

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"sync"
)

//go:embed backtest.json
var raw []byte

// Row는 한 전략의 한 기간 결과다. 금액은 억원, 비율은 %다.
type Row struct {
	Name         string  `json:"name"`
	Star         bool    `json:"star"` // 이 전략(★)
	Principal    float64 `json:"principal"`
	Worst        float64 `json:"worst"`
	Q10          float64 `json:"q10"` // 분위수 하위 10%
	Q30          float64 `json:"q30"`
	Median       float64 `json:"median"`
	Q70          float64 `json:"q70"` // 상위 30%
	Q90          float64 `json:"q90"` // 상위 10%
	Mean         float64 `json:"mean"`
	Best         float64 `json:"best"`
	CVaR10       float64 `json:"cvar10"` // 하위 10% 윈도우의 평균(분위수가 아니다)
	Multiple     float64 `json:"multiple"`
	LossPct      float64 `json:"loss_pct"` // 원금보다 적게 끝난 윈도우 비율
	Windows      int     `json:"windows"`
	MDDMedianPct float64 `json:"mdd_median_pct"` // 음수
	MDDWorstPct  float64 `json:"mdd_worst_pct"`  // 음수
}

// Horizon은 적립 기간 하나(5년~10년)의 두 표본이다.
type Horizon struct {
	Label    string `json:"label"`
	ExDotcom []Row  `json:"ex_dotcom"` // 닷컴 버블(1998~2002)을 뺀 표본
	All      []Row  `json:"all"`       // 전체 표본 — 공식 수치 기준
}

// Table은 표 전체다.
type Table struct {
	Source   string    `json:"source"`
	Period   string    `json:"period"`
	FeeRate  float64   `json:"fee_rate"`
	Unit     string    `json:"unit"`
	Horizons []Horizon `json:"horizons"`
}

var (
	once   sync.Once
	cached Table
	errLd  error
)

// Load는 내장된 표다.
func Load() (Table, error) {
	once.Do(func() {
		errLd = json.Unmarshal(raw, &cached)
		if errLd == nil && len(cached.Horizons) == 0 {
			errLd = fmt.Errorf("백테스트 표가 비었다")
		}
	})
	return cached, errLd
}

// Find는 그 기간·표본에서 이름으로 행을 찾는다(★는 Star로).
func (t Table) Find(label string, all bool, name string) (Row, bool) {
	for _, h := range t.Horizons {
		if h.Label != label {
			continue
		}
		rows := h.ExDotcom
		if all {
			rows = h.All
		}
		for _, r := range rows {
			if (name == "★" && r.Star) || r.Name == name {
				return r, true
			}
		}
	}
	return Row{}, false
}

// Column은 표의 한 열이다. HigherIsBetter가 아니면 0에 가까울수록(작을수록) 좋다.
type Column struct {
	Key            string `json:"key"`
	Label          string `json:"label"`
	Percent        bool   `json:"percent"`
	HigherIsBetter bool   `json:"higher_is_better"`
}

// Columns는 보여주는 순서다 — 최악·하위가 앞이다(②).
var Columns = []Column{
	{"worst", "최악", false, true},
	{"q10", "하위10%", false, true},
	{"q30", "하위30%", false, true},
	{"median", "중앙", false, true},
	{"q70", "상위30%", false, true},
	{"q90", "상위10%", false, true},
	{"loss_pct", "손실%", true, false},
	{"mdd_median_pct", "MDD중앙", true, true}, // 음수라 큰 쪽이 0에 가깝다
	{"mdd_worst_pct", "MDD최악", true, true},
}

// Value는 열 값이다.
func (r Row) Value(key string) float64 {
	switch key {
	case "worst":
		return r.Worst
	case "q10":
		return r.Q10
	case "q30":
		return r.Q30
	case "median":
		return r.Median
	case "q70":
		return r.Q70
	case "q90":
		return r.Q90
	case "loss_pct":
		return r.LossPct
	case "mdd_median_pct":
		return r.MDDMedianPct
	case "mdd_worst_pct":
		return r.MDDWorstPct
	}
	return math.NaN()
}

// Tops는 행마다 그 열에서 1등인 열 키들이다. ★가 모든 칸에서 1등이 아니라는 사실이 그대로 보여야 한다.
func Tops(rows []Row) [][]string {
	best := map[string]float64{}
	for _, c := range Columns {
		for i, r := range rows {
			v := r.Value(c.Key)
			if i == 0 || (c.HigherIsBetter && v > best[c.Key]) || (!c.HigherIsBetter && v < best[c.Key]) {
				best[c.Key] = v
			}
		}
	}
	out := make([][]string, len(rows))
	for i, r := range rows {
		for _, c := range Columns {
			if r.Value(c.Key) == best[c.Key] {
				out[i] = append(out[i], c.Key)
			}
		}
	}
	return out
}

// Notes는 「읽기 전에」다. 🔴 TQ §13의 문장을 그대로 옮긴다 — 톤이 순해지면 그 조항이 무의미해진다.
// 숫자는 표에서 뽑는다(문장에 박아 두면 표만 바뀌고 문장은 옛말을 한다).
func Notes(t Table) []string {
	star, _ := t.Find("7년", true, "★")
	pct := func(v float64) string { return strings.Replace(fmt.Sprintf("%.0f%%", v), "-", "−", 1) }
	return []string{
		fmt.Sprintf("단위는 억원이고, 월 적립식으로 해당 기간을 채웠을 때의 기말 금액이다. 수수료 %.2f%%·양도세 22%%를 반영한 값이다.", t.FeeRate*100),
		"최악·하위10%를 먼저 보라. 이 전략의 근거는 수익이 아니라 하방이다. 평균만 보고 판단하는 것이 가장 흔한 오독이다.",
		fmt.Sprintf("MDD는 계좌가 고점 대비 빠졌던 최대 폭이다. 중앙 %s·최악 %s를 실제로 겪는다는 뜻이다(7년 전체 표본). 그 구간을 못 견디면 이 표의 숫자는 내 것이 아니다.", pct(star.MDDMedianPct), pct(star.MDDWorstPct)),
		"하방 우위는 레버리지 상품군 안에서만 성립한다. 최악 구간에서는 QQQ 단순 보유가 이 전략보다 낫다. \"QQQ보다 안전\"이 아니라 \"TQQQ·QLD를 그냥 보유하는 것보다 안전\"이다.",
		"기본 화면은 닷컴 제외 표본이다. 이 전략이 겪은 최악의 구간을 뺀 값이라 실제보다 좋아 보인다. 전체 표본으로 바꿔서 같이 볼 것 — 그쪽이 공식 수치 기준이다.",
		fmt.Sprintf("표본 절단 때문에 특정 구간에서 손실확률이 0으로 보일 수 있다. 그것은 전략의 성질이 아니다. 전체 표본 7년 손실확률은 %.1f%%다.", star.LossPct),
		"규칙을 100% 지켰을 때의 값이다. 한 달만 건너뛰어도 결과는 달라진다.",
		"정액 적립 전제다. 매달 넣는 금액이 들쭉날쭉하면 이 숫자는 내 계좌를 설명하지 않는다. 힘든 구간에서 \"예상 범위 안이다\"의 근거로 쓰려면 입금을 일정하게 유지할 것.",
		"과거 성과는 미래 수익을 보장하지 않는다.",
	}
}

// KeyNames는 맨 위 요약에서 ★와 나란히 놓는 비교 대상이다 — 붙잡는 논리가 "TQQQ·QLD를 그냥 보유하는 것보다 안전"이라서.
var KeyNames = []string{"★", "TQQQ 보유", "QLD 보유", "QQQ 보유"}
