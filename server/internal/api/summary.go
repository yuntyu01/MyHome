package api

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/yuntyu01/MyHome/server/internal/cattree"
	"github.com/yuntyu01/MyHome/server/internal/dbq"
	"github.com/yuntyu01/MyHome/server/internal/httpx"
	"github.com/yuntyu01/MyHome/server/internal/period"
)

// CategoryBudget은 분류별 예산 현황이다. 폴더는 아래 분류까지 더한 값이다(docs/DECISIONS.md 「분류 폴더」).
// 목록은 위에서 아래로 펼친 순서(폴더 다음에 그 안)이고, 맨 위 분류(ParentID 없음)만 더하면 총계가 된다.
type CategoryBudget struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	ParentID *int64 `json:"parent_id"`
	Depth    int    `json:"depth"` // 맨 위 = 0
	Path     string `json:"path"`  // "생활비 › 식비"
	// OwnBudget은 이 분류에 직접 정한 예산이다. Budget은 실제로 쓰는 예산이다:
	// 직접 정했으면 그 값, 안 정한 폴더면 아래 분류 예산의 합.
	OwnBudget int64   `json:"own_budget"`
	Budget    int64   `json:"budget"`
	Spent     int64   `json:"spent"`
	Settled   int64   `json:"settled"`
	Remaining int64   `json:"remaining"`
	Ratio     float64 `json:"ratio"` // (지출-정산)/예산. 예산이 0이면 0. 표시용
	// InBudget이 false면 "예산 없음"(자기나 위 폴더): 한 달 예산·쓴 돈 합계에 안 든다. true이고 예산 0이면 "자유"
	InBudget bool `json:"in_budget"`
}

// Totals는 기간 총계다.
type Totals struct {
	// Budget은 이 기간 한 달 예산이다: 한 달 전체 예산을 정했으면 그 값, 아니면 맨 위 분류 예산 합계.
	Budget int64 `json:"budget"`
	// CategoryBudget은 맨 위 분류 예산 합계, OwnBudget은 정한 한 달 전체 예산(안 정했으면 null).
	CategoryBudget int64  `json:"category_budget"`
	OwnBudget      *int64 `json:"own_budget"`
	Spent          int64  `json:"spent"`
	Settled        int64  `json:"settled"`
	Remaining      int64  `json:"remaining"`
	Income         int64  `json:"income"`
	Investment     int64  `json:"investment"`
	OwnTransfer    int64  `json:"own_transfer"`
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
	s := Summary{Period: periodDTO(p), Categories: rollUp(rows), Recent: make([]TransactionDTO, 0, len(recent)), NeedsReview: nr, FailedEvents: failed, GeneratedAt: a.now()}
	// 한 달 예산·쓴 돈: "예산 없음" 분류(그 폴더 아래 포함)는 빼고 센다. 자유(예산 0)는 쓴 돈만 센다
	var outSettled int64
	for _, c := range s.Categories {
		if c.ParentID == nil && c.InBudget {
			s.Totals.Budget += c.Budget
		}
	}
	for _, r := range rows {
		if inBudget(rows, r.ID) {
			s.Totals.Spent += r.Spent
		} else {
			outSettled += r.Settled
		}
	}
	s.Totals.CategoryBudget = s.Totals.Budget
	own, err := q.TotalBudgetAt(ctx, p.StartDate())
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return Summary{}, err
	}
	if err == nil && own != nil {
		s.Totals.Budget, s.Totals.OwnBudget = *own, own
	}
	s.Totals.Settled = tot.SettledTotal - outSettled
	s.Totals.Remaining = s.Totals.Budget - s.Totals.Spent + tot.SettledTotal
	s.Totals.Income, s.Totals.Investment, s.Totals.OwnTransfer = tot.Income, tot.Investment, tot.OwnTransfer
	for _, t := range recent {
		s.Recent = append(s.Recent, recentDTO(t))
	}
	a.AttachSuggestions(ctx, s.Recent)
	return s, nil
}

// inBudget은 분류가 한 달 예산에 드는지다: 자기와 위 폴더가 모두 in_budget이어야 한다(폴더를 "예산 없음"으로 하면 아래도).
func inBudget(rows []dbq.CategorySummaryRow, id int64) bool {
	by := make(map[int64]dbq.CategorySummaryRow, len(rows))
	for _, r := range rows {
		by[r.ID] = r
	}
	for steps := 0; steps < 8; steps++ {
		r, ok := by[id]
		if !ok {
			return true
		}
		if !r.InBudget {
			return false
		}
		if r.ParentID == nil {
			return true
		}
		id = *r.ParentID
	}
	return true
}

func ratio(used, budget int64) float64 {
	if budget <= 0 {
		return 0
	}
	return float64(used) / float64(budget)
}

// rollUp은 분류 하나씩의 값을 폴더 합계로 바꾼다. 쓴 돈·정산은 아래를 모두 더하고, 예산은 직접 정했으면 그 값,
// 아니면 아래 예산의 합이다(위 폴더 예산은 아래의 한도를 묶는 큰 한도). 숨긴 분류는 그 아래까지 값이 없으면 뺀다.
func rollUp(rows []dbq.CategorySummaryRow) []CategoryBudget {
	cats := make([]dbq.Category, len(rows))
	own := make(map[int64]dbq.CategorySummaryRow, len(rows))
	for i, r := range rows {
		cats[i] = dbq.Category{ID: r.ID, Name: r.Name, Kind: "expense", Sort: r.Sort, ParentID: r.ParentID, Archived: r.Archived}
		own[r.ID] = r
	}
	t := cattree.New(cats)
	type sums struct{ budget, spent, settled int64 }
	memo := map[int64]sums{}
	var roll func(id int64) sums
	roll = func(id int64) sums {
		var sub sums
		for _, ch := range t.Children(id) {
			x := roll(ch)
			sub.budget += x.budget
			sub.spent += x.spent
			sub.settled += x.settled
		}
		r := own[id]
		out := sums{budget: sub.budget, spent: r.Spent + sub.spent, settled: r.Settled + sub.settled}
		if r.Budget > 0 {
			out.budget = r.Budget
		}
		memo[id] = out
		return out
	}
	for _, id := range t.Children(0) {
		roll(id)
	}
	out := make([]CategoryBudget, 0, len(rows))
	for _, c := range t.Preorder() {
		x := memo[c.ID]
		if c.Archived && x.budget == 0 && x.spent == 0 && x.settled == 0 {
			continue
		}
		var parent *int64
		if pid := t.Parent(c.ID); pid != 0 {
			parent = &pid
		}
		out = append(out, CategoryBudget{
			ID: c.ID, Name: c.Name, ParentID: parent, Depth: t.Depth(c.ID), Path: t.Label(c.ID),
			OwnBudget: own[c.ID].Budget, Budget: x.budget, Spent: x.spent, Settled: x.settled, InBudget: inBudget(rows, c.ID),
			Remaining: x.budget - x.spent + x.settled, Ratio: ratio(x.spent-x.settled, x.budget),
		})
	}
	return out
}
