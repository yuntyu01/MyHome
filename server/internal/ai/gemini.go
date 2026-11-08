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
	timeout          time.Duration // 한 번 부를 때 제한
	retryWait        time.Duration // 5xx·시간 초과·연결 실패 뒤 한 번 더 부르기 전에 기다리는 시간
}

// 한 번 부를 때 25초, 실패하면 1초 쉬고 한 번 더. 끼니 분석 전체(health.AnalyzeBudget 55초)와 서버 WriteTimeout(60초) 안에 끝나게 잡았다.
const (
	callTimeout = 25 * time.Second
	retryWait   = time.Second
)

// StatusError는 Gemini가 2xx가 아닌 상태 코드로 답한 것이다. 402는 선불 크레딧이 떨어졌다는 뜻이다(2026-11 운영에서 본 응답).
type StatusError struct{ Code int }

func (e *StatusError) Error() string { return fmt.Sprintf("gemini status %d", e.Code) }

// NoCredit는 AI 크레딧(선불 잔액)이 없어서 실패했는지다. 다시 불러도 소용없다.
func NoCredit(err error) bool {
	var se *StatusError
	return errors.As(err, &se) && se.Code == http.StatusPaymentRequired
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
	return &Gemini{key: key, model: model, base: geminiBase, http: client, timeout: callTimeout, retryWait: retryWait}, nil
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

// GenerateJSON은 parts와 응답 스키마로 호출하고 첫 후보의 텍스트(JSON)를 돌려준다. 5xx·시간 초과·연결 실패면
// retryWait 뒤에 한 번만 더 부른다(4xx는 다시 불러도 같아서 바로 실패, 402 크레딧 없음도 여기).
// 실패하면 ErrFailed를 감싼 에러다. 응답 내용 검증은 부르는 쪽이 한다.
func (g *Gemini) GenerateJSON(ctx context.Context, parts []Part, schema Schema) (string, error) {
	payload, err := json.Marshal(requestBody(parts, schema))
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrFailed, err)
	}
	text, err := g.call(ctx, payload)
	if err != nil && retryable(err) && ctx.Err() == nil {
		select {
		case <-ctx.Done():
		case <-time.After(g.retryWait):
			text, err = g.call(ctx, payload)
		}
	}
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrFailed, err)
	}
	return text, nil
}

// retryable은 한 번 더 불러 볼 만한 실패인지다: 5xx, 연결·시간 초과(상태 코드를 못 받음). 4xx와 응답 해석 실패는 아니다.
func retryable(err error) bool {
	var se *StatusError
	if errors.As(err, &se) {
		return se.Code >= 500
	}
	var te *transportError
	return errors.As(err, &te)
}

// transportError는 응답을 받지 못한 실패다(연결 실패, 한 번 부르기 제한 시간 초과).
type transportError struct{ err error }

func (e *transportError) Error() string { return "gemini 요청 실패: " + e.err.Error() }
func (e *transportError) Unwrap() error { return e.err }

func (g *Gemini) call(ctx context.Context, payload []byte) (string, error) {
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
			err = ue.Err // 주소(키는 헤더에만 있다)를 빼고 원인만
		}
		return "", &transportError{err}
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", &transportError{err}
	}
	if resp.StatusCode/100 != 2 {
		return "", &StatusError{resp.StatusCode}
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
