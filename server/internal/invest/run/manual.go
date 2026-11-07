package run

import (
	"context"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/yuntyu01/MyHome/server/internal/invest"
	"github.com/yuntyu01/MyHome/server/internal/invest/store"
)

// ExecuteOpts는 execute(수동 체결 기록) 옵션이다.
type ExecuteOpts struct {
	Date   string
	DryRun bool
	Fills  map[string]float64 // 실제 체결단가
	FX     float64            // 증권사 실제 적용 환율. 0이면 시장 환율로 추정
	Shares *int               // 실제 체결 수량. nil이면 적립액에서 계산
	Paid   *float64           // 실제로 넣은 원화. nil이면 계획 적립액
}

// Execute는 체결 기록 → 계좌 갱신이다. 신호를 다시 계산하지 않는다.
// 자동 매매가 다 막혀 증권사 앱에서 직접 샀을 때 쓰는 복구 경로다.
func (a *App) Execute(ctx context.Context, o ExecuteOpts) int {
	today, err := a.today(o.Date)
	if err != nil {
		return a.fail(ExitFail, "%v", err)
	}
	if len(o.Fills) == 0 {
		return a.fail(ExitFail, "--fill TQQQ=62.15 형식으로 체결단가를 넣을 것")
	}
	pos, prices, devs, rc := a.loadTradeState(ctx)
	if rc != ExitOK {
		return rc
	}
	acct := pos.Holdings
	ym := pos.ApplyYM

	// 🔴 같은 달을 두 번 기록하지 않는다. 손으로 치는 명령이라 재실행이 흔하다
	if pos.ExecutedOn != nil && pos.ExecutedThisMonth() {
		return a.fail(ExitFail, "%s은 이미 %s에 체결 기록됨 — 중복 실행 거부.\n  잘못 기록했다면 `amend`로 단가·환율을 정정할 것 (수량·종목은 정정 대상이 아니다)", ym, *pos.ExecutedOn)
	}
	target, deviated, err := allowedSymbol(pos, devs)
	if err != nil {
		return a.fail(ExitFail, "%v", err)
	}
	if target == "" {
		return a.fail(ExitFail, "%s은 미실행 이탈로 기록된 달이다. 사후 체결 기록을 받지 않는다", ym)
	}
	var stray []string
	for s := range o.Fills {
		if s != target && s != acct.Symbol {
			stray = append(stray, s)
		}
	}
	if len(stray) > 0 {
		sort.Strings(stray)
		return a.fail(ExitFail, "%v는 이번 달 매수 대상이 아니다. 허용 종목은 %s뿐이다.\n  규칙을 벗어나려면 `myhome invest deviate`를 쓸 것 (24시간 간격 2회 실행)", stray, target)
	}
	if deviated {
		a.printf("⚠ 이탈 확정분으로 실행 중 — 신호는 %s, 실제 %s", invest.MustTicker(pos.Signal.Target), target)
	}
	if _, ok := o.Fills[target]; !ok {
		return a.fail(ExitFail, "목표 종목 %s의 체결단가가 없다", target)
	}
	if acct.Symbol != "" && acct.Symbol != target {
		if _, ok := o.Fills[acct.Symbol]; !ok {
			return a.fail(ExitFail, "매도할 %s의 체결단가가 없다", acct.Symbol)
		}
	}
	c, err := buildCtx(today, pos, acct, prices.SignalInputs(), sellLegsFor(acct, target), target)
	if err != nil {
		return a.fail(ExitFail, "%v", err)
	}
	if fails := invest.Preflight(c); len(fails) > 0 {
		for _, x := range fails {
			a.warnf("    · %s", x)
		}
		return a.fail(ExitFail, "preflight 실패 — 자동 실행 중단. 우회 옵션은 없다. 원인을 고칠 것")
	}

	// 🔴 원금은 실제로 넣은 돈이다 — 계획 금액이 아니다 (실측 2026-08: 계획 60만 · 실제 244.7만)
	plan, err := a.Store.ContributionFor(ctx, ym)
	if err != nil {
		return a.fail(ExitFail, "%v", err)
	}
	if o.Paid != nil && *o.Paid <= 0 {
		return a.fail(ExitFail, "실제 투입액은 0보다 커야 한다: %v", *o.Paid)
	}
	amount := float64(plan)
	if o.Paid != nil {
		amount = *o.Paid
		if math.Abs(amount-float64(plan)) >= 1 {
			a.printf("  계획 %s원 · 실제 투입 %s원 — 실제 금액으로 기록한다", commas(float64(plan)), won(amount))
		}
	}
	year := today.Year()
	row, ok := prices.AsOf(today)
	if !ok {
		return a.fail(ExitFail, "%s 이전 가격이 없다", invest.FormatDay(today))
	}
	// 🔴 환율은 --fx가 있으면 그것이 정답, 없으면 시장 환율로 추정한다. 추정이라는 사실을 남긴다
	fx, fxEst := invest.TradeFX(o.FX, row)
	if fx <= 0 {
		return a.fail(ExitFail, "환율을 구할 수 없다. --fx로 넣거나 refresh로 USDKRW_MKT를 채울 것")
	}
	if msg := rejectAbsurdFills(o.Fills, row); msg != "" {
		return a.fail(ExitFail, "%s", msg)
	}
	before := snapshot(acct, pos)
	var newAcct invest.Account
	if o.Shares == nil {
		newAcct, err = invest.MonthlyStep(acct, target, amount, fx, o.Fills, year, invest.FeeRate)
	} else {
		// 🔴 --shares를 주면 그 수량을 그대로 적는다. 손으로 산 달은 사람이 정답이다
		if *o.Shares < 0 {
			return a.fail(ExitFail, "체결 수량이 음수: %d", *o.Shares)
		}
		stepped := acct
		if acct.Symbol != "" && acct.Symbol != target {
			if stepped, err = invest.SellAll(stepped, o.Fills[acct.Symbol], fx, year, invest.FeeRate); err != nil {
				return a.fail(ExitFail, "%v", err)
			}
		}
		if stepped, err = invest.Contribute(stepped, amount, fx); err != nil {
			return a.fail(ExitFail, "%v", err)
		}
		newAcct, err = invest.BuyExact(stepped, target, *o.Shares, o.Fills[target], fx, invest.FeeRate)
	}
	if err != nil {
		return a.fail(ExitFail, "%v", err)
	}
	if o.DryRun {
		est := ""
		if fxEst {
			est = "  (환율 추정)"
		}
		a.printf("[dry-run] %s %s %d주 · 잔여 %s%s", ym, target, newAcct.Shares, usd(newAcct.CashUSD), est)
		return ExitOK
	}
	late := c.LateDays
	next := invest.Position{
		Holdings: newAcct, LastRun: pos.LastRun, YM: pos.YM, Signal: pos.Signal, FXRate: fx, ApplyYM: ym,
		// 누적 적립은 실제로 넣은 돈을 더한다
		ContributedKRW: pos.ContributedKRW + amount, MonthsInvested: pos.MonthsInvested + 1,
		JudgmentCount: pos.JudgmentCount, CloseUSD: closesOf(row),
		ExecutedOn: invest.StrPtr(invest.FormatDay(today)), ExecutedYM: invest.StrPtr(ym),
		LateDays: &late, FXEstimated: fxEst, Before: before,
	}
	shares := newAcct.Shares - acct.Shares
	note := ""
	if fxEst {
		note = "환율 추정"
	}
	err = a.Store.InTx(ctx, func(s *store.Store) error {
		if err := s.SavePosition(ctx, next); err != nil {
			return err
		}
		return s.AppendLedger(ctx, invest.LedgerEntry{On: invest.FormatDay(today), YM: ym, AmountKRW: amount, Source: "execute", Symbol: target, Shares: &shares, Note: note})
	})
	if err != nil {
		return a.fail(ExitFail, "저장 실패: %v", err)
	}
	a.printf("%s 체결 완료", ym)
	a.printf("  %s %d주 · 잔여 %s", newAcct.Symbol, newAcct.Shares, usd(newAcct.CashUSD))
	a.printf("  평가 %s원 (세후)", won(newAcct.NetValueKRW(o.Fills[target], fx)))
	if late > 0 {
		a.printf("  ⚠ 실행일(%s)에서 %d일 지연으로 기록", invest.FormatDay(c.ExecDay), late)
	}
	if newAcct.Shares == 0 {
		a.printf("  ⚠ %s 단가 %s가 이번 달 가용액을 넘어 0주 — 잔여 현금은 다음 달로 이월된다", target, usd(o.Fills[target]))
	}
	if fxEst {
		a.printf("  ⚠ 환율 %s는 시장 환율 추정이다. 증권사 적용값이 확인되면", plain2(fx))
		a.printf("     myhome invest amend --fill %s=<체결단가> --fx <실제환율>", target)
	}
	return ExitOK
}

