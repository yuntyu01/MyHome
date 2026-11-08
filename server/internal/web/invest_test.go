package web

import (
	"encoding/json"
	"math"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/yuntyu01/MyHome/server/internal/dbq"
	"github.com/yuntyu01/MyHome/server/internal/invest"
)

// 투자 화면 값 만들기. 날짜는 뉴욕 기준으로 고정한다 — 날짜가 아니라 코드가 바뀔 때만 깨지게.
// 2026-11: 첫 거래일 11/2(월), 마지막 거래일 11/30(월). 2026-12 첫 거래일 12/1(화).

func etNow(y int, m time.Month, d int) time.Time {
	return time.Date(y, m, d, 15, 0, 0, 0, invest.NewYork)
}

func okRuns(at time.Time) []dbq.InvestRun {
	var out []dbq.InvestRun
	zero := int32(0)
	for _, c := range investRunCmds {
		fin := at.Add(time.Minute)
		out = append(out, dbq.InvestRun{Cmd: c.Cmd, StartedAt: at, FinishedAt: &fin, ExitCode: &zero})
	}
	return out
}

func pricesUpTo(last time.Time) invest.Prices {
	prev := invest.EmptyRow(last.AddDate(0, 0, -1))
	prev.SPX, prev.NDX, prev.TQQQ, prev.QQQM, prev.USDKRWMkt = 6000, 21000, 49, 250, 1400
	row := invest.EmptyRow(last) // 시장 환율은 하루 늦게 붙는다 — 마지막 행은 결측
	row.SPX, row.NDX, row.TQQQ, row.QQQM = 6010, 21050, 50.25, 251
	return invest.Prices{prev, row}
}

func basePosition() invest.Position {
	p := invest.Position{
		Holdings: invest.Account{Symbol: "TQQQ", Shares: 10, CostKRW: 600_000, CashUSD: 12.5},
		LastRun:  "2026-10-30", YM: "2026-10", ApplyYM: "2026-11", Signal: invest.Judgment{Target: "TQQQ"},
		FXRate: 1400, ContributedKRW: 600_000, ExecutedOn: invest.StrPtr("2026-11-02"), ExecutedYM: invest.StrPtr("2026-11"),
	}
	p.Normalize()
	return p
}

func baseInputs(now time.Time) investInputs {
	shares := 10
	return investInputs{
		Prices:  pricesUpTo(invest.Date(2026, 11, 5)),
		Ledger:  []invest.LedgerEntry{{On: "2026-11-02", YM: "2026-11", AmountKRW: 600_000, Source: "trade", Symbol: "TQQQ", Shares: &shares}},
		Runs:    okRuns(now.Add(-2 * time.Hour)),
		Contrib: 600_000,
	}
}

func TestInvestStatusDone(t *testing.T) {
	now := etNow(2026, 11, 6)
	s := buildInvestStatus(now, basePosition(), baseInputs(now))
	if s.Problems != 0 {
		t.Errorf("문제 0건이어야 함: %+v", s.Checks)
	}
	if s.Month.Value != "✓ 완료 11월 2일" || s.Month.Bad {
		t.Errorf("이번 달 = %+v", s.Month)
	}
	if got := s.Schedule[0].Value; got != "11월 30일 · D-24" {
		t.Errorf("다음 판정일 = %q", got)
	}
	// 이번 적용월은 끝났다 — 다음 실행일은 다음 판정 뒤 달의 첫 거래일
	if got := s.Schedule[1]; got.Value != "12월 1일 · D-25" || got.Meta != "12월 첫 거래일" {
		t.Errorf("다음 실행일 = %+v", got)
	}
	if s.PriceStale || s.LastPriceDay != "2026-11-05" {
		t.Errorf("가격 신선도: stale=%v last=%s", s.PriceStale, s.LastPriceDay)
	}
}

func TestInvestStatusPlannedAndExecDay(t *testing.T) {
	pos := basePosition()
	pos.ApplyYM, pos.ExecutedOn, pos.ExecutedYM = "2026-12", nil, nil
	now := etNow(2026, 11, 30) // 판정 당일
	s := buildInvestStatus(now, pos, baseInputs(now))
	if s.Month.Value != "예정 12월 1일" || s.Month.Bad {
		t.Errorf("이번 달 = %+v", s.Month)
	}
	if s.Schedule[0].Value != "11월 30일 · D-day" || s.Schedule[1].Value != "12월 1일 · D-1" {
		t.Errorf("일정 = %+v", s.Schedule)
	}
	// 실행일 당일은 아직 기회가 남았다 — 미체결로 알리면 오경보
	pos.ApplyYM = "2026-11"
	s = buildInvestStatus(etNow(2026, 11, 2), pos, baseInputs(etNow(2026, 11, 2)))
	if s.Month.Value != "예정 11월 2일" || s.Month.Bad {
		t.Errorf("실행일 당일 = %+v", s.Month)
	}
}

