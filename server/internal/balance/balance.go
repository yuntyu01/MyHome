// Package balance는 알림에 찍힌 계좌 잔액으로 가계부가 놓친 돈을 찾는다
// (사용자 요청, 2026-11-15 "토스뱅크 잔액 기준으로, 빈 값은 어디서 놓치고 있다는 거니까 채워 가야").
//
// 잔액이 찍힌 거래(transactions.balance)를 출처(source)별로 시간순으로 잇는다. 앞 잔액 ± 이번 거래 금액이 이번 잔액과
// 다르면 그 사이에 알림이 안 왔거나 못 읽은 돈이 있다는 뜻이다(빈 돈). 사용자는 빈 돈을 채우거나(확인 필요 거래로 넣음)
// 무시한다. 은행 잔액을 직접 묻는 API는 없어서(토스뱅크 공개 API 없음, 오픈뱅킹은 승인받은 사업자만) 알림 잔액만 쓴다.
//
// 한계: 출처 하나에 계좌가 여럿이면(토스뱅크 통장 둘 등) 잔액이 섞여 빈 돈이 잘못 잡힌다. 그때는 파서가 계좌 끝자리를
// 뽑아 출처를 나누게 고친다. 잔액이 안 찍히는 같은 계좌 거래(예: 잔액 없는 결제 알림)도 빈 돈으로 잡히는데, 이미 적힌
// 거래면 "무시"로 치운다.
package balance

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/yuntyu01/MyHome/server/internal/dbq"
	"github.com/yuntyu01/MyHome/server/internal/period"
)

// Lookback 안의 잔액만 본다(위젯 요약에도 쓰므로 가볍게).
const Lookback = 90 * 24 * time.Hour

// ReviewBalanceGap은 빈 돈을 채워서 만든 확인 필요 거래의 사유다(pipeline.ReviewBalanceGap과 같다).
const ReviewBalanceGap = "balance_gap"

// Gap은 빈 돈 하나다. Amount가 음수면 나간 돈을, 양수면 들어온 돈을 놓쳤다.
type Gap struct {
	ToID        int64     `json:"to_id"` // 잔액이 안 맞는 거래(빈 돈 바로 뒤)
	FromID      int64     `json:"from_id"`
	FromAt      time.Time `json:"from_at"`
	ToAt        time.Time `json:"to_at"`
	FromBalance int64     `json:"from_balance"`
	ToBalance   int64     `json:"to_balance"`
	ToMerchant  string    `json:"to_merchant"`
	Amount      int64     `json:"amount"`
}

// Account는 출처 하나의 마지막 잔액과 열린 빈 돈(최근 것부터)이다.
type Account struct {
	Source  string    `json:"source"`
	Label   string    `json:"label"`
	Balance int64     `json:"balance"`
	At      time.Time `json:"at"`
	Gaps    []Gap     `json:"gaps"`
}

// Brief는 요약(GET /v1/summary)에 싣는 짧은 모양이다.
type Brief struct {
	Source   string    `json:"source"`
	Label    string    `json:"label"`
	Balance  int64     `json:"balance"`
	At       time.Time `json:"at"`
	OpenGaps int       `json:"open_gaps"`
}

// Label은 출처 이름을 화면 이름으로 바꾼다. 토스 앱에서 잔액이 찍히는 알림은 토스뱅크 입출금이다.
func Label(source string) string {
	switch source {
	case "toss":
		return "토스뱅크"
	case "kb":
		return "KB국민"
	}
	return source
}

// Anchor는 잔액이 찍힌 거래 하나다.
type Anchor = dbq.ListBalanceAnchorsRow

func signed(a Anchor) int64 {
	if a.Flow == "in" {
		return a.Amount
	}
	return -a.Amount
}

// Chain은 한 출처의 잔액 거래(시간순)에서 빈 돈을 찾는다(처리 기록과 상관없이 전부).
// 같은 앱이 같은 알림을 다시 올린 것(금액·방향·잔액이 앞과 같음)은 한 번으로 본다. 금액을 모르는 거래(외화만, 0원)는
// 빈 돈을 따지지 않고 그 잔액부터 다시 잇는다.
func Chain(rows []Anchor) []Gap {
	var gaps []Gap
	var prev *Anchor
	for i := range rows {
		cur := rows[i]
		if prev != nil && cur.Amount == prev.Amount && cur.Flow == prev.Flow && cur.Balance == prev.Balance {
			continue
		}
		if prev != nil && cur.Amount > 0 {
			if d := cur.Balance - (prev.Balance + signed(cur)); d != 0 {
				gaps = append(gaps, Gap{ToID: cur.ID, FromID: prev.ID, FromAt: prev.OccurredAt, ToAt: cur.OccurredAt,
					FromBalance: prev.Balance, ToBalance: cur.Balance, ToMerchant: cur.Merchant, Amount: d})
			}
		}
		prev = &rows[i]
	}
	return gaps
}

