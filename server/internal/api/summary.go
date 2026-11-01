package api

import (
	"net/http"
	"time"

	"github.com/yuntyu01/MyHome/server/internal/dbq"
	"github.com/yuntyu01/MyHome/server/internal/httpx"
	"github.com/yuntyu01/MyHome/server/internal/period"
)

// CategoryBudget은 분류별 예산 현황이다.
type CategoryBudget struct {
	ID        int64   `json:"id"`
	Name      string  `json:"name"`
	Budget    int64   `json:"budget"`
	Spent     int64   `json:"spent"`
	Settled   int64   `json:"settled"`
	Remaining int64   `json:"remaining"`
	Ratio     float64 `json:"ratio"` // (지출-정산)/예산. 예산이 0이면 0. 표시용
}

// Totals는 기간 총계다.
type Totals struct {
	Budget      int64 `json:"budget"`
	Spent       int64 `json:"spent"`
	Settled     int64 `json:"settled"`
	Remaining   int64 `json:"remaining"`
	Income      int64 `json:"income"`
	Investment  int64 `json:"investment"`
	OwnTransfer int64 `json:"own_transfer"`
}

// Summary는 GET /v1/summary 응답이다. 위젯이 쓰므로 가볍게 유지한다.
type Summary struct {
	Period      PeriodDTO        `json:"period"`
	Totals      Totals           `json:"totals"`
	Categories  []CategoryBudget `json:"categories"`
	Recent      []TransactionDTO `json:"recent"`
	NeedsReview int64            `json:"needs_review"`
	GeneratedAt time.Time        `json:"generated_at"`
}

func (a *API) getSummary(w http.ResponseWriter, r *http.Request) {
	p, _, err := a.periodFor(r)
	if err != nil {
		a.fail(w, err)
		return
	}
	s, err := a.BuildSummary(r, p, queryInt(r, "recent", 4, 50))
	if err != nil {
		a.fail(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, s)
}

// BuildSummary는 기간 요약을 만든다(웹·위젯 공용).
func (a *API) BuildSummary(r *http.Request, p period.Period, recentN int) (Summary, error) {
	ctx := r.Context()
	q := a.q()
	rows, err := q.CategorySummary(ctx, dbq.CategorySummaryParams{PeriodStart: p.StartDate(), FromAt: p.Start, ToAt: p.End})
	if err != nil {
		return Summary{}, err
	}
	tot, err := q.PeriodTotals(ctx, dbq.PeriodTotalsParams{FromAt: p.Start, ToAt: p.End})
	if err != nil {
		return Summary{}, err
	}
	recent, err := q.RecentTransactions(ctx, int32(recentN))
	if err != nil {
		return Summary{}, err
	}
	nr, err := q.CountNeedsReview(ctx)
	if err != nil {
		return Summary{}, err
	}
	s := Summary{Period: periodDTO(p), Categories: make([]CategoryBudget, 0, len(rows)), Recent: make([]TransactionDTO, 0, len(recent)), NeedsReview: nr, GeneratedAt: a.now()}
	for _, c := range rows {
		s.Categories = append(s.Categories, CategoryBudget{ID: c.ID, Name: c.Name, Budget: c.Budget, Spent: c.Spent, Settled: c.Settled, Remaining: c.Remaining, Ratio: ratio(c.Spent-c.Settled, c.Budget)})
		s.Totals.Budget += c.Budget
		s.Totals.Spent += c.Spent
	}
	s.Totals.Settled = tot.SettledTotal
	s.Totals.Remaining = s.Totals.Budget - s.Totals.Spent + tot.SettledTotal
	s.Totals.Income, s.Totals.Investment, s.Totals.OwnTransfer = tot.Income, tot.Investment, tot.OwnTransfer
	for _, t := range recent {
		s.Recent = append(s.Recent, recentDTO(t))
	}
	return s, nil
}

func ratio(used, budget int64) float64 {
	if budget <= 0 {
		return 0
	}
	return float64(used) / float64(budget)
}
