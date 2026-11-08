package health

import (
	"context"
	"errors"
	"math"

	"github.com/jackc/pgx/v5"

	"github.com/yuntyu01/MyHome/server/internal/dbq"
)

// 오늘·달력·하루 카드·주간 그래프(핏두잉 internal/stats 이식). 잠금·수면 기록 관련 필드는 뺐다.

// Row는 오늘 식단 표의 한 줄이다. 아직 올 끼니는 nutrition 대신 target(남은 목표를 남은 끼니 수로 똑같이 나눈 값)을
// 갖고, 놓친 끼니(마감이 지났는데 기록 없음)는 둘 다 없다.
type Row struct {
	Index     int        `json:"index"`
	Deadline  string     `json:"deadline"`
	Logged    bool       `json:"logged"`
	Missed    bool       `json:"missed"`
	Nutrition *Nutrition `json:"nutrition"`
	Target    *Targets   `json:"target"`
}

// GymWeek는 이번 주 헬스다.
type GymWeek struct {
	Enabled        bool   `json:"enabled"`
	Done           int    `json:"done"`
	Planned        int    `json:"planned"` // 주간 목표(gym.weeklyTarget, 기본은 요일 수)
	ScheduledToday bool   `json:"scheduledToday"`
	ArriveBy       string `json:"arriveBy"`
}

// TodayView는 홈 화면이다.
type TodayView struct {
	Date        Date        `json:"date"`
	Streak      int         `json:"streak"` // "N일째 기록 중"
	LoggedCount int         `json:"loggedCount"`
	MealCount   int         `json:"mealCount"`
	Total       Nutrition   `json:"total"`
	Targets     Targets     `json:"targets"`
	Rows        []Row       `json:"rows"`
	Gym         GymWeek     `json:"gym"`
	Coupons     RestSummary `json:"coupons"`
}

// Today는 date의 홈 화면이다. now는 그날 기기 시각 "HH:mm"("" = 모름: 놓친 끼니 없음). 마감이 now 이하인데
// 기록이 없으면 놓친 끼니다.
func (s *Service) Today(ctx context.Context, date Date, now string) (TodayView, error) {
	q := s.q()
	st, _, err := loadSettings(ctx, q)
	if err != nil {
		return TodayView{}, err
	}
	ms, err := mealsBetween(ctx, q, date, date, false)
	if err != nil {
		return TodayView{}, err
	}
	logged := map[int]meal{}
	for _, m := range ms {
		logged[m.Index] = m
	}
	rows := []Row{}
	total := Nutrition{}
	ahead := 0
	for _, slot := range st.Meals {
		m, ok := logged[slot.Index]
		switch {
		case ok:
			n := m.nutrition()
			total = total.Plus(n)
			rows = append(rows, Row{Index: slot.Index, Deadline: slot.Deadline, Logged: true, Nutrition: &n})
		case now != "" && slot.Deadline <= now: // 둘 다 "HH:mm"
			rows = append(rows, Row{Index: slot.Index, Deadline: slot.Deadline, Missed: true})
		default:
			ahead++
			rows = append(rows, Row{Index: slot.Index, Deadline: slot.Deadline})
		}
	}
	// "기타"(계획 밖 간식)는 줄이 없지만 먹은 것이라 합계와 남은 목표에 들어간다
	for _, m := range ms {
		if IsExtra(m.Index) {
			total = total.Plus(m.nutrition())
		}
	}
	targets := st.TargetsOn(date)
	left := remainingPerMeal(targets, total, ahead)
	for i := range rows {
		if !rows[i].Logged && !rows[i].Missed {
			t := left
			rows[i].Target = &t
		}
	}
	streak, err := streakOf(ctx, q, date)
	if err != nil {
		return TodayView{}, err
	}
	gym, err := gymWeek(ctx, q, st, date)
	if err != nil {
		return TodayView{}, err
	}
	rest, err := restSummary(ctx, q, st, date)
	if err != nil {
		return TodayView{}, err
	}
	planned := 0
	for i := range logged {
		if !IsExtra(i) {
			planned++
		}
	}
	return TodayView{date, streak, planned, len(st.Meals), total, targets, rows, gym, rest}, nil
}

