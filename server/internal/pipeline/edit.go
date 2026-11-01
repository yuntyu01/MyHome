package pipeline

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/yuntyu01/MyHome/server/internal/classify"
	"github.com/yuntyu01/MyHome/server/internal/db"
	"github.com/yuntyu01/MyHome/server/internal/dbq"
	"github.com/yuntyu01/MyHome/server/internal/parser"
)

// ErrInvalid는 사용자 입력이 잘못됐을 때 반환한다(HTTP 400).
var ErrInvalid = errors.New("invalid input")

func invalid(format string, a ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalid, fmt.Sprintf(format, a...))
}

// Edit는 거래 수정 요청이다. nil인 필드는 바꾸지 않는다.
type Edit struct {
	CategoryID       *int64     `json:"category_id"`
	SettleCategoryID *int64     `json:"settle_category_id"` // 0이면 해제
	Memo             *string    `json:"memo"`
	Merchant         *string    `json:"merchant"`
	Amount           *int64     `json:"amount"`
	OccurredAt       *time.Time `json:"occurred_at"`
	// Status: "excluded"(제외) 또는 "confirmed"(제외 해제·확정)
	Status *string `json:"status"`
	// RememberRule이 true면 "이 가맹점은 항상 이 분류" 규칙을 저장하고, 같은 가맹점의 확인 필요 건도 함께 분류한다.
	RememberRule bool `json:"remember_rule"`
}

// EditResult는 수정 결과다.
type EditResult struct {
	Transaction dbq.Transaction `json:"transaction"`
	RuleID      *int64          `json:"rule_id,omitempty"`
	AlsoUpdated int             `json:"also_updated"` // 학습 규칙으로 함께 분류된 확인 필요 건수
}

// Editor는 사용자 수정을 처리한다.
type Editor struct {
	Pool *pgxpool.Pool
}

// EditTransaction은 거래를 수정한다. 수정한 거래는 manually_edited가 되어 재처리에서 보존된다.
func (ed *Editor) EditTransaction(ctx context.Context, id int64, in Edit) (EditResult, error) {
	var out EditResult
	err := db.InTx(ctx, ed.Pool, func(tx pgx.Tx) error {
		q := dbq.New(tx)
		if err := q.AcquireProcessingLock(ctx); err != nil {
			return err
		}
		cur, err := q.GetTransactionForUpdate(ctx, id)
		if err != nil {
			return err
		}
		e, err := loadEnv(ctx, q)
		if err != nil {
			return err
		}
		upd := updateParamsFrom(cur)
		upd.ManuallyEdited = true

		if in.Memo != nil {
			upd.Memo = strings.TrimSpace(*in.Memo)
		}
		if in.Merchant != nil {
			upd.Merchant = strings.TrimSpace(*in.Merchant)
		}
		if in.Amount != nil {
			if *in.Amount <= 0 {
				return invalid("금액은 1원 이상이어야 합니다")
			}
			upd.Amount = *in.Amount
		}
		if in.OccurredAt != nil {
			upd.OccurredAt = *in.OccurredAt
		}
		if in.CategoryID != nil {
			c, ok := e.cats[*in.CategoryID]
			if !ok {
				return invalid("없는 분류입니다")
			}
			cid := c.ID
			upd.CategoryID = &cid
			upd.Direction = DirectionFor(c)
			if upd.Status == "needs_review" {
				upd.Status = "confirmed"
			}
			upd.ReviewReason = nil
			if c.Code == nil || *c.Code != "settlement" {
				upd.SettleCategoryID = nil
			}
		}
		if in.SettleCategoryID != nil {
			if *in.SettleCategoryID == 0 {
				upd.SettleCategoryID = nil
			} else {
				c, ok := e.cats[*in.SettleCategoryID]
				if !ok || c.Kind != "expense" {
					return invalid("정산은 지출 분류로만 돌려놓을 수 있습니다")
				}
				sid := c.ID
				upd.SettleCategoryID = &sid
			}
		}
		if in.Status != nil {
			switch *in.Status {
			case "excluded":
				upd.Status, upd.ExclusionReason = "excluded", reason(ExcludeUser)
			case "confirmed":
				if upd.CategoryID == nil {
					return invalid("분류를 먼저 정해야 확정할 수 있습니다")
				}
				upd.Status, upd.ExclusionReason, upd.ReviewReason = "confirmed", nil, nil
			default:
				return invalid("status는 excluded 또는 confirmed만 가능합니다")
			}
		}

		saved, err := q.UpdateTransaction(ctx, upd)
		if err != nil {
			return err
		}
		out.Transaction = saved

		if in.RememberRule {
			if saved.CategoryID == nil || strings.TrimSpace(saved.Merchant) == "" {
				return invalid("규칙을 저장하려면 가맹점과 분류가 있어야 합니다")
			}
			flow := saved.Flow
			rule, err := q.UpsertRule(ctx, dbq.UpsertRuleParams{
				Pattern: saved.Merchant, MatchType: "exact", CategoryID: *saved.CategoryID,
				Flow: &flow, Priority: 100, CreatedFrom: "learned",
			})
			if err != nil {
				return err
			}
			out.RuleID = &rule.ID
			n, err := applyRuleToReviewQueue(ctx, q, e, rule, saved.ID)
			if err != nil {
				return err
			}
			out.AlsoUpdated = n
		}
		return nil
	})
	return out, err
}

