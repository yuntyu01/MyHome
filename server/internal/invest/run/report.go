package run

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/yuntyu01/MyHome/server/internal/invest"
)

// StatusOpts는 status 옵션이다.
type StatusOpts struct{ Date string }

// Status는 현재 상태이자 deadman이다. 조용히 죽은 것을 찾아 실패로 끝낸다.
func (a *App) Status(ctx context.Context, o StatusOpts) int {
	today, err := a.today(o.Date)
	if err != nil {
		return a.fail(ExitFail, "%v", err)
	}
	pos, err := a.Store.LoadPosition(ctx)
	if err != nil {
		return a.fail(ExitFail, "%v", err)
	}
	devs, err := a.Store.LoadDeviations(ctx)
	if err != nil {
		return a.fail(ExitFail, "%v", err)
	}
	acct := pos.Holdings
	last, err := pos.SignalDate()
	if err != nil {
		return a.fail(ExitFail, "%v", err)
	}
	stale := daysBetween(last, today)

	a.printf("현재      %s %d주", dash(acct.Symbol), acct.Shares)
	tgt := "—"
	if pos.Signal.Target != "" {
		tgt = invest.MustTicker(pos.Signal.Target)
	}
	a.printf("판정      %s (%s) → %s %s", pos.YM, pos.LastRun, pos.ApplyYM, tgt)
	// 적립 미설정이어도 status는 떠야 한다 — deadman이 이 명령에 기댄다
	if amt, err := a.Store.ContributionFor(ctx, pos.ApplyYM); err != nil {
		a.printf("적립      미설정 — %v", err)
	} else {
		a.printf("적립      %s원/월", commas(float64(amt)))
	}
	judged := pos.JudgmentCount
	a.printf("준수      %d / %d   이탈 %d회", max(0, judged-len(devs)), judged, len(devs))
	cur := ""
	for _, d := range devs {
		if d.YM == pos.ApplyYM {
			cur = "미실행"
			if d.ActualTarget != nil {
				cur = *d.ActualTarget
			}
			break
		}
	}
	if cur != "" {
		a.printf("이탈      %s 이탈 확정 — 실제 %s", pos.ApplyYM, cur)
	} else {
		a.printf("이탈      이번 달 없음")
	}
	a.printf("규칙      SPX %d일선 · 변동성 %.0f%% · 월말", invest.SMAPeriod, invest.VolThreshold*100)

	// deadman ① 판정이 안 돌았나
	if stale > 35 {
		return a.fail(ExitFail, "지난달 판정 로그 없음 — %d일 경과. 판정 작업 실패 의심", stale)
	}
	a.printf("신선도    %d일 전 판정 ✓", stale)

	// 🔴 환율이 아직 추정인가 — 안 알리면 환전 스프레드가 조용히 새어 나간다
	if pos.FXEstimated {
		a.printf("환율      %s — 시장 환율 추정", plain2(pos.FXRate))
		a.printf("          증권사 적용 환율이 확인되면 맞출 것:")
		a.printf("          myhome invest amend --fill %s=<체결단가> --fx <실제환율>", orDefault(acct.Symbol, "<종목>"))
	} else if pos.FXRate != 0 {
		a.printf("환율      %s 확정 ✓", plain2(pos.FXRate))
	}

	// deadman ② 매수를 안 했나 — 안 사는 것이 가장 조용한 이탈이다
	t, err := time.Parse("2006-01", pos.ApplyYM)
	if err != nil {
		return a.fail(ExitFail, "apply_ym 형식 이상")
	}
	execDay := invest.FirstTradingDay(t.Year(), t.Month())
	switch {
	case pos.ExecutedThisMonth():
		a.printf("매수      %s 완료 (%s) ✓", pos.ApplyYM, *pos.ExecutedOn)
	case !today.After(execDay):
		// 실행일 당일은 아직 기회가 남았다 — 여기서 알리면 오경보다
		a.printf("매수      %s 예정 (%s)", pos.ApplyYM, invest.FormatDay(execDay))
	default:
		return a.fail(ExitFail, "%s 매수 미체결 — 실행일(%s) %d일 경과.\n  `myhome invest order` 로 주문서를 확인할 것.\n  안 사는 것도 이탈이다. 다만 사유도 잠금도 남지 않아 더 위험하다.",
			pos.ApplyYM, invest.FormatDay(execDay), daysBetween(execDay, today))
	}
	return ExitOK
}

