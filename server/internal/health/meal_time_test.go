package health

import (
	"encoding/json"
	"testing"
	"time"
)

// 앱(java OffsetDateTime.toString())은 초가 0이면 초를 빼고 보낸다. 2026-11-11 운영에서 끼니 저장이 전부 400이었다.
func TestMealRequestLoggedAtWithoutSeconds(t *testing.T) {
	for in, want := range map[string]string{
		"2026-11-11T11:52+09:00":           "2026-11-11T02:52:00Z",
		"2026-11-11T11:52:07+09:00":        "2026-11-11T02:52:07Z",
		"2026-11-11T11:52:07.123456+09:00": "2026-11-11T02:52:07.123456Z",
		"2026-11-11T02:52Z":                "2026-11-11T02:52:00Z",
	} {
		var r MealRequest
		if err := json.Unmarshal([]byte(`{"date":"2026-11-11","index":1,"loggedAt":"`+in+`","ai":{"items":[]}}`), &r); err != nil {
			t.Fatalf("%s: %v", in, err)
		}
		if r.LoggedAt == nil || r.LoggedAt.UTC().Format(time.RFC3339Nano) != want {
			t.Fatalf("%s → %v, want %s", in, r.LoggedAt, want)
		}
		if r.Date != "2026-11-11" || r.Index != 1 || r.AI == nil {
			t.Fatalf("다른 필드를 잃음: %+v", r)
		}
	}
	var r MealRequest
	if err := json.Unmarshal([]byte(`{"date":"2026-11-11","index":1}`), &r); err != nil || r.LoggedAt != nil {
		t.Fatalf("loggedAt 없음 → nil이어야 함: %v %v", r.LoggedAt, err)
	}
	if err := json.Unmarshal([]byte(`{"loggedAt":"11:52"}`), &r); err == nil {
		t.Fatal("시각 형식이 아니면 실패해야 함")
	}
}
