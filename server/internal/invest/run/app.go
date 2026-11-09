// Package run은 자동매매 명령이다 (TQ strategy/run.py 이식).
//
// 평상시 사람이 칠 명령은 없다. 전부 systemd timer가 돈다.
//
//	자동
//	  refresh → judge     월 마지막 거래일 — 판정만. 매매 없음 (다음 거래일 장 시작 전에 한 번 더 만회)
//	  trade               월 1~7일 매일 — 주문·체결 확인을 한 단계씩
//	  status (deadman)    매주 — 조용히 죽은 것 감지
//
//	수동 — 복구·교정 전용. 정상 달에는 쓸 일이 없다
//	  execute --fill …    자동 매매가 다 막혀 증권사 앱에서 직접 샀을 때 그 체결을 장부에 기록
//	  amend --fill --fx   추정 환율을 증권사 확정값으로 교정. 단가·환율만 (§12.1.9)
//	  adjust --auto       앱 밖 추가 매수를 장부에 반영
//	  deviate / cancel    규칙 이탈 — 애초에 사람 결정이라 자동화 대상이 아님
//	  tax --paid          납부한 양도세 기록 (연 1회)
//	  orders --clear-stale  지난달 주문 기록이 trade를 막을 때 — 증권사에 살아 있는 주문이 없을 때만 지운다
//
// 판정과 실행은 반드시 다른 날이다 (§1.3 룩어헤드 금지). 실패 방향은 언제나 멈춤이다.
// 종료 코드: 0 정상, 1 구조적 실패(기다려도 안 풀림), 75 기다리면 풀리는 실패.
package run

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/yuntyu01/MyHome/server/internal/invest"
	"github.com/yuntyu01/MyHome/server/internal/invest/broker"
	"github.com/yuntyu01/MyHome/server/internal/invest/store"
	"github.com/yuntyu01/MyHome/server/internal/invest/yahoo"
)

// 종료 코드.
const (
	ExitOK    = 0
	ExitFail  = 1
	ExitRetry = 75 // EX_TEMPFAIL — 기다리면 풀리는 실패만 (야후 지연 등)
)

// PriceSource는 일별 종가 원천이다 (야후).
type PriceSource interface {
	DailyCloses(ctx context.Context, symbols []string, start time.Time) (map[string][]yahoo.Bar, error)
}

// App은 명령 실행 환경이다.
type App struct {
	Store  *store.Store
	Out    io.Writer
	Err    io.Writer
	Now    func() time.Time
	Prices PriceSource
	// Broker는 증권사 연결이다. 실제로 쓸 때만 만든다 — dry-run이 자격증명 없이도 돌아야 한다.
	Broker func(ctx context.Context) (broker.Broker, error)
	// UnsafeDate가 true일 때만 --date를 받는다 (INVEST_UNSAFE_DATE=1). 테스트 전용 이음매다.
	UnsafeDate bool
	Sleep      func(time.Duration)
	// Pause는 재시도 사이에 d만큼 기다리는 동안 상태 잠금을 놓았다 다시 잡는다. nil이면 그냥 잔다(잠금 없음).
	// 다시 잡지 못하면 에러다 — 호출부는 상태를 쓰지 않고 75로 끝낸다.
	Pause func(ctx context.Context, d time.Duration) error
}

// pause는 재시도 대기다. 잠금을 쥐고 있으면 그동안 놓는다.
func (a *App) pause(ctx context.Context, d time.Duration) error {
	if a.Pause != nil {
		return a.Pause(ctx, d)
	}
	a.sleep(d)
	return nil
}

func (a *App) now() time.Time {
	if a.Now != nil {
		return a.Now()
	}
	return time.Now()
}

func (a *App) sleep(d time.Duration) {
	if a.Sleep != nil {
		a.Sleep(d)
		return
	}
	time.Sleep(d)
}

func (a *App) printf(format string, args ...any) { fmt.Fprintf(a.Out, format+"\n", args...) }
func (a *App) warnf(format string, args ...any)  { fmt.Fprintf(a.Err, format+"\n", args...) }

// fail은 ✗ 메시지를 남기고 종료 코드를 돌려준다.
func (a *App) fail(code int, format string, args ...any) int {
	fmt.Fprintf(a.Err, "✗ "+format+"\n", args...)
	return code
}

// today는 '오늘'이다. 뉴욕 날짜가 정본이다 — 서버의 UTC·KST 날짜가 아니다.
//
// --date는 테스트 전용이다. 운영에서 임의 날짜로 돌리면 실행일·판정일 검사를 날짜만 바꿔
// 통과시킬 수 있어 preflight가 무의미해진다.
func (a *App) today(date string) (time.Time, error) {
	if date == "" {
		return invest.TodayET(a.now()), nil
	}
	if !a.UnsafeDate {
		return time.Time{}, errors.New("--date는 테스트 전용이다. 운영에서는 오늘 날짜로만 돈다.\n  (정말 필요하면 INVEST_UNSAFE_DATE=1, 다만 그 실행은 우회로 간주한다)")
	}
	a.warnf("⚠ --date %s 사용 중 (INVEST_UNSAFE_DATE=1)", date)
	return invest.ParseDay(date)
}