func orDefault(s, d string) string {
	if s == "" {
		return d
	}
	return s
}

// Verify는 판정을 사람이 검산할 수 있게 원자료와 자체 점검 결과를 편다.
//
// 사람이 실제로 검산할 수 있는 것은 종가 하나다. SMA185나 21일 변동성은 눈으로 못 센다.
// 그래서 종가는 외부 대조용으로 크게 보여주고, 나머지는 기계가 대조한 결과를 보여준다.
func (a *App) Verify(ctx context.Context, date string) int {
	today, err := a.today(date)
	if err != nil {
		return a.fail(ExitFail, "%v", err)
	}
	prices, err := a.Store.LoadPrices(ctx)
	if err != nil {
		return a.fail(ExitFail, "%v", err)
	}
	in := prices.SignalInputs()
	full, err := invest.MonthlySignals(in, invest.SignalStart)
	if err != nil {
		return a.fail(ExitFail, "%v", err)
	}
	log := invest.CompletedSignals(full)
	if len(log) == 0 {
		return a.fail(ExitFail, "판정 가능한 월이 없다")
	}
	row := log[len(log)-1]
	k := 0
	for k < len(in.Days) && !in.Days[k].After(row.Day) {
		k++
	}
	win := in.SPX[max(0, k-invest.SMAPeriod):k]
	winDays := in.Days[max(0, k-invest.SMAPeriod):k]
	allRets := invest.PctChange(in.NDX[:k])
	r0 := max(1, k-invest.VolWindow)
	rets := allRets[r0:]
	retDays := in.Days[r0:k]
	margin := (row.SPX/row.SMA185 - 1) * 100
	vgap := (invest.VolThreshold - row.Vol) * 100

	a.printf("═══ %s 판정 검산 (%s) ═══\n", row.YM, invest.FormatDay(row.Day))
	a.printf("① 추세  SPX > SMA185")
	a.printf("   SPX 종가      %12s   ← 외부에서 확인 가능한 유일한 값", plain2(row.SPX))
	a.printf("   SMA185        %12s   ← 185일 '평균'이지 185일 '전 가격'이 아니다", plain2(row.SMA185))
	lo, hi, loD, hiD := math.Inf(1), math.Inf(-1), time.Time{}, time.Time{}
	for i, v := range win {
		if v < lo {
			lo, loD = v, winDays[i]
		}
		if v > hi {
			hi, hiD = v, winDays[i]
		}
	}
	a.printf("     표본        %d일  %s ~ %s", len(win), invest.FormatDay(winDays[0]), invest.FormatDay(winDays[len(winDays)-1]))
	a.printf("     창 최저      %12s  (%s)", plain2(lo), invest.FormatDay(loD))
	a.printf("     창 최고      %12s  (%s)", plain2(hi), invest.FormatDay(hiD))
	a.printf("     ↑ 평균이 이 둘 사이에 없으면 계산이 틀린 것이다")
	a.printf("     참고: 185일 전 종가 %s — 판정에 쓰지 않는 값", plain2(win[0]))
	near := ""
	if math.Abs(margin) < 0.5 {
		near = "   ⚠ 경계 근접"
	}
	a.printf("   여유          %+11.2f%%   %s%s", margin, passFail(row.C1), near)
	a.printf("")
	a.printf("② 변동성  realized_vol(NDX,21) < 30%%")
	a.printf("   변동성        %11.2f%%", row.Vol*100)
	rmin, rmax := math.Inf(1), math.Inf(-1)
	for _, r := range rets {
		rmin, rmax = math.Min(rmin, r), math.Max(rmax, r)
	}
	mean := 0.0
	for _, r := range rets {
		mean += r
	}
	mean /= float64(len(rets))
	ss := 0.0
	for _, r := range rets {
		ss += (r - mean) * (r - mean)
	}
	std := math.Sqrt(ss / float64(len(rets)-1))
	a.printf("     표본        수익률 %d개 (종가 %d개)  %s ~ %s", len(rets), len(rets)+1, invest.FormatDay(retDays[0]), invest.FormatDay(retDays[len(retDays)-1]))
	a.printf("     일간 표준편차 %9.4f%%  × √252 = %.2f%%  (ddof=1)", std*100, row.Vol*100)
	a.printf("     일간 최대    %+9.2f%%   최소 %+.2f%%", rmax*100, rmin*100)
	a.printf("     ↑ 이 범위에 비해 변동성이 터무니없으면 계산이 틀린 것이다")
	near = ""
	if math.Abs(vgap) < 1 {
		near = "   ⚠ 경계 근접"
	}
	a.printf("   여유          %+11.2f%%p  %s%s", vgap, passFail(row.C2), near)
	a.printf("\n   → %s  %s\n", row.ApplyYM, row.Pos)

	a.printf("═══ 외부 대조 ═══")
	a.printf("   %s S&P500 종가가 %s인지 확인하세요.", invest.FormatDay(row.Day), plain2(row.SPX))
	a.printf("   네이버금융 '스탠다드앤드푸어스500' · investing.com · Yahoo Finance ^GSPC")
	a.printf("   이 값만 맞으면 나머지는 아래 기계 점검이 보증합니다.\n")

	a.printf("═══ 자체 점검 ═══")
	bad, err := invest.CheckFixture(full)
	if err != nil {
		return a.fail(ExitFail, "%v", err)
	}
	if len(bad) == 0 {
		a.printf("   픽스처 %d행 대조      ✓ 일치", invest.FixtureRows)
	} else {
		a.printf("   픽스처 %d행 대조      ✗ %d행 불일치", invest.FixtureRows, len(bad))
	}
	a.printf("   SMA 표본 수              %s %d / %d", mark(len(win) == invest.SMAPeriod), len(win), invest.SMAPeriod)
	a.printf("   변동성 표본 수            %s %d / %d", mark(len(rets) == invest.VolWindow), len(rets), invest.VolWindow)
	a.printf("   SPX 정상범위              %s", mark(row.SPX > 1000 && row.SPX < 50000))
	a.printf("   변동성 정상범위            %s", mark(row.Vol > 0 && row.Vol < 3))
	sw := invest.Switches12M(log)
	a.printf("   12개월 전환               %s %d회 (실측 최대 4)", mark(sw <= invest.MaxSwitches12M), sw)
	lastRow, _ := prices.Last()
	a.printf("   데이터 최신일             %s  (%d일 전)", invest.FormatDay(lastRow.Day), daysBetween(lastRow.Day, today))
	a.printf("   규칙                     SPX %d / vol %.0f%% / 월말", invest.SMAPeriod, invest.VolThreshold*100)

	// 데이터 무결성 — 실제로 겪은 데이터 사고는 전부 세면 잡히는 것들이라 눈이 필요 없다
	a.printf("\n═══ 데이터 무결성 ═══")
	known := map[time.Time]bool{invest.Date(2001, 9, 10): true} // 9·11 — 09-17 재개장
	var gaps []string
	knownHit := 0
	for i := 1; i < len(prices); i++ {
		if d := daysBetween(prices[i-1].Day, prices[i].Day); d > 5 {
			if known[prices[i-1].Day] {
				knownHit++
				continue
			}
			gaps = append(gaps, invest.FormatDay(prices[i-1].Day)+" → "+invest.FormatDay(prices[i].Day))
		}
	}
	if len(gaps) == 0 {
		note := ""
		if knownHit > 0 {
			note = "  (알려진 휴장 " + itoa(knownHit) + "건 제외)"
		}
		a.printf("   거래일 공백 5일 초과        ✓ 없음%s", note)
	} else {
		a.printf("   거래일 공백 5일 초과        ✗ %d건 %s", len(gaps), strings.Join(gaps[max(0, len(gaps)-3):], ", "))
	}
	for _, col := range []string{"SPX", "NDX"} {
		odd := 0
		for i := 1; i < len(prices); i++ {
			if math.Abs(prices[i].Get(col)/prices[i-1].Get(col)-1) > 0.25 {
				odd++
			}
		}
		a.printf("   %s 일간 25%% 초과 변동     %s", col, okCount(odd))
	}
	// 상장 전·동결 후 결측은 정상이다. 구간 한가운데 뚫린 구멍만 사고다
	// USDKRW 2017-10-09은 추석 휴장이라 원래 값이 없다 — 신호가 환율을 쓰지 않으므로 무해하다
	var holes []string
	for _, col := range []string{"QQQ", "QQQM", "QLD", "TQQQ", "USDKRW", "USDKRW_MKT"} {
		first, last := -1, -1
		for i, r := range prices {
			if !math.IsNaN(r.Get(col)) {
				if first < 0 {
					first = i
				}
				last = i
			}
		}
		n := 0
		for i := first; first >= 0 && i <= last; i++ {
			if math.IsNaN(prices[i].Get(col)) {
				n++
			}
		}
		if n > 0 && !(col == "USDKRW" && n == 1) {
			holes = append(holes, col+" "+itoa(n)+"일")
		}
	}
	sort.Strings(holes)
	if len(holes) == 0 {
		a.printf("   컬럼 중간 결측             ✓ 없음")
	} else {
		a.printf("   컬럼 중간 결측             ✗ %s", strings.Join(holes, ", "))
	}
	a.printf("   SPX·NDX 거래일 일치        ✓") // 마스터 스키마가 둘을 NOT NULL로 묶는다
	complete := lastRow.Day.Equal(invest.LastTradingDay(lastRow.Day.Year(), lastRow.Day.Month()))
	if complete {
		a.printf("   마지막 월 완결 여부         ✓ (%s)", invest.FormatDay(lastRow.Day))
	} else {
		a.printf("   마지막 월 완결 여부         ✗ %s 진행 중 (%s)", invest.YM(lastRow.Day), invest.FormatDay(lastRow.Day))
	}
	a.printf("\n   숫자가 전부 ✓면 눈으로 볼 이유가 없다 —")
	a.printf("   과거 사고 넷은 모두 위 항목 중 하나에 걸린다 (SPEC §4.5)")
	a.printf("\n   전체 회귀: cd server && go test ./internal/invest/...")
	return ExitOK
}

