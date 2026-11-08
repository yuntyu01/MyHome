package ai

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// DefaultModel은 GEMINI_MODEL을 비웠을 때 쓰는 모델이다(핏두잉 기본값과 같다).
const DefaultModel = "gemini-3.5-flash-lite"

const geminiBase = "https://generativelanguage.googleapis.com/v1beta"

// Gemini는 generateContent를 고정 응답 스키마(responseSchema)로 부른다. API 키는 서버에만 있고 헤더로만 보낸다
// (주소에 넣지 않아서 에러 메시지·로그에 남지 않는다).
type Gemini struct {
	key, model, base string
	http             *http.Client
	timeout          time.Duration
}

// NewGemini는 키가 없으면 에러다.
func NewGemini(key, model string, client *http.Client) (*Gemini, error) {
	if key == "" {
		return nil, errors.New("GEMINI_API_KEY가 필요해요")
	}
	if model == "" {
		model = DefaultModel
	}
	if client == nil {
		client = &http.Client{}
	}
	return &Gemini{key: key, model: model, base: geminiBase, http: client, timeout: 30 * time.Second}, nil
}

// WithBase는 API 주소를 바꾼다(테스트).
func (g *Gemini) WithBase(base string) *Gemini { g.base = base; return g }

// Model은 쓰는 모델 이름이다.
func (g *Gemini) Model() string { return g.model }

// Part는 요청 한 조각(텍스트 또는 인라인 데이터)이다.
type Part map[string]any

// Text는 글 조각이다.
func Text(s string) Part { return Part{"text": s} }

// Inline은 사진 같은 이진 데이터 조각이다(base64로 실어 보낸다).
func Inline(mimeType string, data []byte) Part {
	return Part{"inline_data": map[string]any{"mime_type": mimeType, "data": base64.StdEncoding.EncodeToString(data)}}
}

// Schema는 Gemini responseSchema(OpenAPI 부분집합)다. 아래 도우미로 만든다.
type Schema map[string]any

// Object는 객체 스키마다.
func Object(props map[string]Schema, required ...string) Schema {
	return Schema{"type": "OBJECT", "properties": props, "required": required}
}

// Array는 배열 스키마다.
func Array(item Schema) Schema { return Schema{"type": "ARRAY", "items": item} }

// String, Integer, Number, Boolean은 값 스키마다.
func String() Schema  { return Schema{"type": "STRING"} }
func Integer() Schema { return Schema{"type": "INTEGER"} }
func Number() Schema  { return Schema{"type": "NUMBER"} }
func Boolean() Schema { return Schema{"type": "BOOLEAN"} }

// Enum은 정해진 문자열 중 하나만 받는 스키마다(분류 이름 고르기 등).
func Enum(values ...string) Schema { return Schema{"type": "STRING", "enum": values} }

func requestBody(parts []Part, schema Schema) map[string]any {
	return map[string]any{
		"contents":         []any{map[string]any{"parts": parts}},
		"generationConfig": map[string]any{"responseMimeType": "application/json", "responseSchema": schema},
	}
}

// GenerateJSON은 parts와 응답 스키마로 한 번 호출하고 첫 후보의 텍스트(JSON)를 돌려준다.
// 실패하면 ErrFailed를 감싼 에러다. 응답 내용 검증은 부르는 쪽이 한다.
func (g *Gemini) GenerateJSON(ctx context.Context, parts []Part, schema Schema) (string, error) {
	text, err := g.call(ctx, requestBody(parts, schema))
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrFailed, err)
	}
	return text, nil
}

func (g *Gemini) call(ctx context.Context, body map[string]any) (string, error) {
	payload, err := json.Marshal(body)
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(ctx, g.timeout)
	defer cancel()
	u := fmt.Sprintf("%s/models/%s:generateContent", g.base, url.PathEscape(g.model))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(payload))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-goog-api-key", g.key)
	resp, err := g.http.Do(req)
	if err != nil {
		var ue *url.Error
		if errors.As(err, &ue) {
			return "", fmt.Errorf("gemini 요청 실패: %w", ue.Err)
		}
		return "", err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", err
	}
	if resp.StatusCode/100 != 2 {
		return "", fmt.Errorf("gemini status %d", resp.StatusCode)
	}
	var out struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", err
	}
	if len(out.Candidates) == 0 || len(out.Candidates[0].Content.Parts) == 0 {
		return "", errors.New("gemini: 빈 응답")
	}
	return out.Candidates[0].Content.Parts[0].Text, nil
}
