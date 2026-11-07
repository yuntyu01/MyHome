package toss

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strconv"
)

// maxBody는 응답 본문 상한이다. 주문 100건 한 페이지도 한참 밑이다.
const maxBody = 8 << 20

// reqOpts의 영값이 기본(토큰·계좌 헤더·연결 재시도·result 봉투 전부 켬)이다.
type reqOpts struct {
	query      url.Values
	json       any
	form       url.Values
	noAccount  bool // X-Tossinvest-Account를 싣지 않는다 (계좌 목록·토큰 발급)
	noAuth     bool // Bearer 토큰을 싣지 않는다 (토큰 발급)
	noRetry    bool // 연결이 끊겨도 다시 보내지 않는다 (주문)
	noEnvelope bool // {"result": ...} 봉투가 없다 (토큰 발급)
}

type reply struct {
	status int
	body   []byte
}

// request는 요청 1회 → result 페이로드다. 401이면 토큰을 버리고 1회만 재발급한다.
//
// 🔴 토큰 발급만 봉투가 없다. POST /oauth2/token은 표준 OAuth2 응답이라 access_token이 최상위에 온다.
// 나머지 API는 전부 {"result": ...}로 감싼다 — 한 규칙으로 읽으면 토큰 발급이 "규격 변경 의심"으로 죽는다.
func (c *Client) request(ctx context.Context, method, path string, o reqOpts) (json.RawMessage, error) {
	where := method + " " + path
	var payload []byte
	contentType := ""
	switch {
	case o.json != nil:
		b, err := json.Marshal(o.json)
		if err != nil {
			return nil, fmt.Errorf("%s 본문 직렬화 실패: %w", where, err)
		}
		payload, contentType = b, "application/json"
	case o.form != nil:
		payload, contentType = []byte(o.form.Encode()), "application/x-www-form-urlencoded"
	}
	target := c.baseURL() + path
	if len(o.query) > 0 {
		target += "?" + o.query.Encode()
	}

	for attempt := range 2 {
		h := http.Header{}
		h.Set("Accept", "application/json")
		if !o.noAuth {
			tok, err := c.Token(ctx)
			if err != nil {
				return nil, err
			}
			h.Set("Authorization", "Bearer "+tok)
		}
		if !o.noAccount {
			seq, err := c.AccountSeq(ctx)
			if err != nil {
				return nil, err
			}
			h.Set(AccountHeader, strconv.FormatInt(seq, 10))
		}
		if contentType != "" {
			h.Set("Content-Type", contentType)
		}

		send := func() (reply, error) { return c.send(ctx, method, target, h, payload) }
		var r reply
		var err error
		if o.noRetry {
			r, err = send()
		} else {
			r, err = c.retry(ctx, where, send)
		}
		if err != nil {
			return nil, err
		}

		if r.status == http.StatusUnauthorized && attempt == 0 && !o.noAuth {
			c.warn(fmt.Sprintf("HTTP %d — 토큰 폐기 후 1회 재발급", r.status))
			c.dropToken()
			continue
		}
		var obj map[string]json.RawMessage
		isObj := json.Unmarshal(r.body, &obj) == nil && obj != nil
		if r.status >= 400 {
			return nil, c.apiError(r.status, obj, where)
		}
		if o.noEnvelope {
			if !isObj {
				return nil, &Error{Message: fmt.Sprintf("응답 형식 이상 (%s): %s", where, jsonKind(r.body)), Status: r.status}
			}
			return r.body, nil
		}
		res, ok := obj["result"]
		if !isObj || !ok {
			keys := jsonKind(r.body)
			if isObj {
				keys = fmt.Sprint(sortedKeys(obj))
			}
			return nil, &Error{Message: fmt.Sprintf("응답에 result 없음 (%s) — 규격 변경 의심. 실제 키=%s", where, keys),
				Status: r.status}
		}
		return res, nil
	}
	return nil, &Error{Message: "재시도 경로 이상"} // 도달 불가
}

func (c *Client) send(ctx context.Context, method, target string, h http.Header, payload []byte) (reply, error) {
	var body io.Reader
	if payload != nil {
		body = bytes.NewReader(payload) // 시도마다 새로 읽는다
	}
	req, err := http.NewRequestWithContext(ctx, method, target, body)
	if err != nil {
		return reply{}, err
	}
	req.Header = h.Clone()
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return reply{}, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return reply{}, err // 본문을 읽다 끊긴 것도 연결 끊김이다
	}
	return reply{status: resp.StatusCode, body: b}, nil
}

