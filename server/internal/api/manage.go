package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/yuntyu01/MyHome/server/internal/classify"
	"github.com/yuntyu01/MyHome/server/internal/db"
	"github.com/yuntyu01/MyHome/server/internal/dbq"
	"github.com/yuntyu01/MyHome/server/internal/httpx"
	"github.com/yuntyu01/MyHome/server/internal/pipeline"
)

// ---- 분류 ----

func (a *API) listCategories(w http.ResponseWriter, r *http.Request) {
	cats, err := a.q().ListCategories(r.Context())
	if err != nil {
		a.fail(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"categories": cats})
}

type categoryInput struct {
	Name     *string `json:"name"`
	Kind     *string `json:"kind"`
	Sort     *int32  `json:"sort"`
	Archived *bool   `json:"archived"`
}

func (a *API) createCategory(w http.ResponseWriter, r *http.Request) {
	var in categoryInput
	if err := httpx.DecodeJSON(r, &in); err != nil || in.Name == nil || strings.TrimSpace(*in.Name) == "" {
		httpx.Error(w, http.StatusBadRequest, "name is required")
		return
	}
	kind := "expense"
	if in.Kind != nil {
		kind = *in.Kind
	}
	if kind != "expense" && kind != "income" {
		httpx.Error(w, http.StatusBadRequest, "kind는 expense 또는 income")
		return
	}
	var sort int32 = 500
	if in.Sort != nil {
		sort = *in.Sort
	}
	c, err := a.q().CreateCategory(r.Context(), dbq.CreateCategoryParams{Name: strings.TrimSpace(*in.Name), Kind: kind, Sort: sort})
	if err != nil {
		if strings.Contains(err.Error(), "duplicate key") {
			httpx.Error(w, http.StatusConflict, "같은 이름의 분류가 있습니다")
			return
		}
		a.fail(w, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, c)
}

func (a *API) patchCategory(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		httpx.Error(w, http.StatusBadRequest, "invalid id")
		return
	}
	var in categoryInput
	if err := httpx.DecodeJSON(r, &in); err != nil || in.Kind != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid json (kind는 바꿀 수 없음)")
		return
	}
	c, err := a.UpdateCategory(r, id, in.Name, in.Sort, in.Archived)
	if err != nil {
		a.fail(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, c)
}

// UpdateCategory는 분류 이름·순서·보관 여부를 바꾼다. 특수 분류는 보관할 수 없다.
func (a *API) UpdateCategory(r *http.Request, id int64, name *string, sort *int32, archived *bool) (dbq.Category, error) {
	q := a.q()
	c, err := q.GetCategory(r.Context(), id)
	if err != nil {
		return c, err
	}
	p := dbq.UpdateCategoryParams{ID: id, Name: c.Name, Sort: c.Sort, Archived: c.Archived}
	if name != nil && strings.TrimSpace(*name) != "" {
		p.Name = strings.TrimSpace(*name)
	}
	if sort != nil {
		p.Sort = *sort
	}
	if archived != nil {
		if *archived && c.Code != nil {
			return c, pipeline.ErrInvalid
		}
		p.Archived = *archived
	}
	return q.UpdateCategory(r.Context(), p)
}

// archiveCategory: 데이터를 지우지 않기 위해 DELETE는 보관(archived)으로 처리한다.
func (a *API) archiveCategory(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		httpx.Error(w, http.StatusBadRequest, "invalid id")
		return
	}
	t := true
	c, err := a.UpdateCategory(r, id, nil, nil, &t)
	if err != nil {
		a.fail(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, c)
}

// ---- 예산 ----

// BudgetItem은 분류별 예산 한 줄이다.
type BudgetItem struct {
	CategoryID int64  `json:"category_id"`
	Category   string `json:"category,omitempty"`
	Amount     int64  `json:"amount"`
}

func (a *API) getBudgets(w http.ResponseWriter, r *http.Request) {
	p, _, err := a.periodFor(r)
	if err != nil {
		a.fail(w, err)
		return
	}
	rows, err := a.q().ListBudgets(r.Context(), p.StartDate())
	if err != nil {
		a.fail(w, err)
		return
	}
	items := make([]BudgetItem, 0, len(rows))
	for _, b := range rows {
		items = append(items, BudgetItem{CategoryID: b.CategoryID, Category: b.CategoryName, Amount: b.Amount})
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"period": periodDTO(p), "items": items})
}

type budgetsInput struct {
	Month string       `json:"month"`
	Items []BudgetItem `json:"items"`
}

func (a *API) putBudgets(w http.ResponseWriter, r *http.Request) {
	var in budgetsInput
	if err := httpx.DecodeJSON(r, &in); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid json")
		return
	}
	r.URL.RawQuery = "month=" + in.Month
	p, _, err := a.periodFor(r)
	if err != nil {
		a.fail(w, err)
		return
	}
	if err := a.SaveBudgets(r.Context(), p.StartDate(), in.Items); err != nil {
		a.fail(w, err)
		return
	}
	a.getBudgets(w, r)
}

// SaveBudgets는 기간 예산을 한 번에 저장한다. 지출 분류만, 0원 이상만 허용한다.
func (a *API) SaveBudgets(ctx context.Context, periodStart time.Time, items []BudgetItem) error {
	return db.InTx(ctx, a.Pool, func(tx pgx.Tx) error {
		q := dbq.New(tx)
		for _, it := range items {
			c, err := q.GetCategory(ctx, it.CategoryID)
			if err != nil || c.Kind != "expense" || it.Amount < 0 {
				return pipeline.ErrInvalid
			}
			if err := q.UpsertBudget(ctx, dbq.UpsertBudgetParams{PeriodStart: periodStart, CategoryID: it.CategoryID, Amount: it.Amount}); err != nil {
				return err
			}
		}
		return nil
	})
}

