package classify

import "testing"

func TestNormalize(t *testing.T) {
	if got := Normalize("(주) 스타벅스 Korea"); got != "스타벅스korea" {
		t.Errorf("Normalize = %q", got)
	}
}

func TestFirstOrdering(t *testing.T) {
	rules := []Rule{
		{ID: 1, Pattern: "쿠팡", MatchType: "contains", CategoryID: 4, Flow: "out", Priority: 10, CreatedFrom: "seed"},
		{ID: 2, Pattern: "쿠팡이츠", MatchType: "contains", CategoryID: 1, Flow: "out", Priority: 10, CreatedFrom: "seed"},
		{ID: 3, Pattern: "KT", MatchType: "exact", CategoryID: 6, Flow: "out", Priority: 10, CreatedFrom: "seed"},
		{ID: 4, Pattern: "KTX", MatchType: "contains", CategoryID: 3, Flow: "out", Priority: 10, CreatedFrom: "seed"},
		{ID: 5, Pattern: "쿠팡 로켓프레시", MatchType: "exact", CategoryID: 1, Flow: "out", Priority: 100, CreatedFrom: "learned"},
		{ID: 6, Pattern: "^GS\\d+", MatchType: "regex", CategoryID: 5, Priority: 10, CreatedFrom: "manual"},
		{ID: 7, Pattern: "급여", MatchType: "contains", CategoryID: 12, Flow: "in", Priority: 10, CreatedFrom: "seed"},
	}
	Sort(rules)
	cases := []struct {
		merchant, flow string
		want           int64
		ok             bool
	}{
		{"쿠팡이츠 강남", "out", 1, true},
		{"쿠팡", "out", 4, true},
		{"쿠팡로켓프레시", "out", 1, true}, // 학습 규칙(공백 무시 exact)이 seed보다 먼저
		{"KTX 서울역", "out", 3, true},
		{"KT", "out", 6, true},
		{"KTF대리점", "out", 0, false},
		{"gs25 역삼", "out", 5, true},
		{"회사 급여", "in", 12, true},
		{"회사 급여", "out", 0, false}, // flow 불일치
		{"", "out", 0, false},
	}
	for _, c := range cases {
		r, ok := First(rules, c.merchant, c.flow, nil)
		if ok != c.ok || (ok && r.CategoryID != c.want) {
			t.Errorf("First(%q,%s) = %d,%v want %d,%v", c.merchant, c.flow, r.CategoryID, ok, c.want, c.ok)
		}
	}
	// 사용자 규칙만
	if _, ok := First(rules, "쿠팡", "out", Rule.IsUserRule); ok {
		t.Error("seed 규칙이 걸러져야 함")
	}
}

func TestStripGatewayAndSpecificity(t *testing.T) {
	gws := []string{"KG이니시스", "이니시스", "카카오페이", "토스페이먼츠"}
	if rest, gw := StripGateway("KG이니시스", gws); !gw || rest != "" {
		t.Errorf("KG이니시스 → %q,%v", rest, gw)
	}
	if rest, gw := StripGateway("카카오페이_스타벅스", gws); !gw || rest != "스타벅스" {
		t.Errorf("카카오페이_스타벅스 → %q,%v", rest, gw)
	}
	if _, gw := StripGateway("스타벅스", gws); gw {
		t.Error("결제대행사 아님")
	}
	if MoreSpecific("토스페이먼츠", "스타벅스 역삼점", gws) != "스타벅스 역삼점" {
		t.Error("구체적인 이름을 골라야 함")
	}
	if MoreSpecific("스타벅스", "", gws) != "스타벅스" {
		t.Error("빈 이름은 고르지 않음")
	}
	if MoreSpecific("스타벅스 역삼점", "카카오페이", gws) != "스타벅스 역삼점" {
		t.Error("기존 구체적 이름 유지")
	}
}

func TestSimilarMerchant(t *testing.T) {
	if !SimilarMerchant("스타벅스강남점", "스타벅스 강남") {
		t.Error("같은 가게")
	}
	if SimilarMerchant("스타벅스", "투썸플레이스") {
		t.Error("다른 가게")
	}
	if !SimilarMerchant("", "투썸") {
		t.Error("빈 이름은 비교 불가 → true")
	}
}

func TestValidPattern(t *testing.T) {
	if ValidPattern("regex", "(") || !ValidPattern("regex", "^a") || ValidPattern("contains", " ") || ValidPattern("x", "a") {
		t.Error("패턴 검사 오류")
	}
}

func TestPrefixRule(t *testing.T) {
	cu := Rule{Pattern: "CU", MatchType: "prefix"}
	kt := Rule{Pattern: "KT", MatchType: "prefix"}
	cases := []struct {
		rule     Rule
		merchant string
		want     bool
	}{
		{cu, "CU", true},
		{cu, "CU 선릉점", true},
		{cu, "CU선릉점", true},
		{cu, "cu 역삼점", true},
		{cu, "CU(씨유) 강남", true},
		{cu, "(주)CU 선릉점", true},
		{cu, "CUBE", false},
		{cu, "CU2호점", false},
		{cu, "맛집CU", false},
		{kt, "KT", true},
		{kt, "KT 강남대리점", true},
		{kt, "주식회사 KT", true},
		{kt, "KTX 서울역", false},
		{kt, "KTF대리점", false},
		{kt, "KT&G 상상마당", false},
	}
	for _, c := range cases {
		if got := c.rule.Matches(c.merchant, "out"); got != c.want {
			t.Errorf("%s 규칙 %q = %v, want %v", c.rule.Pattern, c.merchant, got, c.want)
		}
	}
	if !ValidPattern("prefix", "CU") {
		t.Error("prefix는 유효한 방식이어야 함")
	}
}