func TestInvestStatusMissedPurchase(t *testing.T) {
	pos := basePosition()
	pos.ExecutedOn, pos.ExecutedYM = invest.StrPtr("2026-10-01"), invest.StrPtr("2026-10") // 지난 적용월 체결만 있다
	now := etNow(2026, 11, 6)
	s := buildInvestStatus(now, pos, baseInputs(now))
	if !s.Month.Bad || s.Month.Value != "✗ 매수 안 됨" || !strings.Contains(s.Month.Meta, "실행일 11월 2일에서 4일 지남") {
		t.Errorf("미체결 = %+v", s.Month)
	}
	if !s.Schedule[1].Bad || s.Schedule[1].Value != "11월 2일 · D+4" {
		t.Errorf("지난 실행일 = %+v", s.Schedule[1])
	}
	if s.Problems != 1 {
		t.Errorf("문제 %d건, want 1", s.Problems)
	}
}

func TestInvestStatusChecks(t *testing.T) {
	now := etNow(2026, 11, 6)
	check := func(s investStatus, title string) investRow {
		for _, c := range s.Checks {
			if c.Title == title {
				return c
			}
		}
		t.Fatalf("점검 %q 없음", title)
		return investRow{}
	}

	// 가격: 8일까지는 정상, 9일이면 멈춘 것
	in := baseInputs(now)
	in.Prices = pricesUpTo(invest.Date(2026, 10, 29))
	if s := buildInvestStatus(now, basePosition(), in); s.PriceStale || check(s, "가격 신선도").Bad {
		t.Error("8일 전 가격은 정상")
	}
	in.Prices = pricesUpTo(invest.Date(2026, 10, 28))
	s := buildInvestStatus(now, basePosition(), in)
	if !s.PriceStale || s.LastPriceDay != "2026-10-28" || !check(s, "가격 신선도").Bad {
		t.Error("9일 전 가격이면 갱신이 멈춘 것")
	}
	in.Prices = nil
	if s := buildInvestStatus(now, basePosition(), in); !s.PriceStale || s.LastPriceDay != "" {
		t.Error("가격이 하나도 없으면 멈춘 것")
	}

	// 판정 신선도: 35일 넘으면 문제, 기준일을 함께 보여준다
	pos := basePosition()
	pos.LastRun = "2026-10-01"
	c := check(buildInvestStatus(now, pos, baseInputs(now)), "판정 신선도")
	if !c.Bad || c.Value != "✗ 36일 전" || !strings.Contains(c.Meta, "기준 10월 1일") {
		t.Errorf("판정 신선도 = %+v", c)
	}

	// 환율 추정
	pos = basePosition()
	pos.FXEstimated = true
	if c := check(buildInvestStatus(now, pos, baseInputs(now)), "환율"); !c.Bad || !strings.Contains(c.Meta, "환율 추정") {
		t.Errorf("환율 추정 = %+v", c)
	}

	// 원금 대조: 1원 이상 어긋나면 문제
	in = baseInputs(now)
	in.Ledger[0].AmountKRW = 599_000
	if c := check(buildInvestStatus(now, basePosition(), in), "원금 대조"); !c.Bad || c.Value != "✗ 1,000원 어긋남" {
		t.Errorf("원금 대조 = %+v", c)
	}
	in.Ledger[0].AmountKRW = 600_000.4
	if c := check(buildInvestStatus(now, basePosition(), in), "원금 대조"); c.Bad {
		t.Errorf("1원 미만 차이는 일치 = %+v", c)
	}

	// 적립액 미설정
	in = baseInputs(now)
	in.ContribErr = errTest("없음")
	if c := check(buildInvestStatus(now, basePosition(), in), "적립액"); !c.Bad {
		t.Errorf("적립액 미설정 = %+v", c)
	}
}

type errTest string

func (e errTest) Error() string { return string(e) }