// AmendOpts는 amend(체결 정정) 옵션이다.
type AmendOpts struct {
	Date   string
	DryRun bool
	Fills  map[string]float64
	FX     float64 // 필수
}

// Amend는 직전 체결의 단가·환율만 고친다.
//
// 고칠 수 있는 것은 '실제로 얼마에 체결됐는가'뿐이다. 종목·달·적립액을 바꾸는 것은 정정이 아니라
// 이탈이다. 되돌릴 지점(before)이 없으면 정정하지 않는다 — 추측으로 역산하면 틀린 계좌가 맞는 척한다.
func (a *App) Amend(ctx context.Context, o AmendOpts) int {
	today, err := a.today(o.Date)
	if err != nil {
		return a.fail(ExitFail, "%v", err)
	}
	if o.FX <= 0 {
		return a.fail(ExitFail, "--fx로 증권사 적용 환율을 넣을 것")
	}
	pos, prices, devs, rc := a.loadTradeState(ctx)
	if rc != ExitOK {
		return rc
	}
	acct := pos.Holdings
	ym := pos.ApplyYM
	if !pos.ExecutedThisMonth() {
		return a.fail(ExitFail, "%s에 정정할 체결 기록이 없다. `execute`가 먼저다", ym)
	}
	if pos.Before == nil {
		return a.fail(ExitFail, "%s 체결에 복원 지점이 없다 (구버전 기록). 정정하지 않는다.\n  추측으로 역산하면 틀린 계좌가 맞는 것처럼 남는다 — invest_state_history로 확인할 것", ym)
	}
	if len(o.Fills) == 0 {
		return a.fail(ExitFail, "--fill TQQQ=64.90 형식으로 정정할 체결단가를 넣을 것")
	}
	target, _, err := allowedSymbol(pos, devs)
	if err != nil || target == "" {
		return a.fail(ExitFail, "이 달의 체결 종목을 알 수 없다: %v", err)
	}
	prev := pos.Before.Holdings.Clone()
	allowed := map[string]bool{target: true}
	if prev.Symbol != "" {
		allowed[prev.Symbol] = true
	}
	var stray []string
	for s := range o.Fills {
		if !allowed[s] {
			stray = append(stray, s)
		}
	}
	if len(stray) > 0 {
		sort.Strings(stray)
		return a.fail(ExitFail, "%v는 이 달의 체결 종목이 아니다.\n  종목을 바꾸는 것은 정정이 아니라 이탈이다 — `deviate`를 쓸 것", stray)
	}
	if _, ok := o.Fills[target]; !ok {
		return a.fail(ExitFail, "목표 종목 %s의 체결단가가 없다", target)
	}
	if prev.Symbol != "" && prev.Symbol != target {
		if _, ok := o.Fills[prev.Symbol]; !ok {
			return a.fail(ExitFail, "전환 달이다 — 매도한 %s의 체결단가도 --fill로 넣을 것", prev.Symbol)
		}
	}
	execOn, err := invest.ParseDay(*pos.ExecutedOn)
	if err != nil {
		return a.fail(ExitFail, "executed_on 형식 이상: %v", err)
	}
	// 정정도 같은 오타 방어를 거친다 — 체결 당일 종가와 대조
	if row, ok := prices.AsOf(execOn); ok {
		if msg := rejectAbsurdFills(o.Fills, row); msg != "" {
			return a.fail(ExitFail, "%s", msg)
		}
	}
	// 🔴 수량은 다시 계산하지 않는다 — 단가·환율만 고친다 (실측: 18주 체결을 재계산하니 19주)
	year := execOn.Year()
	stepped := prev
	if prev.Symbol != "" && prev.Symbol != target {
		if stepped, err = invest.SellAll(stepped, o.Fills[prev.Symbol], o.FX, year, invest.FeeRate); err != nil {
			return a.fail(ExitFail, "%v", err)
		}
	}
	bought := acct.Shares - stepped.Shares // 이 달에 실제로 산 수량
	if bought < 0 {
		return a.fail(ExitFail, "장부 수량(%d)이 체결 전(%d)보다 적다. 정정으로 고칠 수 있는 상태가 아니다", acct.Shares, stepped.Shares)
	}
	// 🔴 원금도 trade와 같은 규칙으로 다시 매긴다
	spentNow := float64(bought) * o.Fills[target] * (1 + invest.FeeRate)
	newMoneyKRW := math.Max(0, spentNow-stepped.CashUSD) * o.FX
	if stepped, err = invest.Contribute(stepped, newMoneyKRW, o.FX); err != nil {
		return a.fail(ExitFail, "%v", err)
	}
	newAcct, err := invest.BuyExact(stepped, target, bought, o.Fills[target], o.FX, invest.FeeRate)
	if err != nil {
		return a.fail(ExitFail, "%v", err)
	}
	a.printf("%s 체결 정정", ym)
	a.printf("  이전   %s %d주 · 잔여 %s", acct.Symbol, acct.Shares, usd(acct.CashUSD))
	a.printf("  정정   %s %d주 · 잔여 %s", newAcct.Symbol, newAcct.Shares, usd(newAcct.CashUSD))
	if o.DryRun {
		a.printf("[dry-run] 저장하지 않았다")
		return ExitOK
	}
	n := pos.AmendCount + 1
	next := pos
	next.Holdings = newAcct
	next.FXRate = o.FX
	next.ContributedKRW = pos.Before.ContributedKRW + newMoneyKRW
	next.MonthsInvested = pos.Before.MonthsInvested + 1
	next.AmendedOn = invest.StrPtr(invest.FormatDay(today))
	next.AmendCount = n
	// 🔴 fx_estimated를 여기서 끈다 — 켜지기만 하고 안 꺼지는 경고는 무시된다
	next.FXEstimated = false
	shares := newAcct.Shares - pos.Before.Holdings.Shares
	err = a.Store.InTx(ctx, func(s *store.Store) error {
		if err := s.SavePosition(ctx, next); err != nil {
			return err
		}
		return s.AppendLedger(ctx, invest.LedgerEntry{On: invest.FormatDay(today), YM: ym, AmountKRW: newMoneyKRW, Source: "amend",
			Symbol: newAcct.Symbol, Shares: &shares, Note: "정정 " + itoa(n) + "회차"})
	})
	if err != nil {
		return a.fail(ExitFail, "저장 실패: %v", err)
	}
	a.printf("  정정 %d회차로 기록 — invest_state_history에 그대로 남는다", n)
	return ExitOK
}