// retry는 연결이 끊긴 요청만 다시 보낸다 (3회, 1초·3초 간격). HTTP 오류 응답은 다시 보내지 않는다.
func (c *Client) retry(ctx context.Context, what string, call func() (reply, error)) (reply, error) {
	var last error
	for attempt := range connRetries {
		r, err := call()
		if err == nil {
			return r, nil
		}
		if !isConnError(ctx, err) {
			return reply{}, err
		}
		last = err
		if attempt+1 < connRetries {
			wait := connBackoff[min(attempt, len(connBackoff)-1)]
			c.warn(fmt.Sprintf("%s 연결 끊김 (%s) — %.0f초 뒤 재시도", what, connKind(err), wait.Seconds()))
			if err := c.sleep(ctx, wait); err != nil {
				return reply{}, err
			}
		}
	}
	return reply{}, &Error{Message: fmt.Sprintf("%s 실패 — 연결이 %d회 끊겼다", what, connRetries), Err: last}
}

// isConnError는 다시 보내면 풀릴 수 있는 실패인가다 — 연결 거부·끊김·타임아웃·DNS.
// 호출부가 ctx를 끊은 것, 인증서 검증 실패, 잘못된 주소는 다시 보내도 같으므로 아니다.
func isConnError(ctx context.Context, err error) bool {
	if ctx.Err() != nil {
		return false
	}
	var ue *url.Error
	if errors.As(err, &ue) {
		err = ue.Err // url.Error 자체도 net.Error라 벗겨야 판별이 된다
	}
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return true
	}
	var ne net.Error
	return errors.As(err, &ne)
}

func connKind(err error) string {
	var ue *url.Error
	if errors.As(err, &ue) {
		err = ue.Err
	}
	var ne net.Error
	switch {
	case errors.As(err, &ne) && ne.Timeout():
		return "timeout"
	case errors.Is(err, io.EOF), errors.Is(err, io.ErrUnexpectedEOF):
		return "EOF"
	}
	return fmt.Sprintf("%T", err)
}

// apiError는 오류 응답 → 에러다. 본문 전체를 싣지 않는다 (계좌 정보가 섞인다).
func (c *Client) apiError(status int, body map[string]json.RawMessage, where string) error {
	var e struct {
		Code    any             `json:"code"`
		Message any             `json:"message"`
		Data    json.RawMessage `json:"data"`
	}
	_ = json.Unmarshal(body["error"], &e)
	code, msg := orText(e.Code), orText(e.Message)

	hint := ""
	switch {
	case slices.Contains(fundsErrorCodes, code):
		hint = "\n  → 주문가능금액 부족. 원화 입금과 환전 여부를 확인할 것 (주문 유형을 바꿔도 풀리지 않는다)"
	case slices.Contains(hoursErrorCodes, code):
		var data struct {
			RetryAfterAt any `json:"retryAfterAt"`
		}
		_ = json.Unmarshal(e.Data, &data)
		after := orText(data.RetryAfterAt)
		if after == "" {
			after = "미상"
		}
		hint = "\n  → 주문 접수 가능 시간이 아니다. 재시도 가능 시각 " + after
	case code == "prerequisite-required":
		hint = "\n  → 약관 동의·위험 고지 등 사전 자격 요건이 남았다. WTS에서 처리할 것"
	case status == http.StatusForbidden || code == "edge-blocked":
		// 🔴 이 실패는 자격증명 문제로 오해하기 쉽다. 토스는 등록된 IP에서만 호출을 받는다.
		// 깃허브 호스티드 러너는 IP가 매번 바뀌므로 여기서 막힌다 — 고정 IP가 있는 곳에서 돌려야 한다
		hint = "\n  → 허용 IP 목록에 없는 곳에서 호출했다. WTS → 설정 → Open API → 허용 IP 관리에 " +
			"이 호스트의 공인 IP를 등록할 것 (자격증명 문제가 아니다)"
	}
	return &Error{
		Message: fmt.Sprintf("토스 오류 (%s, HTTP %d) code=%q msg=%q%s", where, status, code, c.Config.Scrub(msg), hint),
		Code:    code,
		Status:  status,
	}
}

func sortedKeys(m map[string]json.RawMessage) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

// jsonKind는 객체가 아닌 응답의 모양이다. 값은 싣지 않는다.
func jsonKind(b []byte) string {
	t := bytes.TrimSpace(b)
	if len(t) == 0 || !json.Valid(t) {
		return "JSON 아님"
	}
	switch t[0] {
	case '{':
		return "object"
	case '[':
		return "array"
	case '"':
		return "string"
	case 't', 'f':
		return "bool"
	case 'n':
		return "null"
	}
	return "number"
}