func (a *API) copyBudgets(w http.ResponseWriter, r *http.Request) {
	p, cfg, err := a.periodFor(r)
	if err != nil {
		a.fail(w, err)
		return
	}
	n, err := a.q().CopyBudgets(r.Context(), dbq.CopyBudgetsParams{FromPeriod: p.Prev(cfg.BudgetStartDay).StartDate(), ToPeriod: p.StartDate()})
	if err != nil {
		a.fail(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"copied": n, "period": periodDTO(p)})
}

// ---- 규칙 ----

func (a *API) listRules(w http.ResponseWriter, r *http.Request) {
	rules, err := a.q().ListRules(r.Context())
	if err != nil {
		a.fail(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"rules": rules})
}

// RuleInput은 규칙 생성 요청이다.
type RuleInput struct {
	Pattern    string  `json:"pattern"`
	MatchType  string  `json:"match_type"`
	CategoryID int64   `json:"category_id"`
	Flow       *string `json:"flow"`
	Priority   *int32  `json:"priority"`
}

func (a *API) createRule(w http.ResponseWriter, r *http.Request) {
	var in RuleInput
	if err := httpx.DecodeJSON(r, &in); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid json")
		return
	}
	rule, err := a.CreateRule(r, in)
	if err != nil {
		a.fail(w, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, rule)
}

// CreateRule은 수동 규칙을 만든다(같은 패턴이 있으면 덮어쓴다).
func (a *API) CreateRule(r *http.Request, in RuleInput) (dbq.MerchantRule, error) {
	in.Pattern = strings.TrimSpace(in.Pattern)
	if in.MatchType == "" {
		in.MatchType = "contains"
	}
	if !classify.ValidPattern(in.MatchType, in.Pattern) {
		return dbq.MerchantRule{}, pipeline.ErrInvalid
	}
	if in.Flow != nil && *in.Flow != "out" && *in.Flow != "in" {
		if *in.Flow == "" {
			in.Flow = nil
		} else {
			return dbq.MerchantRule{}, pipeline.ErrInvalid
		}
	}
	if _, err := a.q().GetCategory(r.Context(), in.CategoryID); err != nil {
		return dbq.MerchantRule{}, pipeline.ErrInvalid
	}
	var prio int32 = 50
	if in.Priority != nil {
		prio = *in.Priority
	}
	return a.q().UpsertRule(r.Context(), dbq.UpsertRuleParams{Pattern: in.Pattern, MatchType: in.MatchType, CategoryID: in.CategoryID, Flow: in.Flow, Priority: prio, CreatedFrom: "manual"})
}

func (a *API) deleteRule(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		httpx.Error(w, http.StatusBadRequest, "invalid id")
		return
	}
	n, err := a.q().DeleteRule(r.Context(), id)
	if err != nil {
		a.fail(w, err)
		return
	}
	if n == 0 {
		httpx.Error(w, http.StatusNotFound, "not found")
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"deleted": id})
}

// ---- 설정 ----

func (a *API) listSettings(w http.ResponseWriter, r *http.Request) {
	rows, err := a.q().ListSettings(r.Context())
	if err != nil {
		a.fail(w, err)
		return
	}
	out := map[string]json.RawMessage{}
	for _, s := range rows {
		out[s.Key] = json.RawMessage(s.Value)
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"settings": out})
}

func (a *API) putSetting(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("key")
	var raw json.RawMessage
	if err := httpx.DecodeJSON(r, &raw); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid json")
		return
	}
	if err := a.SaveSetting(r, key, raw); err != nil {
		a.fail(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"key": key, "value": raw})
}

// SaveSetting은 알려진 설정 키만 검사 후 저장한다.
func (a *API) SaveSetting(r *http.Request, key string, raw json.RawMessage) error {
	if !pipeline.SettingKeys[key] || !pipeline.ValidateSetting(key, raw) {
		return pipeline.ErrInvalid
	}
	return a.q().UpsertSetting(r.Context(), dbq.UpsertSettingParams{Key: key, Value: raw})
}

// ---- 원문 ----

// RawEventDTO는 보정용 원문 표현이다(인증된 본인만 볼 수 있다).
type RawEventDTO struct {
	ID            int64   `json:"id"`
	SourcePackage string  `json:"source_package"`
	Title         string  `json:"title"`
	Text          string  `json:"text"`
	BigText       string  `json:"big_text"`
	PostedAt      string  `json:"posted_at"`
	ParseError    *string `json:"parse_error"`
}

func (a *API) listRawEvents(w http.ResponseWriter, r *http.Request) {
	limit := int32(queryInt(r, "limit", 100, 500))
	var rows []dbq.RawEvent
	var err error
	if r.URL.Query().Get("failed") == "true" {
		rows, err = a.q().ListFailedRawEvents(r.Context(), limit)
	} else {
		rows, err = a.q().ListRawEvents(r.Context(), limit)
	}
	if err != nil {
		a.fail(w, err)
		return
	}
	out := make([]RawEventDTO, 0, len(rows))
	for _, e := range rows {
		out = append(out, RawEventDTO{ID: e.ID, SourcePackage: e.SourcePackage, Title: e.Title, Text: e.Text, BigText: e.BigText, PostedAt: e.PostedAt.Format("2006-01-02T15:04:05Z07:00"), ParseError: e.ParseError})
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"raw_events": out})
}