// applyRuleToReviewQueue는 새 규칙을 같은 가맹점의 확인 필요 건에도 적용한다.
func applyRuleToReviewQueue(ctx context.Context, q *dbq.Queries, e *env, rule dbq.MerchantRule, skipID int64) (int, error) {
	pending, err := q.ListNeedsReview(ctx, 1000)
	if err != nil {
		return 0, err
	}
	flow := ""
	if rule.Flow != nil {
		flow = *rule.Flow
	}
	r := classify.Rule{ID: rule.ID, Pattern: rule.Pattern, MatchType: rule.MatchType, CategoryID: rule.CategoryID, Flow: flow, CreatedFrom: rule.CreatedFrom}
	c, ok := e.cats[rule.CategoryID]
	if !ok {
		return 0, nil
	}
	n := 0
	for _, p := range pending {
		if p.ID == skipID || p.CancelsID != nil || parser.Kind(p.Kind) == parser.KindUnknown || !r.Matches(p.Merchant, p.Flow) {
			continue
		}
		t, err := q.GetTransactionForUpdate(ctx, p.ID)
		if err != nil {
			return n, err
		}
		upd := updateParamsFrom(t)
		cid := c.ID
		upd.CategoryID, upd.Direction, upd.Status, upd.ReviewReason = &cid, DirectionFor(c), "confirmed", nil
		if _, err := q.UpdateTransaction(ctx, upd); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

// Manual은 현금 등 수동 입력이다.
type Manual struct {
	OccurredAt *time.Time `json:"occurred_at"`
	Amount     int64      `json:"amount"`
	Merchant   string     `json:"merchant"`
	Memo       string     `json:"memo"`
	CategoryID int64      `json:"category_id"`
	// Flow: "out"(기본, 지출) 또는 "in"(현금 수입 등)
	Flow string `json:"flow"`
}

// CreateManual은 수동 거래를 만든다.
func (ed *Editor) CreateManual(ctx context.Context, in Manual, now time.Time) (dbq.Transaction, error) {
	if in.Amount <= 0 {
		return dbq.Transaction{}, invalid("금액은 1원 이상이어야 합니다")
	}
	q := dbq.New(ed.Pool)
	c, err := q.GetCategory(ctx, in.CategoryID)
	if errors.Is(err, pgx.ErrNoRows) {
		return dbq.Transaction{}, invalid("없는 분류입니다")
	}
	if err != nil {
		return dbq.Transaction{}, err
	}
	flow := in.Flow
	if flow == "" {
		flow = "out"
		if c.Kind == "income" {
			flow = "in"
		}
	}
	if flow != "out" && flow != "in" {
		return dbq.Transaction{}, invalid("flow는 out 또는 in")
	}
	at := now
	if in.OccurredAt != nil {
		at = *in.OccurredAt
	}
	cid := c.ID
	return q.InsertTransaction(ctx, dbq.InsertTransactionParams{
		OccurredAt: at, Amount: in.Amount, Flow: flow, Merchant: strings.TrimSpace(in.Merchant), Memo: strings.TrimSpace(in.Memo),
		Direction: DirectionFor(c), CategoryID: &cid, Status: "confirmed", Source: "manual", Kind: "manual", ManuallyEdited: true,
	})
}
