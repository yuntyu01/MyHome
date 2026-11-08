package ai

import "context"

func n(v int) *int { return &v }

func food(name, amount string, kcal, carbs, protein, fat int) Item {
	return Item{Name: name, Amount: amount, Kcal: n(kcal), Carbs: n(carbs), Protein: n(protein), Fat: n(fat)}
}

// samples의 합계는 Gemini처럼 음식 합이다.
var samples = []Result{
	{[]Item{food("닭가슴살 샐러드", "1접시", 250, 10, 22, 13), food("고구마", "1개", 130, 25, 2, 3)}, 380, 35, 24, 16, 0.86},
	{[]Item{food("현미밥", "1공기", 300, 64, 6, 2), food("제육볶음", "1인분", 320, 10, 22, 20)}, 620, 74, 28, 22, 0.82},
	{[]Item{food("김치찌개", "1그릇", 230, 6, 18, 16), food("공기밥", "1공기", 310, 64, 6, 2)}, 540, 70, 24, 18, 0.79},
	{[]Item{food("그릭요거트", "1컵", 150, 11, 15, 4), food("바나나", "1개", 110, 27, 2, 1)}, 260, 38, 17, 5, 0.9},
}

// Mock은 API 키가 필요 없다. 같은 (첫) 사진이면 늘 같은 결과를 준다.
type Mock struct{}

// Model은 응답 헤더 X-AI-Model 값이다.
func (Mock) Model() string { return "mock" }

func (Mock) Analyze(_ context.Context, photos []Photo) (Result, error) {
	var image []byte
	if len(photos) > 0 {
		image = photos[0].Data
	}
	var h int32 = 1 // java.util.Arrays.hashCode(byte[])와 같은 해시
	for _, b := range image {
		h = 31*h + int32(int8(b))
	}
	k := int64(len(samples))
	i := ((int64(h) % k) + k) % k // floorMod
	r := samples[i]
	r.Items = append([]Item(nil), r.Items...) // 호출한 쪽이 고쳐도 표본이 바뀌지 않게
	return r, nil
}
