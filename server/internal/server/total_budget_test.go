package server_test

import (
	"testing"
	"time"
)

// 한 달 전체 예산: 정하면 요약의 예산·남은 돈이 그 값으로, 다음 기간에도 이어지고, null이면 분류 합계로 돌아간다.
func TestTotalBudget(t *testing.T) {
	c := newClient(t)
	type totals struct {
		Budget         int64  `json:"budget"`
		CategoryBudget int64  `json:"category_budget"`
		OwnBudget      *int64 `json:"own_budget"`
		Remaining      int64  `json:"remaining"`
	}
	sum := func(m string) totals {
		t.Helper()
		var s struct {
			Totals totals `json:"totals"`
		}
		if code := c.do("GET", "/v1/summary?month="+m, nil, &s); code != 200 {
			t.Fatalf("summary %d", code)
		}
		return s.Totals
	}
	put := func(body map[string]any, want int) {
		t.Helper()
		if code := c.do("PUT", "/v1/budgets/total", body, nil); code != want {
			t.Fatalf("PUT %v: %d, want %d", body, code, want)
		}
	}
	m := month()
	if code := c.do("PUT", "/v1/budgets", map[string]any{"month": m, "items": []map[string]any{{"category_id": c.catID("식비"), "amount": 300000}}}, nil); code != 200 {
		t.Fatalf("budgets %d", code)
	}
	if x := sum(m); x.Budget != 300000 || x.CategoryBudget != 300000 || x.OwnBudget != nil {
		t.Fatalf("처음엔 분류 합계 %+v", x)
	}
	put(map[string]any{"month": m, "amount": 2000000}, 200)
	if x := sum(m); x.Budget != 2000000 || x.CategoryBudget != 300000 || x.OwnBudget == nil || x.Remaining != 2000000 {
		t.Fatalf("정한 값 %+v", x)
	}
	next := nextMonth(m)
	if x := sum(next); x.Budget != 2000000 {
		t.Fatalf("다음 기간에도 이어짐 %+v", x)
	}
	put(map[string]any{"month": next, "amount": nil}, 200) // 다음 기간부터 분류 합계로
	if x := sum(next); x.OwnBudget != nil || x.Budget != x.CategoryBudget {
		t.Fatalf("되돌림 %+v", x)
	}
	if x := sum(m); x.Budget != 2000000 {
		t.Fatalf("이번 기간은 그대로 %+v", x)
	}
	put(map[string]any{"month": m, "amount": -1}, 400)
	put(map[string]any{"month": m, "amount": 100_000_000_001}, 400)
}

func nextMonth(m string) string {
	t, err := time.Parse("2006-01", m)
	if err != nil {
		panic(err)
	}
	return t.AddDate(0, 1, 0).Format("2006-01")
}

// 분류 예산 방식: 자유(예산 0)는 한 달 쓴 돈에 들고, 예산 없음(in_budget=false)은 안 든다. 폴더를 예산 없음으로 하면 아래도.
func TestBudgetScope(t *testing.T) {
	c := newClient(t)
	type cat struct {
		ID       int64 `json:"id"`
		InBudget bool  `json:"in_budget"`
		Spent    int64 `json:"spent"`
	}
	var s struct {
		Totals struct {
			Spent int64 `json:"spent"`
		} `json:"totals"`
		Categories []cat `json:"categories"`
	}
	pay := func(amount int, cat string) {
		t.Helper()
		if code := c.do("POST", "/v1/transactions", map[string]any{"amount": amount, "merchant": "합성", "category_id": c.catID(cat)}, nil); code != 201 {
			t.Fatalf("현금 입력 %d", code)
		}
	}
	pay(10000, "식비") // 자유(예산 0)
	pay(5000, "경조사")
	c.do("GET", "/v1/summary", nil, &s)
	if s.Totals.Spent != 15000 {
		t.Fatalf("처음엔 다 든다 %+v", s.Totals)
	}
	if code := c.do("PATCH", "/v1/categories/"+itoa(c.catID("경조사")), map[string]any{"in_budget": false}, nil); code != 200 {
		t.Fatalf("예산 없음 %d", code)
	}
	c.do("GET", "/v1/summary", nil, &s)
	if s.Totals.Spent != 10000 {
		t.Fatalf("예산 없음은 빠진다 %+v", s.Totals)
	}
	for _, x := range s.Categories {
		if x.ID == c.catID("경조사") && (x.InBudget || x.Spent != 5000) {
			t.Fatalf("경조사 %+v", x)
		}
	}
	// 폴더째: 생활비(예산 없음) 안의 식비도 빠진다
	var life catDTO
	c.do("POST", "/v1/categories", map[string]any{"name": "생활비", "in_budget": false}, &life)
	c.do("PATCH", "/v1/categories/"+itoa(c.catID("식비")), map[string]any{"parent_id": life.ID}, nil)
	c.do("GET", "/v1/summary", nil, &s)
	if s.Totals.Spent != 0 {
		t.Fatalf("폴더째 빠진다 %+v", s.Totals)
	}
}

// 고정으로 빠지는 돈: 만들면 쓸 수 있는 돈에서 빠지고, 고치고 지우면(보관) 따라 바뀐다.
func TestFixedDeductions(t *testing.T) {
	c := newClient(t)
	type totals struct {
		Budget    int64 `json:"budget"`
		Fixed     int64 `json:"fixed"`
		Remaining int64 `json:"remaining"`
	}
	sum := func() totals {
		var s struct {
			Totals totals `json:"totals"`
		}
		c.do("GET", "/v1/summary", nil, &s)
		return s.Totals
	}
	c.do("PUT", "/v1/budgets/total", map[string]any{"amount": 2000000}, nil)
	var f struct {
		ID     int64  `json:"id"`
		Name   string `json:"name"`
		Amount int64  `json:"amount"`
	}
	if code := c.do("POST", "/v1/fixed", map[string]any{"name": " 주식 적립 ", "amount": 500000}, &f); code != 201 || f.Name != "주식 적립" {
		t.Fatalf("만들기 %d %+v", code, f)
	}
	c.do("POST", "/v1/fixed", map[string]any{"name": "적금", "amount": 300000}, nil)
	if x := sum(); x.Fixed != 800000 || x.Remaining != 1200000 {
		t.Fatalf("고정 빼기 %+v", x)
	}
	if code := c.do("PATCH", "/v1/fixed/"+itoa(f.ID), map[string]any{"amount": 400000}, nil); code != 200 {
		t.Fatalf("고치기 %d", code)
	}
	if code := c.do("DELETE", "/v1/fixed/"+itoa(f.ID), nil, nil); code != 200 {
		t.Fatalf("지우기 %d", code)
	}
	var list struct {
		Items []struct {
			Name string `json:"name"`
		} `json:"items"`
	}
	c.do("GET", "/v1/fixed", nil, &list)
	if len(list.Items) != 1 || list.Items[0].Name != "적금" {
		t.Fatalf("목록 %+v", list)
	}
	if x := sum(); x.Fixed != 300000 || x.Remaining != 1700000 {
		t.Fatalf("지운 뒤 %+v", x)
	}
	for _, bad := range []map[string]any{{"name": "", "amount": 1}, {"name": "x", "amount": 0}, {"name": "x"}} {
		if code := c.do("POST", "/v1/fixed", bad, nil); code != 400 {
			t.Fatalf("잘못된 값 %v → %d", bad, code)
		}
	}
	if code := c.do("PATCH", "/v1/fixed/999999", map[string]any{"amount": 1}, nil); code != 404 {
		t.Fatalf("없는 것 %d", code)
	}
}
