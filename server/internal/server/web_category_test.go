package server_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/yuntyu01/MyHome/server/internal/period"
)

// formErr는 관리 폼이 돌려보낸 주소의 오류 문구다(성공이면 빈 글자).
func formErr(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	u, err := url.Parse(rec.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	return u.Query().Get("err")
}

// between은 body에서 from부터 처음 나오는 to까지다(없으면 빈 글자).
func between(body, from, to string) string {
	i := strings.Index(body, from)
	if i < 0 {
		return ""
	}
	j := strings.Index(body[i:], to)
	if j < 0 {
		return body[i:]
	}
	return body[i : i+j]
}

// 관리 화면에서 분류 폴더를 만들고 옮기고 설명을 적는다. 4단계는 이유를 보여주며 거절하고, 화면은 나무로 그린다.
func TestWebCategoryFolders(t *testing.T) {
	c := newWebClient(t)
	ok := func(path string, form url.Values) {
		t.Helper()
		rec := c.post(path, form, false)
		if rec.Code != http.StatusSeeOther {
			t.Fatalf("%s status %d", path, rec.Code)
		}
		if e := formErr(t, rec); e != "" {
			t.Fatalf("%s %v: %s", path, form, e)
		}
	}

	// 맨 위 폴더(설명 포함) → 식비를 그 안으로 옮김 → 식비 안에 배달(종류는 폴더를 따른다)
	ok("/admin/categories", url.Values{"name": {"생활비"}, "kind": {"expense"}, "parent": {""}, "memo": {"매달 나가는 돈"}})
	life, food := c.cat("생활비"), c.cat("식비")
	ok("/admin/categories/"+food, url.Values{"name": {"식비"}, "parent": {life}, "sort": {"10"}, "memo": {"밥, 배달 앱"}})
	ok("/admin/categories", url.Values{"name": {"배달"}, "kind": {"income"}, "parent": {food}, "memo": {"야식 포함"}})
	delivery := c.cat("배달")
	ctx := context.Background()
	cats, _ := c.q.ListCategories(ctx)
	for _, x := range cats {
		switch itoa(x.ID) {
		case life:
			if x.ParentID != nil || x.Memo != "매달 나가는 돈" {
				t.Errorf("생활비 %+v", x)
			}
		case food:
			if x.ParentID == nil || itoa(*x.ParentID) != life || x.Memo != "밥, 배달 앱" {
				t.Errorf("식비 %+v", x)
			}
		case delivery:
			if x.ParentID == nil || itoa(*x.ParentID) != food || x.Kind != "expense" || x.Memo != "야식 포함" {
				t.Errorf("배달 %+v", x)
			}
		}
	}

	// 설명만 고치면 이름·폴더는 그대로
	ok("/admin/categories/"+delivery, url.Values{"memo": {"배달 앱, 포장"}})
	if x, _ := c.q.GetCategory(ctx, mustID(t, delivery)); x.Memo != "배달 앱, 포장" || x.Name != "배달" || x.ParentID == nil || itoa(*x.ParentID) != food {
		t.Errorf("설명만 고치기 %+v", x)
	}

	// 4단계는 이유를 보여주며 거절한다(새로 만들기, 폴더째 옮기기 둘 다)
	rec := c.post("/admin/categories", url.Values{"name": {"야식"}, "kind": {"expense"}, "parent": {delivery}}, false)
	if e := formErr(t, rec); !strings.Contains(e, "폴더는 3단계까지만 만들 수 있어요") {
		t.Fatalf("4단계 만들기: %q", e)
	}
	if body := c.get(rec.Header().Get("Location")).Body.String(); !strings.Contains(body, "추가 실패: 폴더는 3단계까지만 만들 수 있어요") {
		t.Error("거절 이유가 화면에 보여야 함")
	}
	if e := formErr(t, c.post("/admin/categories/"+life, url.Values{"parent": {c.cat("교통")}}, false)); !strings.Contains(e, "3단계") {
		t.Errorf("폴더째 옮겨 4단계: %q", e)
	}
	if e := formErr(t, c.post("/admin/categories/"+c.cat("수입"), url.Values{"parent": {life}}, false)); e == "" {
		t.Error("특수 분류는 폴더에 못 넣음")
	}

	// 나무 화면: 폴더 안에 안의 분류가 묶이고, 폴더 고르기에는 옮길 수 있는 곳만 나온다
	body := c.get("/admin?tab=categories").Body.String()
	lifeNode := between(body, `action="/admin/categories/`+life+`"`, "</form>")
	if strings.Count(lifeNode, "<option") != 1 || !strings.Contains(lifeNode, `name="parent"`) {
		t.Errorf("3단계짜리 폴더는 맨 위에만 둘 수 있음: %s", lifeNode)
	}
	foodForm := between(body, `action="/admin/categories/`+food+`"`, "</form>")
	if !strings.Contains(foodForm, `<option value="`+life+`" selected>생활비</option>`) || strings.Contains(foodForm, `<option value="`+delivery+`"`) ||
		strings.Contains(foodForm, ">식비</option>") || strings.Contains(foodForm, ">수입</option>") || !strings.Contains(foodForm, "밥, 배달 앱</textarea>") {
		t.Errorf("식비 폴더 고르기(자기·안의 분류·특수 분류 제외): %s", foodForm)
	}
	deliveryForm := between(body, `action="/admin/categories/`+delivery+`"`, "</form>")
	if !strings.Contains(deliveryForm, `<option value="`+food+`" selected>생활비 › 식비</option>`) || !strings.Contains(deliveryForm, "AI가 분류를 추천할 때") {
		t.Errorf("배달 폴더 고르기: %s", deliveryForm)
	}
	incomeForm := between(body, `action="/admin/categories/`+c.cat("수입")+`"`, "</form>")
	if strings.Contains(incomeForm, `name="parent"`) || strings.Contains(incomeForm, "숨기기") {
		t.Errorf("특수 분류는 옮기기·숨기기 없음: %s", incomeForm)
	}
	// 생활비 → 식비 → 배달이 겹겹이 묶인다
	tree := between(body, `<div class="cat-tree">`, `<h2>새 분류</h2>`)
	iLife, iFood, iDel := strings.Index(tree, ">생활비<"), strings.Index(tree, ">식비<"), strings.Index(tree, ">배달<")
	if iLife < 0 || iFood < iLife || iDel < iFood ||
		strings.Count(tree[iLife:iFood], `<div class="cat-children">`) != 1 || strings.Count(tree[iFood:iDel], `<div class="cat-children">`) != 1 {
		t.Errorf("나무 모양: 생활비 %d 식비 %d 배달 %d", iLife, iFood, iDel)
	}
	if !strings.Contains(tree, `<span class="cat-memo">매달 나가는 돈</span>`) {
		t.Error("설명이 줄에 보여야 함")
	}
	addForm := between(body, `action="/admin/categories" `, "</form>")
	if !strings.Contains(addForm, ">생활비 › 식비</option>") || strings.Contains(addForm, ">생활비 › 식비 › 배달</option>") || strings.Contains(addForm, ">정산</option>") {
		t.Errorf("새 분류 폴더 고르기(3단계·특수 분류 제외): %s", addForm)
	}

	// 다른 화면: 고르기는 경로 이름, 예산은 폴더 합계, 홈은 맨 위 분류만 막대로
	if body := c.get("/transactions").Body.String(); !strings.Contains(body, ">생활비 › 식비 › 배달</option>") {
		t.Error("거래 분류 고르기에 경로 이름")
	}
	month := time.Now().In(period.KST).Format("2006-01")
	ok("/budgets", url.Values{"month": {month}, "amount_" + food: {"200,000"}})
	ok("/admin/cash", url.Values{"amount": {"12,000"}, "category_id": {delivery}, "merchant": {"치킨집"}})
	budgets := c.get("/budgets?month=" + month).Body.String()
	if !strings.Contains(between(budgets, `name="amount_`+life+`"`, "</tr>"), "아래 합계 <span class=\"nowrap\">200,000원</span>") ||
		!strings.Contains(budgets, `<th class="num">200,000원</th>`) || !strings.Contains(budgets, `tree-name d2">배달`) {
		t.Errorf("예산 화면: 폴더는 아래 합계, 합계는 맨 위만")
	}
	home := c.get("/").Body.String()
	if !strings.Contains(home, "안에 든 분류 2개") || strings.Contains(home, `<span class="name">식비</span>`) {
		t.Error("홈: 맨 위 분류 막대 아래에 안의 분류")
	}
	if rec := c.get("/analytics"); rec.Code != 200 || !strings.Contains(rec.Body.String(), `tree-name d2">배달`) {
		t.Errorf("분석 표: %d", rec.Code)
	}

	// 폴더를 숨기면 안의 분류도 고르기에서 빠지고, 관리 화면에는 (숨김)으로 남는다
	ok("/admin/categories/"+life, url.Values{"archived": {"true"}})
	if body := c.get("/transactions").Body.String(); strings.Contains(body, "생활비 › 식비 › 배달</option>") || !strings.Contains(body, "생활비 › 식비 › 배달 (숨김)</option>") {
		t.Error("숨긴 분류는 고르기에서 빠지고, 이미 그 분류인 거래에만 남아야 함")
	}
	if body := c.get("/admin?tab=categories").Body.String(); !strings.Contains(body, "배달 (숨김)") || !strings.Contains(body, "다시 보이기") {
		t.Error("숨긴 분류는 관리 화면에 남음")
	}
}

func mustID(t *testing.T, s string) int64 {
	t.Helper()
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil || n <= 0 {
		t.Fatalf("id %q", s)
	}
	return n
}
