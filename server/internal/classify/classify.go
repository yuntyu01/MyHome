// Package classify는 가맹점 이름으로 분류 규칙을 찾는 순수 함수들을 담는다.
package classify

import (
	"regexp"
	"sort"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"
)

// Rule은 분류 규칙이다.
type Rule struct {
	ID          int64
	Pattern     string
	MatchType   string // contains, exact, prefix, regex
	CategoryID  int64
	Flow        string // "", "out", "in"
	Priority    int
	CreatedFrom string // manual, learned, seed
}

// IsUserRule은 사용자가 직접 만들었거나 학습된 규칙인지 반환한다.
func (r Rule) IsUserRule() bool { return r.CreatedFrom == "manual" || r.CreatedFrom == "learned" }

var corpRe = regexp.MustCompile(`\(주\)|㈜|주식회사|\(유\)|유한회사`)

// Normalize는 비교용 정규화: 법인 표기 제거, 공백 제거, 소문자.
func Normalize(s string) string {
	s = corpRe.ReplaceAllString(s, "")
	var b strings.Builder
	for _, r := range s {
		if unicode.IsSpace(r) {
			continue
		}
		b.WriteRune(unicode.ToLower(r))
	}
	return b.String()
}

var (
	reCache   = map[string]*regexp.Regexp{}
	reCacheMu sync.Mutex
)

func compiled(p string) *regexp.Regexp {
	reCacheMu.Lock()
	defer reCacheMu.Unlock()
	if re, ok := reCache[p]; ok {
		return re
	}
	re, err := regexp.Compile("(?i)" + p)
	if err != nil {
		re = nil
	}
	reCache[p] = re
	return re
}

// ValidPattern은 규칙 패턴이 유효한지 검사한다.
func ValidPattern(matchType, pattern string) bool {
	if strings.TrimSpace(pattern) == "" {
		return false
	}
	switch matchType {
	case "contains", "exact", "prefix":
		return true
	case "regex":
		_, err := regexp.Compile("(?i)" + pattern)
		return err == nil
	}
	return false
}

// Matches는 규칙이 가맹점에 맞는지 본다.
func (r Rule) Matches(merchant, flow string) bool {
	if r.Flow != "" && r.Flow != flow {
		return false
	}
	switch r.MatchType {
	case "exact":
		return Normalize(merchant) == Normalize(r.Pattern)
	case "contains":
		p := Normalize(r.Pattern)
		return p != "" && strings.Contains(Normalize(merchant), p)
	case "prefix":
		return hasNamePrefix(Normalize(merchant), Normalize(r.Pattern))
	case "regex":
		re := compiled(r.Pattern)
		return re != nil && re.MatchString(merchant)
	}
	return false
}

// hasNamePrefix는 name이 prefix로 시작하고 그 뒤가 다른 영문 이름으로 이어지지 않는지 본다.
// 짧은 영문 상호용이다: CU → "CU 선릉점"은 맞고 "CUBE"는 아니다. KT → "KT 대리점"은 맞고 "KTX", "KT&G"는 아니다.
func hasNamePrefix(name, prefix string) bool {
	if prefix == "" || !strings.HasPrefix(name, prefix) {
		return false
	}
	rest := name[len(prefix):]
	if rest == "" {
		return true
	}
	next, _ := utf8.DecodeRuneInString(rest)
	return !(next == '&' || next >= 'a' && next <= 'z' || next >= '0' && next <= '9')
}

// Sort는 적용 순서로 정렬한다: 우선순위 높은 순 → 사용자 규칙 먼저 → 긴 패턴 먼저 → 최신 먼저.
func Sort(rules []Rule) {
	sort.SliceStable(rules, func(i, j int) bool {
		a, b := rules[i], rules[j]
		if a.Priority != b.Priority {
			return a.Priority > b.Priority
		}
		if a.IsUserRule() != b.IsUserRule() {
			return a.IsUserRule()
		}
		if la, lb := len([]rune(a.Pattern)), len([]rune(b.Pattern)); la != lb {
			return la > lb
		}
		return a.ID > b.ID
	})
}

// First는 정렬된 규칙 중 처음 맞는 것을 반환한다. filter가 nil이 아니면 그 조건도 만족해야 한다.
func First(sorted []Rule, merchant, flow string, filter func(Rule) bool) (Rule, bool) {
	if strings.TrimSpace(merchant) == "" {
		return Rule{}, false
	}
	for _, r := range sorted {
		if filter != nil && !filter(r) {
			continue
		}
		if r.Matches(merchant, flow) {
			return r, true
		}
	}
	return Rule{}, false
}

// StripGateway는 가맹점에서 결제대행사 이름을 떼어낸다.
// 반환: 나머지 이름, 결제대행사 이름이 들어 있었는지 여부.
// 예: "카카오페이_스타벅스" → "스타벅스", true / "KG이니시스" → "", true
func StripGateway(merchant string, gateways []string) (string, bool) {
	norm := Normalize(merchant)
	found := false
	// 긴 이름부터(이니시스보다 KG이니시스 먼저)
	gs := append([]string(nil), gateways...)
	sort.Slice(gs, func(i, j int) bool { return len([]rune(gs[i])) > len([]rune(gs[j])) })
	for _, g := range gs {
		ng := Normalize(g)
		if ng != "" && strings.Contains(norm, ng) {
			norm = strings.ReplaceAll(norm, ng, "")
			found = true
		}
	}
	if !found {
		return merchant, false
	}
	rest := strings.Trim(norm, "_-()[]/.,·| ")
	if len([]rune(rest)) < 2 {
		return "", true
	}
	return rest, true
}

// Specificity는 가맹점 이름이 얼마나 구체적인지 점수로 매긴다(중복 합치기 때 이름 선택용).
// 빈 이름 0, 결제대행사 이름뿐 1, 그 외는 2+길이(최대 40).
func Specificity(merchant string, gateways []string) int {
	m := strings.TrimSpace(merchant)
	if m == "" {
		return 0
	}
	if rest, gw := StripGateway(m, gateways); gw && rest == "" {
		return 1
	}
	n := len([]rune(m))
	if n > 40 {
		n = 40
	}
	return 2 + n
}

// MoreSpecific은 두 이름 중 더 구체적인 것을 고른다. 같으면 a(기존)를 유지한다.
func MoreSpecific(a, b string, gateways []string) string {
	if Specificity(b, gateways) > Specificity(a, gateways) {
		return b
	}
	return a
}

// SimilarMerchant는 취소 상계에서 원거래를 찾을 때 쓰는 느슨한 비교다.
// 어느 한쪽이 비어 있으면 비교할 수 없어서 true(금액으로만 판단).
func SimilarMerchant(a, b string) bool {
	na, nb := Normalize(a), Normalize(b)
	if na == "" || nb == "" {
		return true
	}
	if strings.Contains(na, nb) || strings.Contains(nb, na) {
		return true
	}
	// 앞 4글자가 같으면 같은 가게로 본다(지점명 표기 차이).
	ra, rb := []rune(na), []rune(nb)
	n := 4
	if len(ra) < n || len(rb) < n {
		return false
	}
	return string(ra[:n]) == string(rb[:n])
}
