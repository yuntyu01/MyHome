// Package anonymize는 알림 원문에서 개인정보(이름, 카드·계좌번호, 잔액, 전화번호)를 가린다.
// 파서 보정용 픽스처를 만들 때 쓴다. 가게 이름과 거래 금액은 해석에 필요해서 남긴다.
package anonymize

import (
	"regexp"
	"strings"
	"time"

	"github.com/yuntyu01/MyHome/server/internal/parser"
)

var (
	phoneRe    = regexp.MustCompile(`01[016789][-\s]?[0-9]{3,4}[-\s]?[0-9]{4}`)
	accountRe  = regexp.MustCompile(`[0-9]{2,6}-[0-9]{2,6}-[0-9]{2,8}(?:-[0-9]{1,6})?`)
	maskedCard = regexp.MustCompile(`[0-9]{0,4}\*+[0-9*]*[0-9]+[0-9*]*|[0-9]+\*{2,}[0-9*]*`)
	longDigits = regexp.MustCompile(`[0-9]{8,}`)
	balanceRe  = regexp.MustCompile(`(잔액|잔고|누적|이용가능|가용)(\s*[:：]?\s*(?:[₩￦]\s?)?)[0-9,]+(\s*원)?`)
	maskedName = regexp.MustCompile(`[가-힣]\*[가-힣*]{0,2}`)
	nameSirRe  = regexp.MustCompile(`([가-힣]{2,4})(님)`)
)

// Text는 문자열에서 개인정보를 가린다. extraNames는 함께 가릴 이름(송금 상대 등)이다.
func Text(s string, extraNames ...string) string {
	for _, n := range extraNames {
		if n = strings.TrimSpace(n); len([]rune(n)) >= 2 {
			s = strings.ReplaceAll(s, n, "[상대방]")
		}
	}
	s = phoneRe.ReplaceAllString(s, "[전화]")
	s = accountRe.ReplaceAllString(s, "[계좌]")
	s = balanceRe.ReplaceAllString(s, "$1$2[금액]")
	s = maskedName.ReplaceAllString(s, "○*○")
	s = maskedCard.ReplaceAllString(s, "[카드]")
	s = longDigits.ReplaceAllString(s, "[번호]")
	s = nameSirRe.ReplaceAllString(s, "○○○$2")
	return s
}

// Record는 내보내는 한 줄(JSONL)이다. 픽스처 형식과 비슷하게 맞췄다.
type Record struct {
	ID         int64   `json:"id"`
	Package    string  `json:"package"`
	Title      string  `json:"title"`
	Text       string  `json:"text"`
	BigText    string  `json:"big_text"`
	PostedAt   string  `json:"posted_at"`
	ParseError *string `json:"parse_error,omitempty"`
	// 현재 파서가 본 결과(보정할 때 비교용)
	Parsed *ParsedView `json:"parsed,omitempty"`
}

// ParsedView는 현재 파서 결과 요약이다.
type ParsedView struct {
	Source   string `json:"source"`
	Kind     string `json:"kind"`
	Amount   int64  `json:"amount"`
	Merchant string `json:"merchant"`
}

// Export는 원문 하나를 익명화한 레코드로 만든다.
// 송금·입금이면 파서가 뽑은 상대 이름도 가린다(사람 이름일 가능성이 높아서).
func Export(id int64, ev parser.Event, parseErr *string, reg *parser.Registry) Record {
	var names []string
	var view *ParsedView
	if res, err := reg.Parse(ev); err == nil {
		merchant := res.Merchant
		if res.Kind == parser.KindTransferOut || res.Kind == parser.KindDeposit {
			names = append(names, res.Merchant)
			merchant = "[상대방]"
		}
		view = &ParsedView{Source: res.Source, Kind: string(res.Kind), Amount: res.Amount, Merchant: Text(merchant)}
	}
	return Record{
		ID: id, Package: ev.Package,
		Title: Text(ev.Title, names...), Text: Text(ev.Text, names...), BigText: Text(ev.BigText, names...),
		PostedAt: ev.PostedAt.Format(time.RFC3339), ParseError: parseErr, Parsed: view,
	}
}
