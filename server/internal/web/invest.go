package web

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/yuntyu01/MyHome/server/internal/dbq"
	"github.com/yuntyu01/MyHome/server/internal/httpx"
	"github.com/yuntyu01/MyHome/server/internal/invest"
	"github.com/yuntyu01/MyHome/server/internal/invest/store"
)

// 투자 화면 — 자동매매(internal/invest) 상태를 DB에서 읽기만 한다.
//
// 🔴 웹 프로세스는 증권사(토스)·야후를 부르지 않는다. invest/run·broker·toss·yahoo를 import하지 않고
// store로 DB만 읽는다. 이 화면에는 POST 라우트도 폼도 없다.
//
// 시장 숫자(평가액·가격·수익률·환율)는 첫 화면에 두지 않고 한 번 더 들어간 평가 화면에만 둔다
// (TQ §4.6). 첫 화면은 보유 수량·이번 달 매수·일정·점검만 보여주고, 문제는 첫 화면에서 바로 보이게 한다.
// 날짜는 전부 뉴욕 기준이다(거래일·판정일이 미국 장 기준이라서).

const (
	// investJudgeStaleDays를 넘도록 새 판정이 없으면 판정 작업이 죽은 것이다 (status deadman ①과 같은 값).
	investJudgeStaleDays = 35
	// investPriceStaleDays를 넘도록 새 가격 행이 없으면 갱신이 멈춘 것이다.
	investPriceStaleDays = 8
	// investRunHang보다 오래 끝나지 않은 실행은 죽은 것으로 본다.
	investRunHang = 3 * time.Hour
)

// investRunCmds는 첫 화면에서 마지막 실행을 보여줄 정기 명령이다.
var investRunCmds = []struct{ Cmd, Label string }{
	{"refresh", "가격 갱신"},
	{"judge", "월말 판정"},
	{"trade", "자동 매매"},
	{"status", "상태 점검"},
}

// ledgerSourceLabels는 원금 내역 출처 이름이다 (myhome invest ledger와 같은 말).
var ledgerSourceLabels = map[string]string{"execute": "수동기록", "trade": "자동매수", "adjust": "앱 밖 매수", "amend": "체결정정"}

// investRow는 묶음 목록의 한 줄이다. Bad면 값을 경고색으로 그리고 문제로 센다.
type investRow struct {
	Title string `json:"title"`
	Meta  string `json:"meta"`
	Value string `json:"value"`
	Bad   bool   `json:"bad"`
}

// investStatus는 첫 화면이다. 시장 숫자를 담지 않는다.
// 앱도 GET /v1/invest로 같은 값을 받는다(JSON 이름은 앱 모델 InvestStatus와 맞춘다).
type investStatus struct {
	Migrated       bool        `json:"migrated"`
	Symbol         string      `json:"symbol"`
	Shares         int         `json:"shares"`
	PriceStale     bool        `json:"price_stale"`
	LastPriceDay   string      `json:"last_price_day"`   // 마지막 가격 행 날짜(YYYY-MM-DD). 없으면 ""
	LastPriceLabel string      `json:"last_price_label"` // 화면용(연도가 다르면 연도까지)
	Month          investRow   `json:"month"`
	Schedule       []investRow `json:"schedule"`
	Checks         []investRow `json:"checks"`
	Problems       int         `json:"problems"`
}

// investLedgerRow는 원금 내역 표의 한 줄이다.
type investLedgerRow struct {
	On     string `json:"on"`
	YM     string `json:"ym"`
	Source string `json:"source"`
	Shares string `json:"shares"`
	Note   string `json:"note"`
	Amount int64  `json:"amount"`
}

// investValue는 평가 화면이다.
// 앱은 GET /v1/invest/value로 같은 값을 받는다.
type investValue struct {
	Migrated       bool              `json:"migrated"`
	Symbol         string            `json:"symbol"`
	Shares         int               `json:"shares"`
	PriceStale     bool              `json:"price_stale"`
	LastPriceDay   string            `json:"last_price_day"`
	LastPriceLabel string            `json:"last_price_label"`
	CanValue       bool              `json:"can_value"`
	Why            string            `json:"why"` // 평가할 수 없는 이유
	NetKRW         int64             `json:"net_krw"`
	Basis          string            `json:"basis"` // 평가에 쓴 종가·환율과 그 날짜
	ContributedKRW int64             `json:"contributed_krw"`
	GainKRW        int64             `json:"gain_krw"`
	GainPct        string            `json:"gain_pct"`
	TaxKRW         int64             `json:"tax_krw"`
	CashUSD        string            `json:"cash_usd"`
	Ledger         []investLedgerRow `json:"ledger"`
	LedgerSum      int64             `json:"ledger_sum"`
	LedgerGap      int64             `json:"ledger_gap"`
	LedgerBad      bool              `json:"ledger_bad"`
}

