package health

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
)

// 건강 설정(핏두잉 internal/settings 이식). 핏두잉 앱 설정과 같은 JSON 모양에서 뺀 필드만 지웠다:
// 강도(mode)·잠금·허용 앱·비상 해제·치팅권·닉네임·테마·친구 공유·사진 백업·음식 선택 방식·알레르기·단식.

// MealSlot은 계획한 끼니 하나와 마감 시각이다.
type MealSlot struct {
	Index    int    `json:"index"`
	Deadline string `json:"deadline"`
}

// Gym은 헬스 계획이다. 헬스장 위치(name·address·lat·lng·radiusM)는 없앴다(사용자 결정, 2026-11-08).
// 예전에 저장한 문서에 그 키가 남아 있어도 읽을 때 무시하고, 다음 저장 때 빠진다.
type Gym struct {
	Enabled bool  `json:"enabled"`
	Days    []int `json:"days"` // 0=월 ... 6=일
	// WeeklyTarget은 주에 몇 번 가면 달성인지(1~7). 0이면 요일 수.
	WeeklyTarget int    `json:"weeklyTarget"`
	ArriveBy     string `json:"arriveBy"`
}

// Sleep은 취침·기상 시각이다(알림용 설정만. 수면 기록은 없다).
type Sleep struct {
	Enabled  bool   `json:"enabled"`
	BedTime  string `json:"bedTime"`
	WakeTime string `json:"wakeTime"`
}

// Coupons는 주마다 받는 장수다. 휴식권만 남겼다(치팅권은 뺌).
type Coupons struct {
	Rest int `json:"rest"`
}

// Settings는 설정 문서다. PUT은 보낸 키만 바꾸고 나머지는 저장된 값을 둔다(store.PutSettings).
type Settings struct {
	Goal          Goal       `json:"goal"`
	HeightCm      int        `json:"heightCm"`
	WeightKg      float64    `json:"weightKg"`
	Meals         []MealSlot `json:"meals"`
	Gym           *Gym       `json:"gym"`
	Sleep         *Sleep     `json:"sleep"`
	Coupons       *Coupons   `json:"coupons"`
	Targets       *Targets   `json:"targets"`       // 적용한 최종 목표
	Notifications *bool      `json:"notifications"` // 마감·헬스·취침 알림 켜기/끄기

	// PhotoKeepDays는 폰에 끼니 사진을 며칠 둘지다(null = 계속, 30·90·180). 사진은 폰에만 있다.
	// 핏두잉에서는 폰에만 있던 값인데, 여기서는 설정이 서버에 있어서 같이 둔다.
	PhotoKeepDays Opt[int] `json:"photoKeepDays"`

	// 영양 목표 계산 입력. null은 "모름"(기본값으로 채우지 않는다).
	Sex           Opt[Sex]             `json:"sex"`
	Age           Opt[int]             `json:"age"`
	Activity      Opt[Activity]        `json:"activity"`
	ActivityInput Opt[json.RawMessage] `json:"activityInput"` // 단계를 고를 때 참고한 일상 활동·운동(서버는 모양만 본다)
	Nutrition     Opt[json.RawMessage] `json:"nutrition"`     // 계산 입력(모드, 비율, 버전). 목표는 targets가 기준
	TargetHistory Opt[[]TargetEntry]   `json:"targetHistory"` // 날짜별 목표(health_target_history 표)
}

// Opt는 요청에 키가 있었는지(Set)를 기억한다. 명시적 null은 Set이고 Val이 nil("모름")이다.
type Opt[T any] struct {
	Set bool
	Val *T
}

// Some은 값이 있는 Opt다.
func Some[T any](v T) Opt[T] { return Opt[T]{Set: true, Val: &v} }

func (o *Opt[T]) UnmarshalJSON(b []byte) error {
	o.Set = true
	if bytes.Equal(bytes.TrimSpace(b), []byte("null")) {
		o.Val = nil
		return nil
	}
	var v T
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	o.Val = &v
	return nil
}

func (o Opt[T]) MarshalJSON() ([]byte, error) {
	if o.Val == nil {
		return []byte("null"), nil
	}
	return json.Marshal(*o.Val)
}

func ptr[T any](v T) *T { return &v }

// Defaults는 아직 아무것도 저장하지 않았을 때 보여주는 설정이다(핏두잉 기본값).
func Defaults() Settings {
	t := Recommend(GoalKeep, 70)
	return Settings{
		Goal: GoalKeep, HeightCm: 175, WeightKg: 70,
		Meals:   []MealSlot{{1, "08:00"}, {2, "14:00"}, {3, "17:00"}, {4, "19:00"}},
		Gym:     &Gym{Enabled: true, Days: []int{0, 1, 2, 3, 4}, WeeklyTarget: 5, ArriveBy: "19:00"},
		Sleep:   &Sleep{Enabled: true, BedTime: "23:30", WakeTime: "07:00"},
		Coupons: &Coupons{Rest: 2}, Targets: &t, Notifications: ptr(true),
		PhotoKeepDays: Opt[int]{Set: true}, Sex: Opt[Sex]{Set: true}, Age: Opt[int]{Set: true},
		Activity: Opt[Activity]{Set: true}, ActivityInput: Opt[json.RawMessage]{Set: true},
		Nutrition: Opt[json.RawMessage]{Set: true}, TargetHistory: Some([]TargetEntry{}),
	}
}

// normalize는 빠진 기본값을 채운다.
func (s *Settings) normalize() {
	if s.Notifications == nil {
		s.Notifications = ptr(true)
	}
	if s.Gym != nil {
		days := []int{} // 겹친 요일은 하나로, 월요일부터
		for _, d := range s.Gym.Days {
			if !slices.Contains(days, d) {
				days = append(days, d)
			}
		}
		slices.Sort(days)
		s.Gym.Days = days
		if s.Gym.WeeklyTarget == 0 {
			s.Gym.WeeklyTarget = len(s.Gym.Days)
		}
	}
	if s.TargetHistory.Val == nil {
		s.TargetHistory = Some([]TargetEntry{})
	}
}

