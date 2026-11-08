package health

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
)

// 영양 목표(핏두잉 internal/settings의 targets.go·nutrition.go 이식). 근거와 앱 정책은 핏두잉 docs/nutrition-targets.md.
// 서버는 목표를 다시 계산해서 덮어쓰지 않는다. 추천 API로 계산만 해 주고, 적용한 목표(targets)와 날짜별 이력을 저장한다.

// Goal은 목표다.
type Goal string

const (
	GoalLose Goal = "lose"
	GoalKeep Goal = "keep"
	GoalGain Goal = "gain" // 린매스업(작은 잉여)
	GoalBulk Goal = "bulk" // 벌크업(큰 잉여)
)

// ParseGoal은 goal 값을 확인한다.
func ParseGoal(s string) (Goal, error) {
	switch g := Goal(s); g {
	case GoalLose, GoalKeep, GoalGain, GoalBulk:
		return g, nil
	}
	return "", invalid("goal", "lose, keep, gain, bulk 중 하나예요")
}

func (g *Goal) UnmarshalJSON(b []byte) error {
	s, err := enumJSON(b, "lose", "keep", "gain", "bulk")
	*g = Goal(s)
	return err
}

func (g Goal) kcalPerKg() float64 {
	switch g {
	case GoalLose:
		return 25
	case GoalGain, GoalBulk:
		return 35
	}
	return 30
}

func (g Goal) proteinPerKg() float64 {
	switch g {
	case GoalLose:
		return 2.0
	case GoalGain, GoalBulk:
		return 1.8
	}
	return 1.6
}

// round는 여기서 쓰는 양수 값에 대해 Java Math.round와 같다(앱과 같은 결과).
func round(x float64) int { return int(math.Floor(x + 0.5)) }

// MinKcal은 앱이 도와서 정하는 목표의 하한이다(핏두잉 첫 버전부터의 정책. 의학적 안전선이 아니다).
// 자동 추천이 이보다 낮으면 추천하지 않고, PUT 설정도 거절한다. 끼니 기록·조회는 검사하지 않는다.
const MinKcal = 1200

const fatRatio = 0.25

// Targets는 하루 목표다.
type Targets struct {
	Kcal    int `json:"kcal"`
	Carbs   int `json:"carbs"`
	Protein int `json:"protein"`
	Fat     int `json:"fat"`
}

func clampKcal(kcal float64) int { return max(MinKcal, round(kcal)) }

// Recommend는 체중 기반 옛 기본값이다(새 설정의 자리표시 목표, goal·weightKg만 보낸 추천 요청의 답).
// 몸 정보로 계산한 값이 아니다. RecommendEER을 볼 것.
func Recommend(goal Goal, weightKg float64) Targets {
	kcal := clampKcal(float64(round(weightKg*goal.kcalPerKg()/50.0) * 50))
	return MacrosFor(kcal, round(weightKg*goal.proteinPerKg()))
}

// MacrosFor는 지방을 25%로, 탄수화물을 나머지로 채운다.
func MacrosFor(kcal, protein int) Targets {
	fat := round(float64(kcal) * fatRatio / 9)
	carbs := max(0, round((float64(kcal)-float64(protein)*4-float64(fat)*9)/4))
	return Targets{kcal, carbs, protein, fat}
}

// PerMeal은 끼니 수로 나눈 목표다.
func (t Targets) PerMeal(mealCount int) Targets {
	n := float64(max(1, mealCount))
	f := func(v int) int { return round(float64(v) / n) }
	return Targets{f(t.Kcal), f(t.Carbs), f(t.Protein), f(t.Fat)}
}

// ── EER(2025 KDRI 성인 에너지필요추정량) ─────────────────────────────

// FormulaVersion: 2025 KDRI 성인(19세 이상) EER 식과 신체활동 계수.
const FormulaVersion = "kdri2025-eer-adult"

// PolicyVersion: 앱이 고른 시작값(목표 조정률, 탄단지 배분, 50kcal 반올림, 1200kcal 하한).
const PolicyVersion = "fitdoing-2026.10"

// Sex는 성별이다.
type Sex string

const (
	SexMale   Sex = "male"
	SexFemale Sex = "female"
)

func (x *Sex) UnmarshalJSON(b []byte) error {
	s, err := enumJSON(b, "male", "female")
	*x = Sex(s)
	return err
}

// Activity는 KDRI 신체활동단계다. 값은 정확히 맞아야 한다(대소문자 구분).
type Activity string

const (
	ActivitySedentary  Activity = "sedentary"
	ActivityLow        Activity = "low"
	ActivityActive     Activity = "active"
	ActivityVeryActive Activity = "veryActive"
)

// ParseActivity는 활동 단계를 확인한다.
func ParseActivity(s string) (Activity, bool) {
	switch a := Activity(s); a {
	case ActivitySedentary, ActivityLow, ActivityActive, ActivityVeryActive:
		return a, true
	}
	return "", false
}

func (a *Activity) UnmarshalJSON(b []byte) error {
	var s *string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	if s == nil {
		*a = ""
		return nil
	}
	v, ok := ParseActivity(*s)
	if !ok {
		return fmt.Errorf("invalid activity %q", *s)
	}
	*a = v
	return nil
}

// paCoef는 2025 KDRI 표 2(성인)다.
var paCoef = map[Sex]map[Activity]float64{
	SexMale:   {ActivitySedentary: 1.0, ActivityLow: 1.11, ActivityActive: 1.25, ActivityVeryActive: 1.48},
	SexFemale: {ActivitySedentary: 1.0, ActivityLow: 1.12, ActivityActive: 1.27, ActivityVeryActive: 1.45},
}

// 성인 식은 19세부터. 입력은 AgeMin~AgeMax를 받는다.
const (
	AdultAge = 19
	AgeMin   = 10
	AgeMax   = 100
)

// Body는 EER 입력이다.
type Body struct {
	Sex      Sex
	Age      int
	Activity Activity
	HeightCm float64
	WeightKg float64
}

// EER은 성인 에너지필요추정량(kcal/일, 반올림 전)이다. 키는 cm를 m로 바꿔 넣는다.
func EER(b Body) float64 {
	m := b.HeightCm / 100
	pa := paCoef[b.Sex][b.Activity]
	age := float64(b.Age)
	if b.Sex == SexMale {
		return 662 - 9.53*age + pa*(15.91*b.WeightKg+539.6*m)
	}
	return 354 - 6.91*age + pa*(9.36*b.WeightKg+726*m)
}

// goalAdjustStart는 목표별 시작 조정률(%)이다. 앱 정책이지 KDRI 값이 아니다.
func goalAdjustStart(g Goal) float64 {
	switch g {
	case GoalLose:
		return -20
	case GoalGain:
		return 10
	case GoalBulk:
		return 20
	}
	return 0
}

// Recommendation은 EER 추천 응답이다. 유지 추정치와 목표를 나누어 주고, 배분은 모드의 시작값을 쓴다.
type Recommendation struct {
	Targets
	Maintenance   float64 `json:"maintenanceKcal"`
	AdjustPct     float64 `json:"goalAdjustPct"`
	Mode          string  `json:"mode"`
	Formula       string  `json:"formula"`
	Policy        string  `json:"policy"`
	ProteinPerKg  float64 `json:"proteinPerKg"`
	PercentCarbs  float64 `json:"carbsPct"`
	PercentProt   float64 `json:"proteinPct"`
	PercentFat    float64 `json:"fatPct"`
	RoundedToKcal int     `json:"roundedTo"`
}