func itoa(n int) string { return strconv.Itoa(n) }

// Ledger는 원금 내역이다. 총액과 내역 합을 대조한다 — 어긋나면 실패로 끝낸다.
func (a *App) Ledger(ctx context.Context) int {
	pos, err := a.Store.LoadPosition(ctx)
	if err != nil {
		return a.fail(ExitFail, "%v", err)
	}
	rows, err := a.Store.LoadLedger(ctx)
	if err != nil {
		return a.fail(ExitFail, "%v", err)
	}
	total := pos.ContributedKRW
	if len(rows) == 0 {
		a.printf("원금 내역이 없다")
		if total != 0 {
			a.printf("  ⚠ 그런데 누적적립은 %s원이다", won(total))
		}
		return ExitOK
	}
	label := map[string]string{"execute": "수동기록", "trade": "자동매수", "adjust": "앱 밖 매수", "amend": "체결정정"}
	a.printf("원금 내역")
	running := 0.0
	for _, r := range rows {
		running += r.AmountKRW
		held := ""
		if r.Shares != nil {
			held = " " + r.Symbol + " " + signed(*r.Shares) + "주"
		}
		note := ""
		if r.Note != "" {
			note = "  · " + r.Note
		}
		what := label[r.Source]
		if what == "" {
			what = r.Source
		}
		a.printf("  %s  %s  %11s원  %s%s%s", r.On, r.YM, won(r.AmountKRW), what, held, note)
	}
	a.printf("  %22s  %11s원  (%d건)", "합계", won(running), len(rows))
	// 🔴 여기가 이 명령의 핵심이다
	if gap := total - running; math.Abs(gap) >= 1 {
		a.printf("\n🔴 누적적립 %s원과 %s원 어긋난다", won(total), won(gap))
		a.printf("   내역에 안 남은 투입이 있거나, 총액이 내역 없이 바뀌었다는 뜻이다.")
		return ExitFail
	}
	a.printf("  누적적립 %s원과 일치", won(total))
	return ExitOK
}

