package invest

import (
	_ "embed"
	"fmt"
	"math"
	"strconv"
	"strings"
)

// 픽스처 336행 — 1998-08~2026-07 월별 판정의 정답이다 (TQ fixtures/signal_log_SPX185_vol30.csv).
//
// TQ는 모든 정기 작업 앞에서 이 대조를 회귀 테스트로 돌렸다. 여기서는 테스트뿐 아니라
// judge·trade·execute가 실행 직전에 DB의 가격으로 한 번 더 대조한다 — 마스터가 오염되면
// (장중 값, 거래일 소실, 소스 교체) 판정 전에 멈춘다.
//
// 바꾸면 안 된다. 파라미터를 바꾸면 이 대조가 터지는 것이 잠금이다(§11).
//
//go:embed fixture/signal_log_SPX185_vol30.csv
var fixtureCSV string

// FixtureRow는 픽스처 1행이다.
type FixtureRow struct {
	YM, Day     string
	SPX, SMA185 float64
	C1          bool
	Vol         float64
	C2          bool
	Pos         string
}

// Fixture 대조 기준.
const (
	FixtureRows   = 336
	FixtureTolPx  = 0.005  // 소수 둘째 자리
	FixtureTolVol = 0.0001 // 0.01%p
)

// Fixture는 내장된 픽스처를 읽는다.
func Fixture() ([]FixtureRow, error) {
	var out []FixtureRow
	header := true
	for _, line := range strings.Split(fixtureCSV, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if header {
			header = false
			continue
		}
		f := strings.Split(line, ",")
		if len(f) != 8 {
			return nil, fmt.Errorf("픽스처 행 형식 이상: %q", line)
		}
		num := func(s string) (float64, error) { return strconv.ParseFloat(s, 64) }
		spx, e1 := num(f[2])
		sma, e2 := num(f[3])
		vol, e3 := num(f[5])
		if e1 != nil || e2 != nil || e3 != nil {
			return nil, fmt.Errorf("픽스처 숫자 이상: %q", line)
		}
		out = append(out, FixtureRow{YM: f[0], Day: f[1], SPX: spx, SMA185: sma, C1: f[4] == "True", Vol: vol, C2: f[6] == "True", Pos: f[7]})
	}
	if len(out) != FixtureRows {
		return nil, fmt.Errorf("픽스처 %d행, 기대 %d", len(out), FixtureRows)
	}
	return out, nil
}

// CheckFixture는 계산한 신호 로그를 픽스처 336개 판정월과 대조한다. 어긋난 항목을 돌려준다.
//
// 대조 기준은 픽스처의 판정월이다 — log 전체가 아니다. 데이터가 월 중간까지 늘면 미완결 월이,
// 새 달이 완결되면 337번째 행이 생긴다. 그것은 신호 변경이 아니라 시간 경과다.
func CheckFixture(log []SignalRow) ([]string, error) {
	exp, err := Fixture()
	if err != nil {
		return nil, err
	}
	got := make(map[string]SignalRow, len(log))
	for _, r := range log {
		got[r.YM] = r
	}
	var bad []string
	for _, e := range exp {
		g, ok := got[e.YM]
		switch {
		case !ok:
			bad = append(bad, e.YM+" 판정월 유실")
		case FormatDay(g.Day) != e.Day:
			bad = append(bad, fmt.Sprintf("%s 판정일 %s ≠ %s", e.YM, FormatDay(g.Day), e.Day))
		case math.Abs(g.SPX-e.SPX) > FixtureTolPx:
			bad = append(bad, fmt.Sprintf("%s SPX %.4f ≠ %.4f", e.YM, g.SPX, e.SPX))
		case math.Abs(g.SMA185-e.SMA185) > FixtureTolPx:
			bad = append(bad, fmt.Sprintf("%s SMA185 %.4f ≠ %.4f", e.YM, g.SMA185, e.SMA185))
		case math.Abs(g.Vol-e.Vol) > FixtureTolVol:
			bad = append(bad, fmt.Sprintf("%s vol %.6f ≠ %.6f", e.YM, g.Vol, e.Vol))
		case g.C1 != e.C1 || g.C2 != e.C2 || g.Pos != e.Pos:
			bad = append(bad, fmt.Sprintf("%s 판정 %v/%v %s ≠ %v/%v %s", e.YM, g.C1, g.C2, g.Pos, e.C1, e.C2, e.Pos))
		}
	}
	return bad, nil
}
