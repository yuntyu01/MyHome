package server_test

import (
	"context"
	"fmt"
	"html"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/yuntyu01/MyHome/server/internal/invest"
	"github.com/yuntyu01/MyHome/server/internal/invest/store"
	"github.com/yuntyu01/MyHome/server/internal/period"
)

// 투자 화면(읽기 전용). 상태는 합성 데이터이고, 날짜는 오늘(뉴욕)을 기준으로 잡는다.

// 10주 × $50.25 × 1,400 + $12.50 × 1,400 = 721,000원 (세금 없음)
const investNetWon = "721,000원"

type investSeed struct {
	applyYM    string // 비우면 다음 달(예정)
	lastPrice  time.Time
	executedYM string
}

// seedInvest는 store 메서드로 마이그레이션 직후 같은 상태를 만든다.
func (c *webClient) seedInvest(o investSeed) time.Time {
	c.t.Helper()
	ctx := context.Background()
	st := store.New(c.pool, "test")
	today := invest.TodayET(time.Now())
	if o.lastPrice.IsZero() {
		o.lastPrice = today.AddDate(0, 0, -1)
	}
	if o.applyYM == "" {
		o.applyYM = invest.YM(invest.Date(today.Year(), today.Month(), 1).AddDate(0, 1, 0))
	}
	prev := invest.EmptyRow(o.lastPrice.AddDate(0, 0, -1))
	prev.SPX, prev.NDX, prev.TQQQ, prev.QQQM, prev.USDKRWMkt = 6000, 21000, 49, 250, 1400
	last := invest.EmptyRow(o.lastPrice) // 시장 환율은 하루 늦게 붙는다
	last.SPX, last.NDX, last.TQQQ, last.QQQM = 6010, 21050, 50.25, 251
	if _, err := st.CopyPrices(ctx, invest.Prices{prev, last}); err != nil {
		c.t.Fatal(err)
	}
	if err := st.ImportContribution(ctx, invest.Contribution{EffectiveFrom: "2020-01", Amount: 600_000, Reason: "테스트 적립액", RecordedOn: "2020-01-01"}); err != nil {
		c.t.Fatal(err)
	}
	pos := invest.Position{
		Holdings: invest.Account{Symbol: "TQQQ", Shares: 10, CostKRW: 600_000, CashUSD: 12.5},
		LastRun:  invest.FormatDay(today.AddDate(0, 0, -3)), YM: invest.YM(today.AddDate(0, 0, -3)), ApplyYM: o.applyYM,
		Signal: invest.Judgment{Target: "TQQQ"}, FXRate: 1400, ContributedKRW: 600_000, JudgmentCount: 1,
	}
	if o.executedYM != "" {
		pos.ExecutedYM, pos.ExecutedOn = invest.StrPtr(o.executedYM), invest.StrPtr(o.executedYM+"-02")
	}
	if err := st.SavePosition(ctx, pos); err != nil {
		c.t.Fatal(err)
	}
	shares := 10
	if err := st.ImportLedger(ctx, invest.LedgerEntry{On: "2026-01-02", YM: "2026-01", AmountKRW: 600_000, Source: "trade", Symbol: "TQQQ", Shares: &shares, Note: "합성 매수"}); err != nil {
		c.t.Fatal(err)
	}
	for _, cmd := range []string{"refresh", "judge", "trade", "status"} {
		id, err := st.StartRun(ctx, cmd, "")
		if err != nil {
			c.t.Fatal(err)
		}
		if err := st.FinishRun(ctx, id, 0, "ok"); err != nil {
			c.t.Fatal(err)
		}
	}
	return today
}

// dayLabel은 화면의 기준일 표시다: "10월 15일", 올해가 아니면 "2025년 10월 15일".
func dayLabel(d time.Time) string {
	if d.Year() == time.Now().In(period.KST).Year() {
		return fmt.Sprintf("%d월 %d일", d.Month(), d.Day())
	}
	return fmt.Sprintf("%d년 %d월 %d일", d.Year(), d.Month(), d.Day())
}

func (c *webClient) page(path string) string {
	c.t.Helper()
	rec := c.get(path)
	if rec.Code != http.StatusOK {
		c.t.Fatalf("%s status %d: %s", path, rec.Code, rec.Body.String())
	}
	if csp := rec.Header().Get("Content-Security-Policy"); !strings.Contains(csp, "script-src 'self'") {
		c.t.Errorf("%s CSP 헤더 없음", path)
	}
	return html.UnescapeString(rec.Body.String()) // html/template은 "+"를 &#43;로 쓴다
}