func signed(n int) string {
	if n >= 0 {
		return "+" + itoa(n)
	}
	return "-" + itoa(-n)
}

// AdjustOpts는 adjust(앱 밖 매수 반영) 옵션이다.
type AdjustOpts struct {
	Date   string
	DryRun bool
	Shares *int
	Paid   *float64
	Symbol string
	Auto   bool
	FX     float64
	Reason string
}

// Adjust는 앱 밖에서 추가로 산 것을 장부에 넣는다. 이탈이 아니다 — 적립액을 바꾸는 행위다.
// 증권사는 원화로 얼마를 넣었는지 모르므로 아는 사람이 적는다. 앱 밖 매도는 다루지 않는다.
func (a *App) Adjust(ctx context.Context, o AdjustOpts) int {
	today, err := a.today(o.Date)
	if err != nil {
		return a.fail(ExitFail, "%v", err)
	}
	pos, err := a.Store.LoadPosition(ctx)
	if err != nil {
		return a.fail(ExitFail, "%v", err)
	}
	acct := pos.Holdings
	want := invest.MustTicker(pos.Signal.Target)
	sym := acct.Symbol
	if sym == "" {
		sym = want
	}
	if o.Symbol != "" {
		if sym, err = invest.Ticker(o.Symbol); err != nil {
			return a.fail(ExitFail, "%v", err)
		}
	}
	// 🔴 조회로 채울 수 있는 것은 채운다. 못 알려주는 것은 매수 시점 환율 하나뿐이다
	if o.Auto {
		client, err := a.Broker(ctx)
		if err != nil {
			return a.fail(ExitFail, "증권사 연결 실패: %v", err)
		}
		bal, err := client.Balance(ctx)
		if err != nil {
			return a.fail(ExitFail, "잔고 조회 실패: %v", err)
		}
		var hold *struct {
			shares int
			avg    float64
		}
		for _, h := range bal.Holdings {
			if h.Symbol == sym {
				hold = &struct {
					shares int
					avg    float64
				}{h.Shares, h.AvgPriceUSD}
			}
		}
		if hold == nil {
			return a.fail(ExitFail, "증권사에 %s 보유가 없다", sym)
		}
		gained := hold.shares - acct.Shares
		if gained <= 0 {
			return a.fail(ExitFail, "증권사 %d주 · 장부 %d주 — 늘어난 것이 없다", hold.shares, acct.Shares)
		}
		// 🔴 평단이 0이면 응답에 그 필드가 없었다는 뜻이다 — 원금 0원이 조용히 기록되지 않게 멈춘다
		if hold.avg <= 0 {
			return a.fail(ExitFail, "%s 매입평균가를 못 읽었다 (0).\n  --shares %d --paid <실제 낸 원화> 로 직접 넣을 것", sym, gained)
		}
		fx, est := o.FX, false
		if fx <= 0 {
			prices, err := a.Store.LoadPrices(ctx)
			if err != nil {
				return a.fail(ExitFail, "%v", err)
			}
			last, _ := prices.Last()
			fx, est = invest.TradeFX(0, last)
		}
		if fx <= 0 {
			return a.fail(ExitFail, "환율을 구할 수 없다. --fx로 넣을 것")
		}
		paid := float64(gained) * hold.avg * fx
		o.Shares, o.Paid = &gained, &paid
		a.printf("조회 결과 — %s 증권사 %d주 · 장부 %d주 → 추가 %d주", sym, hold.shares, acct.Shares, gained)
		tail := ""
		if est {
			tail = "  ⚠ 시장 환율 추정"
		}
		a.printf("  매입평균 $%.4f × 환율 %s → %s원%s", hold.avg, plain2(fx), won(paid), tail)
	}
	if o.Shares == nil || o.Paid == nil {
		return a.fail(ExitFail, "--auto 를 쓰거나 --shares 15 --paid 1400000 으로 직접 넣을 것")
	}
	if acct.Symbol != "" && sym != acct.Symbol {
		return a.fail(ExitFail, "보유 중인 %s와 다른 %s — 전량 교체 규칙 위반.\n  종목을 바꾸는 것은 `deviate`의 일이다", acct.Symbol, sym)
	}
	if sym != want && acct.Symbol == "" {
		return a.fail(ExitFail, "이번 달 신호는 %s인데 %s을 넣으려 한다", want, sym)
	}
	// 증권사 실제 보유와 대조 — 숫자가 안 맞으면 뭔가 더 있었다는 뜻
	newShares := acct.Shares + *o.Shares
	if !o.Auto {
		if client, err := a.Broker(ctx); err != nil {
			a.warnf("⚠ 잔고 대조 생략 — %v", err)
		} else if bal, err := client.Balance(ctx); err != nil {
			a.warnf("⚠ 잔고 대조 생략 — %v", err)
		} else if real := bal.SharesOf(sym); real != newShares {
			return a.fail(ExitFail, "반영하면 %d주가 되는데 증권사는 %d주다.\n  현재 장부 %d주 + 입력 %d주. 수량을 다시 확인할 것", newShares, real, acct.Shares, *o.Shares)
		}
	}
	if o.DryRun {
		a.printf("[dry-run] %s %d → %d주 · 누적적립 %s → %s원", sym, acct.Shares, newShares, won(pos.ContributedKRW), won(pos.ContributedKRW+*o.Paid))
		return ExitOK
	}
	next := pos
	next.Holdings = acct.Clone()
	next.Holdings.Symbol, next.Holdings.Shares = sym, newShares
	next.Holdings.CostKRW = acct.CostKRW + *o.Paid // 실제로 낸 돈이 취득원가다
	next.ContributedKRW = pos.ContributedKRW + *o.Paid
	shares := *o.Shares
	err = a.Store.InTx(ctx, func(s *store.Store) error {
		if err := s.AppendAdjustment(ctx, invest.Adjustment{On: invest.FormatDay(today), Symbol: sym, Shares: shares, PaidKRW: *o.Paid, Reason: o.Reason}); err != nil {
			return err
		}
		if err := s.SavePosition(ctx, next); err != nil {
			return err
		}
		return s.AppendLedger(ctx, invest.LedgerEntry{On: invest.FormatDay(today), YM: invest.YM(today), AmountKRW: *o.Paid, Source: "adjust", Symbol: sym, Shares: &shares, Note: "앱 밖 매수"})
	})
	if err != nil {
		return a.fail(ExitFail, "%v", err)
	}
	a.printf("%s %d → %d주", sym, acct.Shares, newShares)
	a.printf("  취득원가   %s → %s원", won(acct.CostKRW), won(next.Holdings.CostKRW))
	a.printf("  누적적립   %s → %s원", won(pos.ContributedKRW), won(next.ContributedKRW))
	a.printf("\n  다음 전환 때 이 수량까지 전량 매도된다 (§12.1.1.2)")
	return ExitOK
}

