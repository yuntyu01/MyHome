// Package foods는 포장 식품을 바코드로 공공 식품 데이터에서 찾는다(핏두잉 internal/foods 이식):
//  1. 식품안전나라 C005(바코드제품정보): 바코드 → 제품명, 제조사, 품목제조보고번호
//  2. 공공데이터포털 식품의약품안전처_식품영양성분DB정보(FoodNtrCpntDbInfo03): 품목제조보고번호 → 영양성분
//
// 키가 둘 다 있어야 한다(FOODSAFETY_API_KEY, DATA_GO_KR_API_KEY). 없으면 조회를 끄고(503) 앱은 바코드만 남긴다.
// 두 키는 요청 주소에 들어가므로(공공 API 방식) 에러 메시지에 주소를 남기지 않는다(getJSON).
package foods

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	defaultFoodSafety = "http://openapi.foodsafetykorea.go.kr"
	defaultDataGoKr   = "https://apis.data.go.kr"
)

// ErrNotFound는 그 바코드의 제품이 없다는 뜻이다.
var ErrNotFound = errors.New("barcode not found")

// Nutrition은 한 번 먹는 양 기준 영양성분이다(소수 한 자리).
type Nutrition struct {
	Kcal    float64 `json:"kcal"`
	Carbs   float64 `json:"carbs"`
	Protein float64 `json:"protein"`
	Fat     float64 `json:"fat"`
}

// Product는 찾은 제품이다. 제품은 알지만 영양 데이터가 없으면 Nutrition이 nil이다.
type Product struct {
	Barcode   string     `json:"barcode"`
	Name      string     `json:"name"`
	Maker     string     `json:"maker"`
	ReportNo  string     `json:"reportNo"`
	Amount    string     `json:"amount"` // 영양값의 기준 양, 예: "1회 섭취참고량 30g"
	Nutrition *Nutrition `json:"nutrition"`
}

// Lookup은 바코드 조회 클라이언트다.
type Lookup struct {
	safetyKey, dataKey   string
	safetyBase, dataBase string
	http                 *http.Client
}

// New: base ""는 실제 서비스, 테스트는 가짜 서버를 가리킨다.
func New(safetyKey, dataKey, safetyBase, dataBase string) *Lookup {
	if safetyBase == "" {
		safetyBase = defaultFoodSafety
	}
	if dataBase == "" {
		dataBase = defaultDataGoKr
	}
	return &Lookup{safetyKey: safetyKey, dataKey: dataKey, safetyBase: strings.TrimRight(safetyBase, "/"),
		dataBase: strings.TrimRight(dataBase, "/"), http: &http.Client{Timeout: 8 * time.Second}}
}

// Enabled는 키가 둘 다 있는지다.
func (l *Lookup) Enabled() bool { return l != nil && l.safetyKey != "" && l.dataKey != "" }

func (l *Lookup) getJSON(ctx context.Context, u string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	resp, err := l.http.Do(req)
	if err != nil {
		// url.Error는 요청 주소(키 포함)를 담는다. 원인만 남긴다
		var ue *url.Error
		if errors.As(err, &ue) {
			return fmt.Errorf("요청 실패: %w", ue.Err)
		}
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("status %d", resp.StatusCode)
	}
	return json.NewDecoder(http.MaxBytesReader(nil, resp.Body, 2<<20)).Decode(out)
}

// Find는 바코드를 찾는다. 그런 제품이 없으면 ErrNotFound.
func (l *Lookup) Find(ctx context.Context, barcode string) (Product, error) {
	var c005 struct {
		C005 struct {
			Row []struct {
				Name     string `json:"PRDLST_NM"`
				Maker    string `json:"BSSH_NM"`
				ReportNo string `json:"PRDLST_REPORT_NO"`
				EndDate  string `json:"END_DT"`
			} `json:"row"`
			Result struct {
				Code string `json:"CODE"`
			} `json:"RESULT"`
		} `json:"C005"`
	}
	u := fmt.Sprintf("%s/api/%s/C005/json/1/5/BAR_CD=%s", l.safetyBase, url.PathEscape(l.safetyKey), url.PathEscape(barcode))
	if err := l.getJSON(ctx, u, &c005); err != nil {
		return Product{}, fmt.Errorf("C005: %w", err)
	}
	switch c005.C005.Result.Code {
	case "INFO-000":
	case "INFO-200":
		return Product{}, ErrNotFound
	default:
		return Product{}, fmt.Errorf("C005: %s", c005.C005.Result.Code)
	}
	if len(c005.C005.Row) == 0 {
		return Product{}, ErrNotFound
	}
	// 바코드는 재사용될 수 있다: 아직 생산 중인 제품을 고른다
	row := c005.C005.Row[0]
	for _, r := range c005.C005.Row {
		if r.EndDate == "" {
			row = r
			break
		}
	}
	p := Product{Barcode: barcode, Name: row.Name, Maker: row.Maker, ReportNo: row.ReportNo}
	if p.ReportNo == "" {
		return p, nil
	}
	n, amount, err := l.nutrients(ctx, p.ReportNo)
	if err != nil {
		return p, err
	}
	p.Nutrition, p.Amount = n, amount
	return p, nil
}