func TestInvestRunChecks(t *testing.T) {
	now := etNow(2026, 11, 6)
	one, retry := int32(1), int32(75)
	fin := now.Add(-time.Hour)
	runs := []dbq.InvestRun{
		{Cmd: "refresh", StartedAt: now.Add(-2 * time.Hour), FinishedAt: &fin, ExitCode: &retry},
		{Cmd: "judge", StartedAt: now.Add(-2 * time.Hour), FinishedAt: &fin, ExitCode: &one},
		{Cmd: "trade", StartedAt: now.Add(-10 * time.Minute)}, // 실행 중
		// status 기록 없음
		{Cmd: "order", StartedAt: now, FinishedAt: &fin, ExitCode: &one}, // 정기 명령이 아니면 안 보여준다
	}
	rows := runChecks(now, runs)
	if len(rows) != 4 {
		t.Fatalf("정기 명령 4개만: %+v", rows)
	}
	want := []struct {
		bad  bool
		meta string
	}{
		{true, "refresh · 나중에 다시 (종료 코드 75)"},
		{true, "judge · 실패 (종료 코드 1)"},
		{false, "trade · 실행 중"},
		{true, "status"},
	}
	for i, w := range want {
		if rows[i].Bad != w.bad || rows[i].Meta != w.meta {
			t.Errorf("%d: %+v, want bad=%v meta=%q", i, rows[i], w.bad, w.meta)
		}
	}
	if rows[3].Value != "✗ 기록 없음" {
		t.Errorf("기록 없음 = %q", rows[3].Value)
	}
	// 오래 끝나지 않은 실행은 죽은 것
	runs[2].StartedAt = now.Add(-investRunHang - time.Minute)
	if r := runChecks(now, runs)[2]; !r.Bad || r.Meta != "trade · 끝나지 않음" {
		t.Errorf("끝나지 않은 실행 = %+v", r)
	}
}

func TestInvestValue(t *testing.T) {
	now := etNow(2026, 11, 6)
	pos := basePosition()
	shares := 10
	ledger := []invest.LedgerEntry{{On: "2026-11-02", YM: "2026-11", AmountKRW: 600_000, Source: "trade", Symbol: "TQQQ", Shares: &shares, Note: "합성"}}
	v := buildInvestValue(now, pos, pricesUpTo(invest.Date(2026, 11, 5)), ledger)
	// 10주 × $50.25 × 1400 + $12.50 × 1400 = 721,000원. 환율은 마지막 유효값(11/4)
	if !v.CanValue || v.NetKRW != 721_000 || v.GainKRW != 121_000 || v.GainPct != "+20.2%" {
		t.Errorf("평가 = %+v", v)
	}
	if v.Basis != "TQQQ 종가 $50.25 (11월 5일) · 환율 1,400.00 (11월 4일)" {
		t.Errorf("기준 = %q", v.Basis)
	}
	if v.CashUSD != "$12.50" || v.ContributedKRW != 600_000 || v.TaxKRW != 0 {
		t.Errorf("원금·현금·세금 = %+v", v)
	}
	if len(v.Ledger) != 1 || v.Ledger[0].On != "11월 2일" || v.Ledger[0].YM != "11월" || v.Ledger[0].Source != "자동매수" || v.Ledger[0].Shares != "TQQQ +10주" || v.LedgerBad {
		t.Errorf("원금 내역 = %+v", v.Ledger)
	}

	// 미납 세금은 평가액에서 뺀다
	pos.Holdings.RealizedGainKRW = map[int]float64{2026: 3_500_000} // (350만 − 250만) × 22% = 22만
	if v := buildInvestValue(now, pos, pricesUpTo(invest.Date(2026, 11, 5)), ledger); v.TaxKRW != 220_000 || v.NetKRW != 501_000 {
		t.Errorf("세금 = %d, 평가 = %d", v.TaxKRW, v.NetKRW)
	}

	// 종가·환율이 없으면 평가하지 않는다 — 조용히 0이나 1을 넣지 않는다
	noFX := pricesUpTo(invest.Date(2026, 11, 5))
	noFX[0].USDKRWMkt = math.NaN()
	if v := buildInvestValue(now, basePosition(), noFX, ledger); v.CanValue || v.Why == "" {
		t.Errorf("환율 없음 = %+v", v)
	}
	noPx := pricesUpTo(invest.Date(2026, 11, 5))
	noPx[0].TQQQ, noPx[1].TQQQ = math.NaN(), math.NaN()
	if v := buildInvestValue(now, basePosition(), noPx, ledger); v.CanValue || !strings.Contains(v.Why, "TQQQ") {
		t.Errorf("종가 없음 = %+v", v)
	}
}