// ---- 핸들러 ----

func (wb *Web) invest(w http.ResponseWriter, r *http.Request) {
	s, err := wb.loadInvestStatus(r.Context())
	if err != nil {
		wb.fail(w, err)
		return
	}
	pd := wb.base(r, "투자", "invest")
	pd.Data = s
	wb.render(w, "invest", pd)
}

func (wb *Web) investValue(w http.ResponseWriter, r *http.Request) {
	v, err := wb.loadInvestValue(r.Context())
	if err != nil {
		wb.fail(w, err)
		return
	}
	pd := wb.base(r, "평가", "invest")
	pd.Wide = true
	pd.Data = v
	wb.render(w, "invest_value", pd)
}

// apiInvest는 앱 투자 탭용 GET /v1/invest다. 웹 첫 화면과 같은 값이고, 시장 숫자가 없다.
func (wb *Web) apiInvest(w http.ResponseWriter, r *http.Request) {
	s, err := wb.loadInvestStatus(r.Context())
	if err != nil {
		wb.Logger.Error("투자 조회 실패", "err", err)
		httpx.Error(w, http.StatusInternalServerError, "internal error")
		return
	}
	httpx.JSON(w, http.StatusOK, s)
}

// apiInvestValue는 앱 평가 화면용 GET /v1/invest/value다.
func (wb *Web) apiInvestValue(w http.ResponseWriter, r *http.Request) {
	v, err := wb.loadInvestValue(r.Context())
	if err != nil {
		wb.Logger.Error("투자 평가 조회 실패", "err", err)
		httpx.Error(w, http.StatusInternalServerError, "internal error")
		return
	}
	httpx.JSON(w, http.StatusOK, v)
}

// loadInvestStatus는 첫 화면 값을 DB에서 읽어 만든다. 옮기기 전이면 Migrated가 false인 빈 값이다.
func (wb *Web) loadInvestStatus(ctx context.Context) (investStatus, error) {
	st := store.New(wb.API.Pool, "web")
	pos, err := st.LoadPosition(ctx)
	if errors.Is(err, store.ErrNoPosition) {
		return investStatus{}, nil
	}
	if err != nil {
		return investStatus{}, err
	}
	in, err := loadInvestInputs(ctx, st, pos)
	if err != nil {
		return investStatus{}, err
	}
	return buildInvestStatus(time.Now(), pos, in), nil
}

// loadInvestValue는 평가 화면 값을 DB에서 읽어 만든다.
func (wb *Web) loadInvestValue(ctx context.Context) (investValue, error) {
	st := store.New(wb.API.Pool, "web")
	pos, err := st.LoadPosition(ctx)
	if errors.Is(err, store.ErrNoPosition) {
		return investValue{}, nil
	}
	if err != nil {
		return investValue{}, err
	}
	prices, err := st.LoadPrices(ctx)
	if err != nil {
		return investValue{}, err
	}
	ledger, err := st.LoadLedger(ctx)
	if err != nil {
		return investValue{}, err
	}
	return buildInvestValue(time.Now(), pos, prices, ledger), nil
}

// investInputs는 첫 화면이 position 말고 더 읽는 것이다.
type investInputs struct {
	Prices     invest.Prices
	Ledger     []invest.LedgerEntry
	Runs       []dbq.InvestRun
	Contrib    int64
	ContribErr error // 적립액 미설정. 화면에 문제로 보여준다
}

func loadInvestInputs(ctx context.Context, st *store.Store, pos invest.Position) (investInputs, error) {
	var in investInputs
	var err error
	if in.Prices, err = st.LoadPrices(ctx); err != nil {
		return in, err
	}
	if in.Ledger, err = st.LoadLedger(ctx); err != nil {
		return in, err
	}
	if in.Runs, err = st.LatestRuns(ctx); err != nil {
		return in, err
	}
	// 적립액이 없다는 에러는 화면에 보여줄 문제다. DB 오류와 구분할 방법이 없어 둘 다 문제로 보여준다
	in.Contrib, in.ContribErr = st.ContributionFor(ctx, pos.ApplyYM)
	return in, nil
}

