package toss

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/big"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/yuntyu01/MyHome/server/internal/invest/broker"
)

// 응답 값은 문서상 문자열 십진수지만 숫자로 와도 받는다. 아래 도우미가 TQ(Python)의
// str()·float()·`x or 0` 규칙을 옮긴 것이다. 숫자는 json.Number로 읽어 자릿수를 잃지 않는다.

func decode(raw []byte, where string, out any) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err := dec.Decode(out); err != nil {
		detail := ""
		var te *json.UnmarshalTypeError
		if errors.As(err, &te) && te.Field != "" {
			detail = " (필드 " + te.Field + ")" // 필드 이름만. 값은 계좌 정보라 싣지 않는다
		}
		return &Error{Message: fmt.Sprintf("응답 형식 이상 (%s)%s — 규격 변경 의심", where, detail)}
	}
	return nil
}

// rowsOf는 JSON 배열 → 객체 행이다. 비어 있으면 행 없음이다.
func rowsOf(v any, where string) ([]map[string]any, error) {
	if !truthy(v) {
		return nil, nil
	}
	list, ok := v.([]any)
	if !ok {
		return nil, &Error{Message: fmt.Sprintf("응답 형식 이상 (%s) — 목록이 아니다", where)}
	}
	rows := make([]map[string]any, 0, len(list))
	for _, x := range list {
		m, ok := x.(map[string]any)
		if !ok {
			return nil, &Error{Message: fmt.Sprintf("응답 형식 이상 (%s) — 행이 객체가 아니다", where)}
		}
		rows = append(rows, m)
	}
	return rows, nil
}

// truthy는 파이썬의 참거짓이다 (None·""·0·빈 목록은 거짓).
func truthy(v any) bool {
	switch x := v.(type) {
	case nil:
		return false
	case bool:
		return x
	case string:
		return x != ""
	case json.Number:
		f, err := x.Float64()
		return err != nil || f != 0
	case float64:
		return x != 0
	case []any:
		return len(x) > 0
	case map[string]any:
		return len(x) > 0
	}
	return true
}

// text는 파이썬 str()에 가깝다. None은 ""다.
func text(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case json.Number:
		return x.String()
	}
	return fmt.Sprint(v)
}

// orText는 str(v or "")다.
func orText(v any) string {
	if !truthy(v) {
		return ""
	}
	return text(v)
}

// num은 문자열 십진수 → float64다. 실패를 0으로 강등하지 않는다.
func num(v any, what string) (float64, error) {
	var s string
	switch x := v.(type) {
	case string:
		s = x
	case json.Number:
		s = x.String()
	case float64:
		s = strconv.FormatFloat(x, 'g', -1, 64)
	default:
		return 0, numError(what)
	}
	f, err := strconv.ParseFloat(strings.TrimSpace(strings.ReplaceAll(s, ",", "")), 64)
	if err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
		return 0, numError(what)
	}
	return f, nil
}

// numOr0은 _num(v or 0)이다 — 비어 있으면 0, 값이 있는데 못 읽으면 에러.
func numOr0(v any, what string) (float64, error) {
	if !truthy(v) {
		return 0, nil
	}
	return num(v, what)
}

func numError(what string) error {
	return &Error{Message: what + "를 숫자로 읽을 수 없다 (값은 계좌 정보라 싣지 않는다)"}
}

func toInt64(v any) (int64, error) {
	switch x := v.(type) {
	case json.Number:
		if n, err := x.Int64(); err == nil {
			return n, nil
		}
		f, err := x.Float64()
		return int64(f), err
	case string:
		return strconv.ParseInt(strings.TrimSpace(x), 10, 64)
	}
	return 0, errors.New("정수가 아니다")
}

// decimalPrec는 파이썬 decimal 기본 정밀도다. quantize 결과가 이보다 길면 InvalidOperation이다.
const decimalPrec = 28

// formatDecimal은 수량·가격을 전문에 실을 문자열로 만든다. float를 그냥 찍으면 1e+03이 전문에 실린다.
//
// 파이썬 format(Decimal(str(v)).quantize(10**-places, ROUND_HALF_UP).normalize(), "f")와 같다 —
// 최단 표기(str(float))에서 출발해 반올림하므로 2.675는 2.68이 된다. 0 이하는 거부한다.
func formatDecimal(v float64, places int, what string) (string, error) {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return "", fmt.Errorf("%s 형식 이상: %s", what, floatText(v))
	}
	s := strconv.FormatFloat(v, 'f', -1, 64)
	neg := strings.HasPrefix(s, "-")
	whole, frac, _ := strings.Cut(strings.TrimPrefix(s, "-"), ".")
	coef, ok := new(big.Int).SetString(whole+frac, 10)
	if !ok {
		return "", fmt.Errorf("%s 형식 이상: %s", what, floatText(v))
	}
	if scale := len(frac); scale <= places {
		coef.Mul(coef, pow10(places-scale))
	} else {
		d := pow10(scale - places)
		r := new(big.Int)
		coef.QuoRem(coef, d, r)
		if r.Lsh(r, 1).Cmp(d) >= 0 { // ROUND_HALF_UP: 절반이면 0에서 먼 쪽
			coef.Add(coef, big.NewInt(1))
		}
	}
	digits := coef.String()
	if len(digits) > decimalPrec {
		return "", fmt.Errorf("%s 형식 이상: %s", what, floatText(v))
	}
	if neg || coef.Sign() == 0 {
		return "", fmt.Errorf("%s는 0보다 커야 한다: %s", what, floatText(v))
	}
	if places == 0 {
		return digits, nil
	}
	if len(digits) <= places {
		digits = strings.Repeat("0", places-len(digits)+1) + digits
	}
	whole, frac = digits[:len(digits)-places], strings.TrimRight(digits[len(digits)-places:], "0")
	if frac == "" {
		return whole, nil
	}
	return whole + "." + frac, nil
}

func pow10(n int) *big.Int {
	return new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(n)), nil)
}

// ymd는 조회 구간 형식이다 — format: date, YYYY-MM-DD (KST 기준).
func ymd(t time.Time) string { return t.In(broker.KST).Format("2006-01-02") }

// money는 파이썬 f"{v:,.2f}"다.
func money(v float64) string {
	s := strconv.FormatFloat(math.Abs(v), 'f', 2, 64)
	whole, frac, _ := strings.Cut(s, ".")
	var b strings.Builder
	if math.Signbit(v) {
		b.WriteByte('-')
	}
	for i := range len(whole) {
		if i > 0 && (len(whole)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteByte(whole[i])
	}
	return b.String() + "." + frac
}

func floatText(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }

func onlyDigits(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// maskAccount는 앞 4자 + ****다.
func maskAccount(account string) string {
	r := []rune(account)
	return string(r[:min(4, len(r))]) + "****"
}

func utf8Len(s string) int { return utf8.RuneCountInString(s) }
