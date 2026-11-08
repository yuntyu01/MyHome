package ai

import (
	"context"
	"encoding/json"
	"fmt"
)

// 음식마다 칼로리·탄단지를 따로 받고 끼니 합계는 서버에서 더한다. 합계만 받을 때와 정확도는 같았고(핏두잉 2026-10-09,
// 실사진 42장), 음식별 값은 나중에 식단 피드백에 쓴다.
// 영양성분표(영양정보 표)를 찍은 사진도 같은 호출로 받는다: 표의 숫자를 그대로 옮기게 한다(추정하지 않게).
const foodPrompt = `식단 사진이다. 보이는 음식(음료 포함)을 하나씩 나열하고, 각 음식마다 이름, 양, 칼로리(kcal), 탄수화물·단백질·지방(g)을 추정해라.
사진이 포장 식품의 영양성분표(영양정보 표)라면 추정하지 말고 표에 적힌 숫자를 그대로 옮겨라. 이때 음식은 하나로 하고,
이름은 포장에 보이는 제품명(없으면 "영양성분표 제품"), 양은 표의 기준(예: "총 내용량 120g", "1회 제공량 30g", "100g당")을 적어라.
총 내용량 기준 값이 있으면 그것을, 없으면 표의 기준량 값을 써라. 표에서 읽을 수 없는 영양소만 추정하고 confidence를 낮춰라.
음식도 영양성분표도 아니면 items를 비우고 confidence는 0으로 답해라. 한국어로 답해라.`

// foodMultiPrompt는 한 끼니가 사진 여러 장으로 올 때 덧붙인다(다른 접시의 반찬, 다른 각도).
const foodMultiPrompt = `사진 여러 장은 모두 같은 한 끼니다. 여러 사진에 같은 음식이 보이면 한 번만 세라(겹쳐 세지 마라).`

// foodParts는 프롬프트 다음에 끼니 사진을 모두 인라인으로 싣는다.
func foodParts(photos []Photo) []Part {
	text := foodPrompt
	if len(photos) > 1 {
		text += "\n" + foodMultiPrompt
	}
	out := []Part{Text(text)}
	for _, p := range photos {
		out = append(out, Inline(p.ContentType, p.Data))
	}
	return out
}

func foodSchema() Schema {
	item := Object(map[string]Schema{
		"name": String(), "amount": String(), "kcal": Integer(), "carbs": Integer(), "protein": Integer(), "fat": Integer(),
	}, "name", "amount", "kcal", "carbs", "protein", "fat")
	return Object(map[string]Schema{"items": Array(item), "confidence": Number()}, "items", "confidence")
}

// Analyze는 FoodAnalyzer 구현이다. 실패하면 ErrFailed를 감싼 에러다.
func (g *Gemini) Analyze(ctx context.Context, photos []Photo) (Result, error) {
	text, err := g.GenerateJSON(ctx, foodParts(photos), foodSchema())
	if err != nil {
		return Result{}, err
	}
	r, err := parseFood(text)
	if err != nil {
		return Result{}, fmt.Errorf("%w: %w", ErrFailed, err)
	}
	return r, nil
}

// parseFood는 모델의 JSON을 앱 요청처럼 검사한다: 음식마다 이름과 숫자 네 개가 다 있어야 하고, 범위는 Result.Validate.
// 끼니 합계는 음식 합이다. responseSchema가 보통 지켜지지만 모델이 숫자를 빼먹거나(json이 0으로 둔다) 범위 밖 값을
// 줄 수 있다. 둘 다 조용한 0kcal 대신 실패로 처리한다.
func parseFood(text string) (Result, error) {
	var raw struct {
		Items []struct {
			Name    string `json:"name"`
			Amount  string `json:"amount"`
			Kcal    *int   `json:"kcal"`
			Carbs   *int   `json:"carbs"`
			Protein *int   `json:"protein"`
			Fat     *int   `json:"fat"`
		} `json:"items"` // 없거나 null = 알아본 것 없음
		Confidence *float64 `json:"confidence"`
	}
	if err := json.Unmarshal([]byte(text), &raw); err != nil {
		return Result{}, err
	}
	if raw.Confidence == nil {
		return Result{}, fmt.Errorf("gemini: confidence 없음 %q", text)
	}
	r := Result{Items: []Item{}, Confidence: *raw.Confidence}
	for _, it := range raw.Items {
		if it.Kcal == nil || it.Carbs == nil || it.Protein == nil || it.Fat == nil {
			return Result{}, fmt.Errorf("gemini: 숫자 없는 음식 %q", text)
		}
		r.Items = append(r.Items, Item{Name: it.Name, Amount: it.Amount, Kcal: it.Kcal, Carbs: it.Carbs, Protein: it.Protein, Fat: it.Fat})
		r.Kcal += *it.Kcal
		r.Carbs += *it.Carbs
		r.Protein += *it.Protein
		r.Fat += *it.Fat
	}
	if err := r.Validate("ai"); err != nil {
		return Result{}, fmt.Errorf("gemini: %w", err)
	}
	return r, nil
}
