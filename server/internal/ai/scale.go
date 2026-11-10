package ai

import (
	"context"
	"encoding/json"
	"fmt"
)

// 몸무게 저울 사진 읽기(사용자 요청, 2026-11-10). 화면 숫자만 읽고 사진은 남기지 않는다.
const scalePrompt = `체중계(몸무게 저울) 화면 사진이다. 화면에 표시된 몸무게 숫자를 읽어라.
단위가 lb(파운드)면 kg로 바꿔라(1lb = 0.45359237kg). 소수점 한 자리까지 답해라.
체중계가 아니거나 숫자를 읽을 수 없으면 weightKg를 0으로, confidence를 0으로 답해라.
confidence는 읽은 숫자가 맞을 가능성(0~1)이다.`

// Scale은 저울 사진에서 읽은 몸무게다. WeightKg가 0이면 읽지 못한 것이다.
type Scale struct {
	WeightKg   float64 `json:"weightKg"`
	Confidence float64 `json:"confidence"`
}

// ScaleReader는 저울 사진을 읽는다(FoodAnalyzer를 만든 쪽이 함께 구현한다: gemini, mock).
type ScaleReader interface {
	ReadScale(ctx context.Context, photo Photo) (Scale, error)
}

func scaleSchema() Schema {
	return Object(map[string]Schema{"weightKg": Number(), "confidence": Number()}, "weightKg", "confidence")
}

// ReadScale은 ScaleReader 구현이다. 20~400kg 밖이면 읽지 못한 것으로 본다.
func (g *Gemini) ReadScale(ctx context.Context, photo Photo) (Scale, error) {
	text, err := g.GenerateJSON(ctx, []Part{Text(scalePrompt), Inline(photo.ContentType, photo.Data)}, scaleSchema())
	if err != nil {
		return Scale{}, err
	}
	s, err := parseScale(text)
	if err != nil {
		return Scale{}, fmt.Errorf("%w: %w", ErrFailed, err)
	}
	return s, nil
}

func parseScale(text string) (Scale, error) {
	var s Scale
	if err := json.Unmarshal([]byte(text), &s); err != nil {
		return Scale{}, err
	}
	if s.WeightKg < 20 || s.WeightKg > 400 {
		return Scale{}, nil // 못 읽음
	}
	s.WeightKg = float64(int64(s.WeightKg*10+0.5)) / 10
	s.Confidence = min(max(s.Confidence, 0), 1)
	return s, nil
}

// ReadScale(mock)은 키 없이 늘 같은 값을 준다(로컬·테스트용).
func (Mock) ReadScale(_ context.Context, photo Photo) (Scale, error) {
	if len(photo.Data) == 0 {
		return Scale{}, nil
	}
	return Scale{WeightKg: 72.4, Confidence: 0.9}, nil
}
