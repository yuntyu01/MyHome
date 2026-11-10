// Package suggest는 규칙으로 분류하지 못한 지출에 AI 분류 추천을 붙인다(사용자 결정, 2026-11-08).
//
// 순서: 사용자 규칙(기존 파이프라인) → 규칙이 없으면(no_rule) AI에 묻는다 → 확신이 AutoConfidence 이상이면 그 분류로 바로 넣고
// (사용자 요청, 2026-11-10), MinConfidence~AutoConfidence면 확인 필요에 추천만 붙인다(한 번 눌러 확정 → "이 가맹점은 항상" 학습 규칙).
// AI에는 가맹점 이름과 지출 분류 이름만 보낸다(금액·시각·알림 원문·카드·계좌 정보는 보내지 않는다).
// 답은 가맹점마다 merchant_suggestions에 한 번 저장하고, 같은 가맹점은 다시 묻지 않는다.
package suggest

import (
	"context"
	"strings"
	"unicode/utf8"

	"github.com/yuntyu01/MyHome/server/internal/classify"
	"github.com/yuntyu01/MyHome/server/internal/dbq"
	"github.com/yuntyu01/MyHome/server/internal/pipeline"
)

// MinConfidence보다 낮은 추천은 저장만 하고 보여주지 않는다. 0.6은 "대체로 맞는" 정도로 잡은 값이다:
// 틀린 추천을 한 번 눌러 버리면 잘못된 학습 규칙이 생기므로, 애매한 것은 숨기고 사용자가 직접 고르게 한다.
// 실사용에서 너무 적게 나오거나 많이 틀리면 이 값만 바꾼다(저장된 값은 그대로라 바로 반영된다).
const MinConfidence = 0.6

// AutoConfidence 이상이면 확인 필요에 두지 않고 그 분류로 바로 넣는다(Worker.Auto). 0.8은 "거의 확실" 정도로 잡은 값이다:
// 가게 이름만으로 분명한 것(커피·편의점·분식)은 넣고, 애매한 것은 추천으로 남겨 사용자가 고르게 한다.
const AutoConfidence = 0.8

// maxNameRunes는 AI에 보내는 가맹점 이름 길이 상한이다(파서가 긴 문장을 가맹점으로 잘못 뽑은 경우에도 프롬프트가 커지지 않게).
const maxNameRunes = 60

// Suggestion은 거래 JSON의 "suggestion" 값이다. 없으면 null.
type Suggestion struct {
	CategoryID int64   `json:"category_id"`
	Category   string  `json:"category"`
	Confidence float64 `json:"confidence"`
}

// Target은 AI에 보낼 가맹점 이름과 캐시 키를 만든다. 결제대행사 이름(review_merchant_patterns)은 파이프라인 분류와 같이 떼어낸다
// ("카카오페이_동네분식" → "동네분식"). 키는 classify.Normalize(공백 제거·소문자·법인 표기 제거)다. 이름이 없으면 ok=false.
func Target(merchant string, gateways []string) (name, key string, ok bool) {
	m := strings.TrimSpace(merchant)
	if m == "" {
		return "", "", false
	}
	if rest, gw := classify.StripGateway(m, gateways); gw {
		if rest == "" {
			return "", "", false
		}
		m = rest
	}
	key = classify.Normalize(m)
	if key == "" {
		return "", "", false
	}
	if utf8.RuneCountInString(m) > maxNameRunes {
		m = string([]rune(m)[:maxNameRunes])
	}
	return m, key, true
}

// Input은 추천을 찾을 거래의 필요한 값이다.
type Input struct {
	Status       string
	ReviewReason *string
	Direction    string
	Flow         string
	Merchant     string
	CancelsID    *int64
}

// Eligible은 AI에 물어볼(그리고 추천을 보여줄) 거래인지 본다: 규칙이 없어서(no_rule) 확인 필요로 온, 가맹점 이름이 있는 나간 지출.
// 결제대행사 이름뿐인 건(payment_gateway)·송금(transfer)·입금·종류 모름·취소는 AI도 알 수 없어서 그대로 확인 필요로 둔다.
// 이 조건은 queries/suggestions.sql의 ListSuggestionMerchants와 같다.
func Eligible(in Input) bool {
	return in.Status == "needs_review" && in.ReviewReason != nil && *in.ReviewReason == pipeline.ReviewNoRule &&
		in.Direction == "expense" && in.Flow == "out" && in.CancelsID == nil && strings.TrimSpace(in.Merchant) != ""
}

// visible은 저장된 답을 화면에 보여줄 추천으로 바꾼다. "모름", 낮은 확신, 보관(삭제)된 분류, 지출이 아닌 분류는 nil.
func visible(r dbq.ListMerchantSuggestionsRow) *Suggestion {
	if r.CategoryID == nil || r.CategoryName == nil || r.CategoryArchived == nil || *r.CategoryArchived ||
		r.CategoryKind == nil || *r.CategoryKind != "expense" || r.Confidence < MinConfidence {
		return nil
	}
	return &Suggestion{CategoryID: *r.CategoryID, Category: *r.CategoryName, Confidence: r.Confidence}
}

// Lookup은 거래마다 보여줄 추천을 찾는다(in과 같은 순서, 없으면 nil). 대상 거래가 없으면 DB를 읽지 않는다.
func Lookup(ctx context.Context, q *dbq.Queries, in []Input) ([]*Suggestion, error) {
	out := make([]*Suggestion, len(in))
	need := false
	for _, t := range in {
		if Eligible(t) {
			need = true
			break
		}
	}
	if !need {
		return out, nil
	}
	cfg, err := pipeline.LoadConfig(ctx, q)
	if err != nil {
		return out, err
	}
	keys := make([]string, len(in))
	var uniq []string
	seen := map[string]bool{}
	for i, t := range in {
		if !Eligible(t) {
			continue
		}
		if _, k, ok := Target(t.Merchant, cfg.ReviewMerchantPatterns); ok {
			keys[i] = k
			if !seen[k] {
				seen[k] = true
				uniq = append(uniq, k)
			}
		}
	}
	if len(uniq) == 0 {
		return out, nil
	}
	rows, err := q.ListMerchantSuggestions(ctx, uniq)
	if err != nil {
		return out, err
	}
	byKey := make(map[string]*Suggestion, len(rows))
	for _, r := range rows {
		byKey[r.MerchantKey] = visible(r)
	}
	for i, k := range keys {
		if k != "" {
			out[i] = byKey[k]
		}
	}
	return out, nil
}
