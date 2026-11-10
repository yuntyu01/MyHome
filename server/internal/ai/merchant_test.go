package ai

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

// 가맹점 분류 요청에는 고정 문구 + 분류 이름 + 가맹점 이름(JSON)만 실린다. 답은 id로 다시 맞춘다.
func TestGeminiClassifyMerchantsRequestAndParse(t *testing.T) {
	var body struct {
		Contents []struct {
			Parts []map[string]any `json:"parts"`
		} `json:"contents"`
		GenerationConfig struct {
			ResponseSchema struct {
				Properties struct {
					Results struct {
						Items struct {
							Properties struct {
								Category struct {
									Enum []string `json:"enum"`
								} `json:"category"`
							} `json:"properties"`
						} `json:"items"`
					} `json:"results"`
				} `json:"properties"`
			} `json:"responseSchema"`
		} `json:"generationConfig"`
	}
	var gotKey string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotKey = r.Header.Get("x-goog-api-key")
		b, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(b, &body); err != nil {
			t.Errorf("요청 JSON: %v", err)
		}
		// 0: 정상, 1: 모름, 2: 모르는 분류 이름 → 모름, 3: 확신 범위 밖 → 모름, 4: 빠짐 → 모름, 9: 없는 id(무시), 0 중복(무시)
		answer := `{"results":[{"id":0,"category":"식비","confidence":0.9},{"id":1,"category":"모름","confidence":0.3},` +
			`{"id":2,"category":"술집","confidence":0.8},{"id":3,"category":"생활","confidence":95},` +
			`{"id":9,"category":"식비","confidence":1},{"id":0,"category":"생활","confidence":1}]}`
		resp, _ := json.Marshal(map[string]any{"candidates": []any{map[string]any{"content": map[string]any{"parts": []any{map[string]any{"text": answer}}}}}})
		_, _ = w.Write(resp)
	}))
	defer srv.Close()
	g, _ := NewGemini("secret", "m", srv.Client())
	g.WithBase(srv.URL)
	merchants := []string{"동네분식", "홍길동", "어떤술집", "철물점", "빠진가게"}
	cats := []CategoryChoice{{Name: "식비", Memo: "밥, 배달\n편의점 도시락"}, {Name: "카페·간식"}, {Name: "생활"}}
	got, err := g.ClassifyMerchants(context.Background(), merchants, cats, "")
	if err != nil {
		t.Fatal(err)
	}
	want := []MerchantGuess{{"식비", 0.9}, {}, {}, {}, {}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("답 %+v", got)
	}
	if gotKey != "secret" {
		t.Errorf("키 헤더 %q", gotKey)
	}
	if len(body.Contents) != 1 || len(body.Contents[0].Parts) != 1 {
		t.Fatalf("글 조각 하나만: %+v", body.Contents)
	}
	if text, _ := body.Contents[0].Parts[0]["text"].(string); text != MerchantPromptText(merchants, cats, "") {
		t.Errorf("프롬프트가 고정 문구 + 분류 + 가맹점 이름만이 아님:\n%s", text)
	}
	if enum := body.GenerationConfig.ResponseSchema.Properties.Results.Items.Properties.Category.Enum; !reflect.DeepEqual(enum, []string{"식비", "카페·간식", "생활", "모름"}) {
		t.Errorf("enum %v", enum)
	}
	// 분류는 한 줄에 하나, 설명은 줄바꿈 없이 이름 뒤에
	text := MerchantPromptText(merchants, cats, "")
	if !strings.Contains(text, "\n- 식비: 밥, 배달 편의점 도시락\n- 카페·간식\n- 생활\n") {
		t.Errorf("분류 목록 모양:\n%s", text)
	}
	// 프롬프트의 가맹점 목록은 id·이름만
	list := text[strings.Index(text, "[{"):]
	var items []map[string]any
	if err := json.Unmarshal([]byte(list), &items); err != nil || len(items) != len(merchants) {
		t.Fatalf("가맹점 목록 %v %s", err, list)
	}
	for i, it := range items {
		if len(it) != 2 || it["name"] != merchants[i] || it["id"] != float64(i) {
			t.Errorf("항목 %d: %v", i, it)
		}
	}
}

// 응답 모양이 틀리면 실패(ErrFailed)이고, 에러에는 응답 내용(가맹점 이름이 섞일 수 있음)이 들어가지 않는다.
func TestGeminiClassifyMerchantsBadAnswer(t *testing.T) {
	for _, answer := range []string{`동네분식은 식비`, `{"items":[]}`, `{"results":"동네분식"}`} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			resp, _ := json.Marshal(map[string]any{"candidates": []any{map[string]any{"content": map[string]any{"parts": []any{map[string]any{"text": answer}}}}}})
			_, _ = w.Write(resp)
		}))
		g, _ := NewGemini("k", "m", srv.Client())
		_, err := g.WithBase(srv.URL).ClassifyMerchants(context.Background(), []string{"동네분식"}, []CategoryChoice{{Name: "식비"}}, "")
		srv.Close()
		if !errors.Is(err, ErrFailed) || strings.Contains(err.Error(), "동네분식") {
			t.Errorf("%s: %v", answer, err)
		}
	}
	g, _ := NewGemini("k", "m", nil)
	many := make([]string, MaxMerchantsPerCall+1)
	if _, err := g.ClassifyMerchants(context.Background(), many, []CategoryChoice{{Name: "식비"}}, ""); err == nil {
		t.Error("한 번에 너무 많은 가맹점")
	}
	if _, err := g.ClassifyMerchants(context.Background(), []string{"a"}, nil, ""); err == nil {
		t.Error("분류 없음")
	}
}

// mock은 낱말 표로 늘 같은 답을 준다. 받은 분류 목록에 없는 분류는 고르지 않고, 폴더 경로는 마지막 이름으로 맞춘다.
func TestMockClassifyMerchants(t *testing.T) {
	cats := []CategoryChoice{{Name: "생활비 › 식비"}, {Name: "카페·간식"}, {Name: "생활"}}
	got, err := Mock{}.ClassifyMerchants(context.Background(), []string{"동네분식", "골목커피", "주유소앞", "대박상회", "알수없음"}, cats, "")
	if err != nil {
		t.Fatal(err)
	}
	want := []MerchantGuess{{"생활비 › 식비", 0.9}, {"카페·간식", 0.9}, {}, {"생활", 0.4}, {}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("mock %+v", got)
	}
	cl, err := NewMerchantClassifier("", "", "")
	if err != nil || cl.Model() != "mock" {
		t.Fatalf("기본 분류기 %v %v", cl, err)
	}
	if cl, err := NewMerchantClassifier("gemini", "k", ""); err != nil || cl.Model() != DefaultModel {
		t.Fatalf("gemini 분류기 %v %v", cl, err)
	}
	if _, err := NewMerchantClassifier("openai", "", ""); err == nil {
		t.Fatal("모르는 제공자")
	}
}