func passFail(ok bool) string {
	if ok {
		return "통과"
	}
	return "탈락"
}

func okCount(n int) string {
	if n == 0 {
		return "✓ 없음"
	}
	return "✗ " + itoa(n) + "건"
}

// Why는 확신이 흔들릴 때 여는 곳이다. 지금 겪는 것이 정상 범위인지를 보여준다.
//
// 🔴 비교표(backtest)를 여기 넣지 않는다. 확신은 다른 전략과의 비교가 아니라 내가 무엇을 알고
// 시작했는가에서 온다.
func (a *App) Why(ctx context.Context, date string) int {
	today, err := a.today(date)
	if err != nil {
		return a.fail(ExitFail, "%v", err)
	}
	prices, err := a.Store.LoadPrices(ctx)
	if err != nil {
		return a.fail(ExitFail, "%v", err)
	}
	full, err := invest.MonthlySignals(prices.SignalInputs(), invest.SignalStart)
	if err != nil {
		return a.fail(ExitFail, "%v", err)
	}
	log := invest.CompletedSignals(full)
	pos, err := a.Store.LoadPosition(ctx)
	if err != nil {
		return a.fail(ExitFail, "%v", err)
	}
	cur := pos.Signal.Target // 신호 로그와 같은 역할 공간에서 센다
	runLen := 0
	for i := len(log) - 1; i >= 0 && log[i].Pos == cur; i-- {
		runLen++
	}
	type run struct {
		pos string
		n   int
	}
	var runs []run
	for _, r := range log {
		if len(runs) > 0 && runs[len(runs)-1].pos == r.Pos {
			runs[len(runs)-1].n++
		} else {
			runs = append(runs, run{r.Pos, 1})
		}
	}
	lengths := func(p string) []int {
		var out []int
		for _, r := range runs {
			if r.pos == p {
				out = append(out, r.n)
			}
		}
		sort.Ints(out)
		return out
	}
	same := lengths(cur)
	if len(same) == 0 {
		return a.fail(ExitFail, "%s 구간이 신호 로그에 없다", cur)
	}
	longer := 0
	for _, n := range same {
		if n >= runLen {
			longer++
		}
	}
	tk := invest.MustTicker(cur)
	a.printf("═══ 지금 %s %d개월째 ═══\n", tk, runLen)
	a.printf("  과거 %s 구간 %d개", tk, len(same))
	a.printf("    최장   %3d개월", same[len(same)-1])
	a.printf("    중앙   %3d개월", same[len(same)/2])
	a.printf("    최단   %3d개월", same[0])
	a.printf("\n  %d개월 이상 지속된 구간이 과거에 %d개 있었다 (%.0f%%)", runLen, longer, float64(longer)/float64(len(same))*100)
	if longer > 0 {
		a.printf("  → 정상 범위다. 더 길었던 적이 %d번 있다.", longer)
	} else {
		a.printf("  → 과거 최장(%d개월)을 넘었다. 데이터 소스를 점검할 것.", same[len(same)-1])
	}
	if q := lengths(invest.RiskOff); len(q) > 0 {
		a.printf("\n  참고 — %s 최장 구간은 %d개월이었다 (2000-09~2003-03, 닷컴)", invest.Instrument[invest.RiskOff], q[len(q)-1])
		a.printf("  그 기간 내내 %s를 들고 있었다면 −80%% 이상이었다.", invest.Instrument[invest.RiskOn])
	}
	fmt.Fprint(a.Out, whyText)

	// 6개월 지난 이탈 사유를 여기서 읽는다. 웹페이지에는 두지 않는다 — 남에게 보일 글이 아니다
	devs, err := a.Store.LoadDeviations(ctx)
	if err != nil {
		return a.fail(ExitFail, "%v", err)
	}
	var due []invest.Deviation
	for _, d := range devs {
		if r, err := invest.ParseDay(d.ResurfaceOn); err == nil && !r.After(today) && strings.TrimSpace(d.Reason) != "" {
			due = append(due, d)
		}
	}
	if len(due) > 0 {
		a.printf("═══ 그때 이렇게 적으셨습니다 ═══\n")
		for _, d := range due {
			act := "미실행"
			if d.ActualTarget != nil {
				act = *d.ActualTarget
			}
			a.printf("  %s · %s %s → %s", d.OccurredOn, d.YM, d.SignalTarget, act)
			a.printf("    %s\n", d.Reason)
		}
	}
	return ExitOK
}

