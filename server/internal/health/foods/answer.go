package foods

import (
	"math"

	"github.com/yuntyu01/MyHome/server/internal/ai"
)

// Answer는 바코드 조회 응답이다: 제품과, 영양성분을 알면 사진 분석과 같은 모양의 결과(앱이 끼니를 같은 방법으로
// 채운다). 제품만 알면 result는 null이다.
type Answer struct {
	Product Product    `json:"product"`
	Result  *ai.Result `json:"result"`
}

// ResultOf는 제품 영양성분을 사진 분석 결과 모양으로 바꾼다(정수로 반올림, 확신도 1). 영양성분이 없으면 nil.
func ResultOf(p Product) *ai.Result {
	if p.Nutrition == nil {
		return nil
	}
	r := func(x float64) int { return int(math.Round(x)) }
	n := p.Nutrition
	k, c, pr, f := r(n.Kcal), r(n.Carbs), r(n.Protein), r(n.Fat)
	return &ai.Result{
		Items: []ai.Item{{Name: p.Name, Amount: p.Amount, Kcal: &k, Carbs: &c, Protein: &pr, Fat: &f}},
		Kcal:  k, Carbs: c, Protein: pr, Fat: f,
		Confidence: 1,
	}
}
