package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"

	"github.com/yuntyu01/MyHome/server/internal/cattree"
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
	// ParentID는 넣을 폴더다. 0이면 맨 위, 빼면 그대로.
	ParentID *int64  `json:"parent_id"`
	Memo     *string `json:"memo"`
}

func (a *API) createCategory(w http.ResponseWriter, r *http.Request) {
	var in categoryInput
	if err := httpx.DecodeJSON(r, &in); err != nil || in.Name == nil || strings.TrimSpace(*in.Name) == "" {
		httpx.Error(w, http.StatusBadRequest, "name is required")
		return
	}
	c, err := a.CreateCategory(r.Context(), CategoryChange{Name: in.Name, Kind: in.Kind, Sort: in.Sort, ParentID: in.ParentID, Memo: in.Memo})
	if err != nil {
		if errors.Is(err, ErrDuplicateName) {
			httpx.Error(w, http.StatusConflict, err.Error())
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
	c, err := a.UpdateCategory(r, id, CategoryChange{Name: in.Name, Sort: in.Sort, Archived: in.Archived, ParentID: in.ParentID, Memo: in.Memo})
	if err != nil {
		if errors.Is(err, ErrDuplicateName) {
			httpx.Error(w, http.StatusConflict, err.Error())
			return
		}
		a.fail(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, c)
}

// CategoryChange는 분류 만들기·고치기 입력이다. nil인 필드는 바꾸지 않는다(만들 때는 기본값).
// ParentID가 0이면 맨 위로 옮긴다. Kind는 만들 때만 쓴다(폴더 안에 만들면 폴더 종류를 따른다).
type CategoryChange struct {
	Name     *string
	Kind     *string
	Sort     *int32
	Archived *bool
	ParentID *int64
	Memo     *string
}

// ErrDuplicateName은 같은 폴더에 같은 이름이 있을 때다(HTTP 409).
var ErrDuplicateName = errors.New("같은 폴더에 같은 이름의 분류가 있어요")

func placeErr(err error) error { return fmt.Errorf("%w: %w", pipeline.ErrInvalid, err) }

func cleanMemo(m *string) (string, error) {
	if m == nil {
		return "", nil
	}
	s := strings.TrimSpace(*m)
	if utf8.RuneCountInString(s) > cattree.MaxMemo {
		return "", placeErr(fmt.Errorf("설명은 %d자까지예요", cattree.MaxMemo))
	}
	return s, nil
}

func dupErr(err error) error {
	if err != nil && strings.Contains(err.Error(), "duplicate key") {
		return ErrDuplicateName
	}
	return err
}

// CreateCategory는 분류를 만든다. 폴더(ParentID) 안에 만들면 종류는 폴더를 따르고, 3단계를 넘으면 거절한다.
func (a *API) CreateCategory(ctx context.Context, in CategoryChange) (dbq.Category, error) {
	var out dbq.Category
	name := ""
	if in.Name != nil {
		name = strings.TrimSpace(*in.Name)
	}
	if name == "" {
		return out, placeErr(errors.New("이름을 넣어 주세요"))
	}
	memo, err := cleanMemo(in.Memo)
	if err != nil {
		return out, err
	}
	err = db.InTx(ctx, a.Pool, func(tx pgx.Tx) error {
		q := dbq.New(tx)
		cats, err := q.ListCategories(ctx)
		if err != nil {
			return err
		}
		t := cattree.New(cats)
		kind := "expense"
		if in.Kind != nil {
			kind = *in.Kind
		}
		var parent *int64
		if in.ParentID != nil && *in.ParentID != 0 {
			pid := *in.ParentID
			if pc, ok := t.ByID[pid]; ok && in.Kind == nil {
				kind = pc.Kind
			}
			parent = &pid
		}
		if kind != "expense" && kind != "income" {
			return placeErr(errors.New("kind는 expense 또는 income"))
		}
		if parent != nil {
			if err := t.CheckPlace(0, kind, *parent); err != nil {
				return placeErr(err)
			}
		}
		var sort int32 = 500
		if in.Sort != nil {
			sort = *in.Sort
		}
		out, err = q.CreateCategory(ctx, dbq.CreateCategoryParams{Name: name, Kind: kind, Sort: sort, ParentID: parent, Memo: memo})
		return dupErr(err)
	})
	return out, err
}

// UpdateCategory는 분류 이름·순서·설명·폴더·숨김을 바꾼다. 특수 분류는 숨기거나 폴더에 넣을 수 없다.
// 폴더를 숨기면 안의 분류도 숨기고, 되살리면 안의 분류와 위 폴더도 되살린다(지난 거래는 그대로 남는다).
func (a *API) UpdateCategory(r *http.Request, id int64, in CategoryChange) (dbq.Category, error) {
	ctx := r.Context()
	var out dbq.Category
	err := db.InTx(ctx, a.Pool, func(tx pgx.Tx) error {
		q := dbq.New(tx)
		cats, err := q.ListCategories(ctx)
		if err != nil {
			return err
		}
		t := cattree.New(cats)
		c, ok := t.ByID[id]
		if !ok {
			return pgx.ErrNoRows
		}
		p := dbq.UpdateCategoryParams{ID: id, Name: c.Name, Sort: c.Sort, Archived: c.Archived, ParentID: c.ParentID, Memo: c.Memo}
		if in.Name != nil && strings.TrimSpace(*in.Name) != "" {
			p.Name = strings.TrimSpace(*in.Name)
		}
		if in.Sort != nil {
			p.Sort = *in.Sort
		}
		if in.Memo != nil {
			if p.Memo, err = cleanMemo(in.Memo); err != nil {
				return err
			}
		}
		// 폴더가 그대로면 검사하지 않는다(이름·설명만 고칠 때 위치 검사로 막히지 않게)
		if in.ParentID != nil && *in.ParentID != t.Parent(id) {
			if err := t.CheckPlace(id, c.Kind, *in.ParentID); err != nil {
				return placeErr(err)
			}
			p.ParentID = nil
			if *in.ParentID != 0 {
				pid := *in.ParentID
				p.ParentID = &pid
			}
		}
		if in.Archived != nil {
			if *in.Archived && c.Code != nil {
				return placeErr(errors.New("이 분류는 계산에 쓰여서 지울 수 없어요"))
			}
			p.Archived = *in.Archived
		}
		if out, err = q.UpdateCategory(ctx, p); err != nil {
			return dupErr(err)
		}
		if in.Archived != nil && *in.Archived != c.Archived {
			ids := t.Descendants(id) // 숨기면 아래까지, 되살리면 아래와 위 폴더까지
			if !*in.Archived && p.ParentID != nil {
				ids = append(ids, t.Ancestors(id)...)
			}
			if len(ids) > 0 {
				return q.SetCategoriesArchived(ctx, dbq.SetCategoriesArchivedParams{Archived: *in.Archived, Ids: ids})
			}
		}
		return nil
	})
	return out, err
}

// CategoryMove는 분류를 폴더(ParentID, 0 = 맨 위) 안의 정한 자리로 옮기는 입력이다.
// Before가 있으면 그 분류 바로 앞, After가 있으면 바로 뒤, 둘 다 없으면 맨 끝이다(앱에서 꾹 눌러 끌기).
type CategoryMove struct {
	ParentID int64 `json:"parent_id"`
	Before   int64 `json:"before"`
	After    int64 `json:"after"`
}

func (a *API) moveCategory(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		httpx.Error(w, http.StatusBadRequest, "invalid id")
		return
	}
	var in CategoryMove
	if err := httpx.DecodeJSON(r, &in); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid json")
		return
	}
	c, err := a.MoveCategory(r.Context(), id, in)
	if err != nil {
		if errors.Is(err, ErrDuplicateName) {
			httpx.Error(w, http.StatusConflict, err.Error())
			return
		}
		a.fail(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, c)
}

// MoveCategory는 분류(폴더면 안까지 통째로)를 옮기고, 들어간 폴더의 형제 순서를 10, 20, 30…으로 다시 매긴다.
// 특수 분류는 옮기지 않고 순서 매기기에서도 뺀다(계산 규칙에 묶여 있다). 폴더 규칙은 CheckPlace와 같다.
func (a *API) MoveCategory(ctx context.Context, id int64, mv CategoryMove) (dbq.Category, error) {
	var out dbq.Category
	if mv.Before != 0 && mv.After != 0 {
		return out, placeErr(errors.New("before와 after 중 하나만 주세요"))
	}
	err := db.InTx(ctx, a.Pool, func(tx pgx.Tx) error {
		q := dbq.New(tx)
		cats, err := q.ListCategories(ctx)
		if err != nil {
			return err
		}
		t := cattree.New(cats)
		c, ok := t.ByID[id]
		if !ok {
			return pgx.ErrNoRows
		}
		if c.Code != nil {
			return placeErr(cattree.ErrSpecial)
		}
		if mv.ParentID != t.Parent(id) {
			if err := t.CheckPlace(id, c.Kind, mv.ParentID); err != nil {
				return placeErr(err)
			}
		}
		var sibs []int64
		for _, s := range t.Children(mv.ParentID) {
			if s != id && t.ByID[s].Code == nil {
				sibs = append(sibs, s)
			}
		}
		at := len(sibs)
		if anchor := max(mv.Before, mv.After); anchor != 0 {
			at = -1
			for i, s := range sibs {
				if s == anchor {
					at = i
				}
			}
			if at < 0 {
				return placeErr(errors.New("옮길 자리를 찾지 못했어요"))
			}
			if mv.After != 0 {
				at++
			}
		}
		order := append(append(append([]int64(nil), sibs[:at]...), id), sibs[at:]...)
		var parent *int64
		if mv.ParentID != 0 {
			p := mv.ParentID
			parent = &p
		}
		for i, cid := range order {
			if err := q.PlaceCategory(ctx, dbq.PlaceCategoryParams{ID: cid, ParentID: parent, Sort: int32((i + 1) * 10)}); err != nil {
				return dupErr(err)
			}
		}
		out, err = q.GetCategory(ctx, id)
		return err
	})
	return out, err
}

// archiveCategory: 데이터를 지우지 않기 위해 DELETE는 보관(archived)으로 처리한다.
func (a *API) archiveCategory(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		httpx.Error(w, http.StatusBadRequest, "invalid id")
		return
	}
	t := true
	c, err := a.UpdateCategory(r, id, CategoryChange{Archived: &t})
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
	if err := a.EnsureBudgets(r.Context(), p); err != nil {
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
