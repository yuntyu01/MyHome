package api

import (
	"net/http"

	"github.com/yuntyu01/MyHome/server/internal/dbq"
	"github.com/yuntyu01/MyHome/server/internal/httpx"
	"github.com/yuntyu01/MyHome/server/internal/period"
)

// DailyPoint는 일별 지출이다.
type DailyPoint struct {
	Day   string `json:"day"` // YYYY-MM-DD (KST)
	Spent int64  `json:"spent"`
}

// CategoryCompare는 분류별 이번 기간·지난 기간 비교다.
// 폴더는 아래 분류까지 더한 값이라, 비중·합계는 맨 위 분류(Depth 0)만 써야 두 번 세지 않는다.
type CategoryCompare struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	ParentID *int64 `json:"parent_id"`
	Depth    int    `json:"depth"`
	Path     string `json:"path"`
	Current  int64  `json:"current"`
	Previous int64  `json:"previous"`
	Diff     int64  `json:"diff"`
}

// Analytics는 분석 화면 데이터다: 분류별 비중, 일별 추이, 전월 대비, 월간 요약.
type Analytics struct {
	Period         PeriodDTO         `json:"period"`
	PreviousPeriod PeriodDTO         `json:"previous_period"`
	Totals         Totals            `json:"totals"`
	PreviousTotals Totals            `json:"previous_totals"`
	Categories     []CategoryCompare `json:"categories"`
	Daily          []DailyPoint      `json:"daily"`
	PreviousDaily  []DailyPoint      `json:"previous_daily"`
	Today          string            `json:"today"` // KST 오늘. 이번 기간 차트에서 미래 날짜를 비우는 데 쓴다
}

func (a *API) getAnalytics(w http.ResponseWriter, r *http.Request) {
	p, cfg, err := a.periodFor(r)
	if err != nil {
		a.fail(w, err)
		return
	}
	an, err := a.BuildAnalytics(r, p, p.Prev(cfg.BudgetStartDay))
	if err != nil {
		a.fail(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, an)
}

// BuildAnalytics는 두 기간을 비교한 분석 데이터를 만든다(웹·리포트 공용).
func (a *API) BuildAnalytics(r *http.Request, cur, prev period.Period) (Analytics, error) {
	cs, err := a.BuildSummary(r, cur, 0)
	if err != nil {
		return Analytics{}, err
	}
	ps, err := a.BuildSummary(r, prev, 0)
	if err != nil {
		return Analytics{}, err
	}
	an := Analytics{Period: periodDTO(cur), PreviousPeriod: periodDTO(prev), Totals: cs.Totals, PreviousTotals: ps.Totals,
		Today: a.now().In(period.KST).Format("2006-01-02")}
	prevBy := map[int64]int64{}
	for _, c := range ps.Categories {
		prevBy[c.ID] = c.Spent
	}
	seen := map[int64]bool{}
	for _, c := range cs.Categories {
		seen[c.ID] = true
		an.Categories = append(an.Categories, CategoryCompare{ID: c.ID, Name: c.Name, ParentID: c.ParentID, Depth: c.Depth, Path: c.Path, Current: c.Spent, Previous: prevBy[c.ID], Diff: c.Spent - prevBy[c.ID]})
	}
	for _, c := range ps.Categories {
		if !seen[c.ID] && c.Spent != 0 {
			an.Categories = append(an.Categories, CategoryCompare{ID: c.ID, Name: c.Name, ParentID: c.ParentID, Depth: c.Depth, Path: c.Path, Previous: c.Spent, Diff: -c.Spent})
		}
	}
	if an.Daily, err = a.daily(r, cur); err != nil {
		return an, err
	}
	if an.PreviousDaily, err = a.daily(r, prev); err != nil {
		return an, err
	}
	return an, nil
}

// daily는 기간의 모든 날짜를 채운 일별 지출이다(지출 없는 날 0).
func (a *API) daily(r *http.Request, p period.Period) ([]DailyPoint, error) {
	rows, err := a.q().DailyExpense(r.Context(), dbq.DailyExpenseParams{FromAt: p.Start, ToAt: p.End})
	if err != nil {
		return nil, err
	}
	by := map[string]int64{}
	for _, d := range rows {
		by[d.Day.Format("2006-01-02")] = d.Spent
	}
	var out []DailyPoint
	for d := p.Start; d.Before(p.End); d = d.AddDate(0, 0, 1) {
		key := d.Format("2006-01-02")
		out = append(out, DailyPoint{Day: key, Spent: by[key]})
	}
	return out, nil
}
