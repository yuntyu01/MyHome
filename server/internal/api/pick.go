package api

import (
	"context"
	"sort"
	"time"

	"github.com/yuntyu01/MyHome/server/internal/balance"
	"github.com/yuntyu01/MyHome/server/internal/cattree"
	"github.com/yuntyu01/MyHome/server/internal/dbq"
	"github.com/yuntyu01/MyHome/server/internal/pipeline"
	"github.com/yuntyu01/MyHome/server/internal/suggest"
)

// Pick은 위젯에서 동그라미 한 번 눌러 분류할 다음 거래다(사용자 요청, 2026-11-16 "애매한 건 위젯에서 고르고 다음 걸로").
// 고르면 PATCH /v1/transactions/{id}(category_id)로 확정하고 요약을 다시 받으면 다음 거래가 온다.
type Pick struct {
	ID         int64        `json:"id"`
	Merchant   string       `json:"merchant"`
	Amount     int64        `json:"amount"`
	OccurredAt time.Time    `json:"occurred_at"`
	Reason     string       `json:"reason"`
	Choices    []PickChoice `json:"choices"`
	Left       int64        `json:"left"` // 이것을 포함해 남은 수
}

// PickChoice는 동그라미 하나(분류)다.
type PickChoice struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

// PickReasons는 위젯에서 고를 확인 필요 사유다. 송금·입금·취소·외화·종류 모름은 분류만 고를 일이 아니라서 앱에서 한다.
var PickReasons = []string{pipeline.ReviewAmbiguous, pipeline.ReviewNoRule, pipeline.ReviewPaymentGateway, pipeline.ReviewNoMerchant, balance.ReviewBalanceGap}

// MaxPickChoices는 위젯 한 줄에 들어가는 동그라미 수다.
const MaxPickChoices = 4

// pickUsageLookback 동안 확정한 지출 건수로 동그라미 순서를 정한다.
const pickUsageLookback = 90 * 24 * time.Hour

// PickChoices는 동그라미로 보여줄 분류 id다(최대 MaxPickChoices). likely(전에 이 가게에 고른 분류나 AI 추천)가 있으면
// 그것을 맨 앞에 두고 같은 폴더(그 폴더 자신과 바로 아래)에서 많이 쓴 순서로 채운다("연관된 폴더에서").
// 없으면 많이 쓴 지출 분류 순이다. 폴더가 작아 칸이 남으면 많이 쓴 다른 분류로 채운다.
func PickChoices(t *cattree.Tree, likely int64, usage map[int64]int64) []int64 {
	ok := func(id int64) bool {
		c, found := t.ByID[id]
		return found && c.Kind == "expense" && !c.Archived
	}
	order := map[int64]int{}
	var all []int64
	for i, c := range t.Preorder() {
		order[c.ID] = i
		if ok(c.ID) {
			all = append(all, c.ID)
		}
	}
	byUse := func(ids []int64) []int64 {
		out := append([]int64(nil), ids...)
		sort.SliceStable(out, func(i, j int) bool {
			if usage[out[i]] != usage[out[j]] {
				return usage[out[i]] > usage[out[j]]
			}
			return order[out[i]] < order[out[j]]
		})
		return out
	}
	var out []int64
	seen := map[int64]bool{}
	add := func(ids ...int64) {
		for _, id := range ids {
			if len(out) < MaxPickChoices && ok(id) && !seen[id] {
				seen[id] = true
				out = append(out, id)
			}
		}
	}
	if ok(likely) {
		add(likely)
		folder := t.Parent(likely)
		if folder == 0 && len(t.Children(likely)) > 0 {
			folder = likely // 폴더이면서 분류인 것(생활 › 식비의 "생활")
		}
		var members []int64
		if folder != 0 {
			members = append([]int64{folder}, t.Children(folder)...)
		} else {
			members = t.Children(0)
		}
		add(byUse(members)...)
	}
	add(byUse(all)...) // 폴더가 작으면 많이 쓴 다른 분류로 채운다
	return out
}

// BuildPick은 위젯에서 고를 다음 거래(최근 것부터)와 동그라미를 만든다. 없으면 nil.
func (a *API) BuildPick(ctx context.Context, q *dbq.Queries, now time.Time) (*Pick, error) {
	rows, err := q.ListPicks(ctx, dbq.ListPicksParams{Reasons: PickReasons, Lim: 1})
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	tx := rows[0]
	left, err := q.CountPicks(ctx, PickReasons)
	if err != nil {
		return nil, err
	}
	cats, err := q.ListCategories(ctx)
	if err != nil {
		return nil, err
	}
	tree := cattree.New(cats)
	// 첫 칸: 이 가게에 전에 고른 분류 → AI 추천
	var likely int64
	hist, err := q.LastCategoriesForMerchant(ctx, tx.Merchant)
	if err != nil {
		return nil, err
	}
	if len(hist) > 0 {
		likely = hist[0]
	} else if a.Suggestions {
		in := []suggest.Input{{Status: tx.Status, ReviewReason: tx.ReviewReason, Direction: tx.Direction, Flow: tx.Flow, Merchant: tx.Merchant, CancelsID: tx.CancelsID}}
		if s, err := suggest.Lookup(ctx, q, in); err == nil && s[0] != nil {
			likely = s[0].CategoryID
		}
	}
	use, err := q.CategoryUsage(ctx, now.Add(-pickUsageLookback))
	if err != nil {
		return nil, err
	}
	usage := make(map[int64]int64, len(use))
	for _, u := range use {
		usage[u.CategoryID] = u.N
	}
	p := &Pick{ID: tx.ID, Merchant: tx.Merchant, Amount: tx.Amount, OccurredAt: tx.OccurredAt, Left: left, Choices: []PickChoice{}}
	if tx.ReviewReason != nil {
		p.Reason = *tx.ReviewReason
	}
	for _, id := range PickChoices(tree, likely, usage) {
		p.Choices = append(p.Choices, PickChoice{ID: id, Name: tree.ByID[id].Name})
	}
	return p, nil
}