// gate는 336행 판정 대조다. 마스터가 오염됐으면 판정·주문 전에 멈춘다.
func (a *App) gate(in invest.SignalInputs) error {
	log, err := invest.MonthlySignals(in, invest.SignalStart)
	if err != nil {
		return err
	}
	bad, err := invest.CheckFixture(log)
	if err != nil {
		return err
	}
	if len(bad) > 0 {
		if len(bad) > 5 {
			bad = bad[:5]
		}
		return fmt.Errorf("픽스처 336행 대조 실패 — 가격 마스터가 오염됐다. 판정·주문하지 않는다: %s", strings.Join(bad, "; "))
	}
	return nil
}

// isLastTradingDay는 실데이터가 있으면 그것이 정답, 없으면 달력으로 답한다.
//
// 🔴 데이터가 답할 수 있는 것은 끝난 달뿐이다. 진행 중인 달은 마지막 행이 곧 그 달의 최댓값이라
// 데이터만 보면 마스터가 멈춘 날이 전부 월말이 된다. 다음 달 행이 있을 때만 데이터를 믿는다.
func isLastTradingDay(day time.Time, days []time.Time) bool {
	var maxSame time.Time
	has := false
	for _, d := range days {
		if d.Year() == day.Year() && d.Month() == day.Month() {
			has = true
			if d.After(maxSame) {
				maxSame = d
			}
		}
	}
	if has && maxSame.After(day) {
		return false // 그 달에 더 늦은 거래일이 있다
	}
	if has && len(days) > 0 && days[len(days)-1].After(maxSame) {
		return maxSame.Equal(day) // 다음 달 행이 있다 — 끝난 달
	}
	return day.Equal(invest.LastTradingDay(day.Year(), day.Month()))
}

// JudgmentDay는 today에 판정할 월말 거래일이다. 없으면 zero.
//
// 정상은 오늘이 그 달 마지막 거래일인 날이다 — 같은 날 재실행도 여기로 온다.
//
// 🔴 판정일 밤 실행이 뉴욕 자정을 넘기면 다음 날 만회한다. 직전 거래일이 월말이고 그 달을 아직
// 판정하지 않았으면 그 날짜로 판정한다. 다음 달 첫 장의 종가가 들어오면 그 행이 최신이 되어
// 판정일 대조에서 멈춘다 — 룩어헤드는 데이터가 막는다. 만회는 아직 판정하지 않은 달만 한다.
func JudgmentDay(today time.Time, days []time.Time, judgedYM string) time.Time {
	if isLastTradingDay(today, days) {
		return today
	}
	prev := invest.PreviousTradingDay(today)
	if !isLastTradingDay(prev, days) {
		return time.Time{}
	}
	if judgedYM != "" && judgedYM >= invest.YM(prev) {
		return time.Time{} // 이미 판정했다
	}
	return prev
}

// allowedSymbol은 이번 달에 매수가 허용된 종목과 이탈 여부다.
//
// 신호가 정한 종목이 원칙이고, 확정된 이탈이 있을 때만 다른 종목이 허용된다. 미실행 이탈은
// actual_target이 없다 — 그 달은 아무것도 사지 않았다는 기록이므로 ""을 돌려준다.
func allowedSymbol(pos invest.Position, devs []invest.Deviation) (string, bool, error) {
	for _, d := range devs {
		if d.YM == pos.ApplyYM {
			if d.ActualTarget == nil || *d.ActualTarget == "" {
				return "", true, nil
			}
			t, err := invest.Ticker(*d.ActualTarget)
			return t, true, err
		}
	}
	t, err := invest.Ticker(pos.Signal.Target)
	return t, false, err
}

func daysBetween(a, b time.Time) int { return int(math.Round(b.Sub(a).Hours() / 24)) }

