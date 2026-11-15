// Package notice는 앱이 카드·은행 앱의 원래 결제 알림을 지워도 되는지 정한다(사용자 요청, 2026-11-15).
//
// 가계부가 알아본 것만 지운다: 분류까지 끝난 거래(confirmed)와 지출에서 뺀 거래(excluded: 카드대금·머니 충전·사용자 제외 등).
// 읽지 못한 알림(parse_error), 결제가 아닌 알림(ignore_reason), 분류를 모르는 확인 필요는 남긴다.
// 규칙이 없는 지출(no_rule)은 AI 판정을 기다린다: AI가 확신하면 바로 넣으므로(suggest.AutoConfidence) 그때 지우고,
// 모름·낮은 확신이면 남긴다. 남긴 알림도 사용자가 분류하면 다음 확인 때 지운다.
package notice

import (
	"context"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/yuntyu01/MyHome/server/internal/dbq"
	"github.com/yuntyu01/MyHome/server/internal/pipeline"
	"github.com/yuntyu01/MyHome/server/internal/suggest"
)

// 앱에 돌려주는 값. 앱은 Dismiss면 지우고, Wait면 조금 뒤 다시 묻고, Keep이면 남겨 두고 가끔 다시 묻는다.
const (
	Dismiss = "dismiss" // 지워도 된다
	Wait    = "wait"    // 아직 모른다(처리 전, AI 판정 전)
	Keep    = "keep"    // 남겨 둔다
)

// maxMergeHops는 중복으로 합친 거래의 대표 거래를 따라가는 횟수 상한이다(markDuplicate는 늘 대표에 바로 붙여서 1이면 된다).
const maxMergeHops = 5

// Checker는 원문 하나의 알림 처리 상태를 정한다. AI는 AI 분류 작업이 도는지(AI_CLASSIFY)다.
// 꺼져 있으면 기다릴 판정이 없으므로 규칙 없는 지출도 바로 남긴다.
type Checker struct {
	AI bool
}

// State는 원문 id 하나의 상태다. 없는 id는 Keep(앱이 알아서 오래되면 잊는다).
func (c Checker) State(ctx context.Context, q *dbq.Queries, rawEventID int64) (string, error) {
	raw, err := q.GetRawEvent(ctx, rawEventID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Keep, nil
	}
	if err != nil {
		return "", err
	}
	if raw.ProcessedAt == nil {
		return Wait, nil
	}
	// 사용자가 앱·웹에서 처리한 읽지 못한 알림(직접 적음·결제 아님)은 알아본 것으로 친다
	userDone := pipeline.IsUserResolved(raw.IgnoreReason)
	if !userDone && (raw.ParseError != nil || raw.IgnoreReason != nil) {
		return Keep, nil
	}
	txs, err := q.ListTransactionsByRawEvent(ctx, &raw.ID)
	if err != nil {
		return "", err
	}
	if len(txs) == 0 {
		// 거래 없이 처리된 원문: 사용자가 "결제 아님"으로 치운 것, 앱의 테스트 알림(지우는지 확인하는 용도)은 지운다
		if userDone || strings.HasPrefix(strings.TrimSpace(raw.Title), pipeline.TestTitleMarker) {
			return Dismiss, nil
		}
		return Keep, nil
	}
	t := txs[0]
	for i := 0; t.MergedInto != nil && i < maxMergeHops; i++ {
		if t, err = q.GetTransaction(ctx, *t.MergedInto); err != nil {
			return "", err
		}
	}
	switch t.Status {
	case "confirmed", "excluded":
		return Dismiss, nil
	case "needs_review":
		if !c.AI {
			return Keep, nil
		}
		v, err := suggest.VerdictFor(ctx, q, suggest.Input{Status: t.Status, ReviewReason: t.ReviewReason, Direction: t.Direction,
			Flow: t.Flow, Merchant: t.Merchant, CancelsID: t.CancelsID})
		if err != nil {
			return "", err
		}
		if v == suggest.Pending || v == suggest.Sure {
			return Wait, nil
		}
	}
	return Keep, nil
}