// streakOf는 연속으로 기록한 날 수다. 오늘 기록이 없으면 어제부터 센다.
func streakOf(ctx context.Context, q *dbq.Queries, date Date) (int, error) {
	days, err := q.ListMealDatesUpTo(ctx, date.Time())
	if err != nil {
		return 0, err
	}
	set := map[Date]bool{}
	for _, d := range days {
		set[DateOf(d)] = true
	}
	d := date
	if !set[d] {
		d = date.AddDays(-1)
	}
	n := 0
	for set[d] {
		n++
		d = d.AddDays(-1)
	}
	return n, nil
}

func gymWeek(ctx context.Context, q *dbq.Queries, st Settings, date Date) (GymWeek, error) {
	mon := date.Monday()
	visits, err := q.ListGymVisitDatesBetween(ctx, dbq.ListGymVisitDatesBetweenParams{FromDate: mon.Time(), ToDate: mon.AddDays(6).Time()})
	if err != nil {
		return GymWeek{}, err
	}
	scheduled := false
	for _, d := range st.Gym.Days {
		if d == date.Weekday0() {
			scheduled = true
		}
	}
	return GymWeek{st.Gym.Enabled, len(visits), st.Gym.WeeklyTarget, scheduled, st.Gym.ArriveBy}, nil
}

// remainingPerMeal은 먹고 남은 하루 목표를 n끼니가 똑같이 나눈 값이다(넘치면 0). 핏두잉 앱 remainingPerMeal과 같다.
func remainingPerMeal(t Targets, eaten Nutrition, n int) Targets {
	d := float64(max(1, n))
	f := func(target, got int) int { return max(0, int(math.Floor(float64(target-got)/d+0.5))) }
	return Targets{
		Kcal: f(t.Kcal, eaten.Kcal), Carbs: f(t.Carbs, eaten.Carbs),
		Protein: f(t.Protein, eaten.Protein), Fat: f(t.Fat, eaten.Fat),
	}
}

// ── 달력 ──────────────────────────────────────────────────────────────

// Day는 달력의 하루다. state: full(계획한 끼니를 다 기록) | partial | none.
type Day struct {
	Date        Date   `json:"date"`
	State       string `json:"state"`
	LoggedMeals int    `json:"loggedMeals"`
	Gym         bool   `json:"gym"`
}

// MonthSummary는 달 요약이다.
type MonthSummary struct {
	FullDays int `json:"fullDays"`
	GymDays  int `json:"gymDays"`
}

// MonthView는 기록 탭 달력이다.
type MonthView struct {
	Month     string       `json:"month"`
	MealCount int          `json:"mealCount"`
	Summary   MonthSummary `json:"summary"`
	Days      []Day        `json:"days"`
}

// Month는 "2026-09" 달력이다.
func (s *Service) Month(ctx context.Context, month string) (MonthView, error) {
	from, to, err := ParseMonth(month)
	if err != nil {
		return MonthView{}, invalid("month", "YYYY-MM 형식이어야 해요")
	}
	q := s.q()
	st, _, err := loadSettings(ctx, q)
	if err != nil {
		return MonthView{}, err
	}
	counts, err := q.CountPlannedMealsByDay(ctx, dbq.CountPlannedMealsByDayParams{FromDate: from.Time(), ToDate: to.Time()})
	if err != nil {
		return MonthView{}, err
	}
	perDay := map[Date]int{}
	for _, c := range counts {
		perDay[DateOf(c.MealDate)] = int(c.N)
	}
	visits, err := q.ListGymVisitDatesBetween(ctx, dbq.ListGymVisitDatesBetweenParams{FromDate: from.Time(), ToDate: to.Time()})
	if err != nil {
		return MonthView{}, err
	}
	gymDays := map[Date]bool{}
	for _, d := range visits {
		gymDays[DateOf(d)] = true
	}
	mealCount := len(st.Meals)
	days := []Day{}
	full := 0
	for d := from; d <= to; d = d.AddDays(1) {
		n := perDay[d]
		state := "none"
		switch {
		case n >= mealCount:
			state = "full"
			full++
		case n > 0:
			state = "partial"
		}
		days = append(days, Day{d, state, n, gymDays[d]})
	}
	return MonthView{month, mealCount, MonthSummary{full, len(gymDays)}, days}, nil
}

