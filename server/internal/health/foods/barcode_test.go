package foods

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fakeServices는 식품안전나라 C005와 공공데이터포털 영양성분 DB처럼 답한다. nutrientBody는 두 번째 서비스의 JSON이다
// (서비스마다 모양이 다르다, dataGoKrItems 참고).
func fakeServices(t *testing.T, c005 string, nutrientBody string) *Lookup {
	t.Helper()
	safety := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/api/safety-key/C005/json/1/5/BAR_CD=") {
			t.Errorf("C005 path %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(c005))
	}))
	data := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if r.URL.Path != "/1471000/FoodNtrCpntDbInfo03/getFoodNtrCpntDbInq03" || q.Get("serviceKey") != "data-key" ||
			q.Get("ITEM_REPORT_NO") != "20210123456789" || q.Get("type") != "json" {
			t.Errorf("nutrient request %s?%s", r.URL.Path, r.URL.RawQuery)
		}
		_, _ = w.Write([]byte(nutrientBody))
	}))
	t.Cleanup(safety.Close)
	t.Cleanup(data.Close)
	return New("safety-key", "data-key", safety.URL, data.URL)
}

const c005Found = `{"C005":{"total_count":"2","row":[
 {"PRDLST_NM":"단종된 옛 과자","BSSH_NM":"옛회사","PRDLST_REPORT_NO":"19990000000000","END_DT":"20200101","BAR_CD":"8801234567890"},
 {"PRDLST_NM":"바삭칩 오리지널","BSSH_NM":"맛있는제과","PRDLST_REPORT_NO":"20210123456789","END_DT":"","BAR_CD":"8801234567890"}],
 "RESULT":{"CODE":"INFO-000","MSG":"정상"}}}`

func item(extra string) string {
	return `{"FOOD_NM_KR":"바삭칩","SERVING_SIZE":"100g","AMT_NUM1":"500","AMT_NUM3":"5","AMT_NUM4":"25","AMT_NUM6":"60"` + extra + `}`
}

func TestBarcodeLookupScalesToOneSitting(t *testing.T) {
	// per 100g: 500 kcal, carbs 60, protein 5, fat 25. 1회 섭취참고량 30g -> x0.3
	l := fakeServices(t, c005Found, `{"header":{"resultCode":"00"},"body":{"items":[`+item(`,"NUTRI_AMOUNT_SERVING":"30g","Z10500":"120g"`)+`]}}`)
	p, err := l.Find(context.Background(), "8801234567890")
	if err != nil {
		t.Fatal(err)
	}
	if p.Name != "바삭칩 오리지널" || p.Maker != "맛있는제과" { // the product still in production wins
		t.Fatalf("product %+v", p)
	}
	if p.Amount != "1회 섭취참고량 30g" || *p.Nutrition != (Nutrition{Kcal: 150, Carbs: 18, Protein: 1.5, Fat: 7.5}) {
		t.Fatalf("nutrition %q %+v", p.Amount, *p.Nutrition)
	}
	r := ResultOf(p)
	if r.Kcal != 150 || r.Carbs != 18 || r.Protein != 2 || r.Fat != 8 || r.Items[0].Amount != "1회 섭취참고량 30g" {
		t.Fatalf("result %+v", r)
	}
}

func TestBarcodeLookupBasisVariants(t *testing.T) {
	cases := map[string]struct {
		body   string
		amount string
		kcal   float64
	}{
		// no reference amount: the whole package (x1.2); items wrapped as {"item": {...}} inside "response"
		"package": {`{"response":{"header":{"resultCode":"00"},"body":{"items":{"item":` + item(`,"Z10500":"120g"`) + `}}}}`, "1개 120g", 600},
		// units differ (ml vs g): the DB basis itself
		"basis": {`{"header":{"resultCode":"00"},"body":{"items":{"item":[` + item(`,"NUTRI_AMOUNT_SERVING":"200ml"`) + `]}}}`, "100g 기준", 500},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			p, err := fakeServices(t, c005Found, c.body).Find(context.Background(), "8801234567890")
			if err != nil || p.Nutrition == nil || p.Amount != c.amount || p.Nutrition.Kcal != c.kcal {
				t.Fatalf("%v %q %+v", err, p.Amount, p.Nutrition)
			}
		})
	}
}

func TestBarcodeNotFoundOrNoNutrients(t *testing.T) {
	_, err := fakeServices(t, `{"C005":{"RESULT":{"CODE":"INFO-200","MSG":"해당하는 데이터가 없습니다."}}}`, `{}`).
		Find(context.Background(), "8800000000000")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("not found: %v", err)
	}
	// the product is known but the nutrient DB has nothing: name only
	p, err := fakeServices(t, c005Found, `{"header":{"resultCode":"00"},"body":{"items":[]}}`).
		Find(context.Background(), "8801234567890")
	if err != nil || p.Name != "바삭칩 오리지널" || p.Nutrition != nil || ResultOf(p) != nil {
		t.Fatalf("name only: %v %+v", err, p)
	}
	// a bad key at 식품안전나라
	_, err = fakeServices(t, `{"C005":{"RESULT":{"CODE":"INFO-100"}}}`, `{}`).Find(context.Background(), "8801234567890")
	if err == nil || errors.Is(err, ErrNotFound) {
		t.Fatalf("bad key must be an error, not 'not found': %v", err)
	}
}

// 연결 실패 에러에 요청 주소(두 키가 들어 있다)가 남지 않는다.
func TestBarcodeErrorHasNoKey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	base := srv.URL
	srv.Close()
	_, err := New("safety-secret", "data-secret", base, base).Find(context.Background(), "8801234567890")
	if err == nil || strings.Contains(err.Error(), "secret") || strings.Contains(err.Error(), base) {
		t.Fatalf("got %v", err)
	}
	if New("", "k", "", "").Enabled() || New("k", "", "", "").Enabled() || (*Lookup)(nil).Enabled() {
		t.Fatal("키 하나로 켜짐")
	}
}
