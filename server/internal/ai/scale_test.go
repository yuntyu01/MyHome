package ai

import (
	"context"
	"strings"
	"testing"
)

func TestParseScale(t *testing.T) {
	cases := []struct {
		in   string
		want Scale
	}{
		{`{"weightKg":72.36,"confidence":0.93}`, Scale{72.4, 0.93}},
		{`{"weightKg":0,"confidence":0}`, Scale{}},      // 못 읽음
		{`{"weightKg":1500,"confidence":0.9}`, Scale{}}, // 말이 안 되는 값
		{`{"weightKg":65,"confidence":1.7}`, Scale{65, 1}},
	}
	for _, c := range cases {
		got, err := parseScale(c.in)
		if err != nil || got != c.want {
			t.Errorf("%s → %+v %v, want %+v", c.in, got, err, c.want)
		}
	}
	if _, err := parseScale(`not json`); err == nil {
		t.Error("깨진 JSON은 실패")
	}
	if s, _ := (Mock{}).ReadScale(context.Background(), Photo{Data: []byte{1}}); s.WeightKg != 72.4 {
		t.Errorf("mock %+v", s)
	}
}

// 공통 메모는 분류 목록 앞에 한 번 실리고, 없으면 예전 글과 같다
func TestMerchantPromptGuide(t *testing.T) {
	cats := []CategoryChoice{{Name: "식비"}, {Name: "생활", Memo: "편의점"}}
	plain := MerchantPromptText([]string{"합성가게"}, cats, "")
	with := MerchantPromptText([]string{"합성가게"}, cats, "  회사 근처 식당은 식비로  ")
	if want := "\n공통 메모:\n회사 근처 식당은 식비로\n"; !strings.Contains(with, want) || strings.Contains(plain, "공통 메모:") {
		t.Fatalf("메모가 실리지 않음:\n%s", with)
	}
	if MerchantPromptText([]string{"합성가게"}, cats, "   ") != plain {
		t.Fatal("빈 메모는 예전 글과 같아야 함")
	}
}
