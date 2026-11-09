// Package cattree는 분류 폴더 구조(최대 3단계)를 다룬다: 경로 이름, 깊이, 아래·위 분류, 옮기기 검사.
// docs/DECISIONS.md 「분류 폴더」
package cattree

import (
	"errors"
	"sort"
	"strings"

	"github.com/yuntyu01/MyHome/server/internal/dbq"
)

// MaxDepth는 폴더 단계 수 상한이다(맨 위 = 1단계).
const MaxDepth = 3

// Sep은 경로 이름의 구분자다(예: "생활비 › 식비 › 배달").
const Sep = " › "

// MaxMemo는 분류 설명 글자 수 상한이다(AI 프롬프트에 그대로 들어간다).
const MaxMemo = 300

// Tree는 분류 목록 스냅샷이다. 부모가 목록에 없거나 순환이 있으면(있으면 안 되지만) 그 분류를 맨 위로 본다.
type Tree struct {
	ByID     map[int64]dbq.Category
	children map[int64][]int64 // 0 = 맨 위
}

// New는 분류 목록으로 트리를 만든다. 형제끼리는 sort, id 순서다.
func New(cats []dbq.Category) *Tree {
	t := &Tree{ByID: make(map[int64]dbq.Category, len(cats)), children: map[int64][]int64{}}
	for _, c := range cats {
		t.ByID[c.ID] = c
	}
	for _, c := range cats {
		t.children[t.parentOf(c)] = append(t.children[t.parentOf(c)], c.ID)
	}
	for k, ids := range t.children {
		sort.SliceStable(ids, func(i, j int) bool {
			a, b := t.ByID[ids[i]], t.ByID[ids[j]]
			if a.Sort != b.Sort {
				return a.Sort < b.Sort
			}
			return a.ID < b.ID
		})
		t.children[k] = ids
	}
	return t
}

// parentOf는 쓸 수 있는 부모 id다(없거나 목록 밖이거나 순환이면 0).
func (t *Tree) parentOf(c dbq.Category) int64 {
	if c.ParentID == nil {
		return 0
	}
	if _, ok := t.ByID[*c.ParentID]; !ok {
		return 0
	}
	// 순환 방어: 위로 MaxDepth*4번 넘게 올라가면 맨 위로 본다
	seen := 0
	for p := *c.ParentID; p != 0; {
		if p == c.ID || seen > MaxDepth*4 {
			return 0
		}
		pc, ok := t.ByID[p]
		if !ok || pc.ParentID == nil {
			break
		}
		p = *pc.ParentID
		seen++
	}
	return *c.ParentID
}

// Parent는 부모 id다(맨 위면 0).
func (t *Tree) Parent(id int64) int64 {
	c, ok := t.ByID[id]
	if !ok {
		return 0
	}
	return t.parentOf(c)
}

// Children은 바로 아래 분류 id들이다(0이면 맨 위 분류들).
func (t *Tree) Children(id int64) []int64 { return t.children[id] }

// Depth는 단계다(맨 위 = 0).
func (t *Tree) Depth(id int64) int {
	d := 0
	for p := t.Parent(id); p != 0; p = t.Parent(p) {
		d++
	}
	return d
}

// Height는 아래로 몇 단계가 더 있는지다(아래가 없으면 0).
func (t *Tree) Height(id int64) int {
	h := 0
	for _, c := range t.children[id] {
		h = max(h, t.Height(c)+1)
	}
	return h
}

// Ancestors는 위 폴더 id들이다(가까운 것부터).
func (t *Tree) Ancestors(id int64) []int64 {
	var out []int64
	for p := t.Parent(id); p != 0; p = t.Parent(p) {
		out = append(out, p)
	}
	return out
}

// Descendants는 아래 분류 전부다(자기 자신 제외, 위에서 아래 순서).
func (t *Tree) Descendants(id int64) []int64 {
	var out []int64
	for _, c := range t.children[id] {
		out = append(out, c)
		out = append(out, t.Descendants(c)...)
	}
	return out
}

// Root는 맨 위 폴더 id다(자기가 맨 위면 자기).
func (t *Tree) Root(id int64) int64 {
	for p := t.Parent(id); p != 0; p = t.Parent(p) {
		id = p
	}
	return id
}

// Path는 맨 위부터 자기까지의 이름이다.
func (t *Tree) Path(id int64) []string {
	c, ok := t.ByID[id]
	if !ok {
		return nil
	}
	anc := t.Ancestors(id)
	out := make([]string, 0, len(anc)+1)
	for i := len(anc) - 1; i >= 0; i-- {
		out = append(out, t.ByID[anc[i]].Name)
	}
	return append(out, c.Name)
}

// Label은 경로 이름이다(예: "생활비 › 식비"). 맨 위면 이름 그대로.
func (t *Tree) Label(id int64) string { return strings.Join(t.Path(id), Sep) }

// Preorder는 위에서 아래로 펼친 순서다(폴더 다음에 그 안의 분류).
func (t *Tree) Preorder() []dbq.Category {
	out := make([]dbq.Category, 0, len(t.ByID))
	var walk func(int64)
	walk = func(p int64) {
		for _, id := range t.children[p] {
			out = append(out, t.ByID[id])
			walk(id)
		}
	}
	walk(0)
	return out
}

// 옮기기·만들기가 안 되는 이유. 화면에 그대로 보여준다.
var (
	ErrSpecial  = errors.New("수입·정산·내 계좌 이체·투자·저축은 폴더에 넣거나 폴더로 쓸 수 없어요")
	ErrKind     = errors.New("쓰는 돈과 들어오는 돈은 서로 다른 폴더에 둬야 해요")
	ErrCycle    = errors.New("자기 자신이나 자기 안의 폴더로는 옮길 수 없어요")
	ErrTooDeep  = errors.New("폴더는 3단계까지만 만들 수 있어요")
	ErrArchived = errors.New("숨긴 폴더에는 넣을 수 없어요")
	ErrNoParent = errors.New("그 폴더가 없어요")
)

// CheckPlace는 분류 id(새 분류면 0, 종류 kind)를 parent 아래(0 = 맨 위)에 둘 수 있는지 본다.
func (t *Tree) CheckPlace(id int64, kind string, parent int64) error {
	if id != 0 {
		c, ok := t.ByID[id]
		if ok && c.Code != nil && parent != 0 {
			return ErrSpecial
		}
	}
	if parent == 0 {
		return nil
	}
	p, ok := t.ByID[parent]
	switch {
	case !ok:
		return ErrNoParent
	case p.Code != nil:
		return ErrSpecial
	case p.Kind != kind:
		return ErrKind
	case p.Archived:
		return ErrArchived
	}
	if id != 0 {
		if parent == id {
			return ErrCycle
		}
		for _, a := range t.Ancestors(parent) {
			if a == id {
				return ErrCycle
			}
		}
	}
	h := 0
	if id != 0 {
		h = t.Height(id)
	}
	if t.Depth(parent)+1+h >= MaxDepth {
		return ErrTooDeep
	}
	return nil
}
