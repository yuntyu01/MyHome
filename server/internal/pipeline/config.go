// Package pipeline은 알림 원문을 거래로 만드는 처리 파이프라인이다.
//
// 순서: 파싱 → 예외 규칙(카드대금·내 계좌 이체·머니 충전) → 중복 합치기 → 취소 상계 → 규칙 분류 → 확인 필요.
package pipeline

import (
	"context"
	"encoding/json"
	"time"
	"unicode/utf8"

	"github.com/yuntyu01/MyHome/server/internal/dbq"
)

// OwnAccount는 내 계좌로 볼 상대 이름과 집계 분류(own_transfer 또는 investment)다.
type OwnAccount struct {
	Pattern string `json:"pattern"`
	Target  string `json:"target"`
}

// Config는 settings 테이블에서 읽은 처리 설정이다.
type Config struct {
	BudgetStartDay         int
	DedupeWindow           time.Duration
	OwnAccounts            []OwnAccount
	CardPaymentKeywords    []string
	CardCompanyNames       []string
	PrepaidKeywords        []string
	ReviewMerchantPatterns []string
	// AIMemo는 AI 분류가 모든 가맹점에 먼저 적용할 사용자 공통 메모다(분류마다 다는 설명과 따로, 2026-11-10)
	AIMemo string
}

// DefaultConfig는 settings에 값이 없을 때 쓰는 기본값이다(마이그레이션 seed와 같다).
func DefaultConfig() Config {
	return Config{
		BudgetStartDay:         1,
		DedupeWindow:           180 * time.Second,
		OwnAccounts:            []OwnAccount{{Pattern: "토스증권", Target: "investment"}},
		CardPaymentKeywords:    []string{"카드대금", "카드 대금", "결제대금", "카드값"},
		CardCompanyNames:       []string{"KB국민카드", "국민카드", "삼성카드", "신한카드", "현대카드", "롯데카드", "하나카드", "우리카드", "BC카드", "비씨카드", "NH카드", "농협카드"},
		PrepaidKeywords:        []string{"카카오페이머니", "페이머니", "쿠팡캐시", "쿠페이머니", "토스머니", "네이버페이머니", "네이버페이 포인트"},
		ReviewMerchantPatterns: []string{"KG이니시스", "이니시스", "토스페이먼츠", "NHN KCP", "KCP", "나이스페이", "나이스정보통신", "다날", "KICC", "한국정보통신", "스마트로", "페이레터", "카카오페이", "네이버페이", "토스페이", "페이코", "PAYCO"},
	}
}

// SettingKeys는 알려진 설정 키다. API는 이 키만 바꿀 수 있다.
var SettingKeys = map[string]bool{
	"budget_start_day": true, "dedupe_window_seconds": true, "own_accounts": true,
	"card_payment_keywords": true, "card_company_names": true, "prepaid_keywords": true,
	"review_merchant_patterns": true, "ai_classify_memo": true,
}

// AIMemoMax는 AI 공통 메모 길이 상한(글자)이다. 분류 설명(300자)보다 넉넉하게.
const AIMemoMax = 1000

// LoadConfig는 settings 테이블을 읽는다. 값이 깨져 있으면 그 항목만 기본값을 쓴다.
func LoadConfig(ctx context.Context, q *dbq.Queries) (Config, error) {
	cfg := DefaultConfig()
	rows, err := q.ListSettings(ctx)
	if err != nil {
		return cfg, err
	}
	for _, r := range rows {
		switch r.Key {
		case "budget_start_day":
			var n int
			if json.Unmarshal(r.Value, &n) == nil && n >= 1 && n <= 28 {
				cfg.BudgetStartDay = n
			}
		case "dedupe_window_seconds":
			var n int
			if json.Unmarshal(r.Value, &n) == nil && n >= 0 && n <= 3600 {
				cfg.DedupeWindow = time.Duration(n) * time.Second
			}
		case "own_accounts":
			var v []OwnAccount
			if json.Unmarshal(r.Value, &v) == nil {
				cfg.OwnAccounts = v
			}
		case "card_payment_keywords":
			unmarshalList(r.Value, &cfg.CardPaymentKeywords)
		case "card_company_names":
			unmarshalList(r.Value, &cfg.CardCompanyNames)
		case "prepaid_keywords":
			unmarshalList(r.Value, &cfg.PrepaidKeywords)
		case "review_merchant_patterns":
			unmarshalList(r.Value, &cfg.ReviewMerchantPatterns)
		case "ai_classify_memo":
			var v string
			if json.Unmarshal(r.Value, &v) == nil {
				cfg.AIMemo = v
			}
		}
	}
	return cfg, nil
}

func unmarshalList(b []byte, dst *[]string) {
	var v []string
	if json.Unmarshal(b, &v) == nil {
		*dst = v
	}
}

// ValidateSetting은 API로 들어온 설정 값을 검사한다.
func ValidateSetting(key string, raw json.RawMessage) bool {
	switch key {
	case "budget_start_day":
		var n int
		return json.Unmarshal(raw, &n) == nil && n >= 1 && n <= 28
	case "dedupe_window_seconds":
		var n int
		return json.Unmarshal(raw, &n) == nil && n >= 0 && n <= 3600
	case "own_accounts":
		var v []OwnAccount
		if json.Unmarshal(raw, &v) != nil {
			return false
		}
		for _, o := range v {
			if o.Pattern == "" || (o.Target != "own_transfer" && o.Target != "investment") {
				return false
			}
		}
		return true
	case "card_payment_keywords", "card_company_names", "prepaid_keywords", "review_merchant_patterns":
		var v []string
		return json.Unmarshal(raw, &v) == nil
	case "ai_classify_memo":
		var v string
		return json.Unmarshal(raw, &v) == nil && utf8.RuneCountInString(v) <= AIMemoMax
	}
	return false
}
