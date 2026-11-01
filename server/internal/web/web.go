// Package web은 PC 웹 화면이다. Go 서버가 html/template + htmx + Chart.js로 직접 서빙한다(별도 프론트 빌드 없음).
package web

import (
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"log/slog"
	"math"
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

//go:embed templates/*.html
var templateFS embed.FS

//go:embed static
var staticFS embed.FS

// assetVersion은 정적 파일 캐시 무효화용 값이다(서버 시작 시각).
var assetVersion = strconv.FormatInt(time.Now().Unix(), 36)

// Web은 웹 화면 핸들러다.
type Web struct {
	API      *api.API
	Auth     *auth.Authenticator
	Devices  *auth.Handlers
	Logger   *slog.Logger
	DevLogin bool // 로그인 화면에 개발용 로그인 링크 표시
	pages    map[string]*template.Template
}

// pageNames는 layout과 함께 파싱할 페이지 템플릿 목록이다.
var pageNames = []string{"dashboard", "transactions", "review", "budgets", "analytics", "admin", "report", "reconcile", "more"}

// New는 템플릿을 파싱해서 Web을 만든다.
func New(a *api.API, authn *auth.Authenticator, devices *auth.Handlers, logger *slog.Logger, devLogin bool) (*Web, error) {
	w := &Web{API: a, Auth: authn, Devices: devices, Logger: logger, DevLogin: devLogin, pages: map[string]*template.Template{}}
	for _, name := range pageNames {
		t, err := template.New("").Funcs(funcs).ParseFS(templateFS, "templates/layout.html", "templates/"+name+".html")
		if err != nil {
			return nil, fmt.Errorf("템플릿 %s: %w", name, err)
		}
		w.pages[name] = t
	}
	t, err := template.New("").Funcs(funcs).ParseFS(templateFS, "templates/login.html")
	if err != nil {
		return nil, err
	}
	w.pages["login"] = t
	return w, nil
}

// Register는 웹 라우트를 등록한다.
func (wb *Web) Register(mux *http.ServeMux) {
	static, _ := fs.Sub(staticFS, "static")
	fileServer := http.StripPrefix("/static/", http.FileServer(http.FS(static)))
	mux.Handle("GET /static/", cacheStatic(fileServer))
	mux.HandleFunc("GET /login", wb.login)

	h := func(pattern string, fn http.HandlerFunc) { mux.Handle(pattern, secureHeaders(wb.Auth.RequireWeb(fn))) }
	h("GET /{$}", wb.dashboard)
	h("GET /transactions", wb.transactions)
	h("POST /web/transactions/{id}", wb.updateTransaction)
	h("GET /review", wb.review)
	h("POST /web/review/{id}", wb.classifyReview)
	h("GET /budgets", wb.budgets)
	h("POST /budgets", wb.saveBudgets)
	h("POST /budgets/copy", wb.copyBudgets)
	h("GET /analytics", wb.analytics)
	h("GET /admin", wb.admin)
	h("POST /admin/cash", wb.addCash)
	h("POST /admin/categories", wb.addCategory)
	h("POST /admin/categories/{id}", wb.updateCategory)
	h("POST /admin/rules", wb.addRule)
	h("POST /admin/rules/{id}/delete", wb.deleteRule)
	h("POST /admin/settings", wb.saveSettings)
	h("POST /admin/devices/{id}/revoke", wb.revokeDevice)
	h("GET /report", wb.report)
	h("GET /reconcile", wb.reconcile)
	h("GET /more", wb.more)
}

func cacheStatic(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=86400")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		next.ServeHTTP(w, r)
	})
}

// secureHeaders는 웹 화면 보안 헤더다. 스크립트는 같은 출처 파일만 허용한다(인라인 스크립트 금지).
func secureHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self'; frame-ancestors 'none'; base-uri 'self'; form-action 'self'")
		h.Set("X-Frame-Options", "DENY")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "same-origin")
		h.Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}

// ---- 렌더링 ----