// Accounts는 출처마다 마지막 잔액과 아직 처리하지 않은 빈 돈이다(잔액이 찍힌 거래가 없는 출처는 없다).
func Accounts(ctx context.Context, q *dbq.Queries, now time.Time) ([]Account, error) {
	rows, err := q.ListBalanceAnchors(ctx, now.Add(-Lookback))
	if err != nil {
		return nil, err
	}
	var out []Account
	var all []Gap
	for start := 0; start < len(rows); {
		end := start
		for end < len(rows) && rows[end].Source == rows[start].Source {
			end++
		}
		chain := rows[start:end]
		last := chain[len(chain)-1]
		gaps := Chain(chain)
		out = append(out, Account{Source: last.Source, Label: Label(last.Source), Balance: last.Balance, At: last.OccurredAt.In(period.KST), Gaps: gaps})
		all = append(all, gaps...)
		start = end
	}
	if len(all) == 0 {
		return out, nil
	}
	ids := make([]int64, len(all))
	for i, g := range all {
		ids[i] = g.ToID
	}
	marks, err := q.ListBalanceGapMarks(ctx, ids)
	if err != nil {
		return nil, err
	}
	done := make(map[int64]int64, len(marks))
	for _, m := range marks {
		done[m.ToTransactionID] = m.Gap
	}
	for i := range out {
		open := make([]Gap, 0, len(out[i].Gaps))
		for j := len(out[i].Gaps) - 1; j >= 0; j-- { // 최근 것부터
			g := out[i].Gaps[j]
			// 처리한 뒤 금액이 바뀌었으면(재처리·금액 수정) 다시 보인다
			if gap, ok := done[g.ToID]; ok && gap == g.Amount {
				continue
			}
			g.FromAt, g.ToAt = g.FromAt.In(period.KST), g.ToAt.In(period.KST)
			open = append(open, g)
		}
		out[i].Gaps = open
	}
	return out, nil
}

// Briefs는 요약용 짧은 모양이다.
func Briefs(accts []Account) []Brief {
	out := make([]Brief, 0, len(accts))
	for _, a := range accts {
		out = append(out, Brief{Source: a.Source, Label: a.Label, Balance: a.Balance, At: a.At, OpenGaps: len(a.Gaps)})
	}
	return out
}

// ErrNoGap은 그런 빈 돈이 (더는) 없을 때다. 이미 처리했거나 잔액이 맞게 됐다.
var ErrNoGap = errors.New("빈 돈이 없어요")

// FindOpen은 to_id로 열린 빈 돈 하나와 그 계좌를 찾는다.
func FindOpen(ctx context.Context, q *dbq.Queries, now time.Time, toID int64) (Account, Gap, error) {
	accts, err := Accounts(ctx, q, now)
	if err != nil {
		return Account{}, Gap{}, err
	}
	for _, a := range accts {
		for _, g := range a.Gaps {
			if g.ToID == toID {
				return a, g, nil
			}
		}
	}
	return Account{}, Gap{}, ErrNoGap
}

// Fill은 빈 돈을 확인 필요 거래로 채운다: 빈 돈 바로 뒤 거래 1초 전에, 놓친 금액만큼, 나간 돈이면 지출·들어온 돈이면 입금으로.
// 분류는 사용자가 확인 필요에서 고른다. 만든 거래를 돌려준다.
func Fill(ctx context.Context, q *dbq.Queries, now time.Time, toID int64) (dbq.Transaction, error) {
	a, g, err := FindOpen(ctx, q, now, toID)
	if err != nil {
		return dbq.Transaction{}, err
	}
	amount, flow, dir := g.Amount, "in", "income"
	if amount < 0 {
		amount, flow, dir = -amount, "out", "expense"
	}
	reason := ReviewBalanceGap
	memo := fmt.Sprintf("잔액으로 찾은 빈 돈: %s %s원 → %s %s원", g.FromAt.Format("1/2 15:04"), won(g.FromBalance), g.ToAt.Format("1/2 15:04"), won(g.ToBalance))
	t, err := q.InsertTransaction(ctx, dbq.InsertTransactionParams{
		OccurredAt: g.ToAt.Add(-time.Second), Amount: amount, Flow: flow, Merchant: a.Label + " 빈 돈", Memo: memo,
		Direction: dir, Status: "needs_review", ReviewReason: &reason, Source: "manual", Kind: "balance_gap", ManuallyEdited: true,
	})
	if err != nil {
		return dbq.Transaction{}, err
	}
	id := t.ID
	return t, q.UpsertBalanceGapMark(ctx, dbq.UpsertBalanceGapMarkParams{ToTransactionID: g.ToID, Gap: g.Amount, Resolution: "filled", FilledTransactionID: &id})
}

// Ignore는 빈 돈을 무시한다(이미 적힌 거래인데 잔액이 안 찍혔던 것 등). 금액이 바뀌면 다시 보인다.
func Ignore(ctx context.Context, q *dbq.Queries, now time.Time, toID int64) error {
	_, g, err := FindOpen(ctx, q, now, toID)
	if err != nil {
		return err
	}
	return q.UpsertBalanceGapMark(ctx, dbq.UpsertBalanceGapMarkParams{ToTransactionID: g.ToID, Gap: g.Amount, Resolution: "ignored"})
}

// won은 1,234,567 꼴이다.
func won(v int64) string {
	neg := v < 0
	if neg {
		v = -v
	}
	s := fmt.Sprintf("%d", v)
	var b strings.Builder
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(c)
	}
	if neg {
		return "-" + b.String()
	}
	return b.String()
}