// DayCard는 달력에서 누른 날이다.
type DayCard struct {
	Date          Date       `json:"date"`
	Total         Nutrition  `json:"total"`
	Targets       Targets    `json:"targets"`
	Meals         []MealView `json:"meals"`
	Gym           bool       `json:"gym"`           // 방문했는지(휴식은 false)
	GymVerifiedBy *Verified  `json:"gymVerifiedBy"` // photo | rest | null (예전 기록은 geofence)
}

// Day는 그날 카드다.
func (s *Service) Day(ctx context.Context, date Date) (DayCard, error) {
	q := s.q()
	st, _, err := loadSettings(ctx, q)
	if err != nil {
		return DayCard{}, err
	}
	ms, err := mealsBetween(ctx, q, date, date, true)
	if err != nil {
		return DayCard{}, err
	}
	card := DayCard{Date: date, Targets: st.TargetsOn(date), Meals: make([]MealView, 0, len(ms))}
	for _, m := range ms {
		v := m.view()
		card.Total = card.Total.Plus(v.Nutrition)
		card.Meals = append(card.Meals, v)
	}
	g, err := q.GetGymLog(ctx, date.Time())
	switch {
	case errors.Is(err, pgx.ErrNoRows):
	case err != nil:
		return DayCard{}, err
	default:
		v := Verified(g.VerifiedBy)
		card.GymVerifiedBy = &v
		card.Gym = v != Rest
	}
	return card, nil
}

// ── 주간 그래프 ───────────────────────────────────────────────────────

// WeekDay는 주간 그래프의 하루다. state: done(기록한 지난 날) | today(진행 중) | missing(기록 없는 지난 날) | future.
// 목표를 넘는 것은 표시하지 않는다.
type WeekDay struct {
	Index   int        `json:"index"` // 0 = 월
	Date    Date       `json:"date"`
	State   string     `json:"state"`
	Current *Nutrition `json:"current"`
	Last    *Nutrition `json:"last"` // 지난주 같은 요일. 기록 없으면 null
}

// Weekly는 홈 주간 그래프 데이터다: 이번 주와 지난주를 요일별로, 지표마다.
type Weekly struct {
	WeekStart Date       `json:"weekStart"`
	Days      []WeekDay  `json:"days"`
	Avg       *Nutrition `json:"avg"`     // 이번 주 기록한 지난 날(오늘 제외)
	LastAvg   *Nutrition `json:"lastAvg"` // 지난주 기록한 날
	Targets   Targets    `json:"targets"`
	BandPct   int        `json:"bandPct"` // 목표 범위 ±10%
	// Summary는 지표별(kcal, carbs, protein, fat) 그래프 아래 한 줄이다. 핏두잉 앱 weeklySummary와 같다.
	Summary map[string]WeekSummary `json:"summary"`
}

// WeekSummary: 기록한 지난 날 수(오늘 제외), 그중 목표 범위 안인 날 수, 범위에서 가장 먼 날(diff = 값 - 목표,
// 모자라면 음수). 모두 범위 안이면 null.
type WeekSummary struct {
	Completed int       `json:"completed"`
	Achieved  int       `json:"achieved"`
	Furthest  *Furthest `json:"furthest"`
}

// Furthest는 범위에서 가장 먼 날이다.
type Furthest struct {
	Index int `json:"index"` // 0 = 월
	Diff  int `json:"diff"`
}

