package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/yuntyu01/MyHome/server/internal/config"
)

// 핏두잉 internal/ai/ai_test.go 이식.

func TestGeminiSendsInlineImageAndParsesSchema(t *testing.T) {
	var gotKey, gotPath, gotQuery string
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotKey, gotPath, gotQuery = r.Header.Get("x-goog-api-key"), r.URL.Path, r.URL.RawQuery
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &body)
		_, _ = w.Write([]byte(`{"candidates":[{"content":{"parts":[{"text":"{\"items\":[{\"name\":\"밥\",\"amount\":\"1공기\",\"kcal\":300,\"carbs\":60,\"protein\":6,\"fat\":1},{\"name\":\"김\",\"amount\":\"1장\",\"kcal\":10,\"carbs\":1,\"protein\":1,\"fat\":1}],\"confidence\":0.7}"}]}}]}`))
	}))
	defer srv.Close()
	g, err := NewGemini("secret", "gemini-2.5-flash", srv.Client())
	if err != nil {
		t.Fatal(err)
	}
	res, err := g.WithBase(srv.URL).Analyze(context.Background(), []Photo{{Data: []byte{1, 2, 3}, ContentType: "image/jpeg"}})
	if err != nil {
		t.Fatal(err)
	}
	// 합계는 음식 합, 음식마다 제 숫자를 가진다
	if res.Kcal != 310 || res.Carbs != 61 || res.Items[0].Name != "밥" || *res.Items[0].Kcal != 300 || *res.Items[1].Fat != 1 {
		t.Fatalf("got %+v", res)
	}
	// 키는 헤더로만 보낸다(주소에 있으면 에러·로그에 남는다)
	if gotKey != "secret" || gotPath != "/models/gemini-2.5-flash:generateContent" || strings.Contains(gotQuery, "secret") {
		t.Fatalf("key=%q path=%q query=%q", gotKey, gotPath, gotQuery)
	}
	raw, _ := json.Marshal(body)
	if !strings.Contains(string(raw), `"inline_data"`) || !strings.Contains(string(raw), `"responseSchema"`) ||
		!strings.Contains(string(raw), "AQID") {
		t.Fatalf("unexpected request: %s", raw)
	}
}

// 한 끼니의 사진 여러 장은 한 요청에, 같은 음식을 두 번 세지 말라는 지시 뒤에 들어간다.
func TestGeminiSendsAllPhotosOfOneMeal(t *testing.T) {
	var body struct {
		Contents []struct {
			Parts []map[string]any `json:"parts"`
		} `json:"contents"`
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &body)
		_, _ = w.Write([]byte(`{"candidates":[{"content":{"parts":[{"text":"{\"items\":[],\"confidence\":0}"}]}}]}`))
	}))
	defer srv.Close()
	g, _ := NewGemini("k", "m", srv.Client())
	photos := []Photo{{[]byte{1}, "image/jpeg"}, {[]byte{2}, "image/jpeg"}, {[]byte{3}, "image/png"}}
	if _, err := g.WithBase(srv.URL).Analyze(context.Background(), photos); err != nil {
		t.Fatal(err)
	}
	parts := body.Contents[0].Parts
	if len(parts) != 4 || !strings.Contains(parts[0]["text"].(string), "한 번만 세라") {
		t.Fatalf("parts: %v", parts)
	}
	if one := foodParts(photos[:1]); strings.Contains(fmt.Sprint(one), "한 번만 세라") {
		t.Fatal("사진 한 장에는 여러 장 지시가 필요 없다")
	}
}

