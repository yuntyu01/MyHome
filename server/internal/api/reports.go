package api

import (
	"net/http"

	"github.com/yuntyu01/MyHome/server/internal/dbq"
	"github.com/yuntyu01/MyHome/server/internal/httpx"
	"github.com/yuntyu01/MyHome/server/internal/period"
)

// MerchantTotal은 가맹점별 합계다.
type MerchantTotal struct {
	Merchant string `json:"merchant"`
	Count    int64  `json:"count"`
	Spent    int64  `json:"spent"`
}

// MonthlyReport는 월간 리포트다: 기간 요약, 전월 대비 분류별 증감, 많이 쓴 가맹점, 예산 초과 분류.
type MonthlyReport struct {
	Analytics
	ExpenseCount  int64            `json:"expense_count"`
	ReviewCount   int64            `json:"review_count"`
	DailyAverage  int64            `json:"daily_average"`
	TopMerchants  []MerchantTotal  `json:"top_merchants"`
	OverBudget    []CategoryBudget `json:"over_budget"`
	BudgetUsedPct float64          `json:"budget_used_ratio"`
}

// BuildMonthlyReport는 p 기간의 리포트를 만든다(이전 기간과 비교).
func (a *API) BuildMonthlyReport(r *http.Request, p, prev period.Period) (MonthlyReport, error) {
	an, err := a.BuildAnalytics(r, p, prev)
	if err != nil {
		return MonthlyReport{}, err
	}
	q := a.q()
	rep := MonthlyReport{Analytics: an}
	counts, err := q.StatusCounts(r.Context(), dbq.StatusCountsParams{FromAt: p.Start, ToAt: p.End})
	if err != nil {
		return rep, err
	}
	rep.ExpenseCount, rep.ReviewCount = counts.ExpenseCount, counts.ReviewCount
	top, err := q.TopMerchants(r.Context(), dbq.TopMerchantsParams{FromAt: p.Start, ToAt: p.End, Lim: 7})
	if err != nil {
		return rep, err
	}
	for _, m := range top {
		rep.TopMerchants = append(rep.TopMerchants, MerchantTotal{Merchant: m.Merchant, Count: m.Cnt, Spent: m.Spent})
	}
	s, err := a.BuildSummary(r, p, 0)
	if err != nil {
		return rep, err
	}
	for _, c := range s.Categories {
		if c.Budget > 0 && c.Remaining < 0 {
			rep.OverBudget = append(rep.OverBudget, c)
		}
	}
	rep.BudgetUsedPct = ratio(s.Totals.Spent-s.Totals.Settled, s.Totals.Budget)
	// 일평균: 끝난 기간은 기간 일수로, 진행 중인 기간은 오늘까지 일수로 나눈다.
	days := int64(p.End.Sub(p.Start).Hours() / 24)
	if now := a.now(); now.Before(p.End) && now.After(p.Start) {
		days = int64(now.Sub(p.Start).Hours()/24) + 1
	}
	if days > 0 {
		rep.DailyAverage = s.Totals.Spent / days
	}
	return rep, nil
}

