package parser

import (
	"regexp"
	"strconv"
	"strings"
	"time"
)

var kst = mustLoc("Asia/Seoul")

func mustLoc(name string) *time.Location {
	loc, err := time.LoadLocation(name)
	if err != nil {
		panic(err)
	}
	return loc
}

// ---- 금액 ----

var amountRe = regexp.MustCompile(`([0-9]{1,3}(?:,[0-9]{3})+|[0-9]+)\s*원`)

type amountHit struct {
	value      int64
	start, end int
	label      string // 잔액, 누적 등 금액 앞의 표시. 비어 있으면 거래 금액 후보
}

// 금액 앞에 이런 말이 있으면 거래 금액이 아니다.
var amountLabels = []string{"잔액", "잔고", "누적", "한도", "포인트", "적립", "남은", "이용가능", "가용", "합계"}

func findAmounts(s string) []amountHit {
	var hits []amountHit
	for _, m := range amountRe.FindAllStringSubmatchIndex(s, -1) {
		num := strings.ReplaceAll(s[m[2]:m[3]], ",", "")
		v, err := strconv.ParseInt(num, 10, 64)
		if err != nil || v <= 0 {
			continue
		}
		before := lastRunes(s[:m[0]], 8)
		label := ""
		for _, l := range amountLabels {
			if strings.Contains(before, l) {
				label = l
				break
			}
		}
		hits = append(hits, amountHit{value: v, start: m[0], end: m[1], label: label})
	}
	return hits
}

func lastRunes(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		r = r[len(r)-n:]
	}
	return string(r)
}

// mainAmount는 잔액·누적 같은 표시가 없는 첫 금액이다.
func mainAmount(hits []amountHit) (int64, bool) {
	for _, h := range hits {
		if h.label == "" {
			return h.value, true
		}
	}
	return 0, false
}

func balanceAmount(hits []amountHit) *int64 {
	for _, h := range hits {
		if h.label == "잔액" || h.label == "잔고" {
			v := h.value
			return &v
		}
	}
	return nil
}

// ---- 종류 ----

// detectKind는 일반적인 문구로 종류를 판단한다. 순서가 중요하다(취소 > 충전 > 입금 > 송금 > 출금 > 승인).
func detectKind(body string) Kind {
	switch {
	case containsAny(body, "취소", "환불"):
		return KindCancel
	case containsAny(body, "충전"):
		return KindCharge
	case containsAny(body, "입금", "받았", "들어왔") || (strings.Contains(body, "님이") && strings.Contains(body, "보냈")):
		return KindDeposit
	case containsAny(body, "송금", "보냈"):
		return KindTransferOut
	case containsAny(body, "출금", "자동이체", "납부", "이체", "옮겼"):
		return KindWithdrawal
	case containsAny(body, "승인", "결제", "사용", "일시불", "할부", "구매"):
		return KindApproval
	}
	return KindUnknown
}

func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

// ---- 할부 ----

var (
	installmentRe  = regexp.MustCompile(`할부\s*\(?\s*([0-9]{1,2})\s*개월`)
	installmentRe2 = regexp.MustCompile(`([0-9]{1,2})\s*개월\s*할부`)
)

func installment(body string) int {
	for _, re := range []*regexp.Regexp{installmentRe, installmentRe2} {
		if m := re.FindStringSubmatch(body); m != nil {
			n, _ := strconv.Atoi(m[1])
			return n
		}
	}
	return 0
}

// ---- 시각 ----

var (
	fullDateTimeRe = regexp.MustCompile(`(20[0-9]{2})[./-]([0-9]{1,2})[./-]([0-9]{1,2})\.?\s+([0-9]{1,2}):([0-9]{2})`)
	monthDayTimeRe = regexp.MustCompile(`(?:^|[^0-9])([0-9]{1,2})[/.]([0-9]{1,2})\s+([0-9]{1,2}):([0-9]{2})`)
)