const whyText = `
═══ 처음부터 알던 것 ═══

  7년 손실확률        11.5%   (8~9번 중 1번은 원금을 잃는다)
  7년 최악            1.26억 → 0.66억  (−48%)
  QQQ에 머무는 기간    전체의 34%
  1년 이상 뒤처지는 구간  3~4년에 한 번

  지금 나쁜 것이 계획에 없던 일이 아니다.
  백테스트 성적은 이 구간들을 전부 통과한 뒤의 값이다.

═══ 왜 이 규칙이었나 ═══

  백테스트 순위 때문이 아니다. 45개 조합의 표본내↔표본외 순위 상관은
  −0.107이었고, 독립 재현에서도 ρ = −0.071이었다.
  1등을 골라도 표본 밖에서는 무작위다.

  고른 이유는 세 요소가 외부 문헌에 각각 존재하기 때문이다.
    · S&P500 이동평균 레버리지 타이밍 — Gayed·Bilello (2016)
    · 신호원으로 QQQ보다 SPX — TradingView "QQQ Timing"
    · 레버리지 ETF 변동성 필터 — QuantPedia

═══ 바꾸면 나아지나 ═══

  SPX N일   7년 중앙(닷컴제외)   7년 손실%(전체)
    175          4.26억            9.5%
    185 (현재)    4.34억           11.5%
    200          4.15억            7.1%

  하방 지표는 200 > 175 > 185 순이다. 중앙값 차이는 노이즈 범위다.
  어느 값이 우월하다는 근거가 데이터에 없다 — 그래서 바꾸는 것은
  개선이 아니라 재추첨이다.

═══ 그래도 못 견디겠으면 ═══

  myhome invest deviate --symbol <종목>          (사유는 선택)

  24시간 뒤 한 번 더 실행해야 확정된다. 사유는 6개월 뒤 다시 보여준다.
  과거 백테스트 원본은 docs/tq/전략_SPX185_백테스트결과_v2.xlsx
`

