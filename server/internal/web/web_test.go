package web

import (
	"strings"
	"testing"
	"time"

	"github.com/yuntyu01/MyHome/server/internal/api"
	"github.com/yuntyu01/MyHome/server/internal/period"
)

func TestWonShort(t *testing.T) {
	cases := map[int64]string{
		9800:        "9,800원",
		99_999:      "99,999원",
		108_600:     "108,600원",
		3_200_000:   "320만",
		120_000_000: "1억 2,000만",
		100_000_000: "1억",
		-5_000_000:  "-500만",
	}
	for in, want := range cases {
		if got := wonShort(in); got != want {
			t.Errorf("wonShort(%d) = %q, want %q", in, got, want)
		}
	}
}

func TestBarClass(t *testing.T) {
	if barClass(0.5) != "ok" || barClass(0.8) != "warn" || barClass(1.01) != "over" {
		t.Error("80% 경고, 100% 초과 빨강")
	}
}

func TestSplitCategories(t *testing.T) {
	cat := func(name string, budget, spent int64) api.CategoryBudget {
		c := api.CategoryBudget{Name: name, Budget: budget, Spent: spent, Remaining: budget - spent}
		if budget > 0 {
			c.Ratio = float64(spent) / float64(budget)
		}
		return c
	}
	top, rest := splitCategories([]api.CategoryBudget{
		cat("식비", 100, 50), cat("카페", 100, 130), cat("교통", 100, 90), cat("쇼핑", 100, 10), cat("생활", 100, 200),
		cat("구독", 100, 20), cat("문화", 100, 30), cat("의료", 100, 0), cat("기타", 0, 0), cat("경조사", 0, 40),
	})
	names := func(cs []api.CategoryBudget) string {
		var out []string
		for _, c := range cs {
			out = append(out, c.Name)
		}
		return strings.Join(out, ",")
	}
	if got, want := names(top), "생활,경조사,카페,교통,식비"; got != want {
		t.Errorf("펼친 분류 = %s, want %s", got, want)
	}
	if got, want := names(rest), "문화,구독,쇼핑,의료"; got != want {
		t.Errorf("접힌 분류 = %s, want %s", got, want)
	}
}

func TestSplitCategoriesKeepsEveryOverBudget(t *testing.T) {
	var cats []api.CategoryBudget
	for i := 0; i < 7; i++ {
		cats = append(cats, api.CategoryBudget{Budget: 100, Spent: 150, Remaining: -50, Ratio: 1.5})
	}
	if top, rest := splitCategories(cats); len(top) != 7 || len(rest) != 0 {
		t.Errorf("초과한 분류는 5개가 넘어도 다 보여야 함: top=%d rest=%d", len(top), len(rest))
	}
}

func TestDisplayNames(t *testing.T) {
	now := time.Date(2026, 11, 2, 0, 0, 0, 0, period.KST)
	if got := monthNameAt("2026-10", now); got != "10월" {
		t.Errorf("올해 = %q", got)
	}
	if got := monthNameAt("2025-12", now); got != "2025년 12월" {
		t.Errorf("지난해 = %q", got)
	}
	if got := shortDate("2026-10-05"); got != "10월 5일" {
		t.Errorf("날짜 = %q", got)
	}
	if got := sourceName("kb"); got != "KB국민카드" {
		t.Errorf("결제 수단 = %q", got)
	}
	if got := sourceName("새앱"); got != "새앱" {
		t.Errorf("모르는 이름은 그대로 = %q", got)
	}
	p := period.ForMonth(2026, time.October, 15)
	if got := monthRange(p, 15); got != "10/15~11/14" {
		t.Errorf("기간 = %q", got)
	}
	if got := monthRange(period.ForMonth(2026, time.October, 1), 1); got != "" {
		t.Errorf("1일 시작이면 기간 표시 없음 = %q", got)
	}
}
