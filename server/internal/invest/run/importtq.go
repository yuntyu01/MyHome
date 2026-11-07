package run

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/yuntyu01/MyHome/server/internal/invest"
	"github.com/yuntyu01/MyHome/server/internal/invest/store"
)

// ImportOpts는 import-tq 옵션이다.
type ImportOpts struct {
	StateDir  string // TQ state/ 디렉터리 (position.json 등)
	MasterCSV string // scripts/tq_export.py가 data/master.parquet에서 뽑은 CSV
	DryRun    bool
}

// ImportTQ는 TQ(Python)의 상태와 가격 마스터를 DB로 옮긴다. 최초 1회다.
//
// 기존 상태를 덮어쓰지 않는다 — 이미 운용 중인 계좌를 초기화하는 것은 되돌릴 수 없는 사고다.
// 쓰기 전에 336행 판정 대조와 원금 장부 대조를 먼저 통과해야 하고, 쓴 뒤에는 다시 읽어 원본과 비교한다.
func (a *App) ImportTQ(ctx context.Context, o ImportOpts) int {
	if o.StateDir == "" || o.MasterCSV == "" {
		return a.fail(ExitFail, "--state <TQ/state 디렉터리> --master <master.csv> 둘 다 필요하다")
	}
	if _, err := a.Store.LoadPosition(ctx); err == nil {
		return a.fail(ExitFail, "position 상태가 이미 있다. import-tq는 최초 1회만 돈다")
	} else if !errors.Is(err, store.ErrNoPosition) {
		return a.fail(ExitFail, "%v", err)
	}
	existing, err := a.Store.LoadPrices(ctx)
	if err != nil {
		return a.fail(ExitFail, "%v", err)
	}
	if len(existing) > 0 {
		return a.fail(ExitFail, "가격 마스터가 이미 %d행 있다. 빈 DB에만 옮긴다", len(existing))
	}

	prices, err := readMasterCSV(o.MasterCSV)
	if err != nil {
		return a.fail(ExitFail, "마스터 CSV: %v", err)
	}
	if err := a.gate(prices.SignalInputs()); err != nil {
		return a.fail(ExitFail, "%v", err)
	}
	first, _ := prices.AsOf(prices[0].Day)
	last, _ := prices.Last()
	a.printf("가격 %s행 (%s~%s) · 픽스처 %d행 대조 ✓", commas(float64(len(prices))), invest.FormatDay(first.Day), invest.FormatDay(last.Day), invest.FixtureRows)

	var pos invest.Position
	if err := readJSON(filepath.Join(o.StateDir, "position.json"), &pos, true); err != nil {
		return a.fail(ExitFail, "%v", err)
	}
	pos.Normalize()
	if err := pos.Validate(); err != nil {
		return a.fail(ExitFail, "position.json: %v", err)
	}
	var contribs []invest.Contribution
	if err := readJSON(filepath.Join(o.StateDir, "contribution.json"), &contribs, true); err != nil {
		return a.fail(ExitFail, "%v", err)
	}
	if len(contribs) == 0 {
		return a.fail(ExitFail, "contribution.json이 비어 있다 — 적립액 계획이 없으면 매수 사이징을 못 한다")
	}
	var ledger []invest.LedgerEntry
	var devs []invest.Deviation
	var adjs []invest.Adjustment
	var pending *invest.Pending
	var orders *invest.OrderBook
	for _, f := range []struct {
		name string
		v    any
	}{{"ledger.json", &ledger}, {"deviations.json", &devs}, {"adjustments.json", &adjs}, {"pending.json", &pending}, {"orders.json", &orders}} {
		if err := readJSON(filepath.Join(o.StateDir, f.name), f.v, false); err != nil {
			return a.fail(ExitFail, "%v", err)
		}
	}

	// 원금 장부 대조 — 총액과 내역 합이 어긋나면 옮기기 전에 멈춘다
	sum := 0.0
	for _, e := range ledger {
		sum += e.AmountKRW
	}
	if gap := pos.ContributedKRW - sum; math.Abs(gap) >= 1 {
		return a.fail(ExitFail, "누적적립 %s원과 장부 합 %s원이 %s원 어긋난다. TQ에서 `ledger`로 먼저 확인할 것", won(pos.ContributedKRW), won(sum), won(gap))
	}
	a.printf("계좌 %s %d주 · 누적적립 %s원 · 장부 %d건 일치 ✓", dash(pos.Holdings.Symbol), pos.Holdings.Shares, won(pos.ContributedKRW), len(ledger))
	a.printf("판정 %s (%s) → %s %s · 적립 계획 %d건 · 이탈 %d건 · 앱 밖 매수 %d건", pos.YM, pos.LastRun, pos.ApplyYM, pos.Signal.Target, len(contribs), len(devs), len(adjs))
	if pending != nil {
		a.printf("⚠ 냉각 중인 이탈 요청이 있다 — %s %s", pending.YM, pending.Symbol)
	}
	if orders != nil {
		a.printf("⚠ 진행 중인 주문이 있다 — %s %s 단계 %d건", orders.YM, orders.Stage, len(orders.Orders))
	}
	if o.DryRun {
		a.printf("[dry-run] 아무것도 쓰지 않았다")
		return ExitOK
	}

	err = a.Store.InTx(ctx, func(s *store.Store) error {
		if _, err := s.CopyPrices(ctx, prices); err != nil {
			return fmt.Errorf("가격: %w", err)
		}
		for _, c := range contribs {
			if err := s.ImportContribution(ctx, c); err != nil {
				return fmt.Errorf("적립 계획: %w", err)
			}
		}
		for _, d := range devs {
			if err := s.ImportDeviation(ctx, d); err != nil {
				return fmt.Errorf("이탈: %w", err)
			}
		}
		for _, ad := range adjs {
			if err := s.AppendAdjustment(ctx, ad); err != nil {
				return fmt.Errorf("앱 밖 매수: %w", err)
			}
		}
		for _, e := range ledger {
			if err := s.ImportLedger(ctx, e); err != nil {
				return fmt.Errorf("원금 내역: %w", err)
			}
		}
		if err := s.SavePosition(ctx, pos); err != nil {
			return fmt.Errorf("계좌: %w", err)
		}
		if pending != nil {
			if err := s.SavePending(ctx, *pending); err != nil {
				return err
			}
		}
		if orders != nil {
			if err := s.SaveOrders(ctx, *orders); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return a.fail(ExitFail, "옮기기 실패 — 아무것도 쓰지 않았다(롤백): %v", err)
	}

	// 다시 읽어 원본과 비교한다 — 옮긴 것을 믿지 말고 센다
	back, err := a.Store.LoadPosition(ctx)
	if err != nil {
		return a.fail(ExitFail, "다시 읽기 실패: %v", err)
	}
	want, _ := json.Marshal(pos)
	got, _ := json.Marshal(back)
	if !bytes.Equal(want, got) {
		return a.fail(ExitFail, "계좌가 원본과 다르게 읽힌다:\n  원본 %s\n  DB   %s", want, got)
	}
	dbPrices, err := a.Store.LoadPrices(ctx)
	if err != nil || len(dbPrices) != len(prices) {
		return a.fail(ExitFail, "가격 행 수가 다르다: CSV %d · DB %d (%v)", len(prices), len(dbPrices), err)
	}
	for i := range prices {
		if !samePriceRow(prices[i], dbPrices[i]) {
			return a.fail(ExitFail, "%s 가격이 원본과 다르다", invest.FormatDay(prices[i].Day))
		}
	}
	a.printf("옮기기 완료 — 다시 읽어 원본과 대조 ✓")
	return a.Ledger(ctx)
}

func samePriceRow(x, y invest.PriceRow) bool {
	if !x.Day.Equal(y.Day) {
		return false
	}
	for _, c := range []string{"SPX", "NDX", "QQQ", "QQQM", "QLD", "TQQQ", "USDKRW", "USDKRW_MKT"} {
		a, b := x.Get(c), y.Get(c)
		if math.IsNaN(a) != math.IsNaN(b) || (!math.IsNaN(a) && a != b) {
			return false
		}
	}
	return true
}

// readJSON은 TQ 상태 파일을 읽는다. 모르는 키가 있으면 멈춘다 — 조용히 버리면 상태가 줄어든다.
func readJSON(path string, v any, required bool) error {
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) && !required {
		return nil
	}
	if err != nil {
		return fmt.Errorf("%s 읽기 실패: %w", filepath.Base(path), err)
	}
	if strings.TrimSpace(string(b)) == "" || strings.TrimSpace(string(b)) == "{}" {
		if required {
			return fmt.Errorf("%s가 비어 있다", filepath.Base(path))
		}
		return nil
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return fmt.Errorf("%s 형식 이상: %w", filepath.Base(path), err)
	}
	return nil
}

// readMasterCSV는 date,SPX,NDX,QQQ,QQQM,QLD,TQQQ,USDKRW,USDKRW_MKT CSV를 읽는다. 빈 칸은 결측.
func readMasterCSV(path string) (invest.Prices, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	var cols []string
	var out invest.Prices
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fs := strings.Split(line, ",")
		if cols == nil {
			cols = fs
			if cols[0] != "date" {
				return nil, fmt.Errorf("첫 열은 date여야 한다: %v", cols)
			}
			continue
		}
		if len(fs) != len(cols) {
			return nil, fmt.Errorf("열 수가 다르다: %q", line)
		}
		d, err := invest.ParseDay(fs[0])
		if err != nil {
			return nil, fmt.Errorf("날짜 형식 이상: %q", fs[0])
		}
		r := invest.EmptyRow(d)
		for i, c := range cols[1:] {
			s := strings.TrimSpace(fs[i+1])
			if s == "" || s == "nan" || s == "NaN" {
				continue
			}
			v, err := strconv.ParseFloat(s, 64)
			if err != nil {
				return nil, fmt.Errorf("%s %s 숫자 아님: %q", fs[0], c, s)
			}
			r.Set(c, v)
		}
		if len(out) > 0 && !d.After(out[len(out)-1].Day) {
			return nil, fmt.Errorf("날짜가 오름차순이 아니거나 중복: %s", fs[0])
		}
		out = append(out, r)
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, errors.New("행이 없다")
	}
	return out, nil
}
