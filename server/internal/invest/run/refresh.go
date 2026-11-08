package run

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/yuntyu01/MyHome/server/internal/invest"
	"github.com/yuntyu01/MyHome/server/internal/invest/store"
	"github.com/yuntyu01/MyHome/server/internal/invest/yahoo"
)

// RefreshOpts는 refresh 옵션이다.
type RefreshOpts struct {
	Retries int           // 기다리면 풀리는 실패(결측·빈 응답)일 때 다시 받을 횟수
	Wait    time.Duration // 재시도 간격
}

// anchorTol은 겹치는 구간의 SPX·NDX 허용 오차다. 넘으면 조정 기준이 다르거나 장중 행이 굳은 것이다.
const anchorTol = 0.0005

// Refresh는 야후로 마스터를 오늘까지 연장한다. 원종가(조정 안 한 종가)다 (§10).
//
// 결측 가드는 그대로다 — 빈 채로 쓰지 않고, 채워질 때까지 기다릴 뿐이다.
func (a *App) Refresh(ctx context.Context, o RefreshOpts) int {
	tries := 1 + max(0, o.Retries)
	rc := ExitOK
	for i := 1; i <= tries; i++ {
		rc = a.refreshOnce(ctx)
		if rc != ExitRetry || i == tries {
			return rc
		}
		a.warnf("  %d/%d회 실패 — 데이터 반영 지연으로 보고 %s 뒤 다시 받는다 (기다리는 동안 잠금을 놓는다)", i, tries, o.Wait)
		// 🔴 기다리는 동안 상태 잠금을 놓는다. 쥐고 기다리면 그 사이 도는 judge·주간 갱신이 잠금을 못 잡고
		// 75로 끝나 실패 알림이 간다. 다음 시도는 마스터를 처음부터 다시 읽는다
		if err := a.pause(ctx, o.Wait); err != nil {
			return a.fail(ExitRetry, "재시도 전에 잠금을 다시 잡지 못했다: %v", err)
		}
	}
	return rc
}