func (l *Lookup) nutrients(ctx context.Context, reportNo string) (*Nutrition, string, error) {
	v := url.Values{"serviceKey": {l.dataKey}, "type": {"json"}, "pageNo": {"1"}, "numOfRows": {"5"}, "ITEM_REPORT_NO": {reportNo}}
	var raw json.RawMessage
	if err := l.getJSON(ctx, l.dataBase+"/1471000/FoodNtrCpntDbInfo03/getFoodNtrCpntDbInq03?"+v.Encode(), &raw); err != nil {
		return nil, "", fmt.Errorf("nutrient DB: %w", err)
	}
	code, items, err := dataGoKrItems(raw)
	if err != nil {
		return nil, "", fmt.Errorf("nutrient DB: %w", err)
	}
	if code != "" && code != "00" && code != "0" {
		return nil, "", fmt.Errorf("nutrient DB: result %s", code)
	}
	for _, it := range items {
		n, amount, ok := perAmount(it)
		if ok {
			return n, amount, nil
		}
	}
	return nil, "", nil
}

// dataGoKrItems는 공공데이터포털 JSON 응답의 결과 코드와 행을 읽는다. 서비스마다 다르다: 봉투가 "response"로
// 감싸여 있을 수 있고, items는 배열, {"item": [...]}, {"item": {...}} 중 하나다.
func dataGoKrItems(raw json.RawMessage) (string, []map[string]any, error) {
	var env struct {
		Response *json.RawMessage `json:"response"`
		Header   struct {
			ResultCode string `json:"resultCode"`
		} `json:"header"`
		Body struct {
			Items json.RawMessage `json:"items"`
		} `json:"body"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		return "", nil, err
	}
	if env.Response != nil {
		return dataGoKrItems(*env.Response)
	}
	var list []map[string]any
	if len(env.Body.Items) == 0 || string(env.Body.Items) == "null" || string(env.Body.Items) == `""` {
		return env.Header.ResultCode, nil, nil
	}
	if json.Unmarshal(env.Body.Items, &list) == nil {
		return env.Header.ResultCode, list, nil
	}
	var wrap struct {
		Item json.RawMessage `json:"item"`
	}
	if err := json.Unmarshal(env.Body.Items, &wrap); err != nil {
		return "", nil, err
	}
	if json.Unmarshal(wrap.Item, &list) == nil {
		return env.Header.ResultCode, list, nil
	}
	var one map[string]any
	if err := json.Unmarshal(wrap.Item, &one); err != nil {
		return "", nil, err
	}
	return env.Header.ResultCode, []map[string]any{one}, nil
}

var qty = regexp.MustCompile(`([\d.]+)\s*(g|ml|mL|ML|G)`)

func parseQty(s string) (float64, string, bool) {
	m := qty.FindStringSubmatch(s)
	if m == nil {
		return 0, "", false
	}
	v, err := strconv.ParseFloat(m[1], 64)
	if err != nil || v <= 0 {
		return 0, "", false
	}
	return v, strings.ToLower(m[2]), true
}

func num(it map[string]any, k string) (float64, bool) {
	switch v := it[k].(type) {
	case float64:
		return v, true
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(strings.ReplaceAll(v, ",", "")), 64)
		return f, err == nil
	}
	return 0, false
}

func str(it map[string]any, k string) string {
	s, _ := it[k].(string)
	return strings.TrimSpace(s)
}

// perAmount는 DB 값(SERVING_SIZE 기준, 보통 100g)을 한 번 먹는 양으로 바꾼다:
// 1회 섭취참고량이 있으면 그것, 없으면 포장 전체(식품중량), 그것도 없으면 DB 기준량 그대로.
func perAmount(it map[string]any) (*Nutrition, string, bool) {
	kcal, ok1 := num(it, "AMT_NUM1")
	protein, ok2 := num(it, "AMT_NUM3")
	fat, ok3 := num(it, "AMT_NUM4")
	carbs, ok4 := num(it, "AMT_NUM6")
	if !ok1 || !ok2 || !ok3 || !ok4 {
		return nil, "", false
	}
	basis := str(it, "SERVING_SIZE")
	base, unit, ok := parseQty(basis)
	if !ok {
		return nil, "", false
	}
	scale, amount := 1.0, basis+" 기준"
	for _, c := range []struct{ key, label string }{{"NUTRI_AMOUNT_SERVING", "1회 섭취참고량"}, {"Z10500", "1개"}} {
		if v, u, ok := parseQty(str(it, c.key)); ok && u == unit {
			scale, amount = v/base, fmt.Sprintf("%s %s%s", c.label, trimNum(v), unit)
			break
		}
	}
	r := func(x float64) float64 { return math.Round(x*scale*10) / 10 }
	return &Nutrition{Kcal: r(kcal), Carbs: r(carbs), Protein: r(protein), Fat: r(fat)}, amount, true
}

func trimNum(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }
