package health

import (
	"encoding/json"
	"math"
	"testing"
)

// 핏두잉 internal/settings(nutrition_test.go, targets_test.go)와 stats 계산 이식. DB 없이 돈다.

// 기댓값은 따로 계산했고(정확한 분수), 처음 두 개는 2025 KDRI(1권 에너지 장)의 산출 예시와 같다.
func TestEERMatchesKDRIAndIndependentValues(t *testing.T) {
	cases := []struct {
		b    Body
		want float64
	}{
		{Body{SexMale, 24, ActivityLow, 174.8, 69.1}, 2700.568},
		{Body{SexFemale, 24, ActivityLow, 161.5, 55.9}, 2087.0},
		{Body{SexMale, 40, ActivitySedentary, 172, 70}, 2322.612},
		{Body{SexMale, 40, ActivityLow, 172, 70}, 2547.21132},
		{Body{SexMale, 40, ActivityActive, 172, 70}, 2833.065},
		{Body{SexMale, 40, ActivityVeryActive, 172, 70}, 3302.68176},
		{Body{SexFemale, 28, ActivitySedentary, 162, 55}, 1851.44},
		{Body{SexFemale, 28, ActivityLow, 162, 55}, 2054.3504},
		{Body{SexFemale, 28, ActivityActive, 162, 55}, 2307.9884},
		{Body{SexFemale, 28, ActivityVeryActive, 162, 55}, 2612.354},
		{Body{SexMale, 40, ActivityLow, 172, 70.5}, 2556.04137},
	}
	for _, c := range cases {
		tol := 1e-6
		if c.want == 2087.0 || c.want == 2700.568 {
			tol = 0.5 // KDRI는 kcal 정수로 적는다
		}
		if got := EER(c.b); math.Abs(got-c.want) > tol {
			t.Errorf("%+v: got %.6f want %.6f", c.b, got, c.want)
		}
	}
}

func TestTargetsOnAndRecord(t *testing.T) {
	a := Targets{2100, 283, 112, 58}
	b := Targets{1600, 180, 100, 53}
	h := recordTargets(nil, a, b, "2026-10-08")
	if len(h) != 2 || h[0].From != HistoryStart || h[0].Targets != a || h[1].Targets != b {
		t.Fatalf("history %+v", h)
	}
	for d, want := range map[Date]Targets{"2026-10-07": a, "2026-10-08": b, "2027-01-01": b} {
		if got := TargetsOn(h, b, d); got != want {
			t.Errorf("%s: %+v", d, got)
		}
	}
	if got := recordTargets(h, b, b, "2026-10-09"); len(got) != 2 {
		t.Fatal("같은 목표면 항목을 더하지 않는다")
	}
	if got := TargetsOn(nil, a, "2020-01-01"); got != a {
		t.Fatal("이력이 없으면 지금 목표")
	}
	// 같은 날 다시 바꾸면 그날 항목을 바꾼다
	c := Targets{1800, 200, 120, 60}
	if got := recordTargets(h, b, c, "2026-10-08"); len(got) != 2 || got[1].Targets != c {
		t.Fatalf("same day: %+v", got)
	}
}

func TestOptKeepsAbsentAndNull(t *testing.T) {
	var s struct {
		A Opt[int] `json:"a"`
		B Opt[int] `json:"b"`
		C Opt[int] `json:"c"`
	}
	if err := json.Unmarshal([]byte(`{"a":3,"b":null}`), &s); err != nil {
		t.Fatal(err)
	}
	if !s.A.Set || *s.A.Val != 3 || !s.B.Set || s.B.Val != nil || s.C.Set {
		t.Fatalf("%+v", s)
	}
	b, _ := json.Marshal(s)
	if string(b) != `{"a":3,"b":null,"c":null}` {
		t.Fatalf("marshal %s", b)
	}
}

func TestRecommendEERRoundsOnceAndRefusesBadInput(t *testing.T) {
	man := Body{SexMale, 40, ActivityLow, 172, 70}
	for g, want := range map[Goal]int{GoalLose: 2050, GoalKeep: 2550, GoalGain: 2800, GoalBulk: 3050} {
		r, err := RecommendEER(g, man, "")
		if err != nil || r.Kcal != want {
			t.Errorf("%s: %v %d", g, err, r.Kcal)
		}
	}
	if _, err := RecommendEER(GoalKeep, Body{SexMale, 17, ActivityLow, 172, 70}, ""); err == nil {
		t.Fatal("미성년자를 받음")
	}
	small := Body{SexFemale, 80, ActivitySedentary, 145, 35}
	if _, err := RecommendEER(GoalLose, small, ""); err == nil {
		t.Fatal("1200 미만은 올리지 않고 에러여야 한다")
	}
	if _, err := RecommendEER(GoalKeep, man, "keto"); err == nil {
		t.Fatal("모르는 모드를 받음")
	}
}

func TestRecommendMatchesAppDefaults(t *testing.T) {
	got := Recommend(GoalKeep, 70)
	if got.Kcal != 2100 || got.Protein != 112 {
		t.Fatalf("got %+v", got)
	}
	if got := Recommend(GoalLose, 40).Kcal; got != MinKcal {
		t.Fatalf("하한: got %d", got)
	}
	if got := Recommend(GoalKeep, 70).PerMeal(4).Kcal; got != 525 {
		t.Fatalf("끼니당: got %d", got)
	}
}

func TestRemainingPerMeal(t *testing.T) {
	tg := Targets{2100, 283, 112, 58}
	got := remainingPerMeal(tg, Nutrition{600, 75, 30, 15}, 2)
	if got != (Targets{750, 104, 41, 22}) {
		t.Fatalf("got %+v", got)
	}
	// 넘치면 0, 남은 끼니가 없으면 1로 나눈다
	if got := remainingPerMeal(tg, Nutrition{3000, 0, 0, 0}, 0); got.Kcal != 0 || got.Carbs != 283 {
		t.Fatalf("got %+v", got)
	}
}

func TestMealNutritionAppliesPortionAndSharing(t *testing.T) {
	m := meal{Base: Nutrition{400, 50, 21, 10}, Portion: 1.5}
	if got := m.nutrition(); got != (Nutrition{600, 75, 32, 15}) { // 31.5 → 32 (반올림)
		t.Fatalf("1.5: %+v", got)
	}
	m.SharedMeal = true // 나눠 먹음 = 절반
	if got := m.nutrition(); got != (Nutrition{300, 38, 16, 8}) {
		t.Fatalf("shared: %+v", got)
	}
	if v := m.view(); v.Base != m.Base || v.Items == nil {
		t.Fatalf("view %+v", v)
	}
}

func TestWeekSummaryJudgesEachDayByItsTarget(t *testing.T) {
	n := func(k int) *Nutrition { return &Nutrition{Kcal: k} }
	days := []WeekDay{
		{Index: 0, Date: "2026-09-21", State: "done", Current: n(1600)},
		{Index: 1, Date: "2026-09-22", State: "done", Current: n(1650)},
		{Index: 2, Date: "2026-09-23", State: "missing"},
		{Index: 3, Date: "2026-09-24", State: "today", Current: n(100)},
	}
	target := func(d Date) int {
		if d < "2026-09-22" {
			return 2100
		}
		return 1600
	}
	s := weekSummary(days, target, func(x Nutrition) int { return x.Kcal })
	if s.Completed != 2 || s.Achieved != 1 || s.Furthest == nil || s.Furthest.Index != 0 || s.Furthest.Diff != -500 {
		t.Fatalf("%+v %+v", s, s.Furthest)
	}
	if meanNutrition(nil) != nil || meanNutrition([]Nutrition{{Kcal: 1}, {Kcal: 2}}).Kcal != 2 {
		t.Fatal("mean")
	}
}

func TestDates(t *testing.T) {
	d := Date("2026-09-26") // 토요일
	if d.Weekday0() != 5 || d.Monday() != "2026-09-21" || d.AddDays(2) != "2026-09-28" {
		t.Fatalf("%d %s %s", d.Weekday0(), d.Monday(), d.AddDays(2))
	}
	if Date("2026-09-21").Monday() != "2026-09-21" || Date("2026-09-27").Monday() != "2026-09-21" {
		t.Fatal("월요일·일요일의 주")
	}
	first, last, err := ParseMonth("2026-02")
	if err != nil || first != "2026-02-01" || last != "2026-02-28" {
		t.Fatalf("%s %s %v", first, last, err)
	}
	if _, err := ParseDate("2026-13-01"); err == nil {
		t.Fatal("잘못된 날짜를 받음")
	}
}

func TestSettingsValidate(t *testing.T) {
	d := Defaults()
	if err := d.Validate(); err != nil {
		t.Fatalf("기본값이 검증에 걸림: %v", err)
	}
	if err := validateRules(d); err != nil {
		t.Fatalf("기본값이 규칙에 걸림: %v", err)
	}
	cases := map[string]func(*Settings){
		"meals 2개":         func(s *Settings) { s.Meals = s.Meals[:2] },
		"마감 형식":            func(s *Settings) { s.Meals[0].Deadline = "8:00" },
		"헬스 요일 7":          func(s *Settings) { s.Gym.Days = []int{7} },
		"취침 25시":           func(s *Settings) { s.Sleep.BedTime = "25:00" },
		"휴식권 8장":           func(s *Settings) { s.Coupons.Rest = 8 },
		"나이 5":             func(s *Settings) { s.Age = Some(5) },
		"사진 보관 7일":         func(s *Settings) { s.PhotoKeepDays = Some(7) },
		"nutrition 모양":     func(s *Settings) { s.Nutrition = Some(json.RawMessage(`{"plan":{"mode":"keto"}}`)) },
		"activityInput 배열": func(s *Settings) { s.ActivityInput = Some(json.RawMessage(`[1]`)) },
		"이력 날짜 겹침": func(s *Settings) {
			s.TargetHistory = Some([]TargetEntry{{From: "2026-01-01"}, {From: "2026-01-01"}})
		},
	}
	for name, mutate := range cases {
		s := Defaults()
		mutate(&s)
		if err := s.Validate(); err == nil {
			t.Errorf("%s: 받아들임", name)
		}
	}
	s := Defaults()
	s.Targets = &Targets{1000, 100, 60, 40}
	if err := validateRules(s); err == nil || err.(*Error).Code != "TARGET_BELOW_MINIMUM" {
		t.Fatalf("1200 미만: %v", err)
	}
	s = Defaults()
	s.Meals = []MealSlot{{1, "08:00"}, {2, "12:00"}, {4, "18:00"}}
	if err := validateRules(s); err == nil {
		t.Fatal("끼니 번호 빠짐을 받음")
	}
	s = Defaults()
	s.PhotoKeepDays = Some(90)
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
}
