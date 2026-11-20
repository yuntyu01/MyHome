package web

import (
	"fmt"
	"time"

	"github.com/yuntyu01/MyHome/server/internal/invest"
)

// 매일 기록(사용자 요청, 2026-11-20 "매일 기록하면서 지금이 QQQM인지 TQQQ인지 화면에 보여주고 원금 수익률 금액도").
// 가격 갱신이 거래일마다 남긴 invest_daily를 그대로 보여 준다. 판정은 여전히 달 마지막 거래일 종가 하나라
// 오늘 신호는 참고이고, 이번 달에 사는 종목은 판정 종목(pos.Signal.Target)이다.

// investDailyMax는 화면에 싣는 최근 기록 수다(약 한 달 반).
const investDailyMax = 30

type investDaily struct {
	Has    bool             `json:"has"`
	Signal string           `json:"signal"` // 마지막 기록일 종가로 본 신호 종목
	Same   bool             `json:"same"`   // 이번 달 판정 종목과 같은가
	Title  string           `json:"title"`  // "11/19 종가 기준"
	Lines  []string         `json:"lines"`  // 근거와 판정과의 관계
	Rows   []investDailyRow `json:"rows"`   // 최근 날부터
}

type investDailyRow struct {
	Day         string `json:"day"` // "11/19"
	Signal      string `json:"signal"`
	Trend       string `json:"trend"` // S&P500이 185일선보다 "+8.5%"
	Vol         string `json:"vol"`   // 나스닥100 21일 변동성 "17.9%"
	C1          bool   `json:"c1"`
	C2          bool   `json:"c2"`
	HasMoney    bool   `json:"has_money"`
	InvestedKRW int64  `json:"invested_krw"` // 원금
	TotalKRW    int64  `json:"total_krw"`    // 금액(주식 + 현금)
	GainKRW     int64  `json:"gain_krw"`
	GainPct     string `json:"gain_pct"` // 수익률(원금 대비, 원화 현금 뺌)
}

func buildInvestDaily(now time.Time, pos invest.Position, rows []invest.DailyRow) investDaily {
	out := investDaily{Rows: []investDailyRow{}}
	for _, r := range rows {
		row := investDailyRow{Day: shortDay(invest.FormatDay(r.Day)), Signal: r.Signal,
			Trend: fmt.Sprintf("%+.1f%%", (r.SPX/r.SMA185-1)*100), Vol: fmt.Sprintf("%.1f%%", r.Vol*100), C1: r.C1, C2: r.C2}
		if r.HasAccount {
			row.HasMoney = true
			row.InvestedKRW, row.TotalKRW, row.GainKRW = r.InvestedKRW, r.StockKRW+r.CashKRW, r.GainKRW
			if r.InvestedKRW > 0 {
				row.GainPct = fmt.Sprintf("%+.1f%%", float64(r.GainKRW)/float64(r.InvestedKRW)*100)
			}
		}
		out.Rows = append(out.Rows, row)
	}
	if len(rows) == 0 {
		return out
	}
	last := rows[0]
	out.Has, out.Signal = true, last.Signal
	out.Title = shortDay(invest.FormatDay(last.Day)) + " 종가 기준"
	out.Lines = append(out.Lines,
		fmt.Sprintf("S&P500 %s · 185일선 %s (%+.1f%%) %s", decimal2(last.SPX), decimal2(last.SMA185), (last.SPX/last.SMA185-1)*100, okMark(last.C1)),
		fmt.Sprintf("나스닥100 21일 변동성 %.1f%% · 기준 %.0f%% 미만 %s", last.Vol*100, invest.VolThreshold*100, okMark(last.C2)))

	// 판정과의 관계. 판정은 달 마지막 거래일 종가로 하고 다음 달에 산다
	today := invest.TodayET(now)
	judge := invest.LastTradingDay(today.Year(), today.Month())
	if today.After(judge) || pos.YM == invest.YM(judge) {
		judge = invest.NextLastTradingDay(judge) // 이번 달 판정은 이미 했다
	}
	next := invest.YM(invest.Date(judge.Year(), judge.Month(), 1).AddDate(0, 1, 0))
	target, err := invest.Ticker(pos.Signal.Target)
	if err != nil {
		out.Lines = append(out.Lines, fmt.Sprintf("매일 기록은 참고예요 · 종목은 %s 종가로 정해요", shortDay(invest.FormatDay(judge))))
		return out
	}
	month := monthNameAt(pos.ApplyYM, now)
	out.Same = target == last.Signal
	if out.Same {
		// 종목 이름 뒤 조사(와/과)가 갈려서 괄호로 쓴다
		out.Lines = append(out.Lines, fmt.Sprintf("%s 종목(%s)과 같아요 · 다음 종목은 %s 종가로 정해요", month, target, shortDay(invest.FormatDay(judge))))
	} else {
		out.Lines = append(out.Lines, fmt.Sprintf("%s 종목은 %s · %s 종가도 이대로면 %s은 %s", month, target,
			shortDay(invest.FormatDay(judge)), monthNameAt(next, now), last.Signal))
	}
	return out
}
