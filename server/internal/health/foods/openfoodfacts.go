package foods

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"strings"
)

// Open Food Facts(키 없음) 예비 조회 (2026-11-08). 식약처 키(FOODSAFETY_API_KEY, DATA_GO_KR_API_KEY)가 없거나
// 거기서 못 찾았을 때 쓴다. 보내는 것은 바코드 번호뿐이다. 한국 제품은 덜 있지만 수입 과자·음료는 꽤 있다.

const defaultOFF = "https://world.openfoodfacts.org"

// offUserAgent: Open Food Facts는 앱 이름이 담긴 User-Agent를 요구한다(연락처는 넣지 않는다).
const offUserAgent = "MyHome/0.2 (personal household app)"

// WithOFF는 Open Food Facts 예비 조회를 켠다. base ""는 실제 서비스.
func (l *Lookup) WithOFF(base string) *Lookup {
	if base == "" {
		base = defaultOFF
	}
	l.offBase = strings.TrimRight(base, "/")
	return l
}

type offProduct struct {
	Name    string         `json:"product_name"`
	NameKo  string         `json:"product_name_ko"`
	Brands  string         `json:"brands"`
	Serving string         `json:"serving_size"`
	RawNutr map[string]any `json:"nutriments"`
}

// offFind는 Open Food Facts에서 바코드를 찾는다. 없으면 ErrNotFound.
func (l *Lookup) offFind(ctx context.Context, barcode string) (Product, error) {
	u := fmt.Sprintf("%s/api/v2/product/%s.json?fields=product_name,product_name_ko,brands,serving_size,nutriments", l.offBase, url.PathEscape(barcode))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return Product{}, err
	}
	req.Header.Set("User-Agent", offUserAgent)
	resp, err := l.http.Do(req)
	if err != nil {
		return Product{}, fmt.Errorf("Open Food Facts: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return Product{}, ErrNotFound
	}
	if resp.StatusCode != http.StatusOK {
		return Product{}, fmt.Errorf("Open Food Facts: status %d", resp.StatusCode)
	}
	var body struct {
		Status  int        `json:"status"`
		Product offProduct `json:"product"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(nil, resp.Body, 2<<20)).Decode(&body); err != nil {
		return Product{}, fmt.Errorf("Open Food Facts: %w", err)
	}
	pr := body.Product
	name := strings.TrimSpace(pr.NameKo)
	if name == "" {
		name = strings.TrimSpace(pr.Name)
	}
	if body.Status != 1 || name == "" {
		return Product{}, ErrNotFound
	}
	p := Product{Barcode: barcode, Name: name, Maker: firstBrand(pr.Brands)}
	p.Nutrition, p.Amount = offNutrition(pr.RawNutr, pr.Serving)
	return p, nil
}

// offNutrition은 1회 제공량 값이 있으면 그것을, 없으면 100g(ml)당 값을 쓴다. 열량이 없으면 nil.
func offNutrition(n map[string]any, serving string) (*Nutrition, string) {
	num := func(k string) (float64, bool) {
		switch v := n[k].(type) {
		case float64:
			return v, true
		case string:
			var f float64
			if _, err := fmt.Sscan(v, &f); err == nil {
				return f, true
			}
		}
		return 0, false
	}
	r := func(x float64) float64 { return math.Round(x*10) / 10 }
	for _, per := range []string{"_serving", "_100g"} {
		kcal, ok := num("energy-kcal" + per)
		if !ok {
			continue
		}
		c, _ := num("carbohydrates" + per)
		p, _ := num("proteins" + per)
		f, _ := num("fat" + per)
		amount := "100g(ml)당"
		if per == "_serving" {
			amount = "1회 제공량"
			if s := strings.TrimSpace(serving); s != "" {
				amount += " " + s
			}
		}
		return &Nutrition{Kcal: r(kcal), Carbs: r(c), Protein: r(p), Fat: r(f)}, amount
	}
	return nil, ""
}

func firstBrand(s string) string {
	b, _, _ := strings.Cut(s, ",")
	return strings.TrimSpace(b)
}

// findWithFallback은 식약처(키가 있으면) → Open Food Facts(켜져 있으면) 순서로 찾는다.
// 식약처에서 제품만 알고 영양성분이 없으면 Open Food Facts의 영양성분으로 채운다.
func (l *Lookup) findWithFallback(ctx context.Context, barcode string) (Product, error) {
	var kr Product
	var krErr error = ErrNotFound
	if l.Enabled() {
		kr, krErr = l.Find(ctx, barcode)
		if krErr == nil && kr.Nutrition != nil {
			return kr, nil
		}
	}
	if l.offBase == "" {
		return kr, krErr
	}
	off, offErr := l.offFind(ctx, barcode)
	switch {
	case offErr == nil && kr.Name != "":
		kr.Nutrition, kr.Amount = off.Nutrition, off.Amount
		return kr, nil
	case offErr == nil:
		return off, nil
	case kr.Name != "":
		return kr, krErr
	case errors.Is(krErr, ErrNotFound) && errors.Is(offErr, ErrNotFound):
		return Product{}, ErrNotFound
	case !errors.Is(krErr, ErrNotFound):
		return Product{}, krErr
	default:
		return Product{}, offErr
	}
}

// Available은 바코드 조회를 할 수 있는지다(식약처 키 또는 Open Food Facts).
func (l *Lookup) Available() bool { return l != nil && (l.Enabled() || l.offBase != "") }

// Search는 서버 핸들러가 부르는 조회다(식약처 → Open Food Facts).
func (l *Lookup) Search(ctx context.Context, barcode string) (Product, error) {
	return l.findWithFallback(ctx, barcode)
}