// shortDay는 다른 화면과 같은 "10월 15일" 표시다.
func shortDay(s string) string {
	d, _ := invest.ParseDay(s)
	return fmt.Sprintf("%d월 %d일", d.Month(), d.Day())
}

func TestWebInvestNotMigrated(t *testing.T) {
	c := newWebClient(t)
	for _, path := range []string{"/invest", "/invest/value"} {
		if body := c.page(path); !strings.Contains(body, "아직 옮기지 않았어요") || !strings.Contains(body, `href="/invest" aria-current="page"`) {
			t.Errorf("%s: 옮기기 전 안내와 투자 탭 선택이 보여야 함", path)
		}
	}
	// 읽기 전용: POST 라우트가 없다
	if rec := c.post("/invest", nil, false); rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST /invest status %d, want 405", rec.Code)
	}
}

func TestWebInvestMainHidesMarketNumbers(t *testing.T) {
	c := newWebClient(t)
	today := c.seedInvest(investSeed{})
	main := c.page("/invest")
	for _, want := range []string{"TQQQ 10주", "문제 없음", "예정 ", "다음 판정일", "다음 실행일", "가격 갱신", "월말 판정", "자동 매매", "상태 점검", "원금 대조", "✓ 일치", `href="/invest/value"`} {
		if !strings.Contains(main, want) {
			t.Errorf("첫 화면에 %q 없음", want)
		}
	}
	// 시장 숫자(평가액·가격·환율·수익률)는 첫 화면에 없다
	for _, banned := range []string{investNetWon, "721,000", "$50.25", "1,400.00", "+20.2%", "가격 갱신이 멈췄", `action="/invest`} {
		if strings.Contains(main, banned) {
			t.Errorf("첫 화면에 %q가 있음", banned)
		}
	}
	if !strings.Contains(main, "마지막 가격 "+dayLabel(today.AddDate(0, 0, -1))) {
		t.Error("신선도 옆에 기준일이 보여야 함")
	}

	value := c.page("/invest/value")
	for _, want := range []string{investNetWon, "$50.25 (" + dayLabel(today.AddDate(0, 0, -1)) + ")", "1,400.00 (" + dayLabel(today.AddDate(0, 0, -2)) + ")",
		"600,000원", "+121,000원", "+20.2%", "$12.50", "자동매수", "TQQQ +10주", "합성 매수", "누적 원금과 일치"} {
		if !strings.Contains(value, want) {
			t.Errorf("평가 화면에 %q 없음", want)
		}
	}
}

func TestWebInvestMissedPurchase(t *testing.T) {
	c := newWebClient(t)
	today := invest.TodayET(time.Now())
	// 지난달이 적용월인데 체결 기록이 없다 — 첫 거래일이 이미 지났다
	lastMonth := invest.YM(invest.Date(today.Year(), today.Month(), 1).AddDate(0, -1, 0))
	c.seedInvest(investSeed{applyYM: lastMonth})
	main := c.page("/invest")
	if !strings.Contains(main, `<span class="amount neg">✗ 매수 안 됨</span>`) || !strings.Contains(main, "확인할 문제 1건") {
		t.Errorf("미체결이 첫 화면에 경고로 보여야 함:\n%s", main)
	}

	// 그달 체결이 있으면 경고 없음
	c2 := newWebClient(t)
	c2.seedInvest(investSeed{applyYM: lastMonth, executedYM: lastMonth})
	if main := c2.page("/invest"); strings.Contains(main, "매수 안 됨") || !strings.Contains(main, "✓ 완료 "+shortDay(lastMonth+"-02")) {
		t.Error("체결한 달은 완료로 보여야 함")
	}
}

func TestWebInvestStalePriceBanner(t *testing.T) {
	c := newWebClient(t)
	today := invest.TodayET(time.Now())
	stale := today.AddDate(0, 0, -20)
	c.seedInvest(investSeed{lastPrice: stale})
	banner := "가격 갱신이 멈췄어요 (마지막 " + dayLabel(stale) + ")"
	for _, path := range []string{"/invest", "/invest/value"} {
		if body := c.page(path); !strings.Contains(body, banner) {
			t.Errorf("%s에 %q 없음", path, banner)
		}
	}
	if main := c.page("/invest"); !strings.Contains(main, "✗ 20일 전") {
		t.Error("가격 신선도가 문제로 보여야 함")
	}
}
