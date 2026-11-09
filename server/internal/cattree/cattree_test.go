package cattree

import (
	"errors"
	"slices"
	"testing"

	"github.com/yuntyu01/MyHome/server/internal/dbq"
)

func p(v int64) *int64 { return &v }

func sample() *Tree {
	income := "income"
	return New([]dbq.Category{
		{ID: 1, Name: "생활비", Kind: "expense", Sort: 10},
		{ID: 2, Name: "식비", Kind: "expense", Sort: 10, ParentID: p(1)},
		{ID: 3, Name: "배달", Kind: "expense", Sort: 10, ParentID: p(2)},
		{ID: 4, Name: "교통", Kind: "expense", Sort: 5, ParentID: p(1)},
		{ID: 5, Name: "고정비", Kind: "expense", Sort: 20},
		{ID: 6, Name: "수입", Kind: "income", Code: &income, Sort: 200},
		{ID: 7, Name: "숨김", Kind: "expense", Sort: 30, Archived: true},
		{ID: 8, Name: "용돈", Kind: "income", Sort: 210},
	})
}

func TestPathsAndOrder(t *testing.T) {
	tr := sample()
	if got := tr.Label(3); got != "생활비 › 식비 › 배달" {
		t.Fatalf("label = %q", got)
	}
	if tr.Depth(3) != 2 || tr.Depth(1) != 0 || tr.Height(1) != 2 || tr.Height(3) != 0 {
		t.Fatalf("depth/height wrong")
	}
	if tr.Root(3) != 1 || !slices.Equal(tr.Ancestors(3), []int64{2, 1}) || !slices.Equal(tr.Descendants(1), []int64{4, 2, 3}) {
		t.Fatalf("root/ancestors/descendants wrong: %v %v", tr.Ancestors(3), tr.Descendants(1))
	}
	var order []int64
	for _, c := range tr.Preorder() {
		order = append(order, c.ID)
	}
	// 형제는 sort 순서(교통 5 < 식비 10), 폴더 다음에 그 안
	if want := []int64{1, 4, 2, 3, 5, 7, 6, 8}; !slices.Equal(order, want) {
		t.Fatalf("preorder = %v, want %v", order, want)
	}
}

func TestCheckPlace(t *testing.T) {
	tr := sample()
	cases := []struct {
		name   string
		id     int64
		kind   string
		parent int64
		want   error
	}{
		{"새 분류 3단계", 0, "expense", 2, nil},
		{"새 분류 4단계", 0, "expense", 3, ErrTooDeep},
		{"맨 위로", 3, "expense", 0, nil},
		{"폴더째 옮겨 4단계", 1, "expense", 5, ErrTooDeep},
		{"식비를 고정비로(3단계 안)", 2, "expense", 5, nil},
		{"자기 안으로", 1, "expense", 3, ErrCycle},
		{"자기 자신", 2, "expense", 2, ErrCycle},
		{"특수 분류 아래", 0, "income", 6, ErrSpecial},
		{"특수 분류를 폴더 안으로", 6, "income", 8, ErrSpecial},
		{"종류가 다른 폴더", 0, "income", 1, ErrKind},
		{"숨긴 폴더", 0, "expense", 7, ErrArchived},
		{"없는 폴더", 0, "expense", 99, ErrNoParent},
		{"들어오는 돈 폴더", 0, "income", 8, nil},
	}
	for _, c := range cases {
		if err := tr.CheckPlace(c.id, c.kind, c.parent); !errors.Is(err, c.want) && !(err == nil && c.want == nil) {
			t.Errorf("%s: err = %v, want %v", c.name, err, c.want)
		}
	}
}

func TestBrokenParentIsTopLevel(t *testing.T) {
	// 목록 밖 부모, 순환은 맨 위로 본다(무한 반복하지 않는다)
	tr := New([]dbq.Category{
		{ID: 1, Name: "a", Kind: "expense", ParentID: p(2)},
		{ID: 2, Name: "b", Kind: "expense", ParentID: p(1)},
		{ID: 3, Name: "c", Kind: "expense", ParentID: p(99)},
	})
	if tr.Parent(3) != 0 || tr.Parent(1) != 0 && tr.Parent(2) != 0 {
		t.Fatalf("broken parents should be top: %d %d %d", tr.Parent(1), tr.Parent(2), tr.Parent(3))
	}
	if len(tr.Preorder()) != 3 {
		t.Fatalf("every category must appear once: %v", tr.Preorder())
	}
}