func TestGeminiFailureIsErrFailed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(500) }))
	defer srv.Close()
	g, _ := NewGemini("k", "m", srv.Client())
	_, err := g.WithBase(srv.URL).Analyze(context.Background(), []Photo{{Data: []byte{1}, ContentType: "image/png"}})
	if !errors.Is(err, ErrFailed) {
		t.Fatalf("got %v", err)
	}
	// 범위 밖 값도 실패(ErrFailed)로, 조용한 0kcal이 아니다
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"candidates":[{"content":{"parts":[{"text":"{\"items\":[],\"confidence\":95}"}]}}]}`))
	}))
	defer bad.Close()
	g, _ = NewGemini("k", "m", bad.Client())
	if _, err := g.WithBase(bad.URL).Analyze(context.Background(), []Photo{{Data: []byte{1}, ContentType: "image/png"}}); !errors.Is(err, ErrFailed) {
		t.Fatalf("got %v", err)
	}
}

// 연결 실패 메시지에 요청 주소가 들어가지 않는다(주소에 비밀값이 섞일 일을 막는다).
func TestGeminiErrorHasNoURL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	base := srv.URL
	srv.Close()
	g, _ := NewGemini("k", "m", nil)
	_, err := g.WithBase(base).Analyze(context.Background(), []Photo{{Data: []byte{1}, ContentType: "image/png"}})
	if err == nil || strings.Contains(err.Error(), base) {
		t.Fatalf("got %v", err)
	}
}

func TestGeminiNeedsKeyAndDefaultsModel(t *testing.T) {
	if _, err := NewGemini("", "m", nil); err == nil {
		t.Fatal("expected error")
	}
	g, _ := NewGemini("k", "", nil)
	if g.Model() != DefaultModel || ModelName(g) != DefaultModel || ModelName(Mock{}) != "mock" || DefaultModel != config.DefaultGeminiModel {
		t.Fatalf("model %q", g.Model())
	}
	if _, err := New("nope", "", ""); err == nil {
		t.Fatal("unknown provider accepted")
	}
	if a, err := New("", "", ""); err != nil || ModelName(a) != "mock" {
		t.Fatalf("default provider: %v %v", a, err)
	}
}

func TestGeminiResultIsValidated(t *testing.T) {
	cases := map[string]string{
		"missing confidence": `{"items":[]}`,
		"food without kcal":  `{"items":[{"name":"밥","amount":"1공기","carbs":60,"protein":6,"fat":1}],"confidence":0.5}`,
		"negative kcal":      `{"items":[{"name":"밥","amount":"1공기","kcal":-5,"carbs":0,"protein":0,"fat":0}],"confidence":0.5}`,
		"confidence > 1":     `{"items":[],"confidence":95}`,
		"empty item name":    `{"items":[{"name":" ","amount":"1개","kcal":100,"carbs":0,"protein":0,"fat":0}],"confidence":0.5}`,
		"fractional kcal":    `{"items":[{"name":"밥","amount":"1공기","kcal":100.5,"carbs":0,"protein":0,"fat":0}],"confidence":0.5}`,
		"carbs over 1000":    `{"items":[{"name":"밥","amount":"1공기","kcal":100,"carbs":5000,"protein":0,"fat":0}],"confidence":0.5}`,
		"not json":           `밥 한 공기 300kcal`,
	}
	for name, text := range cases {
		if _, err := parseFood(text); err == nil {
			t.Errorf("%s: accepted %s", name, text)
		}
	}
	r, err := parseFood(`{"items":null,"confidence":0}`)
	if err != nil || r.Items == nil || r.Kcal != 0 {
		t.Fatalf("not-food answer: %+v %v", r, err)
	}
}

func TestResultValidateItemPortions(t *testing.T) {
	p := func(v float64) *float64 { return &v }
	ok := Result{Items: []Item{{Name: "밥", Amount: "1공기", Portion: p(0.75)}, {Name: "김치", Amount: "조금"}}, Kcal: 100}
	if err := ok.Validate("ai"); err != nil {
		t.Fatal(err)
	}
	bad := Result{Items: []Item{{Name: "밥", Amount: "1공기", Portion: p(0.3)}}}
	var fe *FieldError
	if err := bad.Validate("ai"); !errors.As(err, &fe) || fe.Field != "ai.items.portion" {
		t.Fatalf("got %v", err)
	}
	if err := (&Result{}).Validate("ai"); !errors.As(err, &fe) || fe.Field != "ai.items" {
		t.Fatalf("items가 없으면 거절: %v", err)
	}
}

func TestMockTotalsAreTheSumOfItems(t *testing.T) {
	for _, s := range samples {
		var k, c, p, f int
		for _, it := range s.Items {
			k, c, p, f = k+*it.Kcal, c+*it.Carbs, p+*it.Protein, f+*it.Fat
		}
		if k != s.Kcal || c != s.Carbs || p != s.Protein || f != s.Fat {
			t.Errorf("%v: items sum %d/%d/%d/%d", s.Items[0].Name, k, c, p, f)
		}
	}
}