func (a *App) refreshOnce(ctx context.Context) int {
	master, err := a.Store.LoadPrices(ctx)
	if err != nil {
		return a.fail(ExitFail, "마스터 읽기 실패: %v", err)
	}
	last, ok := master.Last()
	if !ok {
		return a.fail(ExitFail, "마스터가 비어 있다. `myhome invest import-tq`로 먼저 옮길 것")
	}
	symbols := make([]string, 0, len(invest.Columns)+1)
	col := map[string]string{}
	for _, c := range invest.Columns {
		symbols = append(symbols, c.Yahoo)
		col[c.Yahoo] = c.Col
	}
	symbols = append(symbols, invest.FXYahoo)
	col[invest.FXYahoo] = "USDKRW_MKT"

	bars, err := a.Prices.DailyCloses(ctx, symbols, last.Day)
	if err != nil {
		if errors.Is(err, yahoo.ErrTemporary) {
			return a.fail(ExitRetry, "야후 조회 실패 — %v", err) // 네트워크·레이트리밋 — 기다리면 풀린다
		}
		return a.fail(ExitFail, "야후 조회 실패 — %v", err)
	}

	// 날짜별로 모은다 (종목마다 거래일이 다를 수 있다 — 바깥 조인)
	fresh := map[time.Time]*invest.PriceRow{}
	for sym, bs := range bars {
		c := col[sym]
		for _, b := range bs {
			r, ok := fresh[b.Day]
			if !ok {
				nr := invest.EmptyRow(b.Day)
				r = &nr
				fresh[b.Day] = r
			}
			if b.OK() {
				r.Set(c, b.Close)
			}
		}
	}
	if len(fresh) == 0 {
		return a.fail(ExitRetry, "야후가 빈 응답을 줬다")
	}

	// 🔴 장중 시세는 종가가 아니다 — 마감 전 행은 받지 않는다 (2026-08-13 사고)
	cutoff := invest.LastCompleteSession(a.now())
	var days, dropped []time.Time
	for d := range fresh {
		if d.After(cutoff) {
			dropped = append(dropped, d)
			continue
		}
		days = append(days, d)
	}
	sort.Slice(days, func(i, j int) bool { return days[i].Before(days[j]) })
	if len(dropped) > 0 {
		sort.Slice(dropped, func(i, j int) bool { return dropped[i].Before(dropped[j]) })
		a.printf("  장중 제외 %s — 다음 실행이 종가로 받는다", joinDays(dropped))
	}
	if len(days) == 0 {
		return a.fail(ExitFail, "마스터 마지막 행(%s)이 완결 거래일(%s)보다 늦다 — 장중에 쓴 행이 굳어 있다. 그 행을 지우고 다시 실행할 것",
			invest.FormatDay(last.Day), invest.FormatDay(cutoff))
	}

	// 앵커 대조 — 겹치는 구간이 어긋나면 멈춘다
	byDay := make(map[time.Time]invest.PriceRow, len(master))
	for _, r := range master {
		byDay[r.Day] = r
	}
	var overlap []time.Time
	for _, d := range days {
		if _, ok := byDay[d]; ok {
			overlap = append(overlap, d)
		}
	}
	if len(overlap) == 0 {
		return a.fail(ExitFail, "겹치는 거래일이 없다. 데이터 소스를 확인할 것")
	}
	for _, c := range []string{"SPX", "NDX"} {
		worst := 0.0
		for _, d := range overlap {
			got, want := fresh[d].Get(c), byDay[d].Get(c)
			if math.IsNaN(got) || math.IsNaN(want) || want == 0 {
				continue
			}
			worst = math.Max(worst, math.Abs((got-want)/want))
		}
		if worst > anchorTol {
			return a.fail(ExitFail, "%s 앵커 오차 %.3f%% — 조정 기준이 다르거나 마스터에 장중 행이 굳었다. 원인 확인 전까지 중단", c, worst*100)
		}
		a.printf("  앵커 %s: %d일 오차 %.4f%% ✓", c, len(overlap), worst*100)
	}

	// 시장 환율은 보호할 과거값이 없다. 조회된 날짜는 기존 행의 빈칸에도 채운다
	type fill struct {
		day time.Time
		v   float64
	}
	var fills []fill
	for _, d := range days {
		if old, ok := byDay[d]; ok && math.IsNaN(old.USDKRWMkt) {
			if v := fresh[d].USDKRWMkt; !math.IsNaN(v) {
				fills = append(fills, fill{d, v})
			}
		}
	}

	// 새 거래일 — SPX·NDX가 없는 날(미국 휴장인데 환율만 있는 날)은 거래일이 아니다
	var add invest.Prices
	for _, d := range days {
		if _, ok := byDay[d]; ok {
			continue
		}
		r := *fresh[d]
		if math.IsNaN(r.SPX) || math.IsNaN(r.NDX) {
			continue
		}
		add = append(add, r)
	}

	// 🔴 신호 밖 종목의 결측도 막는다. 과거에 SPX·NDX만 검사한 탓에 TQQQ가 64거래일 비었다.
	// 쓰지 않고 멈춘다 — 기록해 두면 다음 실행이 그 날짜를 다시 받지 않는다
	if len(add) > 0 {
		var holes []string
		for _, c := range invest.Columns {
			var ds []time.Time
			for _, r := range add {
				if math.IsNaN(r.Get(c.Col)) {
					ds = append(ds, r.Day)
				}
			}
			if len(ds) > 0 {
				holes = append(holes, fmt.Sprintf("%s %d일 (%s)", c.Col, len(ds), joinDays(ds)))
			}
		}
		if len(holes) > 0 {
			sort.Strings(holes)
			return a.fail(ExitRetry, "신규 %d일(%s~%s)에 결측: %s\n  마스터에 쓰지 않고 중단한다. 원인 확인 후 다시 실행할 것 (기록하면 다음 실행이 이 날짜를 다시 받지 않는다)",
				len(add), invest.FormatDay(add[0].Day), invest.FormatDay(add[len(add)-1].Day), strings.Join(holes, ", "))
		}
	}

	err = a.Store.InTx(ctx, func(s *store.Store) error {
		for _, f := range fills {
			if _, err := s.FillFXMkt(ctx, f.day, f.v); err != nil {
				return err
			}
		}
		for _, r := range add {
			if _, err := s.InsertPrice(ctx, r); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return a.fail(ExitFail, "마스터 쓰기 실패: %v", err)
	}
	if len(add) == 0 {
		a.printf("새 거래일 없음 (%s). USDKRW_MKT %d일 채움", invest.FormatDay(last.Day), len(fills))
		return ExitOK
	}
	gotFX := 0
	for _, r := range add {
		if !math.IsNaN(r.USDKRWMkt) {
			gotFX++
		}
	}
	a.printf("+%d일 → %s (총 %s행)", len(add), invest.FormatDay(add[len(add)-1].Day), commas(float64(len(master)+len(add))))
	a.printf("  환율 USDKRW_MKT +%d일 (시장 종가, 평가 전용)", gotFX)
	a.printf("⚠ 체결 환율은 여전히 증권사 실제 적용값을 쓴다 — execute --fx (§10.1)")
	return ExitOK
}

func joinDays(ds []time.Time) string {
	s := make([]string, len(ds))
	for i, d := range ds {
		s[i] = invest.FormatDay(d)
	}
	return strings.Join(s, ", ")
}
