package server_test

import (
	"testing"
	"time"
)

type catDTO struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	Kind     string `json:"kind"`
	ParentID *int64 `json:"parent_id"`
	Memo     string `json:"memo"`
	Archived bool   `json:"archived"`
}

type sumCat struct {
	ID        int64  `json:"id"`
	ParentID  *int64 `json:"parent_id"`
	Depth     int    `json:"depth"`
	Path      string `json:"path"`
	OwnBudget int64  `json:"own_budget"`
	Budget    int64  `json:"budget"`
	Spent     int64  `json:"spent"`
	Remaining int64  `json:"remaining"`
}

// 분류 폴더: 3단계까지 만들고 옮기며, 폴더 합계는 아래를 더한다. docs/DECISIONS.md 「분류 폴더」
func TestCategoryFolders(t *testing.T) {
	c := newClient(t)
	create := func(body map[string]any, want int) catDTO {
		t.Helper()
		var out catDTO
		if code := c.do("POST", "/v1/categories", body, &out); code != want {
			t.Fatalf("만들기 %v: status %d, want %d", body, code, want)
		}
		return out
	}
	patch := func(id int64, body map[string]any, want int) {
		t.Helper()
		if code := c.do("PATCH", "/v1/categories/"+itoa(id), body, nil); code != want {
			t.Fatalf("고치기 %v: status %d, want %d", body, code, want)
		}
	}

	life := create(map[string]any{"name": "생활비", "memo": "  매달 나가는 생활 돈  "}, 201)
	if life.Memo != "매달 나가는 생활 돈" || life.ParentID != nil {
		t.Fatalf("맨 위 폴더 %+v", life)
	}
	food := c.catID("식비")
	patch(food, map[string]any{"parent_id": life.ID, "memo": "밥, 배달"}, 200)
	delivery := create(map[string]any{"name": "배달", "parent_id": food}, 201)
	if delivery.Kind != "expense" || delivery.ParentID == nil || *delivery.ParentID != food {
		t.Fatalf("3단계 %+v", delivery)
	}
	create(map[string]any{"name": "야식", "parent_id": delivery.ID}, 400)            // 4단계
	patch(life.ID, map[string]any{"parent_id": c.catID("교통")}, 400)                // 폴더째 옮겨 4단계
	patch(life.ID, map[string]any{"parent_id": delivery.ID}, 400)                  // 자기 안으로
	patch(c.catID("수입"), map[string]any{"parent_id": life.ID}, 400)                // 특수 분류
	create(map[string]any{"name": "용돈", "parent_id": c.catID("정산")}, 400)          // 특수 분류 아래
	create(map[string]any{"name": "기타", "parent_id": life.ID}, 201)                // 다른 폴더면 같은 이름 가능
	create(map[string]any{"name": "기타", "parent_id": life.ID}, 409)                // 같은 폴더는 안 됨
	create(map[string]any{"name": "긴 설명", "memo": string(make([]rune, 301))}, 400) // 설명 300자

	// 예산: 생활비는 직접 안 정함 → 아래 합(식비 200,000 + 교통 50,000). 식비는 직접 정함 → 배달 예산(30,000)보다 식비 값이 쓰인다.
	patch(c.catID("교통"), map[string]any{"parent_id": life.ID}, 200)
	month := time.Now().In(time.FixedZone("KST", 9*3600)).Format("2006-01")
	items := []map[string]any{
		{"category_id": food, "amount": 200000},
		{"category_id": delivery.ID, "amount": 30000},
		{"category_id": c.catID("교통"), "amount": 50000},
		{"category_id": c.catID("쇼핑"), "amount": 100000},
	}
	if code := c.do("PUT", "/v1/budgets", map[string]any{"month": month, "items": items}, nil); code != 200 {
		t.Fatalf("예산 저장 %d", code)
	}
	// 배달에 직접 결제를 넣는다(현금 입력)
	if code := c.do("POST", "/v1/transactions", map[string]any{"amount": 12000, "merchant": "치킨집", "category_id": delivery.ID}, nil); code != 201 {
		t.Fatalf("현금 입력 %d", code)
	}
	var sum struct {
		Totals struct {
			Budget int64 `json:"budget"`
			Spent  int64 `json:"spent"`
		} `json:"totals"`
		Categories []sumCat `json:"categories"`
	}
	c.do("GET", "/v1/summary?month="+month, nil, &sum)
	by := map[int64]sumCat{}
	order := map[int64]int{}
	for i, x := range sum.Categories {
		by[x.ID] = x
		order[x.ID] = i
	}
	if x := by[life.ID]; x.Budget != 250000 || x.OwnBudget != 0 || x.Spent != 12000 || x.Depth != 0 {
		t.Errorf("생활비 폴더 합계 %+v", x)
	}
	if x := by[food]; x.Budget != 200000 || x.Spent != 12000 || x.Depth != 1 || x.Path != "생활비 › 식비" {
		t.Errorf("식비 %+v", x)
	}
	if x := by[delivery.ID]; x.Budget != 30000 || x.Remaining != 18000 || x.Depth != 2 || x.Path != "생활비 › 식비 › 배달" {
		t.Errorf("배달 %+v", x)
	}
	if !(order[life.ID] < order[food] && order[food] < order[delivery.ID]) {
		t.Errorf("폴더 다음에 그 안: %v", order)
	}
	// 총계는 맨 위만 더한다: 생활비 250,000 + 쇼핑 100,000 (두 번 세지 않음)
	if sum.Totals.Budget != 350000 || sum.Totals.Spent != 12000 {
		t.Errorf("총계 %+v", sum.Totals)
	}

	// 폴더를 지우면(숨기면) 안의 분류도 숨고, 되살리면 같이 돌아온다. 지난 결제가 있으면 요약에는 남는다.
	if code := c.do("DELETE", "/v1/categories/"+itoa(life.ID), nil, nil); code != 200 {
		t.Fatalf("폴더 숨기기 %d", code)
	}
	var list struct {
		Categories []catDTO `json:"categories"`
	}
	c.do("GET", "/v1/categories", nil, &list)
	for _, x := range list.Categories {
		if (x.ID == food || x.ID == delivery.ID) && !x.Archived {
			t.Errorf("안의 분류도 숨김: %+v", x)
		}
	}
	create(map[string]any{"name": "새것", "parent_id": life.ID}, 400) // 숨긴 폴더에는 못 넣음
	patch(delivery.ID, map[string]any{"archived": false}, 200)      // 안쪽을 되살리면 위 폴더도
	c.do("GET", "/v1/categories", nil, &list)
	for _, x := range list.Categories {
		if (x.ID == food || x.ID == life.ID || x.ID == delivery.ID) && x.Archived {
			t.Errorf("되살리기 %+v", x)
		}
	}
	patch(life.ID, map[string]any{"parent_id": 0}, 200) // 맨 위로(이미 맨 위)
	patch(food, map[string]any{"parent_id": 0}, 200)    // 식비를 맨 위로, 배달은 따라온다
	c.do("GET", "/v1/summary?month="+month, nil, &sum)
	for _, x := range sum.Categories {
		if x.ID == delivery.ID && x.Path != "식비 › 배달" {
			t.Errorf("옮긴 뒤 경로 %+v", x)
		}
	}
}
