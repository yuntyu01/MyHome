package web

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/yuntyu01/MyHome/server/internal/dbq"
	"github.com/yuntyu01/MyHome/server/internal/invest"
	"github.com/yuntyu01/MyHome/server/internal/period"
)

// 투자 첫 화면의 맨 위 요약과 이번 달 자동 매수 단계(사용자 요청, 2026-11-20).
//
// 요약: 지금 종목·수량, 증권 총액(주식 + 현금), 주식에 넣은 돈(누적 원금). TQ는 시장 숫자를 첫 화면에 두지 않았지만
// (§4.6) 사용자가 첫 화면에서 보기를 원했다. 세전이다(토스 앱 숫자와 맞춘다). 세후는 평가 화면에 그대로 있다.
//
// 단계: 판정 검산 → 돈 입금 → 환전 → 구매 신청 → 구매 성공을 GitHub Actions처럼 체크한다. 매매 작업이 증권사에서
// 본 사실(invest_flow)과 position·원금 내역으로만 만든다 — 웹은 증권사를 부르지 않는다.

// 단계 상태.
const (
	stepDone    = "done"    // 끝남 ✓
	stepRunning = "running" // 진행 중(확인 중·체결 기다림)
	stepWaiting = "waiting" // 아직 차례가 아님
	stepFailed  = "failed"  // 문제
	stepSkipped = "skipped" // 이번 달은 해당 없음(기록 없음 등)
)

type investSummary struct {
	Symbol      string `json:"symbol"`
	Shares      int    `json:"shares"`
	Target      string `json:"target"` // 이번 적용월 신호 종목
	CanValue    bool   `json:"can_value"`
	TotalKRW    int64  `json:"total_krw"`    // 증권 총액 = 주식 + 달러 현금 + 원화 현금 (세전)
	StockKRW    int64  `json:"stock_krw"`    // 주식 평가
	CashKRW     int64  `json:"cash_krw"`     // 달러 현금(원화 환산) + 원화 현금
	InvestedKRW int64  `json:"invested_krw"` // 주식에 넣은 돈(누적 원금)
	GainKRW     int64  `json:"gain_krw"`     // (주식 + 달러 현금) − 원금. 아직 안 바꾼 원화는 원금에 없어 뺀다
	GainPct     string `json:"gain_pct"`
	Basis       string `json:"basis"`
}

type investStep struct {
	Key   string   `json:"key"`
	Title string   `json:"title"`
	State string   `json:"state"`
	Value string   `json:"value"` // 오른쪽 짧은 글(시각·예정일)
	Lines []string `json:"lines"` // 눌렀을 때 펼치는 근거
}

type investFlow struct {
	YM    string       `json:"ym"`
	Title string       `json:"title"`
	State string       `json:"state"`
	Value string       `json:"value"`
	Steps []investStep `json:"steps"`
}

// investFlowInputs는 요약·단계가 position 말고 더 읽는 것이다.
type investFlowInputs struct {
	Events     []invest.FlowEvent
	DepositDay int
	KRW        float64 // 마지막 원화 주문가능
	KRWAt      time.Time
	HasKRW     bool
}

func buildInvestSummary(now time.Time, pos invest.Position, prices invest.Prices, in investFlowInputs) investSummary {
	acct := pos.Holdings
	s := investSummary{Symbol: acct.Symbol, Shares: acct.Shares, InvestedKRW: roundKRW(pos.ContributedKRW)}
	if t, err := invest.Ticker(pos.Signal.Target); err == nil {
		s.Target = t
	}
	fx, fxDay := prices.ValuationFX()
	price, priceDay, priceOK := 0.0, time.Time{}, true
	if acct.Symbol != "" && acct.Shares > 0 {
		price, priceDay, priceOK = lastClose(prices, acct.Symbol)
	}
	if !priceOK || fx <= 0 {
		return s
	}
	s.CanValue = true
	krw := 0.0
	if in.HasKRW {
		krw = in.KRW
	}
	// 매일 기록(refresh)과 같은 식이다
	v := invest.Value(acct, pos.ContributedKRW, price, fx, krw)
	s.StockKRW = roundKRW(v.StockKRW)
	s.CashKRW = roundKRW(v.CashKRW)
	s.TotalKRW = roundKRW(v.TotalKRW())
	s.GainKRW = roundKRW(v.GainKRW)
	if g, ok := v.GainRatio(); ok {
		s.GainPct = fmt.Sprintf("%+.1f%%", g*100)
	}
	parts := []string{}
	if acct.Symbol != "" && acct.Shares > 0 {
		parts = append(parts, dayNameAt(invest.FormatDay(priceDay), now)+" 종가")
	}
	parts = append(parts, "환율 "+decimal2(fx)+" ("+dayNameAt(invest.FormatDay(fxDay), now)+")")
	if in.HasKRW {
		parts = append(parts, "원화 "+kstTime(in.KRWAt)+" 기준")
	}
	s.Basis = strings.Join(parts, " · ")
	return s
}

