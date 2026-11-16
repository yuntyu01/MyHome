package pipeline

import (
	"context"
	"time"

	"github.com/yuntyu01/MyHome/server/internal/classify"
	"github.com/yuntyu01/MyHome/server/internal/dbq"
)

// MixedLookback 안에 자동으로 확정된 지출만 다시 본다(오래된 거래는 건드리지 않는다).
const MixedLookback = 24 * time.Hour

// ReviewMixed는 AI가 여러 분류 가게(편의점·마트·쿠팡 등, merchant_suggestions.mixed)라고 본 가맹점의 최근 자동 확정 지출을
// 위젯에서 고르게 확인 필요(ambiguous)로 돌린다. 기본 규칙(CU → 생활 등)으로 바로 들어간 첫 결제를 AI 판정 뒤에 고르게 하려는 것이다.
// 사용자가 만든 규칙("이 가게는 항상")에 맞는 거래와 사람이 고친 거래는 그대로 둔다. 돌린 수를 돌려준다.
func ReviewMixed(ctx context.Context, q *dbq.Queries, since time.Time) (int, error) {
	e, err := loadEnv(ctx, q)
	if err != nil || len(e.mixed) == 0 {
		return 0, err
	}
	rows, err := q.ListRecentAutoConfirmed(ctx, since)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, r := range rows {
		target := r.Merchant
		if rest, gw := classify.StripGateway(target, e.cfg.ReviewMerchantPatterns); gw {
			target = rest
		}
		if target == "" || !e.mixed[classify.Normalize(target)] {
			continue
		}
		if _, user := classify.First(e.rules, target, "out", classify.Rule.IsUserRule); user {
			continue
		}
		got, err := q.ReviewAsAmbiguous(ctx, r.ID)
		if err != nil {
			return n, err
		}
		n += int(got)
	}
	return n, nil
}