// occurredAt은 본문의 날짜·시각을 쓰되, 알림 게시 시각보다 3일 넘게 이르거나 늦으면 게시 시각을 쓴다.
// 월말 자정 근처 결제가 다음 달로 잘못 들어가지 않게 본문 시각을 우선한다.
func occurredAt(body string, posted time.Time) time.Time {
	p := posted.In(kst)
	var t time.Time
	if m := fullDateTimeRe.FindStringSubmatch(body); m != nil {
		t = mkTime(atoi(m[1]), atoi(m[2]), atoi(m[3]), atoi(m[4]), atoi(m[5]))
	} else if m := monthDayTimeRe.FindStringSubmatch(body); m != nil {
		t = mkTime(p.Year(), atoi(m[1]), atoi(m[2]), atoi(m[3]), atoi(m[4]))
		if !t.IsZero() && t.After(p.Add(24*time.Hour)) {
			t = t.AddDate(-1, 0, 0) // 1월 1일에 받은 12/31 결제
		}
	}
	if t.IsZero() || t.Before(posted.Add(-72*time.Hour)) || t.After(posted.Add(time.Hour)) {
		return posted
	}
	return t
}

func mkTime(y, mo, d, h, mi int) time.Time {
	if mo < 1 || mo > 12 || d < 1 || d > 31 || h > 23 || mi > 59 {
		return time.Time{}
	}
	t := time.Date(y, time.Month(mo), d, h, mi, 0, 0, kst)
	if t.Day() != d { // 2/30 같은 날짜
		return time.Time{}
	}
	return t
}

func atoi(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}

// ---- 가맹점 ----

var (
	labeledRe     = regexp.MustCompile(`(?:가맹점|사용처|결제처|받는\s?분|보낸\s?분|입금자|상대방?)\s*[:：]?\s*([^\n|·]+)`)
	atPlaceRe     = regexp.MustCompile(`(?m)(?:^|[|·])\s*([^\n|·]+?)에서\s`)
	toPersonRe    = regexp.MustCompile(`([^\s\n|·]+?)\s?님?에게`)
	fromPersonRe  = regexp.MustCompile(`([^\s\n|·]+?)\s?님(?:이|으로부터|께서)\s`)
	toAccountRe   = regexp.MustCompile(`([^\s\n|·]+?)(?:으로|로)\s+(?:[0-9,]+원[을를]?\s*)?(?:송금|이체|출금|보냈|옮겼)`)
	bracketTagRe  = regexp.MustCompile(`\[[^\]]*\]|\((?:Web발신|광고)\)`)
	maskedNameRe  = regexp.MustCompile(`[가-힣A-Za-z]*\*[가-힣A-Za-z*]*\s?님?`)
	cardNumRe     = regexp.MustCompile(`[0-9]{2,4}[*]+[0-9*]*|\([0-9*]{2,6}\)`)
	dateTimeRe    = regexp.MustCompile(`(20[0-9]{2}[./-])?[0-9]{1,2}[/.-][0-9]{1,2}\.?|[0-9]{1,2}:[0-9]{2}(:[0-9]{2})?`)
	installTokRe  = regexp.MustCompile(`일시불|할부\s*\(?\s*[0-9]{0,2}\s*개월\)?|[0-9]{1,2}\s*개월`)
	labelAmountRe = regexp.MustCompile(`(잔액|잔고|누적|한도|포인트|적립|남은|이용가능|합계)\s*[:：]?\s*[0-9,]+\s*원?`)
	spacesRe      = regexp.MustCompile(`\s+`)
)

// 가맹점 후보에서 지울 일반 단어. 길이가 긴 것부터 지운다.
var noiseWords = []string{
	"KB국민카드", "KB국민", "국민카드", "KB Pay", "KB페이", "토스뱅크", "토스페이로", "체크카드", "신용카드",
	"결제했어요", "결제됐어요", "결제되었습니다", "승인되었습니다", "결제완료", "승인완료", "승인취소", "결제취소",
	"입금됐어요", "입금되었습니다", "출금됐어요", "출금되었습니다", "보냈어요", "받았어요",
	"승인", "결제", "취소", "완료", "사용", "입금", "출금", "이체", "송금", "충전", "알림", "금액", "국내", "해외",
	"체크", "신용", "카드", "님", "원",
	// 문장 끝맺음(앞 단어를 지운 뒤 남는 조각)
	"되었습니다", "되었어요", "됐습니다", "됐어요", "했습니다", "했어요", "합니다", "입니다",
}

