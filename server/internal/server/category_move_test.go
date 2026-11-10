package server_test

import (
	"context"
	"testing"
)

// 앱에서 꾹 눌러 끌어 옮기기: 같은 폴더 안 순서, 다른 폴더 안의 정한 자리, 폴더 맨 끝, 맨 위. 폴더는 안까지 통째로.
func TestCategoryMove(t *testing.T) {
	c := newClient(t)
	move := func(id int64, body map[string]any, want int) {
		t.Helper()
		if code := c.do("POST", "/v1/categories/"+itoa(id)+"/move", body, nil); code != want {
			t.Fatalf("옮기기 %d %v: status %d, want %d", id, body, code, want)
		}
	}
	// order는 parent(0 = 맨 위) 안의 일반 분류 이름을 순서대로 준다(특수 분류 제외)
	order := func(parent int64) []string {
		t.Helper()
		cats, err := c.q.ListCategories(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, x := range cats { // ListCategories는 sort, id 순서다
			p := int64(0)
			if x.ParentID != nil {
				p = *x.ParentID
			}
			if p == parent && x.Code == nil {
				out = append(out, x.Name)
			}
		}
		return out
	}
	same := func(got []string, want ...string) {
		t.Helper()
		if len(got) != len(want) {
			t.Fatalf("순서 %v, want %v", got, want)
		}
		for i := range got {
			if got[i] != want[i] {
				t.Fatalf("순서 %v, want %v", got, want)
			}
		}
	}

	var life catDTO
	if code := c.do("POST", "/v1/categories", map[string]any{"name": "생활비"}, &life); code != 201 {
		t.Fatalf("폴더 만들기 %d", code)
	}
	food, cafe, traffic := c.catID("식비"), c.catID("카페·간식"), c.catID("교통")

	// 생활비 폴더를 맨 앞으로(식비 앞)
	move(life.ID, map[string]any{"parent_id": 0, "before": food}, 200)
	same(order(0)[:3], "생활비", "식비", "카페·간식")

	// 폴더 안으로: 끝에 넣기 → 정한 자리(앞)에 넣기 → 뒤에 넣기
	move(food, map[string]any{"parent_id": life.ID}, 200)
	move(cafe, map[string]any{"parent_id": life.ID}, 200)
	move(traffic, map[string]any{"parent_id": life.ID, "before": food}, 200)
	same(order(life.ID), "교통", "식비", "카페·간식")
	move(traffic, map[string]any{"parent_id": life.ID, "after": cafe}, 200)
	same(order(life.ID), "식비", "카페·간식", "교통")

	// 폴더째 옮기면 안의 분류는 따라온다(경로가 바뀐다)
	var deliv catDTO
	c.do("POST", "/v1/categories", map[string]any{"name": "배달", "parent_id": food}, &deliv)
	move(life.ID, map[string]any{"parent_id": 0}, 200) // 맨 위 맨 끝
	if got := order(0); got[len(got)-1] != "생활비" {
		t.Fatalf("맨 끝으로 %v", got)
	}
	same(order(food), "배달")

	// 규칙: 4단계, 자기 안, 특수 분류, 없는 자리, before·after 같이, 같은 이름
	move(c.catID("쇼핑"), map[string]any{"parent_id": food}, 200)     // 3단계는 됨
	move(c.catID("교통"), map[string]any{"parent_id": deliv.ID}, 400) // 4단계
	move(life.ID, map[string]any{"parent_id": c.catID("생활")}, 400)  // 생활비(3단계)를 한 단계 더 → 4단계
	move(life.ID, map[string]any{"parent_id": deliv.ID}, 400)       // 자기 안으로
	move(c.catID("수입"), map[string]any{"parent_id": 0}, 400)        // 특수 분류
	move(food, map[string]any{"parent_id": life.ID, "before": traffic + 9999}, 400)
	move(food, map[string]any{"parent_id": life.ID, "before": cafe, "after": traffic}, 400)
	c.do("POST", "/v1/categories", map[string]any{"name": "식비"}, nil) // 맨 위에 같은 이름
	move(food, map[string]any{"parent_id": 0}, 409)
	move(999999, map[string]any{"parent_id": 0}, 404)
}