// ---- 화면 값 만들기 (순수 함수: 시각을 받아서 테스트에서 날짜를 고정한다) ----

func buildInvestStatus(now time.Time, pos invest.Position, in investInputs) investStatus {
	today := invest.TodayET(now)
	s := investStatus{Migrated: true, Symbol: pos.Holdings.Symbol, Shares: pos.Holdings.Shares}
	s.PriceStale, s.LastPriceDay = priceStaleness(in.Prices, today)
	s.LastPriceLabel = dayNameAt(s.LastPriceDay, now)

	// 이번 달(적용월) 매수
	execDay, applyOK := applyExecDay(pos.ApplyYM)
	month := investRow{Title: monthNameAt(pos.ApplyYM, now) + " 매수"}
	meta := "적용월 " + monthNameAt(pos.ApplyYM, now)
	if t, err := invest.Ticker(pos.Signal.Target); err == nil {
		meta += " · 목표 " + t
	} else if pos.Signal.Target != "" {
		meta += " · 목표 " + pos.Signal.Target
	}
	if in.ContribErr == nil {
		meta += " · 적립 " + group(in.Contrib) + "원"
	}
	missed := false
	switch {
	case pos.ExecutedThisMonth():
		month.Value = "✓ 완료 " + shortDate(derefStr(pos.ExecutedOn))
		if pos.LateDays != nil && *pos.LateDays > 0 {
			meta += fmt.Sprintf(" · %d일 늦은 매수", *pos.LateDays)
		}
	case !applyOK:
		month.Value, month.Bad = "✗ 적용월 형식 이상", true
	case !today.After(execDay):
		// 실행일 당일은 아직 기회가 남았다 — 여기서 알리면 오경보다 (status와 같다)
		month.Value = "예정 " + shortDate(invest.FormatDay(execDay))
	default:
		missed = true
		month.Value, month.Bad = "✗ 매수 안 됨", true
		meta += fmt.Sprintf(" · 실행일 %s에서 %d일 지남", shortDate(invest.FormatDay(execDay)), daysBetween(execDay, today))
	}
	month.Meta = meta
	s.Month = month

	// 일정: 다음 판정일(이번 달 또는 다음 달 마지막 거래일), 다음 실행일(적용월 첫 거래일)
	judge := invest.LastTradingDay(today.Year(), today.Month())
	if today.After(judge) {
		judge = invest.NextLastTradingDay(today)
	}
	s.Schedule = append(s.Schedule, investRow{Title: "다음 판정일", Meta: "월 마지막 거래일", Value: shortDate(invest.FormatDay(judge)) + " · " + dday(today, judge)})
	if applyOK {
		next, nextYM := execDay, pos.ApplyYM
		if pos.ExecutedThisMonth() {
			// 이번 적용월은 끝났다. 다음 판정 뒤 달의 첫 거래일이다
			after := invest.Date(judge.Year(), judge.Month(), 1).AddDate(0, 1, 0)
			next, nextYM = invest.FirstTradingDay(after.Year(), after.Month()), invest.YM(after)
		}
		s.Schedule = append(s.Schedule, investRow{Title: "다음 실행일", Meta: monthNameAt(nextYM, now) + " 첫 거래일",
			Value: shortDate(invest.FormatDay(next)) + " · " + dday(today, next), Bad: missed})
	}

	// 점검
	s.Checks = append(s.Checks, runChecks(now, in.Runs)...)
	if last, err := invest.ParseDay(pos.LastRun); err == nil {
		n := daysBetween(last, today)
		s.Checks = append(s.Checks, investRow{Title: "판정 신선도", Meta: fmt.Sprintf("기준 %s · %d일이 넘으면 판정 작업을 확인하세요", dayNameAt(pos.LastRun, now), investJudgeStaleDays),
			Value: mark(n <= investJudgeStaleDays) + strconv.Itoa(n) + "일 전", Bad: n > investJudgeStaleDays})
	}
	if s.LastPriceDay == "" {
		s.Checks = append(s.Checks, investRow{Title: "가격 신선도", Meta: "가격 기록이 없어요", Value: "✗ 없음", Bad: true})
	} else {
		last, _ := invest.ParseDay(s.LastPriceDay)
		s.Checks = append(s.Checks, investRow{Title: "가격 신선도", Meta: fmt.Sprintf("마지막 가격 %s · 오늘 %s", s.LastPriceLabel, dayNameAt(invest.FormatDay(today), now)),
			Value: mark(!s.PriceStale) + strconv.Itoa(daysBetween(last, today)) + "일 전", Bad: s.PriceStale})
	}
	switch {
	case pos.FXEstimated:
		s.Checks = append(s.Checks, investRow{Title: "환율", Meta: "환율 추정 — 증권사 적용 환율을 확인하면 amend --fx로 맞춰 주세요", Value: "✗ 추정", Bad: true})
	case pos.FXRate > 0:
		s.Checks = append(s.Checks, investRow{Title: "환율", Meta: "증권사 적용 환율", Value: "✓ 확정"})
	}
	if in.ContribErr != nil {
		s.Checks = append(s.Checks, investRow{Title: "적립액", Meta: monthNameAt(pos.ApplyYM, now) + "에 적용할 적립액이 없어요", Value: "✗ 미설정", Bad: true})
	}
	_, gap, bad := reconcileLedger(pos, in.Ledger)
	led := investRow{Title: "원금 대조", Meta: fmt.Sprintf("원금 내역 %d건 합계와 누적 원금", len(in.Ledger)), Value: "✓ 일치"}
	if bad {
		led.Value, led.Bad = "✗ "+group(gap)+"원 어긋남", true
	}
	s.Checks = append(s.Checks, led)

	if s.Month.Bad {
		s.Problems++
	}
	for _, c := range s.Checks {
		if c.Bad {
			s.Problems++
		}
	}
	return s
}

