package health

import (
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"time"

	"github.com/yuntyu01/MyHome/server/internal/ai"
)

// 끼니 기록(핏두잉 internal/meal 이식). 사진 → 인식 → 고치기는 폰에서 하고, 서버에는 결과만 온다(사진은 오지 않는다).

// 끼니 번호: 1~6은 계획한 끼니(마감 있음), ExtraBase~MaxIndex는 "기타"(간식 등)다. 마감이 없고 계획한 끼니로
// 세지 않지만 먹은 것이라 합계에는 들어간다.
const (
	MaxSlot   = 6
	ExtraBase = 7
	MaxIndex  = 9
)

// IsExtra는 "기타"인지다.
func IsExtra(index int) bool { return index >= ExtraBase }

// Portions는 끼니 전체에 곱하는 먹은 양이다(인식한 한 접시 기준).
var Portions = []float64{0.25, 0.5, 1, 1.5, 2}

// Nutrition은 칼로리와 탄단지(g)다.
type Nutrition struct {
	Kcal    int `json:"kcal"`
	Carbs   int `json:"carbs"`
	Protein int `json:"protein"`
	Fat     int `json:"fat"`
}

// Plus는 합이다.
func (n Nutrition) Plus(o Nutrition) Nutrition {
	return Nutrition{n.Kcal + o.Kcal, n.Carbs + o.Carbs, n.Protein + o.Protein, n.Fat + o.Fat}
}

// meal은 저장된 끼니 하나다.
type meal struct {
	ID         int64
	Date       Date
	Index      int
	Deadline   string
	Barcode    *string
	Base       Nutrition // 인식한 한 접시 기준(먹은 양·나눠 먹음 반영 전)
	Confidence float64
	Portion    float64
	SharedMeal bool
	LoggedAt   time.Time
	Items      []ai.Item
}

// factor는 실제로 먹은 비율이다. 나눠 먹으면 절반.
func (m meal) factor() float64 {
	if m.SharedMeal {
		return m.Portion * 0.5
	}
	return m.Portion
}

// nutrition은 먹은 양으로 바꾼 값이다(화면에는 "약"으로 보여준다).
func (m meal) nutrition() Nutrition {
	f := m.factor()
	r := func(v int) int { return int(math.Floor(float64(v)*f + 0.5)) }
	return Nutrition{r(m.Base.Kcal), r(m.Base.Carbs), r(m.Base.Protein), r(m.Base.Fat)}
}

// MealView는 끼니 응답이다(핏두잉 meal.View에서 shareFriends·cheat를 뺐다).
type MealView struct {
	ID         string    `json:"id"`
	Date       Date      `json:"date"`
	Index      int       `json:"index"`
	Deadline   string    `json:"deadline"`
	Barcode    *string   `json:"barcode"`
	Items      []ai.Item `json:"items"`
	Nutrition  Nutrition `json:"nutrition"` // 먹은 양
	Base       Nutrition `json:"base"`      // 저장한 값(한 접시, 먹은 양·나눠 먹음 반영 전)
	Portion    float64   `json:"portion"`
	SharedMeal bool      `json:"sharedMeal"`
	Confidence float64   `json:"confidence"`
	LoggedAt   time.Time `json:"loggedAt"`
}

func (m meal) view() MealView {
	items := m.Items
	if items == nil {
		items = []ai.Item{}
	}
	return MealView{fmt.Sprint(m.ID), m.Date, m.Index, m.Deadline, m.Barcode, items, m.nutrition(), m.Base,
		m.Portion, m.SharedMeal, m.Confidence, m.LoggedAt.UTC()}
}

// MealRequest는 POST /v1/health/meals 본문이다(핏두잉 meal.Request에서 shareFriends·cheat를 뺐다).
type MealRequest struct {
	Date       Date       `json:"date"`
	Index      int        `json:"index"`
	Deadline   *string    `json:"deadline"` // 없으면 설정의 그 끼니 마감, "기타"는 ""
	Barcode    *string    `json:"barcode"`
	AI         *ai.Result `json:"ai"`
	Portion    *float64   `json:"portion"` // 0.25 · 0.5 · 1 · 1.5 · 2. 없으면 1
	SharedMeal bool       `json:"sharedMeal"`
	LoggedAt   *time.Time `json:"loggedAt"` // 없으면 서버 시각
}

// UnmarshalJSON은 loggedAt을 초가 없는 시각("2026-11-11T11:52+09:00")으로도 받는다. 앱(2026-11-15 이전)은
// java OffsetDateTime.toString()으로 보내는데, 초가 0이면 초를 빼고 써서 RFC 3339만 읽으면 끼니 저장이 400이 됐다.
func (r *MealRequest) UnmarshalJSON(b []byte) error {
	type plain MealRequest
	var v struct {
		plain
		LoggedAt *string `json:"loggedAt"` // plain.LoggedAt보다 얕아서 이쪽이 읽힌다
	}
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	*r = MealRequest(v.plain)
	r.LoggedAt = nil
	if v.LoggedAt != nil {
		t, err := parseClientTime(*v.LoggedAt)
		if err != nil {
			return err
		}
		r.LoggedAt = &t
	}
	return nil
}

// parseClientTime은 RFC 3339 시각을 읽고, 초가 빠진 것("…T11:52+09:00", "…T11:52Z")도 받는다.
func parseClientTime(s string) (time.Time, error) {
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t, nil
	}
	return time.Parse("2006-01-02T15:04Z07:00", s)
}

// Validate는 형식을 확인한다(설정과 맞는지는 저장할 때 본다).
func (r *MealRequest) Validate() error {
	switch {
	case r.Date.IsZero():
		return invalid("date", "필수예요")
	case r.Index < 1 || r.Index > MaxIndex:
		return invalid("index", "1~9 사이여야 해요 (7~9는 기타)")
	case r.Deadline != nil && *r.Deadline != "" && !ValidHHMM(*r.Deadline):
		return invalid("deadline", "HH:mm 형식이어야 해요")
	case r.Barcode != nil && len(*r.Barcode) > 64:
		return invalid("barcode", "너무 길어요")
	case r.AI == nil:
		return invalid("ai", "필수예요")
	case r.Portion != nil && !slices.Contains(Portions, *r.Portion):
		return badRequest("INVALID", "먹은 양은 0.25, 0.5, 1, 1.5, 2 중 하나예요")
	}
	if len(r.AI.Items) > 50 {
		return invalid("ai.items", "50개 이하여야 해요")
	}
	for _, it := range r.AI.Items {
		if len([]rune(it.Name)) > 100 || len([]rune(it.Amount)) > 100 {
			return invalid("ai.items.name", "100자 이하여야 해요")
		}
	}
	return r.AI.Validate("ai")
}

func (r *MealRequest) portion() float64 {
	if r.Portion == nil {
		return 1
	}
	return *r.Portion
}
