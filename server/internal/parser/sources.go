package parser

import (
	"regexp"
	"strings"
)

// ---- 범용 ----

// Generic은 소스를 모를 때 쓰는 범용 파서다. 매핑에서 이름을 주면 그 이름을 source로 쓴다.
type Generic struct{ Source string }

func (g Generic) Name() string {
	if g.Source == "" {
		return "generic"
	}
	return g.Source
}
func (Generic) Detect(Event) bool                { return false }
func (g Generic) Parse(ev Event) (Result, error) { return parseCommon(g.Name(), ev) }

// ---- KB국민카드 (잠정) ----

// KB는 KB국민카드·KB Pay 알림용 잠정 파서다. 실제 형식을 확인하지 못해서 넓게 잡는다.
type KB struct{}

func (KB) Name() string { return "kb" }

func (KB) Detect(ev Event) bool {
	b := ev.Body()
	return containsAny(b, "KB국민", "국민카드", "KB Pay", "KB페이", "KB카드")
}

// KB 승인 문자는 흔히 "금액 일시불|할부 [시각] 가맹점 [누적 금액]" 순서라서
// 일시불·할부 표시 뒤를 가맹점으로 먼저 본다.
var kbAfterInstallRe = regexp.MustCompile(`(?:일시불|할부\s*\(?\s*[0-9]{0,2}\s*개월\)?)\s*(.+)`)

func (k KB) Parse(ev Event) (Result, error) {
	res, err := parseCommon(k.Name(), ev)
	if err != nil {
		return res, err
	}
	for _, line := range strings.Split(ev.Body(), "\n") {
		if m := kbAfterInstallRe.FindStringSubmatch(line); m != nil {
			rest := m[1]
			if i := strings.Index(rest, "누적"); i >= 0 {
				rest = rest[:i]
			}
			if s := cleanMerchant(rest); s != "" {
				res.Merchant = s
				break
			}
		}
	}
	return res, nil
}

// ---- 토스 (잠정) ----

// Toss는 토스·토스뱅크·토스페이 알림용 잠정 파서다.
type Toss struct{}

func (Toss) Name() string { return "toss" }

func (Toss) Detect(ev Event) bool {
	return strings.Contains(ev.Body(), "토스")
}

func (t Toss) Parse(ev Event) (Result, error) {
	res, err := parseCommon(t.Name(), ev)
	if err != nil {
		return res, err
	}
	// "토스페이 결제 | 가맹점" 처럼 결제 수단 이름이 가맹점으로 잡히지 않게 한다.
	if res.Merchant == "토스" || res.Merchant == "토스페이" {
		res.Merchant = ""
	}
	return res, nil
}
