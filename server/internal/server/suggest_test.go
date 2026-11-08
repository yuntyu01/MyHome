package server_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/yuntyu01/MyHome/server/internal/ai"
	"github.com/yuntyu01/MyHome/server/internal/config"
	"github.com/yuntyu01/MyHome/server/internal/dbq"
	"github.com/yuntyu01/MyHome/server/internal/suggest"
)

// 알림 문구는 합성 데이터다.

func aiOn(c *config.Config) { c.AIClassify = true }

// suggestionsByMerchant는 거래 목록 JSON에서 가맹점별 "suggestion" 원문을 꺼낸다. 키가 없으면 테스트 실패.
func suggestionsByMerchant(t *testing.T, items []map[string]json.RawMessage) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, it := range items {
		var m string
		_ = json.Unmarshal(it["merchant"], &m)
		s, ok := it["suggestion"]
		if !ok {
			t.Fatalf("%s: suggestion 키 없음", m)
		}
		out[m] = string(s)
	}
	return out
}

// 앱이 읽는 모양: "suggestion": {"category_id": 정수, "category": 문자열, "confidence": 실수} 또는 null.
// 확정은 기존 PATCH + remember_rule이고, 같은 가맹점의 다른 확인 필요 건도 함께 분류된다(기존 학습과 같음).
func TestAPISuggestionJSONAndConfirm(t *testing.T) {
	c := newClientWith(t, aiOn)
	ctx := context.Background()
	now := time.Now()
	first := c.event("test.kb", "KB국민카드", "홍*동님 9,000원 일시불 동네분식", now)
	c.event("test.kb", "KB국민카드", "홍*동님 6,000원 일시불 동네분식", now.Add(time.Second))
	c.event("test.kb", "KB국민카드", "홍*동님 5,000원 일시불 KG이니시스", now.Add(2*time.Second))
	c.event("test.kb", "KB국민카드", "홍*동님 7,000원 일시불 애매한상호", now.Add(3*time.Second))
	c.event("test.kb", "KB국민카드", "홍*동님 4,500원 일시불 메가커피", now.Add(4*time.Second))
	food := c.catID("식비")
	// 작업이 저장한 것처럼 넣는다(작업 자체는 suggest 패키지 테스트)
	for _, s := range []dbq.UpsertMerchantSuggestionParams{
		{MerchantKey: "동네분식", CategoryID: &food, Confidence: 0.9, Model: "m"},
		{MerchantKey: "애매한상호", CategoryID: &food, Confidence: 0.3, Model: "m"},
		{MerchantKey: "kg이니시스", CategoryID: &food, Confidence: 0.9, Model: "m"}, // 결제대행사 건에는 저장돼 있어도 안 보인다
	} {
		if err := c.q.UpsertMerchantSuggestion(ctx, s); err != nil {
			t.Fatal(err)
		}
	}
	wantObj := `{"category_id":` + itoa(food) + `,"category":"식비","confidence":0.9}`

	var review struct{ Transactions []map[string]json.RawMessage }
	if code := c.do("GET", "/v1/review", nil, &review); code != 200 {
		t.Fatalf("review %d", code)
	}
	got := suggestionsByMerchant(t, review.Transactions)
	if got["동네분식"] != wantObj || got["KG이니시스"] != "null" || got["애매한상호"] != "null" || len(review.Transactions) != 4 {
		t.Fatalf("review suggestion %v", got)
	}
	var list struct{ Transactions []map[string]json.RawMessage }
	c.do("GET", "/v1/transactions?month="+month(), nil, &list)
	if got := suggestionsByMerchant(t, list.Transactions); got["동네분식"] != wantObj || got["메가커피"] != "null" || got["KG이니시스"] != "null" {
		t.Fatalf("transactions suggestion %v", got)
	}
	var sum struct{ Recent []map[string]json.RawMessage }
	c.do("GET", "/v1/summary?month="+month(), nil, &sum)
	if got := suggestionsByMerchant(t, sum.Recent); got["동네분식"] != wantObj || got["메가커피"] != "null" {
		t.Fatalf("summary recent suggestion %v", got)
	}

	// 추천대로 확정: 기존 PATCH + remember_rule
	id := int64(first["result"].(map[string]any)["transaction"].(map[string]any)["id"].(float64))
	var patched struct {
		Transaction map[string]json.RawMessage `json:"transaction"`
		RuleID      *int64                     `json:"rule_id"`
		AlsoUpdated int                        `json:"also_updated"`
	}
	if code := c.do("PATCH", "/v1/transactions/"+itoa(id), map[string]any{"category_id": food, "remember_rule": true}, &patched); code != 200 {
		t.Fatalf("patch %d", code)
	}
	if patched.RuleID == nil || patched.AlsoUpdated != 1 || string(patched.Transaction["suggestion"]) != "null" || string(patched.Transaction["status"]) != `"confirmed"` {
		t.Fatalf("확정·학습 %+v", patched)
	}
	c.do("GET", "/v1/review", nil, &review)
	if got := suggestionsByMerchant(t, review.Transactions); len(got) != 2 || got["동네분식"] != "" {
		t.Fatalf("같은 가맹점 함께 분류: %v", got)
	}
	rules, _ := c.q.ListRules(ctx)
	learned := false
	for _, r := range rules {
		if r.Pattern == "동네분식" && r.MatchType == "exact" && r.CreatedFrom == "learned" && r.CategoryID == food {
			learned = true
		}
	}
	if !learned {
		t.Error("학습 규칙 없음")
	}
	// 다음부터는 규칙으로 바로 확정(AI 추천이 아니라 사용자 규칙)
	next := c.event("test.kb", "KB국민카드", "홍*동님 8,000원 일시불 동네분식", now.Add(5*time.Second))
	if tx := next["result"].(map[string]any)["transaction"].(map[string]any); tx["status"] != "confirmed" || int64(tx["category_id"].(float64)) != food {
		t.Fatalf("학습 규칙으로 확정: %+v", tx)
	}
}

