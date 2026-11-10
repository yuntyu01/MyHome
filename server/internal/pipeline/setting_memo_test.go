package pipeline

import (
	"encoding/json"
	"strings"
	"testing"
)

// AI 공통 메모: 문자열이고 1000자까지
func TestValidateAIMemo(t *testing.T) {
	ok, _ := json.Marshal("회사 근처 식당은 식비")
	long, _ := json.Marshal(strings.Repeat("가", AIMemoMax+1))
	edge, _ := json.Marshal(strings.Repeat("가", AIMemoMax))
	if !ValidateSetting("ai_classify_memo", ok) || !ValidateSetting("ai_classify_memo", edge) {
		t.Fatal("문자열 메모는 받아야 함")
	}
	if ValidateSetting("ai_classify_memo", long) || ValidateSetting("ai_classify_memo", json.RawMessage(`["목록"]`)) {
		t.Fatal("너무 길거나 문자열이 아니면 거절")
	}
	if !SettingKeys["ai_classify_memo"] {
		t.Fatal("API로 바꿀 수 있는 키여야 함")
	}
}