func buildInvestValue(now time.Time, pos invest.Position, prices invest.Prices, ledger []invest.LedgerEntry) investValue {
	today := invest.TodayET(now)
	acct := pos.Holdings
	v := investValue{Migrated: true, Symbol: acct.Symbol, Shares: acct.Shares,
		ContributedKRW: roundKRW(pos.ContributedKRW), TaxKRW: roundKRW(acct.TaxLiabilityKRW()), CashUSD: usd(acct.CashUSD)}
	v.PriceStale, v.LastPriceDay = priceStaleness(prices, today)
	v.LastPriceLabel = dayNameAt(v.LastPriceDay, now)

	fx, fxDay := prices.ValuationFX()
	price, priceDay, priceOK := 0.0, time.Time{}, true
	if acct.Symbol != "" {
		price, priceDay, priceOK = lastClose(prices, acct.Symbol)
	}
	switch {
	case !priceOK:
		v.Why = acct.Symbol + " 종가가 없어서 평가할 수 없어요"
	case fx <= 0:
		v.Why = "환율이 없어서 평가할 수 없어요"
	default:
		v.CanValue = true
		net := acct.NetValueKRW(price, fx)
		v.NetKRW = roundKRW(net)
		v.GainKRW = roundKRW(net - pos.ContributedKRW)
		if pos.ContributedKRW > 0 {
			v.GainPct = fmt.Sprintf("%+.1f%%", (net-pos.ContributedKRW)/pos.ContributedKRW*100)
		}
		v.Basis = "환율 " + decimal2(fx) + " (" + dayNameAt(invest.FormatDay(fxDay), now) + ")"
		if acct.Symbol != "" {
			v.Basis = acct.Symbol + " 종가 " + usd(price) + " (" + dayNameAt(invest.FormatDay(priceDay), now) + ") · " + v.Basis
		}
	}

	for _, e := range ledger {
		// 원금 내역은 여러 해에 걸치므로 올해가 아닌 날짜는 연도까지 보여준다
		row := investLedgerRow{On: dayNameAt(e.On, now), YM: monthNameAt(e.YM, now), Amount: roundKRW(e.AmountKRW), Note: e.Note, Source: e.Source}
		if l, ok := ledgerSourceLabels[e.Source]; ok {
			row.Source = l
		}
		if e.Shares != nil {
			row.Shares = fmt.Sprintf("%s %+d주", e.Symbol, *e.Shares)
		}
		v.Ledger = append(v.Ledger, row)
	}
	var sum float64
	sum, v.LedgerGap, v.LedgerBad = reconcileLedger(pos, ledger)
	v.LedgerSum = roundKRW(sum)
	return v
}

// ---- 도우미 ----

