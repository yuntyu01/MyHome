package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"strings"
)

// UnknownCategory는 AI가 가맹점 이름만으로 분류를 정하지 못했다는 답이다. 응답 스키마의 enum에 분류 이름과 함께 들어간다.
const UnknownCategory = "모름"

// MaxMerchantsPerCall은 한 번의 호출에 묻는 가맹점 수 상한이다.
const MaxMerchantsPerCall = 20

// MerchantGuess는 가맹점 하나에 대한 답이다. Category가 ""이면 모른다는 뜻이다(이때 Confidence는 0).
type MerchantGuess struct {
	Category   string
	Confidence float64
}

// CategoryChoice는 AI가 고를 수 있는 분류 하나다. Name은 폴더 경로까지 붙인 이름(예: "생활비 › 식비")이라 겹치지 않고,
// Memo는 사용자가 적은 분류 설명이다(비어 있을 수 있다). AI는 설명을 가맹점 이름과 함께 참고한다.
type CategoryChoice struct {
	Name string
	Memo string
}

// MerchantClassifier는 가맹점 이름을 지출 분류 중 하나로 고른다(가계부 AI 분류 추천).
// 보내는 것은 가맹점 이름과 분류 이름·설명뿐이다. 금액·시각·알림 원문·카드·계좌 정보는 받지도 보내지도 않는다.
// 답은 merchants와 같은 길이·순서다. Category는 categories의 Name 중 하나이거나 ""(모름)이다.
type MerchantClassifier interface {
	// guide는 분류마다 다는 설명과 따로, 모든 가맹점에 먼저 적용할 사용자 공통 메모다(없으면 "")
	ClassifyMerchants(ctx context.Context, merchants []string, categories []CategoryChoice, guide string) ([]MerchantGuess, error)
	Model() string
}

// NewMerchantClassifier는 AI_PROVIDER로 가맹점 분류기를 고른다. mock은 작은 낱말 표로 늘 같은 답을 준다(키 없음, 로컬·테스트용).
func NewMerchantClassifier(provider, geminiKey, geminiModel string) (MerchantClassifier, error) {
	switch provider {
	case "mock", "":
		return Mock{}, nil
	case "gemini":
		return NewGemini(geminiKey, geminiModel, &http.Client{})
	}
	return nil, fmt.Errorf("AI_PROVIDER는 mock 또는 gemini여야 해요: %q", provider)
}

// merchantPrompt는 고정 문구다. 뒤에 분류 목록(이름과 설명)과 가맹점 목록(JSON)만 붙는다.
const merchantPrompt = `한국 카드 결제 알림에 찍힌 가맹점 이름이다. 가맹점마다 아래 지출 분류 중 가장 알맞은 것 하나를 골라라.
분류 이름의 " › "는 폴더 구분이다(위 폴더 › 아래 분류). 더 알맞은 아래 분류가 있으면 아래 분류를 골라라.
분류 뒤 설명은 사용자가 그 분류에 넣고 싶은 것을 적은 글이다. 설명이 있으면 설명을 가장 먼저 따르라.
"공통 메모"가 있으면 사용자가 모든 가맹점에 대해 적은 규칙이다. 분류 설명과 함께 가장 먼저 따르라.
가맹점 이름만 보고 판단해라. 어떤 가게인지 모르겠거나, 이름만으로 무엇을 샀는지 알 수 없으면(결제대행사·간편결제·사람 이름 등) "모름"이라고 답해라.
confidence는 그 분류가 맞을 가능성(0~1)이다. 추측이면 낮게 줘라. results에는 받은 id마다 하나씩 답해라.`

// merchantItem은 프롬프트에 싣는 가맹점 한 줄이다. id는 답을 다시 맞추는 번호(0부터)다.
type merchantItem struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

// MerchantPromptText는 Gemini에 보내는 글 전체다(테스트가 요청 본문과 똑같이 맞춰 본다).
// 분류는 한 줄에 하나: "- 이름" 또는 "- 이름: 설명"(설명의 줄바꿈은 띄어쓰기로 바꾼다).
func MerchantPromptText(merchants []string, categories []CategoryChoice, guide string) string {
	items := make([]merchantItem, len(merchants))
	for i, m := range merchants {
		items[i] = merchantItem{ID: i, Name: m}
	}
	list, _ := json.Marshal(items) // 문자열·정수뿐이라 실패하지 않는다
	var b strings.Builder
	b.WriteString(merchantPrompt)
	if g := strings.TrimSpace(guide); g != "" {
		b.WriteString("\n공통 메모:\n" + g + "\n")
	}
	b.WriteString("\n분류:\n")
	for _, c := range categories {
		b.WriteString("- " + c.Name)
		if memo := strings.Join(strings.Fields(c.Memo), " "); memo != "" {
			b.WriteString(": " + memo)
		}
		b.WriteString("\n")
	}
	b.WriteString("가맹점 목록(JSON):\n" + string(list))
	return b.String()
}

func choiceNames(categories []CategoryChoice) []string {
	out := make([]string, len(categories))
	for i, c := range categories {
		out[i] = c.Name
	}
	return out
}

// merchantSchema는 답 모양이다: {results:[{id, category(분류 이름 또는 "모름"), confidence}]}.
func merchantSchema(categories []string) Schema {
	values := append(append([]string(nil), categories...), UnknownCategory)
	item := Object(map[string]Schema{"id": Integer(), "category": Enum(values...), "confidence": Number()}, "id", "category", "confidence")
	return Object(map[string]Schema{"results": Array(item)}, "results")
}