const weeklyBandPct = 10

// weekSummary는 기록한 날마다 그날 목표(targetOn)로 판단한다. 나중에 목표를 바꿔도 지난 날 판단은 그대로다.
func weekSummary(days []WeekDay, targetOn func(Date) int, pick func(Nutrition) int) WeekSummary {
	out := WeekSummary{}
	for _, d := range days {
		if d.State != "done" || d.Current == nil {
			continue
		}
		target := targetOn(d.Date)
		lo := float64(target) * (1 - weeklyBandPct/100.0)
		hi := float64(target) * (1 + weeklyBandPct/100.0)
		out.Completed++
		v := pick(*d.Current)
		if float64(v) >= lo && float64(v) <= hi {
			out.Achieved++
			continue
		}
		diff := v - target
		if out.Furthest == nil || abs(diff) > abs(out.Furthest.Diff) {
			out.Furthest = &Furthest{Index: d.Index, Diff: diff}
		}
	}
	return out
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

func meanNutrition(ns []Nutrition) *Nutrition {
	if len(ns) == 0 {
		return nil
	}
	var t Nutrition
	for _, n := range ns {
		t = t.Plus(n)
	}
	c := len(ns)
	avg := func(x int) int { return int(math.Floor(float64(x)/float64(c) + 0.5)) }
	return &Nutrition{Kcal: avg(t.Kcal), Carbs: avg(t.Carbs), Protein: avg(t.Protein), Fat: avg(t.Fat)}
}

// Weekly는 date(사용자 오늘)가 든 주 그래프다.
func (s *Service) Weekly(ctx context.Context, date Date) (Weekly, error) {
	q := s.q()
	st, _, err := loadSettings(ctx, q)
	if err != nil {
		return Weekly{}, err
	}
	mon := date.Monday()
	ms, err := mealsBetween(ctx, q, mon.AddDays(-7), mon.AddDays(6), false)
	if err != nil {
		return Weekly{}, err
	}
	perDay := map[Date]Nutrition{}
	for _, m := range ms {
		perDay[m.Date] = perDay[m.Date].Plus(m.nutrition())
	}
	logged := func(d Date) *Nutrition {
		if n, ok := perDay[d]; ok && n.Kcal+n.Carbs+n.Protein+n.Fat > 0 {
			return &n
		}
		return nil
	}
	days := []WeekDay{}
	var thisDone, lastAll []Nutrition
	for i := 0; i < 7; i++ {
		d := mon.AddDays(i)
		wd := WeekDay{Index: i, Date: d, Last: logged(mon.AddDays(i - 7))}
		cur := logged(d)
		switch {
		case d > date:
			wd.State = "future"
		case d == date:
			wd.State = "today"
			if cur == nil {
				cur = &Nutrition{}
			}
			wd.Current = cur
		case cur != nil:
			wd.State = "done"
			wd.Current = cur
			thisDone = append(thisDone, *cur)
		default:
			wd.State = "missing"
		}
		if wd.Last != nil {
			lastAll = append(lastAll, *wd.Last)
		}
		days = append(days, wd)
	}
	on := func(pick func(Targets) int) func(Date) int {
		return func(d Date) int { return pick(st.TargetsOn(d)) }
	}
	summary := map[string]WeekSummary{
		"kcal":    weekSummary(days, on(func(t Targets) int { return t.Kcal }), func(n Nutrition) int { return n.Kcal }),
		"carbs":   weekSummary(days, on(func(t Targets) int { return t.Carbs }), func(n Nutrition) int { return n.Carbs }),
		"protein": weekSummary(days, on(func(t Targets) int { return t.Protein }), func(n Nutrition) int { return n.Protein }),
		"fat":     weekSummary(days, on(func(t Targets) int { return t.Fat }), func(n Nutrition) int { return n.Fat }),
	}
	return Weekly{mon, days, meanNutrition(thisDone), meanNutrition(lastAll), st.TargetsOn(date), weeklyBandPct, summary}, nil
}
