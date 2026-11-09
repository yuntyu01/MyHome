package pipeline

import (
	"context"
	"strings"

	"github.com/yuntyu01/MyHome/server/internal/classify"
	"github.com/yuntyu01/MyHome/server/internal/dbq"
	"github.com/yuntyu01/MyHome/server/internal/parser"
)

// 확인 필요 사유
const (
	ReviewNoMerchant     = "no_merchant"      // 가맹점 이름을 못 뽑음
	ReviewNoRule         = "no_rule"          // 맞는 규칙 없음
	ReviewPaymentGateway = "payment_gateway"  // 결제대행사 이름으로 찍힘
	ReviewTransfer       = "transfer"         // 송금(친구 송금은 자동 판단 안 함)
	ReviewDeposit        = "deposit"          // 입금 성격(수입/정산/내 계좌) 미확인
	ReviewUnknownKind    = "unknown_kind"     // 승인/입금 등 종류를 모름
	ReviewCancelNoMatch  = "cancel_unmatched" // 취소의 원거래를 못 찾음
	ReviewCancelPartial  = "cancel_partial"   // 부분 취소인데 원거래가 미분류
	ReviewForeign        = "foreign_currency" // 외화만 있어서 원화 금액을 모름(메모에 외화 금액)
)

// 제외 사유
const (
	ExcludeCardPayment  = "card_payment"
	ExcludePrepaid      = "prepaid_charge"
	ExcludeDuplicate    = "duplicate"
	ExcludeCancelled    = "cancelled"    // 전액 취소된 원거래
	ExcludeCancellation = "cancellation" // 원거래를 상계한 취소 건
	ExcludeUser         = "user"         // 사용자가 제외
)

// env는 한 번의 처리에 필요한 설정·규칙·분류 스냅샷이다.
type env struct {
	cfg    Config
	rules  []classify.Rule
	cats   map[int64]dbq.Category
	byCode map[string]dbq.Category
}

func loadEnv(ctx context.Context, q *dbq.Queries) (*env, error) {
	cfg, err := LoadConfig(ctx, q)
	if err != nil {
		return nil, err
	}
	rows, err := q.ListRules(ctx)
	if err != nil {
		return nil, err
	}
	rules := make([]classify.Rule, 0, len(rows))
	for _, r := range rows {
		flow := ""
		if r.Flow != nil {
			flow = *r.Flow
		}
		rules = append(rules, classify.Rule{ID: r.ID, Pattern: r.Pattern, MatchType: r.MatchType, CategoryID: r.CategoryID, Flow: flow, Priority: int(r.Priority), CreatedFrom: r.CreatedFrom})
	}
	classify.Sort(rules)
	cats, err := q.ListCategories(ctx)
	if err != nil {
		return nil, err
	}
	e := &env{cfg: cfg, rules: rules, cats: map[int64]dbq.Category{}, byCode: map[string]dbq.Category{}}
	for _, c := range cats {
		e.cats[c.ID] = c
		if c.Code != nil {
			e.byCode[*c.Code] = c
		}
	}
	return e, nil
}

// DirectionFor는 분류에 맞는 회계상 방향이다.
func DirectionFor(c dbq.Category) string {
	switch c.Kind {
	case "expense":
		return "expense"
	case "income":
		return "income"
	}
	if c.Code != nil && *c.Code == "settlement" {
		return "income"
	}
	return "transfer"
}

// classification은 분류 결과다.
type classification struct {
	categoryID   *int64
	direction    string
	status       string
	reviewReason *string
}

func reason(s string) *string { return &s }

func defaultDirection(kind parser.Kind, flow string) string {
	if kind == parser.KindDeposit || (kind == parser.KindUnknown && flow == "in") {
		return "income"
	}
	return "expense"
}

