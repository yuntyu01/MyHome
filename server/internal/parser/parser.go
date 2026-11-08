// Package parser는 알림 원문에서 금액·가맹점·종류를 뽑는다.
//
// 소스별 플러그인 구조다: 패키지명 → 파서. 패키지 매핑이 없으면 내용으로 파서를 고르고,
// 그래도 없으면 범용 파서를 쓴다. 실제 알림 원문이 아직 없어서 KB·토스 파서는 잠정 버전이다.
// 실데이터로 보정하는 절차는 docs/PARSERS.md.
package parser

import (
	"errors"
	"strings"
	"time"
)

// Kind는 알림이 나타내는 돈의 움직임 종류다.
type Kind string

const (
	KindApproval    Kind = "approval"     // 카드·간편결제 승인 (나감)
	KindCancel      Kind = "cancel"       // 결제 취소·환불 (들어옴)
	KindDeposit     Kind = "deposit"      // 입금 (들어옴)
	KindWithdrawal  Kind = "withdrawal"   // 출금·자동이체 (나감)
	KindTransferOut Kind = "transfer_out" // 송금 (나감)
	KindCharge      Kind = "charge"       // 충전 (나감)
	KindUnknown     Kind = "unknown"
)

// Flow는 Kind의 기본 돈 방향("out" 또는 "in")이다.
func (k Kind) Flow() string {
	switch k {
	case KindCancel, KindDeposit:
		return "in"
	default:
		return "out"
	}
}

// Event는 해석할 알림 원문이다.
type Event struct {
	Package  string
	Title    string
	Text     string
	BigText  string
	PostedAt time.Time
}

// Body는 제목과 본문을 합친 해석 대상 문자열이다. big_text가 text를 포함하면 중복을 피한다.
func (e Event) Body() string {
	parts := []string{strings.TrimSpace(e.Title)}
	text, big := strings.TrimSpace(e.Text), strings.TrimSpace(e.BigText)
	switch {
	case big != "" && strings.Contains(big, text):
		parts = append(parts, big)
	default:
		parts = append(parts, text, big)
	}
	var out []string
	for _, p := range parts {
		if p != "" {
			out = append(out, p)
		}
	}
	return strings.Join(out, "\n")
}

// Result는 해석 결과다.
type Result struct {
	Source      string // kb, toss, generic, 또는 매핑에서 정한 이름
	Kind        Kind
	Amount      int64 // 원 단위, 0이면 못 찾음
	Merchant    string
	OccurredAt  time.Time
	Balance     *int64
	Installment int // 할부 개월 수(일시불 0)
	// Foreign은 원화 금액 없이 외화로만 온 승인·취소의 금액 표시(예: "USD 12.34")다. 이때 Amount는 0이다.
	Foreign string
}

// ErrNoAmount는 금액을 찾지 못했을 때 반환한다. 원문은 보존되고 parse_error로 남는다.
var ErrNoAmount = errors.New("금액을 찾지 못함")

// Parser는 소스별 해석기다.
type Parser interface {
	Name() string
	// Detect는 패키지 매핑이 없을 때 내용으로 이 파서가 맞는지 판단한다.
	Detect(ev Event) bool
	Parse(ev Event) (Result, error)
}