// AI_CLASSIFY=off면 저장된 추천이 있어도 null이다.
func TestAPISuggestionOff(t *testing.T) {
	c := newClient(t)
	c.event("test.kb", "KB국민카드", "홍*동님 9,000원 일시불 동네분식", time.Now())
	food := c.catID("식비")
	if err := c.q.UpsertMerchantSuggestion(context.Background(), dbq.UpsertMerchantSuggestionParams{MerchantKey: "동네분식", CategoryID: &food, Confidence: 0.9, Model: "m"}); err != nil {
		t.Fatal(err)
	}
	var review struct{ Transactions []map[string]json.RawMessage }
	if code := c.do("GET", "/v1/review", nil, &review); code != http.StatusOK {
		t.Fatalf("review %d", code)
	}
	if got := suggestionsByMerchant(t, review.Transactions); got["동네분식"] != "null" {
		t.Fatalf("꺼져 있으면 null: %v", got)
	}
}

// 웹 확인 필요 화면: "AI 추천: 분류" 한 번 누르기 = 그 분류 + "이 가맹점은 항상"(체크 상자를 안 보내도). 수동 분류 버튼은 그대로.
// 낮은 확신 추천은 버튼이 없다. 추천은 mock 분류기(낱말 표)로 실제 작업이 만든다.
func TestWebReviewAISuggestion(t *testing.T) {
	c := newWebClientWith(t, aiOn)
	ctx := context.Background()
	a := c.event("홍*동님 8,000원 일시불 동네분식")
	b := c.event("홍*동님 6,000원 일시불 동네분식")
	c.event("홍*동님 3,000원 일시불 대박상회") // mock: 확신 0.4
	st, err := (&suggest.Worker{Pool: c.pool, Classifier: ai.Mock{}}).RunOnce(ctx)
	if err != nil || st.Suggested != 1 || st.Low != 1 {
		t.Fatalf("mock 작업 %+v %v", st, err)
	}
	food := c.cat("식비")
	page := c.get("/review").Body.String()
	if !strings.Contains(page, `name="suggest_category_id" value="`+food+`"`) || !strings.Contains(page, "AI 추천: 식비") {
		t.Fatalf("추천 버튼 없음: %s", page)
	}
	if !strings.Contains(page, `name="category_id" value="`+food+`"`) || !strings.Contains(page, `name="remember"`) {
		t.Error("수동 분류 버튼·체크 상자는 그대로")
	}
	if strings.Contains(page, "<script>") {
		t.Error("인라인 스크립트")
	}
	rec := c.post("/web/review/"+itoa(a), url.Values{"suggest_category_id": {food}, "memo": {"점심"}}, true)
	body := rec.Body.String()
	if rec.Code != 200 || !strings.Contains(body, "AI 추천대로 분류했어요. 다음부터 자동으로 분류해요. 같은 가맹점 1건도 함께 분류했어요.") {
		t.Fatalf("추천 확정 %d %s", rec.Code, body)
	}
	// 다음 건(대박상회)은 확신이 낮아 추천 버튼이 없다
	if !strings.Contains(body, "대박상회") || strings.Contains(body, "AI 추천:") {
		t.Errorf("낮은 확신 건에 추천 버튼: %s", body)
	}
	for _, id := range []int64{a, b} {
		tx, _ := c.q.GetTransaction(ctx, id)
		if tx.Status != "confirmed" || tx.CategoryID == nil || itoa(*tx.CategoryID) != food {
			t.Errorf("거래 #%d: %s %v", id, tx.Status, tx.CategoryID)
		}
	}
	if tx, _ := c.q.GetTransaction(ctx, a); tx.Memo != "점심" || !tx.ManuallyEdited {
		t.Errorf("메모·수정 표시 %+v", tx)
	}
	rules, _ := c.q.ListRules(ctx)
	learned := false
	for _, r := range rules {
		if r.Pattern == "동네분식" && r.CreatedFrom == "learned" && itoa(r.CategoryID) == food {
			learned = true
		}
	}
	if !learned {
		t.Error("학습 규칙 없음")
	}
	again := c.event("홍*동님 9,500원 일시불 동네분식")
	if tx, _ := c.q.GetTransaction(ctx, again); tx.Status != "confirmed" {
		t.Errorf("다음 거래는 규칙으로 확정: %s", tx.Status)
	}
}

// AI_CLASSIFY=off면 웹에도 추천 버튼이 없다.
func TestWebReviewAISuggestionOff(t *testing.T) {
	c := newWebClient(t)
	c.event("홍*동님 8,000원 일시불 동네분식")
	food, _ := strconv.ParseInt(c.cat("식비"), 10, 64)
	if err := c.q.UpsertMerchantSuggestion(context.Background(), dbq.UpsertMerchantSuggestionParams{MerchantKey: "동네분식", CategoryID: &food, Confidence: 0.9, Model: "m"}); err != nil {
		t.Fatal(err)
	}
	if page := c.get("/review").Body.String(); strings.Contains(page, "AI 추천") || !strings.Contains(page, "동네분식") {
		t.Error("꺼져 있으면 추천 버튼 없음")
	}
}
