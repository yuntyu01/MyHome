// Package web은 PC 웹 화면이다. Go 서버가 html/template + htmx + Chart.js로 직접 서빙한다(별도 프론트 빌드 없음).
package web

import (
	"bytes"
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
	"github.com/yuntyu01/MyHome/server/internal/cattree"
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
	DevLogin bool // 로그인 화면에 개발용 로그인 링크 표시(Access를 쓰면 항상 false)
	pages    map[string]*template.Template
}

// pageNames는 layout과 함께 파싱할 페이지 템플릿 목록이다.
var pageNames = []string{"dashboard", "transactions", "review", "budgets", "analytics", "admin", "report", "reconcile", "more", "invest", "invest_value"}

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
	// 레이아웃 없이 단독으로 그리는 화면
	for _, name := range []string{"login", "applink"} {
		t, err := template.New("").Funcs(funcs).ParseFS(templateFS, "templates/"+name+".html")
		if err != nil {
			return nil, fmt.Errorf("템플릿 %s: %w", name, err)
		}
		w.pages[name] = t
	}
	return w, nil
}

// Register는 웹 라우트를 등록한다.
func (wb *Web) Register(mux *http.ServeMux) {
	static, _ := fs.Sub(staticFS, "static")
	fileServer := http.StripPrefix("/static/", http.FileServer(http.FS(static)))
	mux.Handle("GET /static/", cacheStatic(fileServer))
	mux.HandleFunc("GET /login", wb.login)
	// 자동매매 신선도: 외부 하트비트가 로그인 없이 부른다(Access는 /healthz 우회로 통과). 금액은 싣지 않는다
	mux.HandleFunc("GET /healthz/invest", wb.healthzInvest)

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
	// 앱 연결(처음 한 번): Access 로그인 뒤 1회용 코드를 만들어 myhome://auth 로 앱을 부른다
	h("GET /app/link", wb.appLink)
	// 투자: 읽기 전용(DB만 읽는다). POST 라우트를 만들지 않는다
	h("GET /invest", wb.invest)
	h("GET /invest/value", wb.investValue)
	// 앱 투자 탭: 기기 토큰(또는 웹 세션)으로 같은 값을 JSON으로 읽는다. 이것도 GET만 있다
	api := func(pattern string, fn http.HandlerFunc) { mux.Handle(pattern, wb.Auth.RequireAPI(fn)) }
	api("GET /v1/invest", wb.apiInvest)
	api("GET /v1/invest/value", wb.apiInvestValue)
	// 매매 실패 알림: 앱 주기 작업이 마지막으로 본 id 다음을 읽어 폰 알림을 띄운다
	api("GET /v1/invest/alerts", wb.apiInvestAlerts)
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
	Month       string // 현재 선택 기간 라벨(주소·폼 값). 화면에는 monthName으로 보여준다
	MonthRange  string // 시작일이 1일이 아닐 때 기간("10/15~11/14")
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
	pd.MonthRange = monthRange(p, startDay)
	pd.PrevMonth = p.Prev(startDay).Label
	pd.NextMonth = period.ForMonth(p.Start.Year(), p.Start.Month()+1, startDay).Label
}

func (wb *Web) render(w http.ResponseWriter, page string, pd pageData) {
	t, ok := wb.pages[page]
	if !ok {
		http.Error(w, "unknown page", http.StatusInternalServerError)
		return
	}
	wb.writeTemplate(w, t, "layout", page, pd)
}

// writeTemplate은 버퍼에 먼저 그린다. 중간에 실패해도 잘린 화면을 200으로 보내지 않고 500으로 알린다.
func (wb *Web) writeTemplate(w http.ResponseWriter, t *template.Template, name, page string, data any) {
	var buf bytes.Buffer
	if err := t.ExecuteTemplate(&buf, name, data); err != nil {
		wb.Logger.Error("템플릿 렌더링 실패", "page", page, "name", name, "err", err)
		http.Error(w, "화면을 그리지 못했습니다.", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = buf.WriteTo(w)
}

func (wb *Web) partial(w http.ResponseWriter, page, name string, data any) {
	wb.writeTemplate(w, wb.pages[page], name, page, data)
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
	"dt":      shortTime,
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
	"reason":     reasonLabel,
	"monthName":  monthName,
	"shortDate":  shortDate,
	"sourceName": sourceName,
	"matchLabel": matchLabel,
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
	"maxMemo":  func() int { return cattree.MaxMemo },
	"catBtn":   catButton,
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

// shortTime은 KST "01/02 15:04"다.
func shortTime(t time.Time) string { return t.In(period.KST).Format("01/02 15:04") }

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

// barClass: 80% 이상 경고색(라벤더), 100% 초과 살구.
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
	pipeline.ReviewNoRule:         "처음 보는 가맹점",
	pipeline.ReviewPaymentGateway: "결제대행사로 찍힘",
	pipeline.ReviewTransfer:       "송금",
	pipeline.ReviewDeposit:        "어떤 입금인지 확인",
	pipeline.ReviewUnknownKind:    "어떤 거래인지 모름",
	pipeline.ReviewCancelNoMatch:  "원래 결제를 못 찾은 취소",
	pipeline.ReviewCancelPartial:  "부분 취소",
	pipeline.ReviewForeign:        "외화 결제(원화 금액 확인)",
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