// classify는 가맹점 규칙으로 분류한다(파이프라인 5·6단계).
func (e *env) classify(merchant string, kind parser.Kind, flow string) classification {
	review := func(r string) classification {
		return classification{direction: defaultDirection(kind, flow), status: "needs_review", reviewReason: reason(r)}
	}
	apply := func(r classify.Rule) classification {
		c, ok := e.cats[r.CategoryID]
		if !ok {
			return review(ReviewNoRule)
		}
		id := c.ID
		cl := classification{categoryID: &id, direction: DirectionFor(c), status: "confirmed"}
		if kind == parser.KindUnknown {
			// 종류를 모르면 분류는 제안만 하고 확인을 받는다.
			cl.status, cl.reviewReason = "needs_review", reason(ReviewUnknownKind)
		}
		return cl
	}

	// 숨긴(지운) 분류로 가는 규칙은 건너뛴다. 그 가맹점은 다른 규칙이 없으면 확인 필요(AI 추천 대상)로 간다.
	live := func(r classify.Rule) bool {
		c, ok := e.cats[r.CategoryID]
		return ok && !c.Archived
	}
	and := func(f func(classify.Rule) bool) func(classify.Rule) bool {
		return func(r classify.Rule) bool { return live(r) && f(r) }
	}

	if strings.TrimSpace(merchant) == "" {
		return review(ReviewNoMerchant)
	}
	// 친구 송금은 자동 판단하지 않는다. 사용자가 직접 만든 정확 일치 규칙만 적용.
	if kind == parser.KindTransferOut {
		if r, ok := classify.First(e.rules, merchant, flow, and(func(r classify.Rule) bool { return r.IsUserRule() && r.MatchType == "exact" })); ok {
			return apply(r)
		}
		return review(ReviewTransfer)
	}
	// 사용자 규칙(수동·학습)이 가장 먼저.
	if r, ok := classify.First(e.rules, merchant, flow, and(classify.Rule.IsUserRule)); ok {
		return apply(r)
	}
	// 결제대행사 이름이면 떼어낸 나머지로 분류하고, 남는 게 없으면 확인 필요.
	target := merchant
	if rest, gw := classify.StripGateway(merchant, e.cfg.ReviewMerchantPatterns); gw {
		if rest == "" {
			return review(ReviewPaymentGateway)
		}
		target = rest
	}
	if r, ok := classify.First(e.rules, target, flow, live); ok {
		return apply(r)
	}
	if flow == "in" {
		return review(ReviewDeposit)
	}
	if kind == parser.KindUnknown {
		return review(ReviewUnknownKind)
	}
	return review(ReviewNoRule)
}

func containsAnyNorm(body string, words []string) bool {
	nb := classify.Normalize(body)
	for _, w := range words {
		if nw := classify.Normalize(w); nw != "" && strings.Contains(nb, nw) {
			return true
		}
	}
	return false
}

func kindIn(k parser.Kind, ks ...parser.Kind) bool {
	for _, x := range ks {
		if k == x {
			return true
		}
	}
	return false
}

// isCardPayment: 카드대금 출금. 승인 알림에는 카드사 이름이 늘 있으므로 출금·송금류에만 적용한다.
func (e *env) isCardPayment(k parser.Kind, body string) bool {
	if kindIn(k, parser.KindWithdrawal, parser.KindTransferOut, parser.KindUnknown) && containsAnyNorm(body, e.cfg.CardPaymentKeywords) {
		return true
	}
	return kindIn(k, parser.KindWithdrawal, parser.KindTransferOut) && containsAnyNorm(body, e.cfg.CardCompanyNames)
}

// ownAccount: 내 계좌 간 이체. 입출금·송금류에만 적용한다.
func (e *env) ownAccount(k parser.Kind, body string) (dbq.Category, bool) {
	if !kindIn(k, parser.KindDeposit, parser.KindWithdrawal, parser.KindTransferOut, parser.KindUnknown) {
		return dbq.Category{}, false
	}
	for _, o := range e.cfg.OwnAccounts {
		if containsAnyNorm(body, []string{o.Pattern}) {
			if c, ok := e.byCode[o.Target]; ok {
				return c, true
			}
		}
	}
	return dbq.Category{}, false
}

// isPrepaidCharge: 충전식 머니 충전(실제 결제 때 기록되므로 제외). 티머니 같은 교통카드 충전은 해당 안 됨.
func (e *env) isPrepaidCharge(k parser.Kind, body string) bool {
	return k == parser.KindCharge && containsAnyNorm(body, e.cfg.PrepaidKeywords)
}
