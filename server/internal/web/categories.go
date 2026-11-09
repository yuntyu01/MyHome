package web

import (
	"net/http"

	"github.com/yuntyu01/MyHome/server/internal/api"
	"github.com/yuntyu01/MyHome/server/internal/cattree"
	"github.com/yuntyu01/MyHome/server/internal/dbq"
)

// categoryGroups는 화면용 분류 묶음이다. 목록은 폴더 다음에 그 안의 분류가 오는 순서다.
type categoryGroups struct {
	All     []dbq.Category
	Expense []dbq.Category
	Other   []dbq.Category // 수입·정산·내 계좌·투자
	ByID    map[int64]dbq.Category
	tree    *cattree.Tree
}

// Label은 경로 이름이다("생활비 › 식비"). 모르는 id면 빈 글자.
func (g categoryGroups) Label(id int64) string {
	if g.tree == nil {
		return ""
	}
	return g.tree.Label(id)
}

// Up은 위 폴더의 경로 이름이다(맨 위 분류면 빈 글자).
func (g categoryGroups) Up(id int64) string {
	if g.tree == nil {
		return ""
	}
	return g.tree.Label(g.tree.Parent(id))
}

// catBtn은 확인 필요 화면의 분류 버튼 하나다.
type catBtn struct {
	ID    int64
	Name  string
	Label string // 경로 이름(title)
	Up    string // 위 폴더 경로(맨 위면 빈 글자)
	Class string
}

func catButton(c dbq.Category, g categoryGroups, class string) catBtn {
	return catBtn{ID: c.ID, Name: c.Name, Label: g.Label(c.ID), Up: g.Up(c.ID), Class: class}
}

func (wb *Web) categories(r *http.Request, includeArchived bool) (categoryGroups, error) {
	cats, err := wb.API.Q().ListCategories(r.Context())
	if err != nil {
		return categoryGroups{}, err
	}
	return groupCategories(cats, includeArchived), nil
}

func groupCategories(cats []dbq.Category, includeArchived bool) categoryGroups {
	t := cattree.New(cats)
	g := categoryGroups{ByID: t.ByID, tree: t}
	for _, c := range t.Preorder() {
		if c.Archived && !includeArchived {
			continue
		}
		g.All = append(g.All, c)
		if c.Kind == "expense" {
			g.Expense = append(g.Expense, c)
		} else {
			g.Other = append(g.Other, c)
		}
	}
	return g
}

// ---- 관리: 분류 폴더 ----

// catOption은 폴더 고르기의 한 항목이다.
type catOption struct {
	ID    int64
	Label string
}

// catNode는 관리 화면의 분류 한 줄과 그 안의 분류다.
type catNode struct {
	dbq.Category
	Depth    int
	Parent   int64       // 지금 폴더(0 = 맨 위)
	CanMove  bool        // 특수 분류·숨긴 분류는 옮기지 않는다
	Folders  []catOption // 옮길 수 있는 폴더(맨 위 제외)
	Children []catNode
	CSRF     string
}

// categoryTree는 관리 화면용 분류 나무다. 폴더 고르기에는 옮겨도 되는 곳만 넣는다(cattree.CheckPlace).
func categoryTree(cats []dbq.Category, csrf string) []catNode {
	t := cattree.New(cats)
	order := t.Preorder()
	var build func(parent int64) []catNode
	build = func(parent int64) []catNode {
		var out []catNode
		for _, id := range t.Children(parent) {
			c := t.ByID[id]
			n := catNode{Category: c, Depth: t.Depth(id), Parent: parent, CanMove: c.Code == nil && !c.Archived, CSRF: csrf}
			if n.CanMove {
				for _, f := range order {
					// 지금 폴더는 (검사에 걸리더라도) 늘 넣는다. 빠지면 저장할 때 맨 위로 옮겨져 버린다.
					if f.ID == parent || (f.ID != id && t.CheckPlace(id, c.Kind, f.ID) == nil) {
						n.Folders = append(n.Folders, catOption{ID: f.ID, Label: t.Label(f.ID)})
					}
				}
			}
			n.Children = build(id)
			out = append(out, n)
		}
		return out
	}
	return build(0)
}

// newCategoryFolders는 새 분류를 넣을 수 있는 폴더들이다. 들어오는 돈 폴더는 표시를 붙인다(안의 분류가 종류를 따른다).
func newCategoryFolders(cats []dbq.Category) []catOption {
	t := cattree.New(cats)
	var out []catOption
	for _, c := range t.Preorder() {
		if t.CheckPlace(0, c.Kind, c.ID) != nil {
			continue
		}
		label := t.Label(c.ID)
		if c.Kind != "expense" {
			label += " (들어오는 돈)"
		}
		out = append(out, catOption{ID: c.ID, Label: label})
	}
	return out
}

// ---- 홈·분석: 맨 위 분류와 그 안 ----

// dashCat은 홈의 분류 한 줄이다. 맨 위 분류이고, 안에 든 분류는 펼쳐 볼 수 있게 붙인다.
type dashCat struct {
	api.CategoryBudget
	Children []api.CategoryBudget // 안의 분류(펼친 순서). 예산도 쓴 돈도 없는 것은 뺀다
}

// withChildren은 맨 위 분류에 안의 분류를 붙인다. cats는 요약의 펼친 순서 목록이다.
func withChildren(top []api.CategoryBudget, cats []api.CategoryBudget) []dashCat {
	if len(top) == 0 {
		return nil
	}
	parent := map[int64]int64{}
	for _, c := range cats {
		if c.ParentID != nil {
			parent[c.ID] = *c.ParentID
		}
	}
	root := func(id int64) int64 {
		for i := 0; i <= cattree.MaxDepth; i++ {
			p, ok := parent[id]
			if !ok {
				break
			}
			id = p
		}
		return id
	}
	kids := map[int64][]api.CategoryBudget{}
	for _, c := range cats {
		if c.ParentID == nil || (c.Budget == 0 && c.Spent == 0 && c.Settled == 0) {
			continue
		}
		r := root(c.ID)
		kids[r] = append(kids[r], c)
	}
	out := make([]dashCat, 0, len(top))
	for _, c := range top {
		out = append(out, dashCat{CategoryBudget: c, Children: kids[c.ID]})
	}
	return out
}

// treeOrder는 비교 목록을 폴더 다음에 그 안이 오도록 다시 늘어놓는다.
// 지난 기간에만 있던 분류는 목록 끝에 붙어 오므로, 제 폴더 아래(형제 중 마지막)로 옮긴다.
func treeOrder(cats []api.CategoryCompare) []api.CategoryCompare {
	present := make(map[int64]bool, len(cats))
	for _, c := range cats {
		present[c.ID] = true
	}
	kids := map[int64][]int{}
	for i, c := range cats {
		var p int64
		if c.ParentID != nil && present[*c.ParentID] {
			p = *c.ParentID
		}
		kids[p] = append(kids[p], i)
	}
	out := make([]api.CategoryCompare, 0, len(cats))
	var walk func(p int64, depth int)
	walk = func(p int64, depth int) {
		if depth > cattree.MaxDepth {
			return
		}
		for _, i := range kids[p] {
			out = append(out, cats[i])
			walk(cats[i].ID, depth+1)
		}
	}
	walk(0, 0)
	return out
}