func TestInvestFormat(t *testing.T) {
	d := invest.Date(2026, 11, 6)
	if dday(d, d) != "D-day" || dday(d, d.AddDate(0, 0, 3)) != "D-3" || dday(d, d.AddDate(0, 0, -2)) != "D+2" {
		t.Error("D-day 표시")
	}
	// 기준일은 올해가 아니면 연도까지
	if got := dayNameAt("2026-10-05", d); got != "10월 5일" {
		t.Errorf("올해 = %q", got)
	}
	if got := dayNameAt("2025-12-31", d); got != "2025년 12월 31일" {
		t.Errorf("지난해 = %q", got)
	}
	for in, want := range map[float64]string{0: "$0.00", 12.5: "$12.50", 1234.567: "$1,234.57", -3.2: "-$3.20"} {
		if got := usd(in); got != want {
			t.Errorf("usd(%v) = %q, want %q", in, got, want)
		}
	}
}

// 웹 프로세스는 증권사·야후를 부르지 않는다. 그 코드가 의존성에 아예 없어야 한다.
func TestWebDoesNotDependOnBrokerOrYahoo(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go 명령 없음")
	}
	out, err := exec.Command("go", "list", "-deps", ".").Output()
	if err != nil {
		t.Fatalf("go list: %v", err)
	}
	for _, pkg := range strings.Fields(string(out)) {
		for _, banned := range []string{"/internal/invest/run", "/internal/invest/broker", "/internal/invest/toss", "/internal/invest/yahoo"} {
			if strings.HasSuffix(pkg, banned) {
				t.Errorf("web이 %s에 의존한다", pkg)
			}
		}
	}
}

// 이탈 기록: 잠금 기한은 일정에, 6개월 지난 기록은 "다시 보기"로 보여주되 사유 본문은 싣지 않는다(TQ 웹과 같다).
func TestInvestStatusShowsLockAndResurfacingWithoutReason(t *testing.T) {
	now := etNow(2026, 11, 2)
	in := baseInputs(now)
	actual := "QQQM"
	in.Devs = []invest.Deviation{
		// 6개월 지난 기록: 다시 보여준다(본문은 숨김)
		{YM: "2026-03", OccurredOn: "2026-03-02", SignalTarget: "TQQQ", ActualTarget: &actual, Reason: "비밀 사유 문장", LockedUntil: "2026-04-30", ResurfaceOn: "2026-09-02"},
		// 아직 잠금 중인 기록: 가장 늦은 해제일이 잠금 기한
		{YM: "2026-10", OccurredOn: "2026-10-01", SignalTarget: "TQQQ", ActualTarget: &actual, Reason: "또 다른 사유", LockedUntil: "2026-12-31", ResurfaceOn: "2027-04-01"},
		{YM: "2026-09", OccurredOn: "2026-09-01", SignalTarget: "TQQQ", Reason: "", LockedUntil: "2026-11-30", ResurfaceOn: "2026-10-01"},
	}
	s := buildInvestStatus(now, basePosition(), in)
	var lock, again []investRow
	for _, r := range s.Schedule {
		if r.Title == "포지션 잠금" {
			lock = append(lock, r)
		}
	}
	for _, r := range s.Checks {
		if r.Title == "이탈 기록 다시 보기" {
			again = append(again, r)
		}
	}
	if len(lock) != 1 || !strings.Contains(lock[0].Value, "12") || !strings.Contains(lock[0].Value, "31") {
		t.Fatalf("잠금: %+v", lock)
	}
	if len(again) != 1 || again[0].Bad || !strings.Contains(again[0].Meta, "why") {
		t.Fatalf("다시 보기(사유 없는 기록은 빼고 1건): %+v", again)
	}
	b, _ := json.Marshal(s)
	if strings.Contains(string(b), "비밀 사유") || strings.Contains(string(b), "또 다른 사유") {
		t.Fatal("사유 본문이 응답에 실렸다")
	}
}

// status는 주 1회다. 7일 23시간 전 성공은 정상, 8일을 넘으면 deadman이 멈춘 것이다.
func TestInvestHealthStaleBoundary(t *testing.T) {
	now := etNow(2026, 11, 16)
	for _, tc := range []struct {
		ago  time.Duration
		want int
	}{{7*24*time.Hour + 23*time.Hour, 200}, {8*24*time.Hour + time.Minute, 503}} {
		_, code := buildInvestHealth(now, dbq.InvestHealthRow{HasPosition: true, LastStatusOk: now.Add(-tc.ago)})
		if code != tc.want {
			t.Errorf("%s 전 성공: %d, want %d", tc.ago, code, tc.want)
		}
	}
	if h, code := buildInvestHealth(now, dbq.InvestHealthRow{}); code != 200 || h.Status != "ok" || h.Trading != "not_started" {
		t.Errorf("시작 전: %d %+v", code, h)
	}
}
