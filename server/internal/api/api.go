// Package api는 앱·웹이 쓰는 JSON API(/v1/*)를 담는다.
package api

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/yuntyu01/MyHome/server/internal/dbq"
	"github.com/yuntyu01/MyHome/server/internal/httpx"
	"github.com/yuntyu01/MyHome/server/internal/period"
	"github.com/yuntyu01/MyHome/server/internal/pipeline"
)

// API는 JSON API 핸들러 묶음이다.
type API struct {
	Pool      *pgxpool.Pool
	Editor    *pipeline.Editor
	Processor *pipeline.Processor
	Logger    *slog.Logger
	Now       func() time.Time
	// Suggestions가 true면 거래 목록에 AI 분류 추천(suggestion)을 붙인다(AI_CLASSIFY). false면 늘 null.
	Suggestions bool
}

func (a *API) now() time.Time {
	if a.Now != nil {
		return a.Now()
	}
	return time.Now()
}

// Register는 라우트를 등록한다. wrap은 인증 미들웨어다.
func (a *API) Register(mux *http.ServeMux, wrap func(http.Handler) http.Handler) {
	h := func(pattern string, fn http.HandlerFunc) { mux.Handle(pattern, wrap(fn)) }
	h("GET /v1/summary", a.getSummary)
	h("GET /v1/transactions", a.listTransactions)
	h("POST /v1/transactions", a.createTransaction)
	h("PATCH /v1/transactions/{id}", a.patchTransaction)
	h("GET /v1/review", a.listReview)
	h("GET /v1/categories", a.listCategories)
	h("POST /v1/categories", a.createCategory)
	h("PATCH /v1/categories/{id}", a.patchCategory)
	h("DELETE /v1/categories/{id}", a.archiveCategory)
	h("POST /v1/categories/{id}/move", a.moveCategory)
	h("GET /v1/budgets", a.getBudgets)
	h("PUT /v1/budgets", a.putBudgets)
	h("PUT /v1/budgets/total", a.putTotalBudget)
	h("GET /v1/fixed", a.listFixed)
	h("POST /v1/fixed", a.createFixed)
	h("PATCH /v1/fixed/{id}", a.patchFixed)
	h("DELETE /v1/fixed/{id}", a.archiveFixed)
	h("POST /v1/budgets/copy-previous", a.copyBudgets)
	h("GET /v1/rules", a.listRules)
	h("POST /v1/rules", a.createRule)
	h("DELETE /v1/rules/{id}", a.deleteRule)
	h("GET /v1/settings", a.listSettings)
	h("PUT /v1/settings/{key}", a.putSetting)
	h("GET /v1/raw-events", a.listRawEvents)
	h("GET /v1/analytics", a.getAnalytics)
	h("GET /v1/reports/monthly", a.getMonthlyReport)
	h("GET /v1/reconcile", a.getReconcile)
}

func (a *API) q() *dbq.Queries { return dbq.New(a.Pool) }

// Q는 쿼리 객체다(웹 핸들러용).
func (a *API) Q() *dbq.Queries { return a.q() }

// fail은 에러 종류에 맞는 HTTP 응답을 쓴다.
func (a *API) fail(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, pipeline.ErrInvalid):
		httpx.Error(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, pgx.ErrNoRows):
		httpx.Error(w, http.StatusNotFound, "not found")
	default:
		a.Logger.Error("API 오류", "err", err)
		httpx.Error(w, http.StatusInternalServerError, "internal error")
	}
}

func pathID(r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	return id, err == nil && id > 0
}

func queryInt(r *http.Request, key string, def, max int) int {
	n, err := strconv.Atoi(r.URL.Query().Get(key))
	if err != nil || n <= 0 {
		return def
	}
	if n > max {
		return max
	}
	return n
}

// PeriodFor는 ?month=YYYY-MM(기본: 이번 기간)을 읽는다.
func (a *API) PeriodFor(r *http.Request) (period.Period, pipeline.Config, error) {
	return a.periodFor(r)
}

// periodFor는 ?month=YYYY-MM(기본: 이번 기간)을 읽는다.
func (a *API) periodFor(r *http.Request) (period.Period, pipeline.Config, error) {
	cfg, err := pipeline.LoadConfig(r.Context(), a.q())
	if err != nil {
		return period.Period{}, cfg, err
	}
	p, err := period.Parse(r.URL.Query().Get("month"), cfg.BudgetStartDay, a.now())
	if err != nil {
		return p, cfg, errors.Join(pipeline.ErrInvalid, err)
	}
	return p, cfg, nil
}

// PeriodDTO는 기간 표시용이다(날짜는 KST).
// Label은 "2026-11", Start/End는 YYYY-MM-DD.
type PeriodDTO struct {
	Label string `json:"label"`
	Start string `json:"start"`
	End   string `json:"end"` // 마지막 날(포함)
}

func periodDTO(p period.Period) PeriodDTO {
	return PeriodDTO{Label: p.Label, Start: p.Start.Format("2006-01-02"), End: p.LastDay().Format("2006-01-02")}
}