// TaxOpts는 tax 옵션이다.
type TaxOpts struct {
	Year   int // 0이면 전체
	Paid   int // 그 연도분 납부 기록. 0이면 리포트
	DryRun bool
}

// Tax는 연도별 해외주식 양도소득세다. 신고를 대신하지 않는다 — 대조용 숫자다.
func (a *App) Tax(ctx context.Context, o TaxOpts) int {
	pos, err := a.Store.LoadPosition(ctx)
	if err != nil {
		return a.fail(ExitFail, "%v", err)
	}
	acct := pos.Holdings
	gains := acct.RealizedGainKRW
	// 🔴 납부를 기록하지 않으면 낸 세금이 평가금액에서 영원히 차감된다
	if o.Paid != 0 {
		y := o.Paid
		due := acct.TaxDueKRW(y)
		if due <= 0 {
			return a.fail(ExitFail, "%d년분 세액이 0원이다 — 납부할 것이 없다", y)
		}
		if acct.TaxPaidKRW[y] >= due {
			a.printf("%d년분 %s원은 이미 납부로 기록돼 있다", y, won(due))
			return ExitOK
		}
		next := pos
		next.Holdings = acct.Clone()
		next.Holdings.TaxPaidKRW[y] = due
		if o.DryRun {
			a.printf("[dry-run] %d년분 %s원 납부 기록 안 함", y, won(due))
			return ExitOK
		}
		if err := a.Store.SavePosition(ctx, next); err != nil {
			return a.fail(ExitFail, "%v", err)
		}
		a.printf("%d년분 양도세 %s원 납부 기록", y, won(due))
		a.printf("  미납 합계   %s → %s원", won(acct.TaxLiabilityKRW()), won(next.Holdings.TaxLiabilityKRW()))
		a.printf("  이제 평가금액에서 차감되지 않는다")
		return ExitOK
	}
	if len(gains) == 0 {
		a.printf("실현손익 없음 — 아직 매도가 없다 (전환 시점에 발생)")
		return ExitOK
	}
	var years []int
	if o.Year != 0 {
		years = []int{o.Year}
	} else {
		for y := range gains {
			years = append(years, y)
		}
		sort.Ints(years)
	}
	a.printf("%-6s%16s%14s%16s%14s   납부", "연도", "실현손익", "기본공제", "과세표준", "세액(22%)")
	total := 0.0
	loss := false
	for _, y := range years {
		g := gains[y]
		base := math.Max(0, g-invest.AnnualDeductKRW)
		due := acct.TaxDueKRW(y)
		unpaid := acct.TaxUnpaidKRW(y)
		total += unpaid
		when := itoa(y+1) + "년 5월"
		if due > 0 && unpaid <= 0 {
			when = "납부 완료"
		}
		if g < 0 {
			loss = true
		}
		a.printf("%-6d%16s%14s%16s%14s   %s", y, won(g), won(invest.AnnualDeductKRW), won(base), won(due), when)
	}
	a.printf("\n  미납 합계   %s원", won(total))
	a.printf("  평가액에서만 차감 중 — 매도 시점에 현금을 깎지 않는다")
	if loss {
		a.printf("  손실 연도는 0으로 종료 — 이월공제 없음")
	}
	a.printf("\n  이 표는 대조용이다. 신고는 증권사 양도소득 명세와 맞춰볼 것")
	return ExitOK
}