// buildInvestFlow는 적용월(pos.ApplyYM)의 단계다. judgeRun·tradeRun은 마지막 실행 기록(없으면 nil)이다.
func buildInvestFlow(now time.Time, pos invest.Position, ledger []invest.LedgerEntry, plan int64, judgeRun, tradeRun *dbq.InvestRun, in investFlowInputs) investFlow {
	today := invest.TodayET(now)
	ym := pos.ApplyYM
	f := investFlow{YM: ym, Title: monthNameAt(ym, now) + " 자동 매수"}
	byStep := map[string][]invest.FlowEvent{}
	for _, e := range in.Events {
		byStep[e.Step] = append(byStep[e.Step], e)
	}
	executed := pos.ExecutedThisMonth()
	buyDay, _ := invest.BuyDayOf(ym, in.DepositDay)
	deadline, _ := invest.BuyDeadlineOf(ym)
	var entry *invest.LedgerEntry
	for i := range ledger {
		if ledger[i].YM == ym && ledger[i].Source != "amend" {
			entry = &ledger[i]
		}
	}
	tradeFailed := tradeRun != nil && tradeRun.FinishedAt != nil && tradeRun.ExitCode != nil &&
		*tradeRun.ExitCode != 0 && *tradeRun.ExitCode != 75 && !executed

	// ① 판정 검산
	j := investStep{Key: "judge", Title: "판정 검산", State: stepDone, Value: shortDay(pos.LastRun)}
	sig := pos.Signal
	if sig.SMA185 > 0 {
		j.Lines = append(j.Lines, fmt.Sprintf("S&P500 %s · 185일선 %s (%+.1f%%) %s",
			decimal2(sig.SPX), decimal2(sig.SMA185), (sig.SPX/sig.SMA185-1)*100, okMark(sig.C1)))
	}
	j.Lines = append(j.Lines, fmt.Sprintf("나스닥100 21일 변동성 %.1f%% · 기준 %.0f%% 미만 %s", sig.Vol*100, invest.VolThreshold*100, okMark(sig.C2)))
	target := "—"
	if t, err := invest.Ticker(sig.Target); err == nil {
		target = t
	}
	j.Lines = append(j.Lines, fmt.Sprintf("→ %s은 %s (둘 다 맞으면 %s, 아니면 %s)", monthNameAt(ym, now), target, invest.Instrument[invest.RiskOn], invest.Instrument[invest.RiskOff]))
	j.Lines = append(j.Lines, dayNameAt(pos.LastRun, now)+" 종가로 판정 · 판정 전에 336행 검산표와 맞춰 봄")
	if judgeRun != nil && judgeRun.FinishedAt != nil && judgeRun.ExitCode != nil && *judgeRun.ExitCode != 0 && *judgeRun.ExitCode != 75 {
		if last, err := invest.ParseDay(pos.LastRun); err == nil && judgeRun.StartedAt.After(last) {
			j.State, j.Value = stepFailed, "실패"
			j.Lines = append(j.Lines, fmt.Sprintf("마지막 판정 실행이 실패했어요 (%s, 종료 코드 %d) · 위 숫자는 그 전 판정", kstTime(judgeRun.StartedAt), *judgeRun.ExitCode))
		}
	}

	// ② 돈 입금
	dep := investStep{Key: "deposit", Title: "돈 입금"}
	var deposited float64
	for _, e := range byStep[invest.StepDeposit] {
		deposited += e.KRW
		dep.Lines = append(dep.Lines, fmt.Sprintf("%s · %s원 들어옴", kstTime(e.At), group(roundKRW(e.KRW))))
	}
	seen := invest.DepositSeen(deposited, float64(plan))
	switch {
	case seen:
		dep.State = stepDone
		dep.Value = kstTime(byStep[invest.StepDeposit][0].At)
	case executed:
		dep.State, dep.Value = stepSkipped, "기록 없음"
		dep.Lines = append(dep.Lines, "이번 달 입금 기록이 없어요 (있던 돈으로 샀거나, 기록을 시작하기 전)")
	case in.DepositDay == 0 && today.Before(buyDay):
		dep.State, dep.Value = stepWaiting, shortDay(invest.FormatDay(buyDay))+" 예정"
		dep.Lines = append(dep.Lines, "설정 → 투자에서 입금일을 정하면 그 날 전에는 사지 않아요")
	case in.DepositDay > 0 && (today.Before(invest.Date(today.Year(), today.Month(), in.DepositDay)) || invest.YM(today) != ym):
		dep.State, dep.Value = stepWaiting, fmt.Sprintf("%d일 예정", in.DepositDay)
	case !today.After(deadline):
		dep.State, dep.Value = stepRunning, "확인 중"
	default:
		dep.State, dep.Value = stepFailed, "안 보임"
		dep.Lines = append(dep.Lines, fmt.Sprintf("%s까지 적립금이 안 들어와서 사지 못했어요", shortDay(invest.FormatDay(deadline))))
	}
	if !seen && !executed {
		dep.Lines = append(dep.Lines, fmt.Sprintf("주문가능금액이 적립액에 모자라면 %s까지 기다려요(그래도 모자라면 알림)", shortDay(invest.FormatDay(deadline))))
	}
	if plan > 0 {
		dep.Lines = append(dep.Lines, "적립 계획 "+group(plan)+"원/월")
	}
	if in.DepositDay > 0 && !seen && !executed {
		dep.Lines = append(dep.Lines, fmt.Sprintf("입금일 %d일 · 매일 밤 0시 17분에 토스증권 입금을 확인해요", in.DepositDay))
	}
	if in.HasKRW {
		dep.Lines = append(dep.Lines, fmt.Sprintf("토스증권 원화 %s원 (%s 기준)", group(roundKRW(in.KRW)), kstTime(in.KRWAt)))
	}

	// ③ 환전
	fxs := investStep{Key: "exchange", Title: "환전"}
	exch := byStep[invest.StepExchange]
	for _, e := range exch {
		fxs.Lines = append(fxs.Lines, fmt.Sprintf("%s · %s원 → %s", kstTime(e.At), group(roundKRW(e.KRW)), usd(e.USD)))
		if e.FX > 0 {
			if e.Mid > 0 {
				d := e.FX - e.Mid
				fxs.Lines = append(fxs.Lines, "적용 환율 "+decimal2(e.FX)+"원 · 그때 기준율 "+decimal2(e.Mid)+"원",
					fmt.Sprintf("기준율보다 %s원 (%s)", signed2(d), pct2(d/e.Mid)))
			} else {
				fxs.Lines = append(fxs.Lines, "적용 환율 "+decimal2(e.FX)+"원")
			}
		}
	}
	newMoney := entry != nil && entry.AmountKRW > 0
	switch {
	case len(exch) > 0:
		fxs.State, fxs.Value = stepDone, kstTime(exch[len(exch)-1].At)
	case executed && entry != nil && !newMoney:
		fxs.State, fxs.Value = stepSkipped, "필요 없음"
		fxs.Lines = append(fxs.Lines, "이번 달은 있던 달러로 샀어요")
	case executed && pos.FXEstimated:
		fxs.State, fxs.Value = stepRunning, "결제 기다림"
		fxs.Lines = append(fxs.Lines, "추정 환율 "+decimal2(pos.FXRate)+"원 · 결제(다음 날)가 끝나면 토스 적용 환율로 바뀌어요")
	case executed:
		fxs.State, fxs.Value = stepDone, "확정"
		fxs.Lines = append(fxs.Lines, "토스 적용 환율 "+decimal2(pos.FXRate)+"원 (결제 내역 기준, 환전 시각 기록은 없음)")
	case len(byStep[invest.StepOrder]) > 0:
		fxs.State, fxs.Value = stepRunning, "주문과 함께"
		fxs.Lines = append(fxs.Lines, "통합증거금이 주문할 때 필요한 만큼 원화를 달러로 바꿔요")
	default:
		fxs.State = stepWaiting
		fxs.Lines = append(fxs.Lines, "주문할 때 통합증거금이 필요한 만큼 원화를 달러로 바꿔요 (따로 할 일 없음)")
	}

	// ④ 구매 신청
	ord := investStep{Key: "order", Title: "구매 신청"}
	orders := byStep[invest.StepOrder]
	for _, e := range orders {
		price := ""
		if e.Price > 0 && strings.Contains(e.Style, "지정가") {
			price = " @ " + usd(e.Price)
		}
		ord.Lines = append(ord.Lines, fmt.Sprintf("%s · %s %s %d주 · %s%s", kstTime(e.At), e.Symbol, sideWord(e.Side), e.Qty, e.Style, price))
	}
	switch {
	case len(orders) > 0:
		ord.State, ord.Value = stepDone, kstTime(orders[len(orders)-1].At)
	case executed:
		ord.State, ord.Value = stepDone, "직접"
		ord.Lines = append(ord.Lines, "자동 주문 기록이 없어요 (토스 앱에서 직접 산 것을 장부에 적은 달)")
	case tradeFailed:
		ord.State, ord.Value = stepFailed, "실패"
		ord.Lines = append(ord.Lines, fmt.Sprintf("자동 매매 실행이 실패했어요 (%s, 종료 코드 %d) · 알림을 확인하세요", kstTime(tradeRun.StartedAt), *tradeRun.ExitCode))
	default:
		ord.State = stepWaiting
		if invest.YM(today) == ym {
			ord.Value = shortDay(invest.FormatDay(buyDay)) + " 예정"
		}
		if in.DepositDay > 0 {
			ord.Lines = append(ord.Lines, fmt.Sprintf("입금이 보이면 그날 밤(한국 0시 17분, 미국 장중)에 주문해요 · 늦어도 %s", shortDay(invest.FormatDay(deadline))))
		} else {
			ord.Lines = append(ord.Lines, shortDay(invest.FormatDay(buyDay))+" 밤(한국 0시 17분, 미국 장중)에 주문해요")
		}
	}

	// ⑤ 구매 성공
	fill := investStep{Key: "fill", Title: "구매 성공"}
	fills := byStep[invest.StepFill]
	for _, e := range fills {
		fill.Lines = append(fill.Lines, fmt.Sprintf("%s · %s %s %d주 · 평균 %s", kstTime(e.At), e.Symbol, sideWord(e.Side), e.Qty, usd(e.Price)))
	}
	switch {
	case executed:
		// 체결을 확인한 한국 시각(다른 단계와 같은 시계). 기록이 없으면 장부의 체결일(뉴욕)
		fill.State, fill.Value = stepDone, shortDay(derefStr(pos.ExecutedOn))
		if len(fills) > 0 {
			fill.Value = kstTime(fills[len(fills)-1].At)
		}
		if entry != nil && entry.Shares != nil {
			fill.Lines = append(fill.Lines, fmt.Sprintf("%s %+d주 · 원금 +%s원", entry.Symbol, *entry.Shares, group(roundKRW(entry.AmountKRW))))
		}
		fill.Lines = append(fill.Lines, fmt.Sprintf("지금 %s %d주 · 남은 달러 %s", pos.Holdings.Symbol, pos.Holdings.Shares, usd(pos.Holdings.CashUSD)))
	case len(orders) > 0:
		fill.State, fill.Value = stepRunning, "체결 기다림"
		fill.Lines = append(fill.Lines, "다음 실행(매일 밤 0시 17분)에 체결을 확인해요")
	default:
		fill.State = stepWaiting
	}

	f.Steps = []investStep{j, dep, fxs, ord, fill}
	// 전체 상태: 문제가 하나라도 있으면 문제, 구매가 끝났으면 완료, 무엇이든 진행됐으면 진행 중
	f.State, f.Value = stepWaiting, "대기"
	for _, s := range f.Steps {
		if s.State == stepFailed {
			f.State, f.Value = stepFailed, "확인 필요"
			return f
		}
	}
	switch {
	case executed:
		f.State, f.Value = stepDone, "완료"
	case dep.State == stepDone || len(orders) > 0:
		f.State, f.Value = stepRunning, "진행 중"
	}
	return f
}

func okMark(ok bool) string {
	if ok {
		return "✓"
	}
	return "✗"
}

func sideWord(side string) string {
	if side == "sell" {
		return "매도"
	}
	return "매수"
}

// shortDay는 "2026-12-05"를 "12/5"로.
func shortDay(s string) string {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return s
	}
	return fmt.Sprintf("%d/%d", t.Month(), t.Day())
}

// kstTime은 한국 시각 "12/5 14:02".
func kstTime(t time.Time) string {
	k := t.In(period.KST)
	return fmt.Sprintf("%d/%d %s", k.Month(), k.Day(), k.Format("15:04"))
}

// signed2는 "+1.45" / "-0.30".
func signed2(v float64) string {
	if v >= 0 {
		return "+" + decimal2(v)
	}
	return decimal2(v)
}

// pct2는 비율을 "+0.10%"로.
func pct2(r float64) string {
	return fmt.Sprintf("%+.2f%%", math.Round(r*10000)/100)
}