// TargetsOn은 이 설정에서 d에 쓰던 목표다(이력에 없으면 지금 목표).
func (s Settings) TargetsOn(d Date) Targets {
	var h []TargetEntry
	if s.TargetHistory.Val != nil {
		h = *s.TargetHistory.Val
	}
	return TargetsOn(h, *s.Targets, d)
}

var hhmm = regexp.MustCompile(`^([01]\d|2[0-3]):[0-5]\d$`)

// ValidHHMM은 24시간 "HH:mm"인지다.
func ValidHHMM(s string) bool { return hhmm.MatchString(s) }

func between(v, lo, hi int) bool { return v >= lo && v <= hi }

// PhotoKeepChoices는 폰 사진 보관 기간으로 고를 수 있는 값이다(null = 계속).
var PhotoKeepChoices = []int{30, 90, 180}

// Validate는 필드 제약을 확인한다(400 INVALID, 처음 걸린 것).
func (s *Settings) Validate() error {
	switch {
	case s.Goal == "":
		return invalid("goal", "필수예요")
	case !between(s.HeightCm, 100, 250):
		return invalid("heightCm", "100~250 사이여야 해요")
	case s.WeightKg < 20 || s.WeightKg > 400:
		return invalid("weightKg", "20~400 사이여야 해요")
	case len(s.Meals) < 3 || len(s.Meals) > 6:
		return invalid("meals", "3~6개여야 해요")
	case s.Gym == nil:
		return invalid("gym", "필수예요")
	case s.Sleep == nil:
		return invalid("sleep", "필수예요")
	case s.Coupons == nil:
		return invalid("coupons", "필수예요")
	case s.Targets == nil:
		return invalid("targets", "필수예요")
	case s.Age.Val != nil && !between(*s.Age.Val, AgeMin, AgeMax):
		return invalid("age", "10~100 사이여야 해요")
	case s.PhotoKeepDays.Val != nil && !slices.Contains(PhotoKeepChoices, *s.PhotoKeepDays.Val):
		return invalid("photoKeepDays", "null, 30, 90, 180 중 하나예요")
	}
	if err := checkObject("activityInput", s.ActivityInput.Val, nil); err != nil {
		return err
	}
	if err := checkObject("nutrition", s.Nutrition.Val, validNutrition); err != nil {
		return err
	}
	if s.TargetHistory.Val != nil {
		if err := validateHistory(*s.TargetHistory.Val); err != nil {
			return err
		}
	}
	for _, m := range s.Meals {
		if !between(m.Index, 1, 6) {
			return invalid("meals.index", "1~6 사이여야 해요")
		}
		if !ValidHHMM(m.Deadline) {
			return invalid("meals.deadline", "HH:mm 형식이어야 해요")
		}
	}
	g := s.Gym
	for _, d := range g.Days {
		if !between(d, 0, 6) {
			return invalid("gym.days", "0~6 사이여야 해요")
		}
	}
	switch {
	case !between(g.WeeklyTarget, 0, 7):
		return invalid("gym.weeklyTarget", "0~7 사이여야 해요")
	case !ValidHHMM(g.ArriveBy):
		return invalid("gym.arriveBy", "HH:mm 형식이어야 해요")
	case !ValidHHMM(s.Sleep.BedTime):
		return invalid("sleep.bedTime", "HH:mm 형식이어야 해요")
	case !ValidHHMM(s.Sleep.WakeTime):
		return invalid("sleep.wakeTime", "HH:mm 형식이어야 해요")
	case !between(s.Coupons.Rest, 0, 7):
		return invalid("coupons.rest", "0~7 사이여야 해요")
	}
	t := s.Targets
	if t.Kcal < 0 || t.Carbs < 0 || t.Protein < 0 || t.Fat < 0 {
		return invalid("targets", "0 이상이어야 해요")
	}
	return nil
}

// validateRules는 앱이 도와서 정하는 범위다: 목표 1200kcal 이상, 끼니 번호는 1부터 빠짐없이.
func validateRules(s Settings) error {
	if s.Targets.Kcal < MinKcal {
		return badRequest("TARGET_BELOW_MINIMUM", fmt.Sprintf("목표 칼로리는 %dkcal 아래로 못 낮춰요", MinKcal))
	}
	idx := map[int]bool{}
	for _, m := range s.Meals {
		idx[m.Index] = true
	}
	for i := 1; i <= len(s.Meals); i++ {
		if !idx[i] {
			return badRequest("INVALID", "끼니 번호는 1부터 빠짐없이 이어져야 해요")
		}
	}
	return nil
}

// ── 계산 입력(서버는 모양과 크기만 본다) ──────────────────────────────

const maxInputBytes = 4 << 10

func checkObject(field string, raw *json.RawMessage, check func(map[string]any) bool) error {
	if raw == nil {
		return nil
	}
	if len(*raw) > maxInputBytes {
		return invalid(field, "너무 커요")
	}
	var m map[string]any
	if err := json.Unmarshal(*raw, &m); err != nil || m == nil {
		return invalid(field, "객체여야 해요")
	}
	if check != nil && !check(m) {
		return invalid(field, "모양이 잘못됐어요")
	}
	return nil
}

func validNutrition(m map[string]any) bool {
	plan, ok := m["plan"].(map[string]any)
	if !ok {
		return false
	}
	mode, _ := plan["mode"].(string)
	return mode == "balanced" || mode == "training" || mode == "custom"
}