type pageData struct {
	Title       string
	Active      string
	Wide        bool // 표·차트가 많은 화면은 본문 폭을 넓게
	CSRF        string
	ReviewCount int64
	AssetVer    string
	Flash       string
	Error       string
	Month       string // 현재 선택 기간 라벨
	PrevMonth   string
	NextMonth   string
	Data        any
}

func (wb *Web) base(r *http.Request, title, active string) pageData {
	u, _ := auth.UserFromContext(r.Context())
	n, _ := wb.API.Q().CountNeedsReview(r.Context())
	return pageData{Title: title, Active: active, CSRF: u.CSRF, ReviewCount: n, AssetVer: assetVersion,
		Flash: r.URL.Query().Get("ok"), Error: r.URL.Query().Get("err")}
}

func (wb *Web) withPeriod(pd *pageData, p period.Period, startDay int) {
	pd.Month = p.Label
	pd.PrevMonth = p.Prev(startDay).Label
	pd.NextMonth = period.ForMonth(p.Start.Year(), p.Start.Month()+1, startDay).Label
}

func (wb *Web) render(w http.ResponseWriter, page string, pd pageData) {
	t, ok := wb.pages[page]
	if !ok {
		http.Error(w, "unknown page", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := t.ExecuteTemplate(w, "layout", pd); err != nil {
		wb.Logger.Error("템플릿 렌더링 실패", "page", page, "err", err)
	}
}

func (wb *Web) partial(w http.ResponseWriter, page, name string, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := wb.pages[page].ExecuteTemplate(w, name, data); err != nil {
		wb.Logger.Error("부분 템플릿 렌더링 실패", "name", name, "err", err)
	}
}

func (wb *Web) fail(w http.ResponseWriter, err error) {
	if errors.Is(err, pipeline.ErrInvalid) {
		http.Error(w, "입력값이 올바르지 않습니다: "+err.Error(), http.StatusBadRequest)
		return
	}
	wb.Logger.Error("웹 오류", "err", err)
	http.Error(w, "서버 오류가 발생했습니다.", http.StatusInternalServerError)
}

// redirect는 POST 후 GET으로 돌려보낸다(새로고침 시 재전송 방지). msg는 안내 문구.
func redirect(w http.ResponseWriter, r *http.Request, path, msg string, isErr bool) {
	u, _ := url.Parse(path)
	q := u.Query()
	if msg != "" {
		if isErr {
			q.Set("err", msg)
		} else {
			q.Set("ok", msg)
		}
	}
	u.RawQuery = q.Encode()
	http.Redirect(w, r, u.String(), http.StatusSeeOther)
}

// ---- 템플릿 함수 ----

var funcs = template.FuncMap{
	"won":     func(n int64) string { return group(n) + "원" },
	"num":     group,
	"pct":     func(f float64) string { return strconv.Itoa(int(math.Round(f*100))) + "%" },
	"barW":    func(f float64) string { return strconv.Itoa(int(math.Max(0, math.Min(100, f*100)))) },
	"barCls":  barClass,
	"dt":      func(t time.Time) string { return t.In(period.KST).Format("01/02 15:04") },
	"day":     func(t time.Time) string { return t.In(period.KST).Format("2006-01-02") },
	"dtLocal": func(t time.Time) string { return t.In(period.KST).Format("2006-01-02T15:04") },
	"deref": func(p *string) string {
		if p == nil {
			return ""
		}
		return *p
	},
	"derefID": func(p *int64) int64 {
		if p == nil {
			return 0
		}
		return *p
	},
	"reason": reasonLabel,
	"reasonText": func(s string) string {
		if s == "" {
			return "(사유 없음)"
		}
		return reasonLabel(&s)
	},
	"signed": func(t api.TransactionDTO) string {
		if t.Flow == "in" {
			return "+" + group(t.Amount) + "원"
		}
		return group(t.Amount) + "원"
	},
	"diffCls": func(n int64) string {
		switch {
		case n > 0:
			return "up"
		case n < 0:
			return "down"
		}
		return ""
	},
	"plus": func(n int64) string {
		if n > 0 {
			return "+" + group(n)
		}
		return group(n)
	},
	"toJSON": func(v any) (template.JS, error) {
		b, err := json.Marshal(v)
		return template.JS(b), err
	},
	"lines":    func(ss []string) string { return strings.Join(ss, "\n") },
	"sub_":     func(a, b int64) int64 { return a - b },
	"wonShort": wonShort,
	"ratio_": func(used, budget int64) float64 {
		if budget <= 0 {
			return 0
		}
		return float64(used) / float64(budget)
	},
	"row": func(t api.TransactionDTO, c categoryGroups, csrf string) txRowData {
		return txRowData{T: t, Cats: c, CSRF: csrf}
	},
}

func group(n int64) string {
	neg := n < 0
	if neg {
		n = -n
	}
	s := strconv.FormatInt(n, 10)
	var b strings.Builder
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(c)
	}
	if neg {
		return "-" + b.String()
	}
	return b.String()
}

// wonShort는 좁은 칸용 금액이다: 100만 원 미만은 그대로, 그 이상은 만 단위.
// 3200000 → "320만", 120000000 → "1억 2,000만", 108600 → "108,600원".
func wonShort(n int64) string {
	sign := ""
	if n < 0 {
		sign, n = "-", -n
	}
	switch {
	case n >= 100_000_000:
		s := sign + group(n/100_000_000) + "억"
		if m := (n % 100_000_000) / 10_000; m > 0 {
			s += " " + group(m) + "만"
		}
		return s
	case n >= 1_000_000:
		return sign + group(n/10_000) + "만"
	}
	return sign + group(n) + "원"
}

// barClass: 80% 이상 경고색, 100% 초과 빨강.
func barClass(ratio float64) string {
	switch {
	case ratio > 1:
		return "over"
	case ratio >= 0.8:
		return "warn"
	}
	return "ok"
}

var reasonLabels = map[string]string{
	pipeline.ReviewNoMerchant:     "가맹점 이름 없음",
	pipeline.ReviewNoRule:         "규칙 없음",
	pipeline.ReviewPaymentGateway: "결제대행사 이름",
	pipeline.ReviewTransfer:       "송금",
	pipeline.ReviewDeposit:        "입금 성격 확인",
	pipeline.ReviewUnknownKind:    "종류 모름",
	pipeline.ReviewCancelNoMatch:  "원거래 없는 취소",
	pipeline.ReviewCancelPartial:  "부분 취소",
	pipeline.ExcludeCardPayment:   "카드대금",
	pipeline.ExcludePrepaid:       "머니 충전",
	pipeline.ExcludeDuplicate:     "중복",
	pipeline.ExcludeCancelled:     "취소됨",
	pipeline.ExcludeCancellation:  "취소 건",
	pipeline.ExcludeUser:          "직접 제외",
}

func reasonLabel(p *string) string {
	if p == nil {
		return ""
	}
	if l, ok := reasonLabels[*p]; ok {
		return l
	}
	return *p
}

// ---- 공용 도우미 ----

func formInt64(r *http.Request, key string) (int64, bool) {
	n, err := strconv.ParseInt(strings.TrimSpace(strings.ReplaceAll(r.PostFormValue(key), ",", "")), 10, 64)
	return n, err == nil
}

func pathID(r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	return id, err == nil && id > 0
}

// categoryGroups는 화면용 분류 묶음이다.
type categoryGroups struct {
	All     []dbq.Category
	Expense []dbq.Category
	Other   []dbq.Category // 수입·정산·내 계좌·투자
	ByID    map[int64]dbq.Category
}

func (wb *Web) categories(r *http.Request, includeArchived bool) (categoryGroups, error) {
	cats, err := wb.API.Q().ListCategories(r.Context())
	if err != nil {
		return categoryGroups{}, err
	}
	g := categoryGroups{ByID: map[int64]dbq.Category{}}
	for _, c := range cats {
		g.ByID[c.ID] = c
		if c.Archived && !includeArchived {
			continue
		}
		g.All = append(g.All, c)
		if c.Kind == "expense" {
			g.Expense = append(g.Expense, c)
		} else {
			g.Other = append(g.Other, c)
		}
	}
	return g, nil
}
