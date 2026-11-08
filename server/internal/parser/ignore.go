package parser

import (
	"errors"
	"regexp"
	"strings"
)

// 무시 사유(raw_events.ignore_reason)
const (
	IgnoreAd         = "ad"         // 광고·혜택 안내
	IgnoreSecurities = "securities" // 증권 체결·시세 알림
	IgnoreNotice     = "notice"     // 안내·예정 알림(아직 돈이 움직이지 않음)
	IgnoreCustom     = "custom"     // PARSER_IGNORE_PATTERNS에 넣은 문구
)

// ErrIgnored는 결제 알림이 아니라서 거래를 만들지 않을 때 반환한다(해석 실패와 다르다).
var ErrIgnored = errors.New("결제 알림 아님")

// IgnoredError는 무시 사유를 담는다. errors.Is(err, ErrIgnored)로 확인한다.
type IgnoredError struct{ Reason string }

func (e *IgnoredError) Error() string        { return ErrIgnored.Error() + ": " + e.Reason }
func (e *IgnoredError) Is(target error) bool { return target == ErrIgnored }

type ignoreRule struct {
	re     *regexp.Regexp
	reason string
}

func rules(reason string, pats ...string) []ignoreRule {
	out := make([]ignoreRule, len(pats))
	for i, p := range pats {
		out[i] = ignoreRule{regexp.MustCompile(p), reason}
	}
	return out
}

// hardIgnore는 결제 문구가 같이 있어도 무시한다. 실제 결제 알림에는 나오지 않는 표현만 둔다.
var hardIgnore = concat(
	rules(IgnoreAd,
		`(?m)^\s*\(광고\)`,
		`적립해\s?드`, // 적립해드려요, 적립해 드립니다
		`(결제|구매|가입|이용|사용|충전|송금)하면\s`,
	),
	rules(IgnoreSecurities,
		`[0-9,]+\s*주를?\s*[0-9,]+\s*원에\s*(구매|판매|매수|매도)`,
		`[0-9.]+\s*%\s*(올랐|내렸|상승|하락)`,
	),
	rules(IgnoreNotice,
		`취소(는|하려면|하시려면|할\s?수)`,
	),
)

func concat(groups ...[]ignoreRule) []ignoreRule {
	var out []ignoreRule
	for _, g := range groups {
		out = append(out, g...)
	}
	return out
}

// softIgnore는 돈이 실제로 움직였다는 문구(paidMarker)가 없을 때만 무시한다.
var softIgnore = concat(
	rules(IgnoreAd,
		`혜택`, `쿠폰`, `이벤트`, `받기`, `받으세요`, `받아\s?가세요`, `[0-9]+\s*%\s*(할인|적립|캐시백)`, `만보기`,
	),
	rules(IgnoreNotice, `예정`, `수\s?있어요`),
)

// paidMarker는 실제 승인·입출금이 일어났다는 표현이다.
var paidMarker = regexp.MustCompile(`승인|일시불|할부|결제\s?(했|됐|되었|완료)|(입금|출금|이체|송금|충전|취소|환불)\s?(했|됐|되었|완료)|보냈어요|(결제|출금|입금|승인)\s*[0-9,]+\s*원`)

// Ignorer는 결제가 아닌 알림(광고·혜택, 증권, 안내)을 거른다. 실제 결제를 놓치지 않게 보수적으로 잡는다.
type Ignorer struct {
	extra []string // PARSER_IGNORE_PATTERNS: 들어 있으면 무조건 무시하는 문구
}

// ParseIgnorePatterns는 PARSER_IGNORE_PATTERNS("문구1;문구2", 정규식 아님)를 나눈다.
func ParseIgnorePatterns(spec string) []string {
	var out []string
	for _, p := range strings.Split(spec, ";") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// Match는 무시할 알림이면 사유를 돌려준다.
func (ig Ignorer) Match(ev Event) (string, bool) {
	body := ev.Body()
	for _, p := range ig.extra {
		if strings.Contains(body, p) {
			return IgnoreCustom, true
		}
	}
	for _, r := range hardIgnore {
		if r.re.MatchString(body) {
			return r.reason, true
		}
	}
	if paidMarker.MatchString(body) {
		return "", false
	}
	for _, r := range softIgnore {
		if r.re.MatchString(body) {
			return r.reason, true
		}
	}
	return "", false
}
