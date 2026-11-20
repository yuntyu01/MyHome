package web

import (
	"strings"
	"testing"

	"github.com/yuntyu01/MyHome/server/internal/invest"
)

// 매일 기록: 오늘 신호(참고)와 거래일마다의 원금·금액·수익률.

func dailyRows() []invest.DailyRow {
	return []invest.DailyRow{
		{Day: invest.Date(2026, 11, 19), SPX: 6840.2, SMA185: 6301.55, Vol: 0.179, C1: true, C2: true, Signal: "TQQQ",
			HasAccount: true, Symbol: "TQQQ", Shares: 64, StockKRW: 6_617_695, CashKRW: 2_405, InvestedKRW: 6_166_291, GainKRW: 452_808},
		{Day: invest.Date(2026, 11, 18), SPX: 6200, SMA185: 6300, Vol: 0.21, C1: false, C2: true, Signal: "QQQM"},
	}
}

func TestInvestDailyShowsTodaysSignalAndMoney(t *testing.T) {
	d := buildInvestDaily(etNow(2026, 11, 20), basePosition(), dailyRows())
	if !d.Has || d.Signal != "TQQQ" || !d.Same || d.Title != "11/19 종가 기준" {
		t.Fatalf("%+v", d)
	}
	all := strings.Join(d.Lines, "\n")
	for _, want := range []string{"S&P500 6,840.20 · 185일선 6,301.55 (+8.5%) ✓", "변동성 17.9% · 기준 30% 미만 ✓", "11월 종목(TQQQ)과 같아요", "11/30 종가로"} {
		if !strings.Contains(all, want) {
			t.Fatalf("%q 없음:\n%s", want, all)
		}
	}
	r := d.Rows[0]
	if r.Day != "11/19" || !r.HasMoney || r.InvestedKRW != 6_166_291 || r.TotalKRW != 6_620_100 || r.GainPct != "+7.3%" || r.Trend != "+8.5%" {
		t.Fatalf("%+v", r)
	}
	// 밀린 날은 신호만
	if o := d.Rows[1]; o.HasMoney || o.Signal != "QQQM" || o.Trend != "-1.6%" || o.C1 {
		t.Fatalf("%+v", o)
	}
}

// 신호가 판정 종목과 다르면 이대로 가면 다음 달에 바뀐다고만 말한다(이번 달 매수는 그대로).
func TestInvestDailySaysWhatTheNextJudgmentWouldBe(t *testing.T) {
	rows := dailyRows()[1:]
	d := buildInvestDaily(etNow(2026, 11, 20), basePosition(), rows)
	if d.Same || d.Signal != "QQQM" {
		t.Fatalf("%+v", d)
	}
	if got := d.Lines[len(d.Lines)-1]; got != "11월 종목은 TQQQ · 11/30 종가도 이대로면 12월은 QQQM" {
		t.Fatalf("%q", got)
	}
	// 그달 판정을 마친 저녁이면 다음 판정은 다음 달 말이다
	p := basePosition()
	p.YM, p.ApplyYM, p.LastRun = "2026-11", "2026-12", "2026-11-30"
	d = buildInvestDaily(etNow(2026, 11, 30), p, rows)
	if got := d.Lines[len(d.Lines)-1]; got != "12월 종목은 TQQQ · 12/31 종가도 이대로면 1월은 QQQM" &&
		got != "12월 종목은 TQQQ · 12/31 종가도 이대로면 2027년 1월은 QQQM" {
		t.Fatalf("%q", got)
	}
}

func TestInvestDailyEmptyBeforeTheFirstRefresh(t *testing.T) {
	d := buildInvestDaily(etNow(2026, 11, 20), basePosition(), nil)
	if d.Has || d.Rows == nil || len(d.Rows) != 0 {
		t.Fatalf("%+v", d)
	}
}
