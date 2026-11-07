package invest

import (
	"bufio"
	"math"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

// 신호 검증 (TQ tests/test_signal.py).
//
// 핵심 관문 3개
//  1. 픽스처 336행 전체 대조 (전 행이 완결 월)
//  2. 회귀 테스트 — 2017-10 (환율 격리) · 1998-11 (워밍업)
//  3. 경계 연산자 — 픽스처에는 정확히 경계값인 달이 없어서 잡히지 않는다
//
// 🔴 관문은 고정 구간(2026-07-31까지)에서 돈다. 입력이 매주 자라는 실데이터에 기대면
// 그 달이 오는 날 관문이 한꺼번에 멈춘다(2026-09 장애). 그래서 testdata의 가격은 픽스처가
// 덮는 구간까지만 담았다.

const (
	fixtureRows = 336 // 1998-08 ~ 2026-07
	tqqqMonths  = 223
	switches    = 43
	tolPrice    = 0.005
	tolVol      = 0.0001
)

func readCSV(t *testing.T, path string) [][]string {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var rows [][]string
	sc := bufio.NewScanner(f)
	header := true
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if header {
			header = false
			continue
		}
		rows = append(rows, strings.Split(line, ","))
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	return rows
}

func mustFloat(t *testing.T, s string) float64 {
	t.Helper()
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		t.Fatalf("숫자 아님 %q", s)
	}
	return v
}

func loadPrices(t *testing.T) SignalInputs {
	t.Helper()
	var in SignalInputs
	for _, r := range readCSV(t, "testdata/spx_ndx_1985-10_2026-07.csv") {
		d, err := ParseDay(r[0])
		if err != nil {
			t.Fatal(err)
		}
		in.Days = append(in.Days, d)
		in.SPX = append(in.SPX, mustFloat(t, r[1]))
		in.NDX = append(in.NDX, mustFloat(t, r[2]))
	}
	return in
}

