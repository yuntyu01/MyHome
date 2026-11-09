package backtest

import (
	"strings"
	"testing"
)

// 표가 원본(xlsx 2026-08 v2, 수수료 0.10%)과 같은지 몇 칸을 못 박는다. 값이 바뀌면 원본을 다시 옮긴 것이다.
func TestTheTableIsTheFixedOriginal(t *testing.T) {
	tb, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(tb.Horizons) != 6 || tb.Horizons[0].Label != "5년" || tb.Horizons[5].Label != "10년" {
		t.Fatalf("기간 %+v", tb.Horizons)
	}
	for _, h := range tb.Horizons {
		if len(h.All) != 9 || len(h.ExDotcom) != 9 || !h.All[0].Star {
			t.Fatalf("%s 행 수·★ 위치가 다르다", h.Label)
		}
	}
	star, _ := tb.Find("7년", true, "★")
	if star.Principal != 1.26 || star.Worst != 0.6601 || star.LossPct != 11.51 || star.MDDWorstPct != -59.56 || star.Windows != 252 {
		t.Fatalf("7년 전체 ★ %+v", star)
	}
	tq, _ := tb.Find("7년", true, "TQQQ 보유")
	if tq.MDDWorstPct != -97.93 {
		t.Fatalf("TQQQ 보유 %+v", tq)
	}
}

// ★가 모든 칸에서 1등이 아니라는 것이 표에 그대로 나와야 한다(손실%·MDD는 작을수록·0에 가까울수록 1등).
func TestTopsFollowEachColumnsDirection(t *testing.T) {
	tb, _ := Load()
	var rows []Row
	for _, h := range tb.Horizons {
		if h.Label == "7년" {
			rows = h.All
		}
	}
	tops := Tops(rows)
	byName := map[string][]string{}
	for i, r := range rows {
		byName[r.Name] = tops[i]
	}
	has := func(name, key string) bool {
		for _, k := range byName[name] {
			if k == key {
				return true
			}
		}
		return false
	}
	// 7년 전체 MDD최악: SPX 보유 −23% · QQQ −48% · ★ −60% · TQQQ −98%. 0에 가장 가까운 쪽이 1등
	if !has("SPX 보유", "mdd_worst_pct") || has("TQQQ 보유", "mdd_worst_pct") {
		t.Fatalf("MDD최악 1등은 0에 가장 가까운 SPX 보유여야 한다: %v", byName)
	}
	if !has("TQQQ 보유", "q90") {
		t.Fatalf("상위10%% 1등은 TQQQ 보유: %v", byName)
	}
	if !has("SPX 보유", "worst") {
		t.Fatalf("최악 1등은 SPX 보유: %v", byName)
	}
	star := byName[rows[0].Name]
	if len(star) == len(Columns) {
		t.Fatal("★가 모든 칸 1등으로 나온다 — 방향이 틀렸다")
	}
}

func TestNotesTakeTheirNumbersFromTheTable(t *testing.T) {
	tb, _ := Load()
	all := strings.Join(Notes(tb), "\n")
	for _, want := range []string{"수수료 0.10%", "중앙 −39%·최악 −60%", "7년 손실확률은 11.5%", "TQQQ·QLD를 그냥 보유하는 것보다 안전"} {
		if !strings.Contains(all, want) {
			t.Errorf("읽기 전에에 %q가 없다", want)
		}
	}
}
