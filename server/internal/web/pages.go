package web

import (
	"encoding/json"
	"errors"
	"html/template"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/yuntyu01/MyHome/server/internal/api"
	"github.com/yuntyu01/MyHome/server/internal/auth"
	"github.com/yuntyu01/MyHome/server/internal/dbq"
	"github.com/yuntyu01/MyHome/server/internal/period"
	"github.com/yuntyu01/MyHome/server/internal/pipeline"
)

// ---- 로그인 ----

// login은 로그인 안내 화면이다. 운영(Access)에서는 로그인 자체를 Cloudflare Access가 하므로 "/"로 가는 링크만 둔다.
func (wb *Web) login(w http.ResponseWriter, r *http.Request) {
	if wb.Auth.Access == nil {
		if u, ok, _ := wb.Auth.Authenticate(r); ok && u.Session != nil {
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = wb.pages["login"].ExecuteTemplate(w, "login", map[string]any{
		"Access": wb.Auth.Access != nil, "DevLogin": wb.DevLogin, "AssetVer": assetVersion})
}

// appLink: GET /app/link?challenge=<S256 43자>&state=<16~64자>. RequireWeb 뒤에 있다(Access 로그인 필요).
// 1회용 코드를 만들어 고정 주소 myhome://auth?code=…&state=… 로 앱을 부른다. 돌아갈 주소는 요청에서 받지 않는다.
func (wb *Web) appLink(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	challenge, state := q.Get("challenge"), q.Get("state")
	if !auth.ValidChallenge(challenge) || !auth.ValidState(state) {
		http.Error(w, "잘못된 앱 연결 요청입니다. 앱에서 다시 시작해 주세요.", http.StatusBadRequest)
		return
	}
	u, _ := auth.UserFromContext(r.Context())
	appURL, err := wb.Devices.CreateAppLink(r.Context(), u.Email, challenge, state)
	if err != nil {
		wb.fail(w, err)
		return
	}
	// appURL은 고정 접두(myhome://auth)에 base64url 값만 붙인 것이라 template.URL로 넘긴다(html/template은 http(s) 밖의 주소를 막는다).
	wb.writeTemplate(w, wb.pages["applink"], "applink", "applink", map[string]any{
		"AppURL": template.URL(appURL), "Email": u.Email, "AssetVer": assetVersion})
}

// ---- 대시보드 ----

type dashboardData struct {
	Summary api.Summary
	Top     []dashCat // 바로 보이는 분류(맨 위 분류만, 안의 분류는 그 아래에 붙는다)
	Rest    []dashCat // 접어 둔 분류
}

func (wb *Web) dashboard(w http.ResponseWriter, r *http.Request) {
	p, cfg, err := wb.API.PeriodFor(r)
	if err != nil {
		wb.fail(w, err)
		return
	}
	s, err := wb.API.BuildSummary(r, p, 5)
	if err != nil {
		wb.fail(w, err)
		return
	}
	pd := wb.base(r, "내 가계부", "dashboard")
	wb.withPeriod(&pd, p, cfg.BudgetStartDay)
	d := dashboardData{Summary: s}
	top, rest := splitCategories(s.Categories)
	d.Top, d.Rest = withChildren(top, s.Categories), withChildren(rest, s.Categories)
	pd.Data = d
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
	Open bool // 수정 항목을 펼친 채로 그린다(방금 고친 행)
}

func (wb *Web) transactions(w http.ResponseWriter, r *http.Request) {
	f, err := wb.API.ParseTransactionFilter(r)
	if err != nil {
		wb.fail(w, err)
		return
	}
	f.Limit = 50
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
	wb.partial(w, "transactions", "tx-row", txRowData{T: dtoFromTx(t, name), Cats: cats, CSRF: pd.CSRF, Open: true})
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
		items := []api.TransactionDTO{dtoFromTx(t, name)}
		wb.API.AttachSuggestions(r.Context(), items)
		d.Item = &items[0]
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
	// "AI 추천: 분류" 버튼: 그 분류로 정하고 "이 가맹점은 항상"을 켠다(체크 상자와 상관없이). 학습은 기존 PATCH 경로와 같다.
	fromAI := false
	if v := r.PostFormValue("suggest_category_id"); v != "" {
		if cid, err := strconv.ParseInt(v, 10, 64); err == nil && cid > 0 {
			e.CategoryID, e.RememberRule, fromAI = &cid, true, true
		}
	}
	if r.PostFormValue("action") == "exclude" {
		s := "excluded"
		e = pipeline.Edit{Status: &s}
		fromAI = false
	}
	res, err := wb.API.Editor.EditTransaction(r.Context(), id, e)
	if err != nil {
		wb.fail(w, err)
		return
	}
	flash := "분류했어요."
	if fromAI {
		flash = "AI 추천대로 분류했어요."
	}
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

// budgetRow는 예산 화면의 한 줄이다. 입력칸은 이 분류에 직접 정한 예산이다.
type budgetRow struct {
	Category dbq.Category
	Label    string // 경로 이름("생활비 › 식비")
	Depth    int
	Folder   bool  // 안에 분류가 있다
	Amount   int64 // 직접 정한 예산
	ChildSum int64 // 안의 분류 예산 합계(직접 정하지 않은 폴더는 이 값을 쓴다)
	Spent    int64 // 안의 분류까지 더한 지출
	Previous int64
}

type budgetsData struct {
	Rows     []budgetRow
	Total    int64 // 맨 위 분류의 실제 예산 합계(홈의 예산과 같다)
	StartDay int
}

func (wb *Web) budgets(w http.ResponseWriter, r *http.Request) {
	p, cfg, err := wb.API.PeriodFor(r)
	if err != nil {
		wb.fail(w, err)
		return
	}
	q := wb.API.Q()
	if err := wb.API.EnsureBudgets(r.Context(), p); err != nil {
		wb.fail(w, err)
		return
	}
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
	amt, prevAmt, spent, childSum := map[int64]int64{}, map[int64]int64{}, map[int64]int64{}, map[int64]int64{}
	for _, b := range cur {
		amt[b.CategoryID] = b.Amount
	}
	for _, b := range prev {
		prevAmt[b.CategoryID] = b.Amount
	}
	for _, c := range s.Categories {
		spent[c.ID] = c.Spent
		if c.ParentID != nil {
			childSum[*c.ParentID] += c.Budget
		}
	}
	folder := map[int64]bool{}
	for _, c := range cats.Expense {
		if c.ParentID != nil {
			folder[*c.ParentID] = true
		}
	}
	d := budgetsData{StartDay: cfg.BudgetStartDay, Total: s.Totals.Budget}
	for _, c := range cats.Expense {
		d.Rows = append(d.Rows, budgetRow{Category: c, Label: cats.Label(c.ID), Depth: cats.tree.Depth(c.ID), Folder: folder[c.ID],
			Amount: amt[c.ID], ChildSum: childSum[c.ID], Spent: spent[c.ID], Previous: prevAmt[c.ID]})
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
	an.Categories = treeOrder(an.Categories)
	pd := wb.base(r, "분석", "analytics")
	pd.Wide = true
	wb.withPeriod(&pd, p, cfg.BudgetStartDay)
	pd.Data = an
	wb.render(w, "analytics", pd)
}

// ---- 관리 ----

type adminData struct {
	Tab        string
	Cats       categoryGroups
	Tree       []catNode   // 분류 탭: 폴더 나무
	NewFolders []catOption // 새 분류를 넣을 수 있는 폴더
	Rules      []dbq.ListRulesRow
	Failed     []dbq.RawEvent
	Devices    any
	Config     pipeline.Config
	OwnLines   string
	Now        time.Time
}

// adminTabs는 관리 화면의 탭이다. 없는 값이면 첫 탭(현금 입력)을 보여준다.
var adminTabs = map[string]bool{"cash": true, "categories": true, "rules": true, "settings": true, "failed": true, "devices": true}

func (wb *Web) admin(w http.ResponseWriter, r *http.Request) {
	tab := r.URL.Query().Get("tab")
	if !adminTabs[tab] {
		tab = "cash"
	}
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
		own = append(own, o.Pattern+"="+ownTargetLabel(o.Target))
	}
	pd := wb.base(r, "관리", "admin")
	pd.Wide = true
	d := adminData{Tab: tab, Cats: cats, Rules: rules, Failed: failed, Devices: devs, Config: cfg, OwnLines: strings.Join(own, "\n"), Now: time.Now()}
	if tab == "categories" {
		d.Tree, d.NewFolders = categoryTree(cats.All, pd.CSRF), newCategoryFolders(cats.All)
	}
	pd.Data = d
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
		redirect(w, r, "/admin?tab=cash", "금액과 분류를 입력하세요", true)
		return
	}
	m := pipeline.Manual{Amount: amount, Merchant: r.PostFormValue("merchant"), Memo: r.PostFormValue("memo"), CategoryID: catID, Flow: r.PostFormValue("flow")}
	if v := r.PostFormValue("occurred_at"); v != "" {
		if t, err := time.ParseInLocation("2006-01-02T15:04", v, period.KST); err == nil {
			m.OccurredAt = &t
		}
	}
	if _, err := wb.API.Editor.CreateManual(r.Context(), m, time.Now()); err != nil {
		redirect(w, r, "/admin?tab=cash", "입력 실패: "+err.Error(), true)
		return
	}
	redirect(w, r, "/admin?tab=cash", "현금 거래를 기록했어요", false)
}

func (wb *Web) addCategory(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	name := strings.TrimSpace(r.PostFormValue("name"))
	kind := r.PostFormValue("kind")
	parent := formParent(r)
	if name == "" || (kind != "expense" && kind != "income" && parent == nil) {
		redirect(w, r, "/admin?tab=categories", "이름과 종류를 확인하세요", true)
		return
	}
	in := api.CategoryChange{Name: &name, ParentID: parent, Memo: formMemo(r)}
	if parent == nil || *parent == 0 {
		in.Kind = &kind
	}
	if _, err := wb.API.CreateCategory(r.Context(), in); err != nil {
		redirect(w, r, "/admin?tab=categories", "추가 실패: "+categoryErrText(err), true)
		return
	}
	redirect(w, r, "/admin?tab=categories", "분류를 추가했어요", false)
}

// formParent는 폼의 폴더 값이다(키가 없으면 nil = 그대로, 빈 값이면 0 = 맨 위).
func formParent(r *http.Request) *int64 {
	if _, ok := r.PostForm["parent"]; !ok {
		return nil
	}
	v, _ := strconv.ParseInt(r.PostFormValue("parent"), 10, 64)
	if v < 0 {
		v = 0
	}
	return &v
}

// formMemo는 폼의 설명 값이다(키가 없으면 nil = 그대로).
func formMemo(r *http.Request) *string {
	if _, ok := r.PostForm["memo"]; !ok {
		return nil
	}
	m := r.PostFormValue("memo")
	return &m
}

// categoryErrText는 분류 저장 실패 이유를 화면 문구로 바꾼다.
func categoryErrText(err error) string {
	if errors.Is(err, api.ErrDuplicateName) {
		return api.ErrDuplicateName.Error()
	}
	if errors.Is(err, pipeline.ErrInvalid) {
		return strings.TrimPrefix(err.Error(), pipeline.ErrInvalid.Error()+": ")
	}
	return "다시 시도해 주세요"
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
	in := api.CategoryChange{Name: name, Sort: sort, Archived: archived, ParentID: formParent(r), Memo: formMemo(r)}
	if _, err := wb.API.UpdateCategory(r, id, in); err != nil {
		redirect(w, r, "/admin?tab=categories", "수정 실패: "+categoryErrText(err), true)
		return
	}
	redirect(w, r, "/admin?tab=categories", "분류를 수정했어요", false)
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
		redirect(w, r, "/admin?tab=rules", "규칙 저장 실패(패턴·분류를 확인하세요)", true)
		return
	}
	redirect(w, r, "/admin?tab=rules", "규칙을 저장했어요. 다음 결제부터 적용돼요", false)
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
	redirect(w, r, "/admin?tab=rules", "규칙을 지웠어요", false)
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
			redirect(w, r, "/admin?tab=settings", "예산 시작일은 1~28", true)
			return
		}
	}
	if n, err := strconv.Atoi(r.PostFormValue("dedupe_window_seconds")); err == nil {
		if err := save("dedupe_window_seconds", n); err != nil {
			redirect(w, r, "/admin?tab=settings", "중복 판단 시간은 0~3600초", true)
			return
		}
	}
	var own []pipeline.OwnAccount
	for _, line := range splitList(r.PostFormValue("own_accounts")) {
		pat, target, ok := strings.Cut(line, "=")
		own = append(own, pipeline.OwnAccount{Pattern: strings.TrimSpace(pat), Target: ownTargetFromInput(target, ok)})
	}
	if err := save("own_accounts", own); err != nil {
		redirect(w, r, "/admin?tab=settings", "내 계좌는 이름=투자 또는 이름=이체로 적어 주세요", true)
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
	redirect(w, r, "/admin?tab=settings", "설정을 저장했어요", false)
}

// ownTargetLabel과 ownTargetFromInput은 내 계좌 설정의 값을 화면 말로 바꾼다(투자, 이체).
func ownTargetLabel(target string) string {
	switch target {
	case "investment":
		return "투자"
	case "own_transfer":
		return "이체"
	}
	return target
}

func ownTargetFromInput(s string, hasTarget bool) string {
	s = strings.TrimSpace(s)
	switch {
	case !hasTarget || s == "" || s == "이체":
		return "own_transfer"
	case s == "투자":
		return "investment"
	}
	return s // investment, own_transfer를 직접 적었거나 잘못된 값(저장할 때 검사한다)
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
	redirect(w, r, "/admin?tab=devices", "기기를 해제했어요", false)
}

// more는 휴대폰 하단바의 "더보기" 화면이다(예산·리포트·명세서 대조·관리·테마·로그아웃).
func (wb *Web) more(w http.ResponseWriter, r *http.Request) {
	wb.render(w, "more", wb.base(r, "더보기", "more"))
}