func loadFixture(t *testing.T) []FixtureRow {
	t.Helper()
	out, err := Fixture()
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func computed(t *testing.T) map[string]SignalRow {
	t.Helper()
	log, err := MonthlySignals(loadPrices(t), SignalStart)
	if err != nil {
		t.Fatal(err)
	}
	m := map[string]SignalRow{}
	for _, r := range log {
		m[r.YM] = r
	}
	return m
}

func TestFixtureFullMatch(t *testing.T) {
	exp := loadFixture(t)
	if len(exp) != fixtureRows {
		t.Fatalf("픽스처 %d행, 기대 %d", len(exp), fixtureRows)
	}
	got := computed(t)
	var bad []string
	tq := 0
	for _, e := range exp {
		g, ok := got[e.YM]
		if !ok {
			t.Fatalf("%s 판정월 유실", e.YM)
		}
		if FormatDay(g.Day) != e.Day {
			bad = append(bad, e.YM+" 판정일 "+FormatDay(g.Day)+" != "+e.Day)
		}
		if math.Abs(g.SPX-e.SPX) > tolPrice {
			bad = append(bad, e.YM+" SPX")
		}
		if math.Abs(g.SMA185-e.SMA185) > tolPrice {
			bad = append(bad, e.YM+" SMA185 "+strconv.FormatFloat(g.SMA185, 'f', 6, 64)+" != "+strconv.FormatFloat(e.SMA185, 'f', 6, 64))
		}
		if math.Abs(g.Vol-e.Vol) > tolVol {
			bad = append(bad, e.YM+" vol")
		}
		if g.C1 != e.C1 || g.C2 != e.C2 {
			bad = append(bad, e.YM+" 조건")
		}
		if g.Pos != e.Pos {
			bad = append(bad, e.YM+" pos "+g.Pos+" != "+e.Pos)
		}
		if g.Pos == RiskOn {
			tq++
		}
	}
	if len(bad) > 0 {
		if len(bad) > 20 {
			bad = bad[:20]
		}
		t.Fatalf("%d건 불일치:\n%s", len(bad), strings.Join(bad, "\n"))
	}
	if tq != tqqqMonths {
		t.Fatalf("TQQQ %d개월, 기대 %d", tq, tqqqMonths)
	}
	sw := 0
	for i := 1; i < len(exp); i++ {
		if got[exp[i].YM].Pos != got[exp[i-1].YM].Pos {
			sw++
		}
	}
	if sw != switches {
		t.Fatalf("전환 %d회, 기대 %d", sw, switches)
	}
}

func TestLastFixtureMonthIsComplete(t *testing.T) {
	got := computed(t)
	if d := FormatDay(got["2026-07"].Day); d != "2026-07-31" {
		t.Fatalf("2026-07 판정일 %s — 07-24면 데이터가 낡은 것", d)
	}
}

func TestRegression2017FXIsolation(t *testing.T) {
	g := computed(t)["2017-10"]
	if math.Abs(g.SMA185-2432.5398) > 0.001 || g.Pos != RiskOn {
		t.Fatalf("2017-10 SMA185 %.4f pos %s", g.SMA185, g.Pos)
	}
}

func TestRegression1998Warmup(t *testing.T) {
	g := computed(t)["1998-11"]
	if math.Abs(g.SMA185-1093.82) > 0.01 || !g.C1 || !g.C2 || g.Pos != RiskOn {
		t.Fatalf("1998-11 %+v", g)
	}
}

// 2017-10-09를 드롭하면 SMA가 실제로 어긋나는지 (사고 재현).
func TestFXJoinWouldBreakIt(t *testing.T) {
	in := loadPrices(t)
	drop := Date(2017, 10, 9)
	var b SignalInputs
	for i, d := range in.Days {
		if d.Equal(drop) {
			continue
		}
		b.Days = append(b.Days, d)
		b.SPX = append(b.SPX, in.SPX[i])
		b.NDX = append(b.NDX, in.NDX[i])
	}
	log, err := MonthlySignals(b, SignalStart)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range log {
		if r.YM == "2017-10" {
			if math.Abs(r.SMA185-2431.1795) > 0.001 {
				t.Fatalf("드롭 시 SMA %.4f, 기대 2431.1795", r.SMA185)
			}
			return
		}
	}
	t.Fatal("2017-10 없음")
}

// 잘린 시리즈에 rolling을 걸면 워밍업 결측으로 멈춰야 한다 (조용히 QQQ로 판정하지 않는다).
func TestTruncatedSeriesFails(t *testing.T) {
	in := loadPrices(t)
	k := 0
	for k < len(in.Days) && in.Days[k].Before(Date(1998, 8, 1)) {
		k++
	}
	cut := SignalInputs{Days: in.Days[k:], SPX: in.SPX[k:], NDX: in.NDX[k:]}
	if _, err := MonthlySignals(cut, SignalStart); err == nil {
		t.Fatal("잘린 시리즈인데 에러가 없다")
	}
}

// 주간 갱신이 월 중간 데이터를 넣어도 앞의 판정은 그대로여야 한다.
func TestMidMonthDataOnlyGrowsTail(t *testing.T) {
	in := loadPrices(t)
	base, err := MonthlySignals(in, SignalStart)
	if err != nil {
		t.Fatal(err)
	}
	ext := SignalInputs{Days: append([]time.Time{}, in.Days...), SPX: append([]float64{}, in.SPX...), NDX: append([]float64{}, in.NDX...)}
	for _, d := range []time.Time{Date(2026, 8, 3), Date(2026, 8, 4)} {
		ext.Days = append(ext.Days, d)
		ext.SPX = append(ext.SPX, 7500)
		ext.NDX = append(ext.NDX, 26000)
	}
	log, err := MonthlySignals(ext, SignalStart)
	if err != nil {
		t.Fatal(err)
	}
	if len(log) != len(base)+1 {
		t.Fatalf("행 %d, 기대 %d", len(log), len(base)+1)
	}
	for i := range base {
		if log[i] != base[i] {
			t.Fatalf("%s가 바뀌었다", base[i].YM)
		}
	}
	if c := CompletedSignals(log); len(c) != len(base) {
		t.Fatalf("미완결 월이 떨어지지 않았다: %d", len(c))
	}
}

func TestBoundaryOperators(t *testing.T) {
	if CondTrend(100, 100) || !CondTrend(100.01, 100) {
		t.Fatal("조건 ①은 strict >")
	}
	if CondVol(0.30) || !CondVol(0.2999) {
		t.Fatal("조건 ②는 strict <")
	}
	cases := []struct {
		c1, c2 bool
		want   string
	}{{true, true, RiskOn}, {true, false, RiskOff}, {false, true, RiskOff}, {false, false, RiskOff}}
	for _, c := range cases {
		if got := ResolvePosition(c.c1, c.c2); got != c.want {
			t.Fatalf("%v/%v → %s", c.c1, c.c2, got)
		}
	}
}

func TestNoMonthSitsExactlyOnBoundary(t *testing.T) {
	for ym, r := range computed(t) {
		if r.SPX == r.SMA185 || r.Vol == VolThreshold {
			t.Fatalf("%s가 정확히 경계값", ym)
		}
	}
}

func TestRealizedVolUsesDDOF1(t *testing.T) {
	close := make([]float64, 200)
	x := 100.0
	for i := range close {
		x *= 1 + 0.01*math.Sin(float64(i)*1.7)
		close[i] = x
	}
	got := RealizedVol(close, VolWindow, TradingDays)[199]
	r := PctChange(close)[179:]
	mean := 0.0
	for _, v := range r {
		mean += v
	}
	mean /= float64(len(r))
	ss := 0.0
	for _, v := range r {
		ss += (v - mean) * (v - mean)
	}
	want1 := math.Sqrt(ss/float64(len(r)-1)) * math.Sqrt(252)
	want0 := math.Sqrt(ss/float64(len(r))) * math.Sqrt(252)
	if math.Abs(got-want1) > 1e-12 || math.Abs(got-want0) < 1e-9 {
		t.Fatalf("ddof=1이 아니다: got %v want %v", got, want1)
	}
}

func TestWindowsNeedEnoughCloses(t *testing.T) {
	c22 := make([]float64, 22)
	for i := range c22 {
		c22[i] = 100 + float64(i)*10/21
	}
	v := RealizedVol(c22, VolWindow, TradingDays)
	for i := 0; i < 21; i++ {
		if !math.IsNaN(v[i]) {
			t.Fatalf("수익률 21개 미만인데 값이 있다 (%d)", i)
		}
	}
	if math.IsNaN(v[21]) {
		t.Fatal("종가 22개면 값이 있어야 한다")
	}
	c185 := make([]float64, 185)
	for i := range c185 {
		c185[i] = float64(i)
	}
	s := SMA(c185, SMAPeriod)
	if !math.IsNaN(s[183]) || math.Abs(s[184]-92) > 1e-12 {
		t.Fatalf("SMA 워밍업 %v %v", s[183], s[184])
	}
}

func TestApplyMonthAndSignalDate(t *testing.T) {
	in := loadPrices(t)
	lastOfMonth := map[string]time.Time{}
	for _, d := range in.Days {
		lastOfMonth[YM(d)] = d
	}
	for ym, r := range computed(t) {
		if r.ApplyYM != NextYM(ym) || r.ApplyYM <= ym {
			t.Fatalf("%s 적용월 %s", ym, r.ApplyYM)
		}
		if !r.Day.Equal(lastOfMonth[ym]) {
			t.Fatalf("%s 판정일 %s ≠ 그 달 마지막 데이터 %s", ym, FormatDay(r.Day), FormatDay(lastOfMonth[ym]))
		}
	}
	if NextYM("2026-12") != "2027-01" {
		t.Fatal("연말 넘김")
	}
}

func TestValidateRejectsHolesAndDuplicates(t *testing.T) {
	in := SignalInputs{Days: []time.Time{Date(2020, 1, 2), Date(2020, 1, 2)}, SPX: []float64{1, 1}, NDX: []float64{1, 1}}
	if in.Validate() == nil {
		t.Fatal("중복 날짜 통과")
	}
	in = SignalInputs{Days: []time.Time{Date(2020, 1, 2), Date(2020, 1, 3)}, SPX: []float64{1, math.NaN()}, NDX: []float64{1, 1}}
	if in.Validate() == nil {
		t.Fatal("결측 통과")
	}
}

func TestSwitchesAndSamples(t *testing.T) {
	log, err := MonthlySignals(loadPrices(t), SignalStart)
	if err != nil {
		t.Fatal(err)
	}
	if n := Switches12M(log); n < 0 || n > MaxSwitches12M {
		t.Fatalf("12개월 전환 %d", n)
	}
	smaN, volN := SampleCounts(loadPrices(t), Date(2026, 7, 31))
	if smaN != SMAPeriod || volN != VolWindow {
		t.Fatalf("표본 %d/%d", smaN, volN)
	}
}

func TestCheckFixtureOnGoldenPrices(t *testing.T) {
	log, err := MonthlySignals(loadPrices(t), SignalStart)
	if err != nil {
		t.Fatal(err)
	}
	bad, err := CheckFixture(log)
	if err != nil || len(bad) != 0 {
		t.Fatalf("%v %v", err, bad)
	}
	// 2017-10-09를 잃은 마스터는 관문에서 걸려야 한다
	in := loadPrices(t)
	var b SignalInputs
	for i, d := range in.Days {
		if d.Equal(Date(2017, 10, 9)) {
			continue
		}
		b.Days, b.SPX, b.NDX = append(b.Days, d), append(b.SPX, in.SPX[i]), append(b.NDX, in.NDX[i])
	}
	broken, err := MonthlySignals(b, SignalStart)
	if err != nil {
		t.Fatal(err)
	}
	if bad, _ := CheckFixture(broken); len(bad) == 0 {
		t.Fatal("거래일이 사라진 마스터가 관문을 통과했다")
	}
}
