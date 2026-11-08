// Package ai는 Gemini 호출을 담는다(핏두잉 internal/ai 이식).
//
// 끼니 사진 분석(FoodAnalyzer, food.go)과 가계부 가맹점 이름 분류 추천(MerchantClassifier, merchant.go)이 같은 키를 쓴다.
// 호출부는 Gemini.GenerateJSON(프롬프트 조각 + 응답 스키마 → JSON 문자열) 하나다.
// 사진은 저장하지 않는다. 분석 요청에만 쓰고 버린다.
package ai

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
)

// ErrFailed는 AI 호출이 실패했다는 뜻이다(네트워크, 응답 형식, 범위 밖 값). 원인은 감싼 에러에 있다.
var ErrFailed = errors.New("AI 호출 실패")

// FieldError는 필드 하나의 검증 실패다. API에서는 400 INVALID "field: msg"가 된다.
type FieldError struct {
	Field string
	Msg   string
}

func (e *FieldError) Error() string { return e.Field + ": " + e.Msg }

func invalid(field, msg string) error { return &FieldError{Field: field, Msg: msg} }

// Item은 알아본 음식 하나다. 칼로리·탄단지는 음식마다 Gemini가 추정한 값이고, 사용자가 직접 넣은 음식이면 null이다.
// 끼니 합계는 Result 쪽이 기준이다.
type Item struct {
	Name    string `json:"name"`
	Amount  string `json:"amount"`
	Kcal    *int   `json:"kcal"`
	Carbs   *int   `json:"carbs"`
	Protein *int   `json:"protein"`
	Fat     *int   `json:"fat"`
	// Portion은 이 음식을 먹은 비율이다(사용자가 고름: 0.25, 0.5, 0.75, 1, 1.5, 2. null = 1).
	// 위 숫자는 음식 전체 기준이고, 끼니 합계에는 이미 반영돼 있다.
	Portion *float64 `json:"portion"`
}

// ItemPortions는 음식별로 고를 수 있는 먹은 비율이다.
var ItemPortions = []float64{0.25, 0.5, 0.75, 1, 1.5, 2}

// Result는 앱·Gemini와 같이 쓰는 고정 JSON 모양이다:
// {items:[{name, amount, kcal, carbs, protein, fat, portion}], kcal, carbs, protein, fat, confidence}.
type Result struct {
	Items      []Item  `json:"items"`
	Kcal       int     `json:"kcal"`
	Carbs      int     `json:"carbs"`
	Protein    int     `json:"protein"`
	Fat        int     `json:"fat"`
	Confidence float64 `json:"confidence"`
}

// Validate는 요청 제약을 확인한다. field는 JSON 경로 앞부분(예: "ai")이다.
func (r *Result) Validate(field string) error {
	if r.Items == nil {
		return invalid(field+".items", "필수예요")
	}
	for _, it := range r.Items {
		if strings.TrimSpace(it.Name) == "" {
			return invalid(field+".items.name", "비어 있으면 안 돼요")
		}
		if it.Portion != nil && !slices.Contains(ItemPortions, *it.Portion) {
			return invalid(field+".items.portion", "0.25, 0.5, 0.75, 1, 1.5, 2 중 하나예요")
		}
		if it.Kcal != nil && (*it.Kcal < 0 || *it.Kcal > 10000) {
			return invalid(field+".items.kcal", "0~10000 사이여야 해요")
		}
		for _, m := range []struct {
			k string
			v *int
		}{{"carbs", it.Carbs}, {"protein", it.Protein}, {"fat", it.Fat}} {
			if m.v != nil && (*m.v < 0 || *m.v > 1000) {
				return invalid(field+".items."+m.k, "0~1000 사이여야 해요")
			}
		}
	}
	switch {
	case r.Kcal < 0 || r.Kcal > 10000:
		return invalid(field+".kcal", "0~10000 사이여야 해요")
	case r.Carbs < 0 || r.Carbs > 1000:
		return invalid(field+".carbs", "0~1000 사이여야 해요")
	case r.Protein < 0 || r.Protein > 1000:
		return invalid(field+".protein", "0~1000 사이여야 해요")
	case r.Fat < 0 || r.Fat > 1000:
		return invalid(field+".fat", "0~1000 사이여야 해요")
	case r.Confidence < 0 || r.Confidence > 1:
		return invalid(field+".confidence", "0~1 사이여야 해요")
	}
	return nil
}

// Photo는 분석에 보내는 끼니 사진 한 장이다.
type Photo struct {
	Data        []byte
	ContentType string
}

// MaxPhotos는 한 끼니를 한 번에 분석할 때 보낼 수 있는 사진 수다.
const MaxPhotos = 3

// FoodAnalyzer는 한 끼니의 사진(1~MaxPhotos장)을 보고 음식·칼로리·탄단지를 추정한다.
// 구현은 AI_PROVIDER(mock | gemini)로 고른다. 사진은 저장하지 않는다.
type FoodAnalyzer interface {
	Analyze(ctx context.Context, photos []Photo) (Result, error)
}

// ModelName은 분석기가 쓰는 모델 이름이다(응답 헤더 X-AI-Model). 모르면 "".
func ModelName(a FoodAnalyzer) string {
	if m, ok := a.(interface{ Model() string }); ok {
		return m.Model()
	}
	return ""
}

// New는 제공자 이름으로 분석기를 고른다.
func New(provider, geminiKey, geminiModel string) (FoodAnalyzer, error) {
	switch provider {
	case "mock", "":
		return Mock{}, nil
	case "gemini":
		return NewGemini(geminiKey, geminiModel, &http.Client{})
	}
	return nil, fmt.Errorf("AI_PROVIDER는 mock 또는 gemini여야 해요: %q", provider)
}