// priceStaleness는 마지막 가격 행이 오늘(뉴욕)보다 investPriceStaleDays일 넘게 오래됐는지다.
func priceStaleness(prices invest.Prices, today time.Time) (bool, string) {
	last, ok := prices.Last()
	if !ok {
		return true, ""
	}
	return daysBetween(last.Day, today) > investPriceStaleDays, invest.FormatDay(last.Day)
}

// applyExecDay는 적용월의 첫 거래일(실행일)이다.
func applyExecDay(ym string) (time.Time, bool) {
	t, err := time.Parse("2006-01", ym)
	if err != nil {
		return time.Time{}, false
	}
	return invest.FirstTradingDay(t.Year(), t.Month()), true
}

// lastClose는 그 종목의 마지막 유효 종가와 날짜다. 마지막 행에 값이 빠졌으면 그 앞을 본다.
func lastClose(prices invest.Prices, symbol string) (float64, time.Time, bool) {
	for i := len(prices) - 1; i >= 0; i-- {
		if v := prices[i].Get(symbol); !math.IsNaN(v) && v > 0 {
			return v, prices[i].Day, true
		}
	}
	return 0, time.Time{}, false
}

// reconcileLedger는 원금 내역 합계와 누적 원금(position.contributed_krw)의 차이다. 1원 이상이면 어긋남.
func reconcileLedger(pos invest.Position, ledger []invest.LedgerEntry) (sum float64, gap int64, bad bool) {
	for _, e := range ledger {
		sum += e.AmountKRW
	}
	g := pos.ContributedKRW - sum
	return sum, roundKRW(g), math.Abs(g) >= 1
}

// runChecks는 정기 명령마다 마지막 실행 결과다.
func runChecks(now time.Time, runs []dbq.InvestRun) []investRow {
	by := map[string]dbq.InvestRun{}
	for _, r := range runs {
		by[r.Cmd] = r
	}
	out := make([]investRow, 0, len(investRunCmds))
	for _, c := range investRunCmds {
		row := investRow{Title: c.Label, Meta: c.Cmd}
		r, ok := by[c.Cmd]
		switch {
		case !ok:
			row.Value, row.Bad = "✗ 기록 없음", true
		case r.FinishedAt == nil && now.Sub(r.StartedAt) < investRunHang:
			row.Value, row.Meta = shortTime(r.StartedAt), c.Cmd+" · 실행 중"
		case r.FinishedAt == nil:
			row.Value, row.Meta, row.Bad = "✗ "+shortTime(r.StartedAt), c.Cmd+" · 끝나지 않음", true
		case r.ExitCode != nil && *r.ExitCode == 0:
			row.Value = "✓ " + shortTime(r.StartedAt)
		default:
			code := "?"
			if r.ExitCode != nil {
				code = strconv.Itoa(int(*r.ExitCode))
			}
			what := "실패"
			if code == "75" {
				what = "나중에 다시"
			}
			row.Value, row.Meta, row.Bad = "✗ "+shortTime(r.StartedAt), fmt.Sprintf("%s · %s (종료 코드 %s)", c.Cmd, what, code), true
		}
		out = append(out, row)
	}
	return out
}

func mark(ok bool) string {
	if ok {
		return "✓ "
	}
	return "✗ "
}

// dday는 D-3 / D-day / D+2.
func dday(today, d time.Time) string {
	switch n := daysBetween(today, d); {
	case n > 0:
		return "D-" + strconv.Itoa(n)
	case n < 0:
		return "D+" + strconv.Itoa(-n)
	}
	return "D-day"
}

// daysBetween은 달력 날짜 사이 일수다(b - a).
func daysBetween(a, b time.Time) int { return int(math.Round(b.Sub(a).Hours() / 24)) }

func roundKRW(v float64) int64 { return int64(math.Round(v)) }

// decimal2는 소수 둘째 자리까지, 정수 부분은 천 단위 쉼표.
func decimal2(v float64) string {
	cents := int64(math.Round(math.Abs(v) * 100))
	s := group(cents/100) + "." + fmt.Sprintf("%02d", cents%100)
	if v < 0 && cents != 0 {
		return "-" + s
	}
	return s
}

// usd는 달러 금액 "$1,234.56".
func usd(v float64) string {
	s := decimal2(v)
	if len(s) > 0 && s[0] == '-' {
		return "-$" + s[1:]
	}
	return "$" + s
}

func derefStr(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}