// ContributeOpts는 contribute(적립액 조회·수정) 옵션이다.
type ContributeOpts struct {
	Date          string
	Amount        int64 // 0이면 조회
	EffectiveFrom string
	Reason        string
}

// Contribute는 적립액을 바꾼다. 이탈이 아니다. 다만 신호를 알고 나서 바꾸는 것은 마켓 타이밍이므로 거부한다.
func (a *App) Contribute(ctx context.Context, o ContributeOpts) int {
	today, err := a.today(o.Date)
	if err != nil {
		return a.fail(ExitFail, "%v", err)
	}
	pos, err := a.Store.LoadPosition(ctx)
	if err != nil {
		return a.fail(ExitFail, "%v", err)
	}
	if o.Amount == 0 {
		cur, err := a.Store.ContributionFor(ctx, pos.ApplyYM)
		if err != nil {
			return a.fail(ExitFail, "%v", err)
		}
		hist, err := a.Store.LoadContributions(ctx)
		if err != nil {
			return a.fail(ExitFail, "%v", err)
		}
		a.printf("현재  %s원/월  (%s 기준)", commas(float64(cur)), pos.ApplyYM)
		for _, r := range hist {
			a.printf("  %s부터 %10s원   %s", r.EffectiveFrom, commas(float64(r.Amount)), r.Reason)
		}
		a.printf("\n  변경: myhome invest contribute --amount 1000000 --from YYYY-MM --reason \"...\"")
		return ExitOK
	}
	if o.EffectiveFrom == "" {
		return a.fail(ExitFail, "--from YYYY-MM 으로 적용 시작월을 지정할 것")
	}
	rec, err := a.Store.SetContribution(ctx, o.Amount, o.EffectiveFrom, o.Reason, today, pos.ApplyYM)
	if err != nil {
		return a.fail(ExitFail, "%v", err)
	}
	cur, _ := a.Store.ContributionFor(ctx, pos.ApplyYM)
	a.printf("적립액 변경 — %s부터 %s원", rec.EffectiveFrom, commas(float64(rec.Amount)))
	a.printf("  %s까지는 %s원 유지", pos.ApplyYM, commas(float64(cur)))
	return ExitOK
}