// getMonthlyReport: ?month=YYYY-MM(기본: 지난 기간 — "지난달 요약").
func (a *API) getMonthlyReport(w http.ResponseWriter, r *http.Request) {
	p, startDay, err := a.ReportPeriod(r)
	if err != nil {
		a.fail(w, err)
		return
	}
	rep, err := a.BuildMonthlyReport(r, p, p.Prev(startDay))
	if err != nil {
		a.fail(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, rep)
}

// ReportPeriod는 리포트 기간과 예산 시작일이다. month가 없으면 지난 기간("지난달 요약").
func (a *API) ReportPeriod(r *http.Request) (period.Period, int, error) {
	p, cfg, err := a.periodFor(r)
	if err != nil {
		return p, 1, err
	}
	if r.URL.Query().Get("month") == "" {
		p = p.Prev(cfg.BudgetStartDay)
	}
	return p, cfg.BudgetStartDay, nil
}

// SourceRow는 출처별 합계 한 줄이다.
type SourceRow struct {
	Source string `json:"source"`
	Count  int64  `json:"count"`
	Spent  int64  `json:"spent"` // 확정 지출(부분 취소 차감)
}

// FlowRow는 출처·성격·방향별 확정 거래 합계다.
type FlowRow struct {
	Source    string `json:"source"`
	Direction string `json:"direction"`
	Flow      string `json:"flow"`
	Count     int64  `json:"count"`
	Amount    int64  `json:"amount"`
}

// ExcludedRow는 제외 사유·출처별 합계다.
type ExcludedRow struct {
	Reason string `json:"reason"`
	Source string `json:"source"`
	Count  int64  `json:"count"`
	Amount int64  `json:"amount"`
}

// MonthSources는 한 기간의 출처별 지출 합계다(최근 몇 달 비교표용).
type MonthSources struct {
	Period PeriodDTO        `json:"period"`
	Totals map[string]int64 `json:"totals"`
	Sum    int64            `json:"sum"`
}

// Reconcile은 대사 도우미 데이터다. 토스 소비 탭·카드 명세서 합계와 비교할 때 쓴다.
type Reconcile struct {
	Period   PeriodDTO      `json:"period"`
	Sources  []SourceRow    `json:"sources"`
	Flows    []FlowRow      `json:"flows"`
	Excluded []ExcludedRow  `json:"excluded"`
	History  []MonthSources `json:"history"`
	AllSrcs  []string       `json:"all_sources"`
}

// BuildReconcile은 p 기간과 그 전 months-1개 기간의 출처별 합계를 만든다.
func (a *API) BuildReconcile(r *http.Request, p period.Period, startDay int, months int) (Reconcile, error) {
	q := a.q()
	rc := Reconcile{Period: periodDTO(p)}
	src, err := q.SourceTotals(r.Context(), dbq.SourceTotalsParams{FromAt: p.Start, ToAt: p.End})
	if err != nil {
		return rc, err
	}
	for _, s := range src {
		rc.Sources = append(rc.Sources, SourceRow{Source: s.Source, Count: s.Cnt, Spent: s.Spent})
	}
	flows, err := q.SourceFlowTotals(r.Context(), dbq.SourceFlowTotalsParams{FromAt: p.Start, ToAt: p.End})
	if err != nil {
		return rc, err
	}
	for _, f := range flows {
		rc.Flows = append(rc.Flows, FlowRow{Source: f.Source, Direction: f.Direction, Flow: f.Flow, Count: f.Cnt, Amount: f.Amount})
	}
	ex, err := q.ExcludedTotals(r.Context(), dbq.ExcludedTotalsParams{FromAt: p.Start, ToAt: p.End})
	if err != nil {
		return rc, err
	}
	for _, e := range ex {
		rc.Excluded = append(rc.Excluded, ExcludedRow{Reason: e.Reason, Source: e.Source, Count: e.Cnt, Amount: e.Amount})
	}
	seen := map[string]bool{}
	cur := p
	for i := 0; i < months; i++ {
		rows, err := q.SourceTotals(r.Context(), dbq.SourceTotalsParams{FromAt: cur.Start, ToAt: cur.End})
		if err != nil {
			return rc, err
		}
		ms := MonthSources{Period: periodDTO(cur), Totals: map[string]int64{}}
		for _, s := range rows {
			ms.Totals[s.Source] = s.Spent
			ms.Sum += s.Spent
			if !seen[s.Source] {
				seen[s.Source] = true
				rc.AllSrcs = append(rc.AllSrcs, s.Source)
			}
		}
		rc.History = append(rc.History, ms)
		cur = cur.Prev(startDay)
	}
	return rc, nil
}

func (a *API) getReconcile(w http.ResponseWriter, r *http.Request) {
	p, cfg, err := a.periodFor(r)
	if err != nil {
		a.fail(w, err)
		return
	}
	rc, err := a.BuildReconcile(r, p, cfg.BudgetStartDay, 6)
	if err != nil {
		a.fail(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, rc)
}
