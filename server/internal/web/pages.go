package web

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/yuntyu01/MyHome/server/internal/api"
	"github.com/yuntyu01/MyHome/server/internal/dbq"
	"github.com/yuntyu01/MyHome/server/internal/period"
	"github.com/yuntyu01/MyHome/server/internal/pipeline"
)

// ---- 로그인 ----

func (wb *Web) login(w http.ResponseWriter, r *http.Request) {
	if u, ok, _ := wb.Auth.Authenticate(r); ok && u.Session != nil {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = wb.pages["login"].ExecuteTemplate(w, "login", map[string]any{"DevLogin": wb.DevLogin, "AssetVer": assetVersion})
}

// ---- 대시보드 ----

type dashboardData struct {
	Summary api.Summary
}

func (wb *Web) dashboard(w http.ResponseWriter, r *http.Request) {
	p, cfg, err := wb.API.PeriodFor(r)
	if err != nil {
		wb.fail(w, err)
		return
	}
	s, err := wb.API.BuildSummary(r, p, 10)
	if err != nil {
		wb.fail(w, err)
		return
	}
	pd := wb.base(r, "내 가계부", "dashboard")
	wb.withPeriod(&pd, p, cfg.BudgetStartDay)
	pd.Data = dashboardData{Summary: s}
	wb.render(w, "dashboard", pd)
}

// ---- 거래 목록 ----

type txPageData struct {
	Transactions []api.TransactionDTO
	Cats         categoryGroups
	Q            string
	Status       string
	CategoryID   int64
	From, To     string
	NextOffset   int
	HasMore      bool
}

type txRowData struct {
	T    api.TransactionDTO
	Cats categoryGroups
	CSRF string
}

func (wb *Web) transactions(w http.ResponseWriter, r *http.Request) {
	f, err := wb.API.ParseTransactionFilter(r)
	if err != nil {
		wb.fail(w, err)
		return
	}
	f.Limit = 200
	txs, err := wb.API.ListTransactions(r, f)
	if err != nil {
		wb.fail(w, err)
		return
	}
	cats, err := wb.categories(r, true)
	if err != nil {
		wb.fail(w, err)
		return
	}
	p, cfg, err := wb.API.PeriodFor(r)
	if err != nil {
		wb.fail(w, err)
		return
	}
	v := r.URL.Query()
	d := txPageData{Transactions: txs, Cats: cats, Q: v.Get("q"), Status: v.Get("status"), From: v.Get("from"), To: v.Get("to"),
		HasMore: len(txs) == f.Limit, NextOffset: f.Offset + f.Limit}
	d.CategoryID, _ = strconv.ParseInt(v.Get("category_id"), 10, 64)
	pd := wb.base(r, "거래", "transactions")
	wb.withPeriod(&pd, p, cfg.BudgetStartDay)
	pd.Data = d
	wb.render(w, "transactions", pd)
}

// editFromForm은 폼 값을 거래 수정 요청으로 바꾼다.
func editFromForm(r *http.Request) pipeline.Edit {
	var e pipeline.Edit
	if v := r.PostFormValue("category_id"); v != "" {
		if id, err := strconv.ParseInt(v, 10, 64); err == nil && id > 0 {
			e.CategoryID = &id
		}
	}
	if _, ok := r.PostForm["memo"]; ok {
		m := r.PostFormValue("memo")
		e.Memo = &m
	}
	if v := r.PostFormValue("settle_category_id"); v != "" {
		if id, err := strconv.ParseInt(v, 10, 64); err == nil {
			e.SettleCategoryID = &id
		}
	}
	if s := r.PostFormValue("status"); s == "excluded" || s == "confirmed" {
		e.Status = &s
	}
	e.RememberRule = r.PostFormValue("remember") == "on"
	return e
}

// updateTransaction은 거래 목록의 한 줄을 수정하고 그 줄만 다시 그린다(htmx).
func (wb *Web) updateTransaction(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok || r.ParseForm() != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	res, err := wb.API.Editor.EditTransaction(r.Context(), id, editFromForm(r))
	if err != nil {
		wb.fail(w, err)
		return
	}
	cats, err := wb.categories(r, true)
	if err != nil {
		wb.fail(w, err)
		return
	}
	t := res.Transaction
	name := ""
	if t.CategoryID != nil {
		name = cats.ByID[*t.CategoryID].Name
	}
	pd := wb.base(r, "", "")
	if res.AlsoUpdated > 0 {
		w.Header().Set("HX-Trigger", `{"toast":"같은 가맹점 `+strconv.Itoa(res.AlsoUpdated)+`건도 함께 분류했어요"}`)
	}
	wb.partial(w, "transactions", "tx-row", txRowData{T: dtoFromTx(t, name), Cats: cats, CSRF: pd.CSRF})
}

func dtoFromTx(t dbq.Transaction, catName string) api.TransactionDTO {
	return api.TransactionDTO{
		ID: t.ID, OccurredAt: t.OccurredAt.In(period.KST), Amount: t.Amount, Flow: t.Flow, Merchant: t.Merchant, Memo: t.Memo,
		Direction: t.Direction, CategoryID: t.CategoryID, Category: catName, SettleCategoryID: t.SettleCategoryID,
		Status: t.Status, ExclusionReason: t.ExclusionReason, ReviewReason: t.ReviewReason, MergedInto: t.MergedInto,
		CancelsID: t.CancelsID, Source: t.Source, Kind: t.Kind, Installment: t.InstallmentMonths,
		ManuallyEdited: t.ManuallyEdited, RawEventID: t.RawEventID,
	}
}

// ---- 확인 필요 큐 ----

type reviewData struct {
	Item  *api.TransactionDTO
	Left  int
	Cats  categoryGroups
	CSRF  string
	Flash string
}

func (wb *Web) nextReview(r *http.Request, flash string) (reviewData, error) {
	rows, err := wb.API.Q().ListNeedsReview(r.Context(), 500)
	if err != nil {
		return reviewData{}, err
	}
	cats, err := wb.categories(r, false)
	if err != nil {
		return reviewData{}, err
	}
	pd := wb.base(r, "", "")
	d := reviewData{Left: len(rows), Cats: cats, CSRF: pd.CSRF, Flash: flash}
	if len(rows) > 0 {
		// 오래된 것부터 처리
		last := rows[len(rows)-1]
		t := dbq.Transaction{ID: last.ID, RawEventID: last.RawEventID, OccurredAt: last.OccurredAt, Amount: last.Amount, Flow: last.Flow,
			Merchant: last.Merchant, Memo: last.Memo, Direction: last.Direction, CategoryID: last.CategoryID, Status: last.Status,
			ReviewReason: last.ReviewReason, Source: last.Source, Kind: last.Kind, CancelsID: last.CancelsID, InstallmentMonths: last.InstallmentMonths}
		name := ""
		if last.CategoryName != nil {
			name = *last.CategoryName
		}
		dto := dtoFromTx(t, name)
		d.Item = &dto
	}
	return d, nil
}

func (wb *Web) review(w http.ResponseWriter, r *http.Request) {
	d, err := wb.nextReview(r, "")
	if err != nil {
		wb.fail(w, err)
		return
	}
	pd := wb.base(r, "확인 필요", "review")
	pd.Data = d
	wb.render(w, "review", pd)
}

// classifyReview는 한 건을 분류하고 다음 건을 돌려준다(htmx).
func (wb *Web) classifyReview(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok || r.ParseForm() != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	e := editFromForm(r)
	if r.PostFormValue("action") == "exclude" {
		s := "excluded"
		e = pipeline.Edit{Status: &s}
	}
	res, err := wb.API.Editor.EditTransaction(r.Context(), id, e)
	if err != nil {
		wb.fail(w, err)
		return
	}
	flash := "분류했어요."
	if e.Status != nil && *e.Status == "excluded" {
		flash = "제외했어요."
	}
	if res.RuleID != nil {
		flash += " 다음부터 자동으로 분류해요."
	}
	if res.AlsoUpdated > 0 {
		flash += " 같은 가맹점 " + strconv.Itoa(res.AlsoUpdated) + "건도 함께 분류했어요."
	}
	d, err := wb.nextReview(r, flash)
	if err != nil {
		wb.fail(w, err)
		return
	}
	wb.partial(w, "review", "review-item", d)
}

// ---- 예산 ----

type budgetRow struct {
	Category dbq.Category
	Amount   int64
	Spent    int64
	Previous int64
}

type budgetsData struct {
	Rows     []budgetRow
	Total    int64
	StartDay int
}

func (wb *Web) budgets(w http.ResponseWriter, r *http.Request) {
	p, cfg, err := wb.API.PeriodFor(r)
	if err != nil {
		wb.fail(w, err)
		return
	}
	q := wb.API.Q()
	cur, err := q.ListBudgets(r.Context(), p.StartDate())
	if err != nil {
		wb.fail(w, err)
		return
	}
	prev, err := q.ListBudgets(r.Context(), p.Prev(cfg.BudgetStartDay).StartDate())
	if err != nil {
		wb.fail(w, err)
		return
	}
	s, err := wb.API.BuildSummary(r, p, 0)
	if err != nil {
		wb.fail(w, err)
		return
	}
	cats, err := wb.categories(r, false)
	if err != nil {
		wb.fail(w, err)
		return
	}
	amt, prevAmt, spent := map[int64]int64{}, map[int64]int64{}, map[int64]int64{}
	for _, b := range cur {
		amt[b.CategoryID] = b.Amount
	}
	for _, b := range prev {
		prevAmt[b.CategoryID] = b.Amount
	}
	for _, c := range s.Categories {
		spent[c.ID] = c.Spent
	}
	d := budgetsData{StartDay: cfg.BudgetStartDay}
	for _, c := range cats.Expense {
		d.Rows = append(d.Rows, budgetRow{Category: c, Amount: amt[c.ID], Spent: spent[c.ID], Previous: prevAmt[c.ID]})
		d.Total += amt[c.ID]
	}
	pd := wb.base(r, "예산", "budgets")
	wb.withPeriod(&pd, p, cfg.BudgetStartDay)
	pd.Data = d
	wb.render(w, "budgets", pd)
}

func (wb *Web) saveBudgets(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	month := r.PostFormValue("month")
	r.URL.RawQuery = url.Values{"month": {month}}.Encode()
	p, _, err := wb.API.PeriodFor(r)
	if err != nil {
		wb.fail(w, err)
		return
	}
	var items []api.BudgetItem
	for key := range r.PostForm {
		if !strings.HasPrefix(key, "amount_") {
			continue
		}
		id, err := strconv.ParseInt(strings.TrimPrefix(key, "amount_"), 10, 64)
		if err != nil {
			continue
		}
		v := strings.TrimSpace(strings.ReplaceAll(r.PostFormValue(key), ",", ""))
		if v == "" {
			v = "0"
		}
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil || n < 0 {
			redirect(w, r, "/budgets?month="+p.Label, "금액은 0 이상의 숫자로 입력하세요", true)
			return
		}
		items = append(items, api.BudgetItem{CategoryID: id, Amount: n})
	}
	if err := wb.API.SaveBudgets(r.Context(), p.StartDate(), items); err != nil {
		wb.fail(w, err)
		return
	}
	redirect(w, r, "/budgets?month="+p.Label, "예산을 저장했어요", false)
}

func (wb *Web) copyBudgets(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	r.URL.RawQuery = url.Values{"month": {r.PostFormValue("month")}}.Encode()
	p, cfg, err := wb.API.PeriodFor(r)
	if err != nil {
		wb.fail(w, err)
		return
	}
	n, err := wb.API.Q().CopyBudgets(r.Context(), dbq.CopyBudgetsParams{FromPeriod: p.Prev(cfg.BudgetStartDay).StartDate(), ToPeriod: p.StartDate()})
	if err != nil {
		wb.fail(w, err)
		return
	}
	redirect(w, r, "/budgets?month="+p.Label, "지난달 예산 "+strconv.FormatInt(n, 10)+"개를 복사했어요(이미 있던 분류는 유지)", false)
}

// ---- 분석 ----

func (wb *Web) analytics(w http.ResponseWriter, r *http.Request) {
	p, cfg, err := wb.API.PeriodFor(r)
	if err != nil {
		wb.fail(w, err)
		return
	}
	an, err := wb.API.BuildAnalytics(r, p, p.Prev(cfg.BudgetStartDay))
	if err != nil {
		wb.fail(w, err)
		return
	}
	pd := wb.base(r, "분석", "analytics")
	pd.Wide = true
	wb.withPeriod(&pd, p, cfg.BudgetStartDay)
	pd.Data = an
	wb.render(w, "analytics", pd)
}

// ---- 관리 ----

type adminData struct {
	Cats     categoryGroups
	Rules    []dbq.ListRulesRow
	Failed   []dbq.RawEvent
	Devices  any
	Config   pipeline.Config
	OwnLines string
	Now      time.Time
}

func (wb *Web) admin(w http.ResponseWriter, r *http.Request) {
	q := wb.API.Q()
	cats, err := wb.categories(r, true)
	if err != nil {
		wb.fail(w, err)
		return
	}
	rules, err := q.ListRules(r.Context())
	if err != nil {
		wb.fail(w, err)
		return
	}
	failed, err := q.ListFailedRawEvents(r.Context(), 50)
	if err != nil {
		wb.fail(w, err)
		return
	}
	devs, err := wb.Devices.ListDevices(r)
	if err != nil {
		wb.fail(w, err)
		return
	}
	cfg, err := pipeline.LoadConfig(r.Context(), q)
	if err != nil {
		wb.fail(w, err)
		return
	}
	var own []string
	for _, o := range cfg.OwnAccounts {
		own = append(own, o.Pattern+"="+o.Target)
	}
	pd := wb.base(r, "관리", "admin")
	pd.Wide = true
	pd.Data = adminData{Cats: cats, Rules: rules, Failed: failed, Devices: devs, Config: cfg, OwnLines: strings.Join(own, "\n"), Now: time.Now()}
	wb.render(w, "admin", pd)
}

func (wb *Web) addCash(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	amount, ok := formInt64(r, "amount")
	catID, ok2 := formInt64(r, "category_id")
	if !ok || !ok2 {
		redirect(w, r, "/admin", "금액과 분류를 입력하세요", true)
		return
	}
	m := pipeline.Manual{Amount: amount, Merchant: r.PostFormValue("merchant"), Memo: r.PostFormValue("memo"), CategoryID: catID, Flow: r.PostFormValue("flow")}
	if v := r.PostFormValue("occurred_at"); v != "" {
		if t, err := time.ParseInLocation("2006-01-02T15:04", v, period.KST); err == nil {
			m.OccurredAt = &t
		}
	}
	if _, err := wb.API.Editor.CreateManual(r.Context(), m, time.Now()); err != nil {
		redirect(w, r, "/admin", "입력 실패: "+err.Error(), true)
		return
	}
	redirect(w, r, "/admin", "현금 거래를 기록했어요", false)
}

func (wb *Web) addCategory(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	name := strings.TrimSpace(r.PostFormValue("name"))
	kind := r.PostFormValue("kind")
	if name == "" || (kind != "expense" && kind != "income") {
		redirect(w, r, "/admin#categories", "이름과 종류를 확인하세요", true)
		return
	}
	if _, err := wb.API.Q().CreateCategory(r.Context(), dbq.CreateCategoryParams{Name: name, Kind: kind, Sort: 500}); err != nil {
		redirect(w, r, "/admin#categories", "추가 실패(같은 이름이 있을 수 있어요)", true)
		return
	}
	redirect(w, r, "/admin#categories", "분류를 추가했어요", false)
}

func (wb *Web) updateCategory(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok || r.ParseForm() != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	var name *string
	if n := strings.TrimSpace(r.PostFormValue("name")); n != "" {
		name = &n
	}
	var sort *int32
	if s, err := strconv.Atoi(r.PostFormValue("sort")); err == nil {
		v := int32(s)
		sort = &v
	}
	var archived *bool
	switch r.PostFormValue("archived") {
	case "true":
		t := true
		archived = &t
	case "false":
		f := false
		archived = &f
	}
	if _, err := wb.API.UpdateCategory(r, id, name, sort, archived); err != nil {
		redirect(w, r, "/admin#categories", "수정 실패(특수 분류는 보관할 수 없어요)", true)
		return
	}
	redirect(w, r, "/admin#categories", "분류를 수정했어요", false)
}

func (wb *Web) addRule(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	catID, _ := formInt64(r, "category_id")
	in := api.RuleInput{Pattern: r.PostFormValue("pattern"), MatchType: r.PostFormValue("match_type"), CategoryID: catID}
	if f := r.PostFormValue("flow"); f == "out" || f == "in" {
		in.Flow = &f
	}
	if p, err := strconv.Atoi(r.PostFormValue("priority")); err == nil {
		v := int32(p)
		in.Priority = &v
	}
	if _, err := wb.API.CreateRule(r, in); err != nil {
		redirect(w, r, "/admin#rules", "규칙 저장 실패(패턴·분류를 확인하세요)", true)
		return
	}
	redirect(w, r, "/admin#rules", "규칙을 저장했어요. 기존 거래에 적용하려면 재처리하세요", false)
}

func (wb *Web) deleteRule(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if _, err := wb.API.Q().DeleteRule(r.Context(), id); err != nil {
		wb.fail(w, err)
		return
	}
	redirect(w, r, "/admin#rules", "규칙을 지웠어요", false)
}

// saveSettings는 관리 화면의 설정 폼을 저장한다. 목록은 줄바꿈 또는 쉼표로 구분한다.
func (wb *Web) saveSettings(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	save := func(key string, v any) error {
		b, _ := json.Marshal(v)
		return wb.API.SaveSetting(r, key, b)
	}
	if n, err := strconv.Atoi(r.PostFormValue("budget_start_day")); err == nil {
		if err := save("budget_start_day", n); err != nil {
			redirect(w, r, "/admin#settings", "예산 시작일은 1~28", true)
			return
		}
	}
	if n, err := strconv.Atoi(r.PostFormValue("dedupe_window_seconds")); err == nil {
		if err := save("dedupe_window_seconds", n); err != nil {
			redirect(w, r, "/admin#settings", "중복 판단 시간은 0~3600초", true)
			return
		}
	}
	var own []pipeline.OwnAccount
	for _, line := range splitList(r.PostFormValue("own_accounts")) {
		pat, target, ok := strings.Cut(line, "=")
		target = strings.TrimSpace(target)
		if !ok || target == "" {
			target = "own_transfer"
		}
		own = append(own, pipeline.OwnAccount{Pattern: strings.TrimSpace(pat), Target: target})
	}
	if err := save("own_accounts", own); err != nil {
		redirect(w, r, "/admin#settings", "내 계좌 형식: 이름=own_transfer 또는 이름=investment", true)
		return
	}
	for _, key := range []string{"card_payment_keywords", "card_company_names", "prepaid_keywords", "review_merchant_patterns"} {
		if _, ok := r.PostForm[key]; ok {
			if err := save(key, splitList(r.PostFormValue(key))); err != nil {
				wb.fail(w, err)
				return
			}
		}
	}
	redirect(w, r, "/admin#settings", "설정을 저장했어요", false)
}

func splitList(s string) []string {
	out := []string{}
	for _, part := range strings.FieldsFunc(s, func(r rune) bool { return r == '\n' || r == ',' || r == '\r' }) {
		if p := strings.TrimSpace(part); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func (wb *Web) revokeDevice(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if _, err := wb.API.Q().RevokeDevice(r.Context(), id); err != nil {
		wb.fail(w, err)
		return
	}
	redirect(w, r, "/admin#devices", "기기 토큰을 폐기했어요", false)
}

// more는 휴대폰 하단바의 "더보기" 화면이다(예산·리포트·대사·관리·테마·로그아웃).
func (wb *Web) more(w http.ResponseWriter, r *http.Request) {
	wb.render(w, "more", wb.base(r, "더보기", "more"))
}
