package api

import (
	"reflect"
	"testing"

	"github.com/yuntyu01/MyHome/server/internal/cattree"
	"github.com/yuntyu01/MyHome/server/internal/dbq"
)

func pickTree() *cattree.Tree {
	p := func(v int64) *int64 { return &v }
	return cattree.New([]dbq.Category{
		{ID: 5, Name: "생활", Kind: "expense", Sort: 50},
		{ID: 1, Name: "식비", Kind: "expense", Sort: 10, ParentID: p(5)},
		{ID: 2, Name: "카페·간식", Kind: "expense", Sort: 20, ParentID: p(5)},
		{ID: 3, Name: "교통", Kind: "expense", Sort: 30, ParentID: p(5)},
		{ID: 4, Name: "쇼핑", Kind: "expense", Sort: 40, ParentID: p(5)},
		{ID: 7, Name: "구독", Kind: "expense", Sort: 70, ParentID: p(5)},
		{ID: 6, Name: "주거·통신", Kind: "expense", Sort: 60, Archived: true},
		{ID: 11, Name: "기타", Kind: "expense", Sort: 110},
		{ID: 16, Name: "주식", Kind: "expense", Sort: 500},
		{ID: 12, Name: "월급", Kind: "income", Sort: 200},
	})
}

func TestPickChoices(t *testing.T) {
	tree := pickTree()
	usage := map[int64]int64{1: 5, 2: 4, 5: 3, 4: 1, 12: 9}
	cases := []struct {
		name   string
		likely int64
		want   []int64
	}{
		// 전에 식비로 골랐으면 식비 먼저, 같은 폴더(생활)에서 많이 쓴 순
		{"폴더 안 분류", 1, []int64{1, 2, 5, 4}},
		// 폴더이면서 분류인 "생활"이면 그 아래에서
		{"폴더 자신", 5, []int64{5, 1, 2, 4}},
		// 모르면 많이 쓴 지출 분류(수입·숨긴 분류는 빼고)
		{"모름", 0, []int64{1, 2, 5, 4}},
		// 맨 위 분류(아래 없음)는 맨 위끼리
		{"맨 위", 11, []int64{11, 5, 16, 1}},
		// 숨긴 분류는 첫 칸에 안 둔다
		{"숨긴 분류", 6, []int64{1, 2, 5, 4}},
	}
	for _, c := range cases {
		if got := PickChoices(tree, c.likely, usage); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}