// ClassifyMerchants는 MerchantClassifier 구현이다. 실패하면 ErrFailed를 감싼 에러다.
func (g *Gemini) ClassifyMerchants(ctx context.Context, merchants []string, categories []CategoryChoice, guide string) ([]MerchantGuess, error) {
	if err := checkMerchantArgs(merchants, categories); err != nil {
		return nil, err
	}
	names := choiceNames(categories)
	text, err := g.GenerateJSON(ctx, []Part{Text(MerchantPromptText(merchants, categories, guide))}, merchantSchema(names))
	if err != nil {
		return nil, err
	}
	out, err := parseMerchantGuesses(text, len(merchants), names)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrFailed, err)
	}
	return out, nil
}

func checkMerchantArgs(merchants []string, categories []CategoryChoice) error {
	switch {
	case len(merchants) == 0:
		return errors.New("가맹점이 없어요")
	case len(merchants) > MaxMerchantsPerCall:
		return fmt.Errorf("한 번에 가맹점 %d개까지예요", MaxMerchantsPerCall)
	case len(categories) == 0:
		return errors.New("분류가 없어요")
	}
	return nil
}

// parseMerchantGuesses는 모델 JSON을 merchants 순서로 바꾼다. 응답 모양 자체가 틀리면 에러(다음에 다시 묻는다).
// 빠진 id, 범위 밖 id, 모르는 분류 이름, 0~1 밖의 confidence는 그 가맹점만 "모름"으로 둔다
// (가맹점마다 한 번만 묻는다는 약속을 지키려고, 이상한 답 때문에 같은 가맹점을 거듭 묻지 않는다).
// 에러 메시지에는 응답 내용(가맹점 이름이 섞일 수 있다)을 넣지 않는다.
func parseMerchantGuesses(text string, n int, categories []string) ([]MerchantGuess, error) {
	var raw struct {
		Results *[]struct {
			ID         *int     `json:"id"`
			Category   string   `json:"category"`
			Confidence *float64 `json:"confidence"`
		} `json:"results"`
	}
	if err := json.Unmarshal([]byte(text), &raw); err != nil {
		return nil, errors.New("gemini: 답이 JSON이 아님")
	}
	if raw.Results == nil {
		return nil, errors.New("gemini: results 없음")
	}
	known := make(map[string]bool, len(categories))
	for _, c := range categories {
		known[c] = true
	}
	out := make([]MerchantGuess, n)
	seen := make([]bool, n)
	for _, r := range *raw.Results {
		if r.ID == nil || *r.ID < 0 || *r.ID >= n || seen[*r.ID] {
			continue
		}
		seen[*r.ID] = true
		if !known[r.Category] || r.Confidence == nil || math.IsNaN(*r.Confidence) || *r.Confidence < 0 || *r.Confidence > 1 {
			continue // 모름
		}
		out[*r.ID] = MerchantGuess{Category: r.Category, Confidence: *r.Confidence}
	}
	return out, nil
}

// mockMerchantWords는 mock 분류기의 낱말 표다. 로컬·테스트에서 키 없이 추천 화면을 보려고 둔다(실제 판단 아님).
// 이름에 낱말이 들어 있고 그 분류가 있으면 고른다. 위에서부터 먼저 맞는 것.
var mockMerchantWords = []struct {
	word, category string
	confidence     float64
}{
	{"분식", "식비", 0.9}, {"식당", "식비", 0.9}, {"국밥", "식비", 0.9}, {"반점", "식비", 0.85}, {"치킨", "식비", 0.85},
	{"커피", "카페·간식", 0.9}, {"카페", "카페·간식", 0.9}, {"베이커리", "카페·간식", 0.85},
	{"마트", "생활", 0.8}, {"철물", "생활", 0.8},
	{"주유", "교통", 0.85}, {"주차", "교통", 0.8},
	{"서점", "문화·여가", 0.8}, {"꽃집", "경조사", 0.65},
	{"상회", "생활", 0.4}, // 낮은 확신: 저장은 하지만 화면에는 나오지 않는다(MinConfidence 확인용)
}

// ClassifyMerchants는 Mock의 MerchantClassifier 구현이다. 같은 이름이면 늘 같은 답이다.
// 낱말 표의 분류 이름은 폴더 경로의 마지막 이름과 맞춘다("생활비 › 식비"도 "식비"로 본다).
func (Mock) ClassifyMerchants(_ context.Context, merchants []string, categories []CategoryChoice, _ string) ([]MerchantGuess, error) {
	if err := checkMerchantArgs(merchants, categories); err != nil {
		return nil, err
	}
	have := make(map[string]string, len(categories))
	for _, c := range categories {
		parts := strings.Split(c.Name, " › ")
		if _, ok := have[parts[len(parts)-1]]; !ok {
			have[parts[len(parts)-1]] = c.Name
		}
	}
	out := make([]MerchantGuess, len(merchants))
	for i, m := range merchants {
		for _, w := range mockMerchantWords {
			if full, ok := have[w.category]; ok && strings.Contains(m, w.word) {
				out[i] = MerchantGuess{Category: full, Confidence: w.confidence}
				break
			}
		}
	}
	return out, nil
}
