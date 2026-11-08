package api

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"

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
	// FailedEvents는 최근 7일 동안 읽지 못했거나(해석 실패) 처리하지 못한 알림 수다. 웹 관리 > 읽지 못한 알림에서 본다.
	FailedEvents int64     `json:"failed_events"`
	GeneratedAt  time.Time `json:"generated_at"`
}

// attentionLookback 안에 받은 알림만 실패 수에 센다(지난 실패가 영영 남지 않게).
const attentionLookback = 7 * 24 * time.Hour

// staleUnprocessed가 지나도 처리되지 않은 원문은 처리 실패로 센다(주기 재처리도 실패한 것).
const staleUnprocessed = 15 * time.Minute

// EnsureBudgets는 지금 기간에 예산이 하나도 없으면 예산이 있던 가장 최근 기간의 예산을 복사한다(이월).
// 새 기간 첫날 예산이 비어서 위젯이 '예산 없음'이 되고 예산 알림이 멈추지 않게 한다.
// 지금 기간이고 비어 있을 때만 복사하므로 여러 번 불러도 한 번만 일어난다.
func (a *API) EnsureBudgets(ctx context.Context, p period.Period) error {
	now := a.now()
	if now.Before(p.Start) || !now.Before(p.End) {
		return nil
	}
	q := a.q()
	has, err := q.HasBudgets(ctx, p.StartDate())
	if err != nil || has {
		return err
	}
	from, err := q.LatestBudgetPeriodBefore(ctx, p.StartDate())
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	n, err := q.CopyBudgets(ctx, dbq.CopyBudgetsParams{FromPeriod: from, ToPeriod: p.StartDate()})
	if err == nil && n > 0 && a.Logger != nil {
		a.Logger.Info("예산 이월", "from", from.Format("2006-01-02"), "to", p.Label, "count", n)
	}
	return err
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
	if err := a.EnsureBudgets(ctx, p); err != nil {
		return Summary{}, err
	}
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
	// received_at은 DB 시계라서 a.now(테스트용 고정 시각)가 아니라 실제 시각으로 센다.
	wall := time.Now()
	failed, err := q.CountAttentionRawEvents(ctx, dbq.CountAttentionRawEventsParams{Since: wall.Add(-attentionLookback), StaleBefore: wall.Add(-staleUnprocessed)})
	if err != nil {
		return Summary{}, err
	}
	s := Summary{Period: periodDTO(p), Categories: make([]CategoryBudget, 0, len(rows)), Recent: make([]TransactionDTO, 0, len(recent)), NeedsReview: nr, FailedEvents: failed, GeneratedAt: a.now()}
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
	a.AttachSuggestions(ctx, s.Recent)
	return s, nil
}

func ratio(used, budget int64) float64 {
	if budget <= 0 {
		return 0
	}
	return float64(used) / float64(budget)
}