// ReconcileOpts는 reconcile 옵션이다.
type ReconcileOpts struct{ ApplyCash bool }

// Reconcile은 앱이 기억하는 계좌 ↔ 증권사 실제 계좌 대조다. 읽기 전용이다.
//
// 불일치가 나면 자동으로 고치지 않는다. 수량이 다르면 외부 매매가 있었다는 뜻이고, 그건 사람이
// 확인해야 할 사건이다. 현금 차이만 --apply-cash로 맞출 수 있다(배당 등).
func (a *App) Reconcile(ctx context.Context, o ReconcileOpts) int {
	client, err := a.Broker(ctx)
	if err != nil {
		return a.fail(ExitFail, "%v", err)
	}
	a.printf("증권사 %s 조회 중… (계좌 %s)", client.Env(), client.AccountMasked())
	bal, err := client.Balance(ctx)
	if err != nil {
		return a.fail(ExitFail, "잔고 조회 실패: %v", err)
	}
	pos, err := a.Store.LoadPosition(ctx)
	if err != nil {
		return a.fail(ExitFail, "%v", err)
	}
	acct := pos.Holdings
	a.printf("\n%10s%10s%10s   판정", "", "앱", "실제")
	ok := true
	if acct.Symbol != "" {
		real := bal.SharesOf(acct.Symbol)
		match := real == acct.Shares
		ok = ok && match
		verdict := "✓"
		if !match {
			verdict = "🚨 불일치"
		}
		a.printf("%10s%9d주%9d주   %s", acct.Symbol, acct.Shares, real, verdict)
	}
	for _, h := range bal.Holdings {
		if h.Symbol != acct.Symbol {
			ok = false
			a.printf("%10s%10s%9d주   🚨 앱이 모르는 보유", h.Symbol, "—", h.Shares)
		}
	}
	diff := bal.CashUSD - acct.CashUSD
	cashOK := math.Abs(diff) < 1
	verdict := "✓"
	if !cashOK {
		verdict = plain2(diff) + "$ (배당·수수료 추정)"
	}
	a.printf("%10s%9.2f$%9.2f$   %s", "현금", acct.CashUSD, bal.CashUSD, verdict)

	// 매수가능 탐침 — 읽기 전용. 통합증거금이 실제로 걸리는지 주문 전에 본다
	if prices, err := a.Store.LoadPrices(ctx); err == nil {
		if last, okp := prices.Last(); okp {
			tgt := invest.MustTicker(orDefault(pos.Signal.Target, invest.RiskOn))
			est := last.Get(tgt) * (1 + SizingBuffer)
			if b, err := client.Buyable(ctx, tgt, est); err != nil {
				a.printf("%9s%10s%10s   ⚠ 조회 실패: %v", "매수가능", "—", "—", err)
			} else if b.MaxQty <= 0 {
				a.printf("%9s%10s%9d주   🚨 0주 (주문가능 %s) — 통합증거금 적용 여부·입금 확인", "매수가능", "—", b.MaxQty, usd(b.CashUSD))
			} else {
				a.printf("%9s%10s%9d주   ✓ (주문가능 %s · %s 기준)", "매수가능", "—", b.MaxQty, usd(b.CashUSD), tgt)
			}
		}
	}
	if !ok {
		return a.fail(ExitFail, "\n수량 불일치 — 증권사에서 매매가 있었다. 즉시 확인할 것.\n  앱이 자동으로 맞추지 않는다. 무슨 일이 있었는지가 먼저다.")
	}
	if !cashOK {
		a.printf("\n⚠ 현금만 %s$ 차이. 배당이면 정상이다.", plain2(diff))
		if o.ApplyCash {
			next := pos
			next.Holdings = acct.Clone()
			next.Holdings.CashUSD = bal.CashUSD
			if err := a.Store.SavePosition(ctx, next); err != nil {
				return a.fail(ExitFail, "%v", err)
			}
			a.printf("  → 현금을 %.2f$로 맞췄다", bal.CashUSD)
		} else {
			a.printf("  반영하려면: myhome invest reconcile --apply-cash")
		}
		return ExitOK
	}
	a.printf("\n✓ 일치")
	return ExitOK
}