// RecommendEER: 유지량 × (1 + 조정률)을 50kcal 단위로 한 번 반올림하고, 그 kcal로 나눈다.
// balanced: 칼로리의 55/20/25 %. training: 단백질 = 체중 × g/kg, 지방 25 %, 탄수화물은 나머지.
// g은 마지막에만 반올림한다. 나머지가 음수이거나 MinKcal 미만이면 올리지 않고 에러다.
func RecommendEER(goal Goal, b Body, mode string) (Recommendation, error) {
	if b.Age < AdultAge {
		return Recommendation{}, badRequest("ADULT_ONLY", "일반 자동 계산은 19세 이상 성인만 쓸 수 있어요")
	}
	maint := EER(b)
	adj := goalAdjustStart(goal)
	kcal := float64(round(maint*(1+adj/100)/50) * 50)
	if kcal < MinKcal {
		return Recommendation{}, badRequest("TARGET_BELOW_MINIMUM",
			fmt.Sprintf("자동 추천 결과가 앱의 목표 범위(%dkcal 이상)보다 낮아요. 조정률을 줄이거나 전문가와 목표를 정하세요", MinKcal))
	}
	var c, p, f float64
	switch mode {
	case "", "balanced":
		mode = "balanced"
		c, p, f = kcal*0.55/4, kcal*0.20/4, kcal*0.25/9
	case "training":
		p = b.WeightKg * goal.proteinPerKg()
		f = kcal * 0.25 / 9
		c = (kcal - p*4 - f*9) / 4
		if c < 0 {
			return Recommendation{}, badRequest("NEGATIVE_REMAINDER", "남는 칼로리가 모자라 탄수화물이 음수가 돼요")
		}
	default:
		return Recommendation{}, invalid("mode", "balanced, training 중 하나예요")
	}
	return Recommendation{
		Targets:     Targets{Kcal: int(kcal), Carbs: round(c), Protein: round(p), Fat: round(f)},
		Maintenance: maint, AdjustPct: adj, Mode: mode, Formula: FormulaVersion, Policy: PolicyVersion,
		ProteinPerKg: p / b.WeightKg, PercentCarbs: c * 400 / kcal, PercentProt: p * 400 / kcal, PercentFat: f * 900 / kcal,
		RoundedToKcal: 50,
	}, nil
}

// ── 날짜별 목표 ───────────────────────────────────────────────────────

// TargetEntry는 from 날짜부터 쓰는 목표다.
type TargetEntry struct {
	From Date `json:"from"`
	Targets
}

// HistoryStart는 첫 변경 이전 모든 날짜를 덮는다.
const HistoryStart Date = "2000-01-01"

const historyMax = 400

// TargetsOn은 d에 쓰던 목표다. d 이전 항목이 없으면 cur.
func TargetsOn(h []TargetEntry, cur Targets, d Date) Targets {
	var hit *TargetEntry
	for i := range h {
		if h[i].From <= d && (hit == nil || h[i].From > hit.From) {
			hit = &h[i]
		}
	}
	if hit == nil {
		return cur
	}
	return hit.Targets
}

// recordTargets는 prev → next 변경을 today부터로 더한다(핏두잉 앱의 recordTargets와 같다).
// 첫 변경이면 prev를 HistoryStart부터로 남겨서 지난 날이 그때 목표를 유지한다.
func recordTargets(h []TargetEntry, prev, next Targets, today Date) []TargetEntry {
	if prev == next {
		return h
	}
	base := h
	if len(base) == 0 {
		base = []TargetEntry{{From: HistoryStart, Targets: prev}}
	}
	out := make([]TargetEntry, 0, len(base)+1)
	for _, e := range base {
		if e.From < today {
			out = append(out, e)
		}
	}
	out = append(out, TargetEntry{From: today, Targets: next})
	sort.Slice(out, func(i, j int) bool { return out[i].From < out[j].From })
	if len(out) > historyMax {
		out = out[len(out)-historyMax:]
	}
	return out
}

func validateHistory(h []TargetEntry) error {
	if len(h) > historyMax {
		return invalid("targetHistory", "너무 많아요")
	}
	seen := map[Date]bool{}
	for _, e := range h {
		if _, err := ParseDate(string(e.From)); err != nil {
			return invalid("targetHistory.from", "YYYY-MM-DD 형식이어야 해요")
		}
		if seen[e.From] {
			return invalid("targetHistory.from", "겹치면 안 돼요")
		}
		seen[e.From] = true
		if e.Kcal < 0 || e.Carbs < 0 || e.Protein < 0 || e.Fat < 0 {
			return invalid("targetHistory", "0 이상이어야 해요")
		}
	}
	return nil
}

// enumJSON은 JSON 문자열을 소문자로 바꿔 허용 값과 맞춘다. null은 ""(필수 검사에서 걸린다).
func enumJSON(b []byte, allowed ...string) (string, error) {
	var s *string
	if err := json.Unmarshal(b, &s); err != nil {
		return "", err
	}
	if s == nil {
		return "", nil
	}
	v := lower(*s)
	for _, a := range allowed {
		if a == v {
			return v, nil
		}
	}
	return "", fmt.Errorf("invalid value %q", *s)
}

func lower(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + 32
		}
	}
	return string(b)
}