// guessMerchant는 일반적인 패턴으로 가맹점(또는 송금 상대)을 추측한다.
func guessMerchant(body string, kind Kind) string {
	if m := labeledRe.FindStringSubmatch(body); m != nil {
		if s := cleanMerchant(m[1]); s != "" {
			return s
		}
	}
	switch kind {
	case KindTransferOut, KindWithdrawal:
		if m := toPersonRe.FindStringSubmatch(body); m != nil {
			return cleanMerchant(m[1])
		}
		if m := toAccountRe.FindStringSubmatch(body); m != nil {
			return cleanMerchant(m[1])
		}
	case KindDeposit:
		if m := fromPersonRe.FindStringSubmatch(body); m != nil {
			return cleanMerchant(m[1])
		}
	}
	if m := atPlaceRe.FindStringSubmatch(body); m != nil {
		if s := cleanMerchant(m[1]); s != "" {
			return s
		}
	}
	return leftoverMerchant(body)
}

// leftoverMerchant는 금액·날짜·이름·일반 단어를 지우고 남은 조각 중 가장 그럴듯한 것을 고른다.
// 구분자(|, ·, 줄바꿈)로 나눈 조각 중 금액이 있던 줄의 조각을 우선한다.
func leftoverMerchant(body string) string {
	var best string
	bestScore := -1
	for li, line := range strings.Split(body, "\n") {
		hasAmount := amountRe.MatchString(line)
		for _, seg := range splitSegments(line) {
			s := cleanMerchant(seg)
			if s == "" {
				continue
			}
			score := len([]rune(s))
			if score > 30 {
				score = 5 // 문장처럼 긴 조각은 가맹점이 아닐 가능성이 높다
			}
			if hasAmount {
				score += 10
			}
			if li == 0 {
				score -= 3 // 첫 줄은 보통 앱 이름·제목
			}
			if score > bestScore {
				best, bestScore = s, score
			}
		}
	}
	return best
}

func splitSegments(line string) []string {
	f := func(r rune) bool { return r == '|' || r == '·' || r == '\t' }
	return strings.FieldsFunc(line, f)
}

// cleanMerchant는 조각에서 잡음을 지운다. 남은 게 가맹점처럼 보이지 않으면 빈 문자열.
func cleanMerchant(s string) string {
	s = bracketTagRe.ReplaceAllString(s, " ")
	s = labelAmountRe.ReplaceAllString(s, " ")
	s = amountRe.ReplaceAllString(s, " ")
	s = maskedNameRe.ReplaceAllString(s, " ")
	s = cardNumRe.ReplaceAllString(s, " ")
	s = dateTimeRe.ReplaceAllString(s, " ")
	s = installTokRe.ReplaceAllString(s, " ")
	for _, w := range noiseWords {
		s = strings.ReplaceAll(s, w, " ")
	}
	s = strings.Trim(spacesRe.ReplaceAllString(s, " "), " .,:：-~!()")
	if !hasLetter(s) || len([]rune(s)) < 2 {
		return ""
	}
	return s
}

func hasLetter(s string) bool {
	for _, r := range s {
		if (r >= '가' && r <= '힣') || (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') {
			return true
		}
	}
	return false
}

// parseCommon은 모든 파서가 공유하는 기본 해석이다.
func parseCommon(source string, ev Event) (Result, error) {
	body := ev.Body()
	hits := findAmounts(body)
	amount, ok := mainAmount(hits)
	kind := detectKind(body)
	res := Result{
		Source:      source,
		Kind:        kind,
		Amount:      amount,
		OccurredAt:  occurredAt(body, ev.PostedAt),
		Balance:     balanceAmount(hits),
		Installment: installment(body),
	}
	if !ok {
		return res, ErrNoAmount
	}
	res.Merchant = guessMerchant(body, kind)
	return res, nil
}