// buildCtx는 preflight 입력이다. 표본 수와 전환 횟수는 원자료에서 실제로 센다.
//
// 🔴 target은 이번 달에 실제로 살 종목이다 — 신호 종목이 아니다. 이탈이 확정된 달은 다른 종목이
// 허용되고 매도 레그도 그 기준이다. 여기서만 신호 종목을 쓰면 보유 종목과 신호가 같은 달의 이탈이
// 전부 '불필요한 전환'으로 막힌다.
func buildCtx(today time.Time, pos invest.Position, acct invest.Account, in invest.SignalInputs, sellLegs []string, target string) (invest.PreflightCtx, error) {
	sd, err := pos.SignalDate()
	if err != nil {
		return invest.PreflightCtx{}, err
	}
	smaN, volN := invest.SampleCounts(in, sd)
	log, err := invest.MonthlySignals(in, invest.SignalStart)
	if err != nil {
		return invest.PreflightCtx{}, err
	}
	ay, err := time.Parse("2006-01", pos.ApplyYM)
	if err != nil {
		return invest.PreflightCtx{}, fmt.Errorf("apply_ym 형식 이상: %q", pos.ApplyYM)
	}
	execDay := invest.FirstTradingDay(ay.Year(), ay.Month())
	if target == "" {
		if target, err = invest.Ticker(pos.Signal.Target); err != nil {
			return invest.PreflightCtx{}, err
		}
	}
	late := daysBetween(execDay, today)
	if late < 0 {
		late = 0
	}
	return invest.PreflightCtx{
		ExecDate: today, SignalDate: sd, SPX: pos.Signal.SPX, Vol: pos.Signal.Vol,
		SMASampleCount: smaN, VolSampleCount: volN, ApplyYM: pos.ApplyYM,
		InApplyMonth: invest.YM(today) == pos.ApplyYM, ExecDay: execDay, LateDays: late,
		Switches12M: invest.Switches12M(invest.CompletedSignals(log)),
		Target:      target, Current: acct.Symbol, SellLegs: sellLegs,
	}, nil
}

func sellLegsFor(acct invest.Account, target string) []string {
	if acct.Symbol != "" && acct.Symbol != target {
		return []string{acct.Symbol}
	}
	return nil
}

// closesOf는 그날 행의 거래 종목 종가다(값이 있는 것만).
func closesOf(row invest.PriceRow) map[string]float64 {
	out := map[string]float64{}
	for _, t := range invest.Tickers {
		if v := row.Get(t); !math.IsNaN(v) {
			out[t] = v
		}
	}
	return out
}

// rejectAbsurdFills는 수동 입력 오타 방어다 — 체결단가를 그날 종가와 대조한다.
//
// execute --fill TQQQ=0.649 (실제 $64.90)를 치면 장부가 649주가 된다(실측 — 증권사는 6주).
// 체결은 종가 부근에서 일어나므로 ±25%를 벗어나면 시장이 아니라 손가락이다.
func rejectAbsurdFills(fills map[string]float64, row invest.PriceRow) string {
	syms := make([]string, 0, len(fills))
	for s := range fills {
		syms = append(syms, s)
	}
	sort.Strings(syms)
	for _, sym := range syms {
		px := fills[sym]
		close := row.Get(sym)
		if math.IsNaN(close) || close <= 0 {
			continue
		}
		if d := math.Abs(px/close - 1); d > 0.25 {
			return fmt.Sprintf("%s 체결단가 $%.4f가 종가 $%.2f와 %.0f%% 차이 — 오타로 보인다.\n  실제로 이 가격이 맞다면 refresh로 데이터를 먼저 갱신할 것", sym, px, close, d*100)
		}
	}
	return ""
}

func snapshot(acct invest.Account, pos invest.Position) *invest.Snapshot {
	return &invest.Snapshot{Holdings: acct.Clone(), ContributedKRW: pos.ContributedKRW, MonthsInvested: pos.MonthsInvested}
}

// ParseFills는 SYM=PRICE 목록을 읽는다.
func ParseFills(pairs []string) (map[string]float64, error) {
	out := map[string]float64{}
	for _, p := range pairs {
		k, v, ok := strings.Cut(p, "=")
		if !ok {
			return nil, fmt.Errorf("--fill은 SYM=PRICE 형식: %q", p)
		}
		var f float64
		if _, err := fmt.Sscanf(strings.TrimSpace(v), "%g", &f); err != nil {
			return nil, fmt.Errorf("--fill 단가 형식 이상: %q", p)
		}
		out[strings.TrimSpace(k)] = f
	}
	return out, nil
}

func won(v float64) string    { return commas(math.Round(v)) }
func usd(v float64) string    { return "$" + commas2(v) }
func plain2(v float64) string { return commas2(v) }

// commas는 정수 부분에 천 단위 쉼표를 넣는다.
func commas(v float64) string {
	neg := v < 0
	s := fmt.Sprintf("%.0f", math.Abs(v))
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

func commas2(v float64) string {
	s := fmt.Sprintf("%.2f", v)
	ip, fp, _ := strings.Cut(s, ".")
	neg := strings.HasPrefix(ip, "-")
	ip = strings.TrimPrefix(ip, "-")
	var f float64
	fmt.Sscanf(ip, "%f", &f)
	out := commas(f) + "." + fp
	if neg {
		return "-" + out
	}
	return out
}