// CoolingHours는 이탈 확정까지의 냉각 시간이다. 충동은 대부분 하루를 못 넘긴다.
const CoolingHours = 24

// DeviateOpts는 deviate 옵션이다.
type DeviateOpts struct {
	Date   string
	Symbol string
	Reason string
}

// Deviate는 규칙을 벗어난다. 두 번 실행해야 확정된다.
//
//	1차  요청 기록 → pending
//	24시간 뒤 2차  확정 → deviations + 다음 판정일까지 잠금
func (a *App) Deviate(ctx context.Context, o DeviateOpts) int {
	today, err := a.today(o.Date)
	if err != nil {
		return a.fail(ExitFail, "%v", err)
	}
	now := a.now().In(kst)
	if o.Date != "" {
		now = time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, kst)
	}
	pos, err := a.Store.LoadPosition(ctx)
	if err != nil {
		return a.fail(ExitFail, "%v", err)
	}
	ym := pos.ApplyYM
	if !invest.IsTradable(o.Symbol) {
		return a.fail(ExitFail, "%s는 이 전략의 종목이 아니다 (%s)", o.Symbol, strings.Join(invest.Tickers, " / "))
	}
	want := invest.MustTicker(o.Symbol)
	target := invest.MustTicker(pos.Signal.Target)
	if want == target {
		return a.fail(ExitFail, "%s는 신호가 정한 종목이다. 이탈이 아니다", want)
	}
	p, err := a.Store.LoadPending(ctx)
	if err != nil {
		return a.fail(ExitFail, "%v", err)
	}
	if p == nil || p.Symbol != want || p.YM != ym {
		rec := invest.Pending{Kind: "deviation", YM: ym, Symbol: want, Reason: strings.TrimSpace(o.Reason), RequestedAt: now.Format("2006-01-02T15:04:05")}
		if err := a.Store.SavePending(ctx, rec); err != nil {
			return a.fail(ExitFail, "%v", err)
		}
		ready := now.Add(CoolingHours * time.Hour)
		a.printf("이탈 요청 접수 — %s %s → %s", ym, target, want)
		a.printf("  아직 확정되지 않았다. %s 이후 같은 명령을 다시 실행할 것.", ready.Format("2006-01-02 15:04"))
		a.printf("  그때까지 마음이 바뀌면 아무것도 안 하면 된다 (자동 소멸하지는 않으니 `myhome invest cancel`).")
		return ExitOK
	}
	req, err := time.ParseInLocation("2006-01-02T15:04:05", p.RequestedAt, kst)
	if err != nil {
		return a.fail(ExitFail, "pending.requested_at 형식 이상: %q", p.RequestedAt)
	}
	if elapsed := now.Sub(req); elapsed < CoolingHours*time.Hour {
		left := CoolingHours*time.Hour - elapsed
		return a.fail(ExitFail, "냉각 중 — %d시간 %d분 남음", int(left.Hours()), int(left.Minutes())%60)
	}
	// 다음 판정일까지 잠근다. 적용월의 판정일이 이미 지났으면 그 다음 달로 민다
	t, _ := time.Parse("2006-01", ym)
	lock := invest.LastTradingDay(t.Year(), t.Month())
	if !lock.After(today) {
		lock = invest.NextLastTradingDay(today)
	}
	var rec invest.Deviation
	err = a.Store.InTx(ctx, func(s *store.Store) error {
		var err error
		if rec, err = s.AppendDeviation(ctx, ym, today, target, want, p.Reason, lock); err != nil {
			return err
		}
		return s.ClearPending(ctx)
	})
	if err != nil {
		return a.fail(ExitFail, "%v", err)
	}
	a.printf("이탈 확정 — %s %s → %s", ym, target, want)
	a.printf("  %s까지 잠금 (되돌리기 금지)", rec.LockedUntil)
	if strings.TrimSpace(rec.Reason) != "" {
		a.printf("  %s에 이 사유를 다시 보여준다", rec.ResurfaceOn)
	}
	a.printf("\n  이제 실행: myhome invest execute --fill %s=<단가> --fx <환율>", want)
	return ExitOK
}

var kst = time.FixedZone("KST", 9*60*60)

// Cancel은 대기 중인 이탈 요청을 취소한다.
func (a *App) Cancel(ctx context.Context) int {
	p, err := a.Store.LoadPending(ctx)
	if err != nil {
		return a.fail(ExitFail, "%v", err)
	}
	if p == nil {
		a.printf("대기 중인 요청 없음")
		return ExitOK
	}
	if err := a.Store.ClearPending(ctx); err != nil {
		return a.fail(ExitFail, "%v", err)
	}
	a.printf("이탈 요청 취소 — %s %s", p.YM, p.Symbol)
	return ExitOK
}
