package server_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/yuntyu01/MyHome/server/internal/ai"
	"github.com/yuntyu01/MyHome/server/internal/auth"
	"github.com/yuntyu01/MyHome/server/internal/config"
	"github.com/yuntyu01/MyHome/server/internal/dbq"
	"github.com/yuntyu01/MyHome/server/internal/health/foods"
	"github.com/yuntyu01/MyHome/server/internal/server"
	"github.com/yuntyu01/MyHome/server/internal/testdb"
)

// 건강 API(/v1/health, 핏두잉 이식). 핏두잉 internal/api 테스트(flow·portion·foods·places·nutrition)에서
// 개인용으로 남긴 부분을 옮기고, 기기 토큰으로 부른다. 날짜는 2026-09-26(토) 기준.

type hc struct {
	t    *testing.T
	h    http.Handler
	tok  string
	pool *pgxpool.Pool
}

type hresp struct {
	t    *testing.T
	Code int
	Raw  string
	Body any
	Hdr  http.Header
}

// fixedNow는 서버의 "오늘"(KST)이다. 휴식권 기본 날짜와 목표 이력 기록에 쓴다.
var fixedNow = time.Date(2026, 9, 26, 12, 0, 0, 0, time.FixedZone("KST", 9*3600))

func newHealthClient(t *testing.T, opt ...func(*server.Deps)) *hc {
	t.Helper()
	pool := testdb.New(t)
	q := dbq.New(pool)
	tok, _, err := auth.IssueDeviceToken(context.Background(), q, "phone")
	if err != nil {
		t.Fatal(err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	d := server.Deps{Config: config.Config{MaxBodyBytes: 64 * 1024, RateLimitRPS: 1000, RateLimitBurst: 1000},
		Pool: pool, Logger: logger, Now: func() time.Time { return fixedNow }}
	for _, o := range opt {
		o(&d)
	}
	return &hc{t: t, h: server.New(d), tok: tok, pool: pool}
}

func (c *hc) serve(req *http.Request, token string) hresp {
	c.t.Helper()
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	c.h.ServeHTTP(rec, req)
	r := hresp{t: c.t, Code: rec.Code, Raw: rec.Body.String(), Hdr: rec.Header()}
	_ = json.Unmarshal(rec.Body.Bytes(), &r.Body)
	return r
}

func (c *hc) do(method, path, body string) hresp {
	c.t.Helper()
	var rd io.Reader
	if body != "" {
		rd = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, rd)
	req.Header.Set("Content-Type", "application/json")
	return c.serve(req, c.tok)
}

func (c *hc) get(path string) hresp { c.t.Helper(); return c.do("GET", path, "") }

func (c *hc) post(path, body string) hresp { c.t.Helper(); return c.do("POST", path, body) }

func (c *hc) put(path, body string) hresp { c.t.Helper(); return c.do("PUT", path, body) }

func (r hresp) is(code int) hresp {
	r.t.Helper()
	if r.Code != code {
		r.t.Fatalf("status %d, want %d: %s", r.Code, code, r.Raw)
	}
	return r
}

// code는 상태와 오류 코드를 본다. 오류 본문은 MyHome 모양 {"error": 메시지}에 "code"가 붙는다.
func (r hresp) code(status int, code string) hresp {
	r.t.Helper()
	r.is(status)
	m, _ := r.Body.(map[string]any)
	if m["code"] != code || m["error"] == nil || m["error"] == "" {
		r.t.Fatalf("code %v, want %s: %s", m["code"], code, r.Raw)
	}
	return r
}

// at은 "rows.2.target.kcal" 같은 경로의 값이다(숫자는 float64).
func at(t *testing.T, v any, path string) any {
	t.Helper()
	for _, k := range strings.Split(path, ".") {
		switch x := v.(type) {
		case map[string]any:
			v = x[k]
		case []any:
			i, err := strconv.Atoi(k)
			if err != nil || i >= len(x) {
				t.Fatalf("경로 %s: %s 없음", path, k)
			}
			v = x[i]
		default:
			return nil
		}
	}
	return v
}

func (r hresp) eq(path string, want any) hresp {
	r.t.Helper()
	got := at(r.t, r.Body, path)
	if n, ok := want.(int); ok {
		want = float64(n)
	}
	if got != want {
		r.t.Fatalf("%s = %v, want %v (%s)", path, got, want, r.Raw)
	}
	return r
}

func (r hresp) size(path string, n int) hresp {
	r.t.Helper()
	v := r.Body
	if path != "" {
		v = at(r.t, r.Body, path)
	}
	l, _ := v.([]any)
	if len(l) != n {
		r.t.Fatalf("len(%s) = %d, want %d (%s)", path, len(l), n, r.Raw)
	}
	return r
}

// mealBody는 핏두잉 테스트의 mealBody에서 cheat를 뺀 것이다.
func mealBody(date string, index int, portion float64, kcal int) string {
	return fmt.Sprintf(`{"date":"%s","index":%d,"portion":%v,"sharedMeal":false,
	 "ai":{"items":[{"name":"밥","amount":"1공기"}],"kcal":%d,"carbs":50,"protein":20,"fat":10,"confidence":0.8}}`,
		date, index, portion, kcal)
}

func js(v any) string { b, _ := json.Marshal(v); return string(b) }

func TestHealthAuthRequired(t *testing.T) {
	c := newHealthClient(t)
	for _, p := range []string{"GET /v1/health/today?date=2026-09-26", "GET /v1/health/settings", "POST /v1/health/meals/analyze",
		"POST /v1/health/meals", "GET /v1/health/rest", "GET /v1/health/foods/barcode/8801234567890",
		"GET /v1/health/targets/recommend?goal=keep&weightKg=70"} {
		method, path, _ := strings.Cut(p, " ")
		if r := c.serve(httptest.NewRequest(method, path, nil), ""); r.Code != http.StatusUnauthorized {
			t.Errorf("%s: status %d, want 401", p, r.Code)
		}
	}
	if r := c.serve(httptest.NewRequest("GET", "/v1/health/settings", nil), "garbage"); r.Code != http.StatusUnauthorized {
		t.Errorf("틀린 토큰: %d", r.Code)
	}
}

// 웹 세션으로도 읽을 수 있고, 쓰기는 CSRF가 있어야 한다(RequireAPI).
func TestHealthWebSession(t *testing.T) {
	c := newWebClient(t)
	if r := c.get("/v1/health/settings"); r.Code != http.StatusOK || !strings.Contains(r.Body.String(), `"meals"`) {
		t.Fatalf("세션 읽기 %d %s", r.Code, r.Body.String())
	}
	req := httptest.NewRequest("POST", "/v1/health/meals", strings.NewReader(mealBody("2026-09-26", 1, 1, 400)))
	req.AddCookie(c.sess)
	if r := serve(c.h, req); r.Code != http.StatusForbidden {
		t.Fatalf("CSRF 없이 %d", r.Code)
	}
	req = httptest.NewRequest("POST", "/v1/health/meals", strings.NewReader(mealBody("2026-09-26", 1, 1, 400)))
	req.AddCookie(c.sess)
	req.Header.Set(auth.CSRFHeader, c.csrf)
	if r := serve(c.h, req); r.Code != http.StatusOK {
		t.Fatalf("CSRF와 함께 %d %s", r.Code, r.Body.String())
	}
}

func TestHealthSettingsRoundTrip(t *testing.T) {
	c := newHealthClient(t)
	r := c.get("/v1/health/settings").is(200)
	r.eq("settings.goal", "keep").size("settings.meals", 4).eq("settings.targets.kcal", 2100).eq("settings.coupons.rest", 2)
	r.eq("settings.notifications", true).eq("settings.sex", nil).eq("settings.photoKeepDays", nil).size("settings.targetHistory", 0)
	r.eq("settings.gym.weeklyTarget", 5).eq("settings.gym.arriveBy", "19:00")
	for _, dropped := range []string{"mode", "allowedApps", "lockMinutes", "emergencyPerWeek", "photoBackup", "nickname", "themeMode",
		"accent", "gradient", "shareFriends", "foodStyle", "allergies", "fasting", "cheat", "pendingMode",
		"radiusM", "lat", "lng", "address"} {
		if strings.Contains(r.Raw, `"`+dropped+`"`) {
			t.Errorf("뺀 필드 %s가 응답에 있음", dropped)
		}
	}

	// 핏두잉 앱 설정 그대로(뺀 필드 포함)를 보내도 받는다. 처음 저장이면 기본값을 "예전 목표"로 남기지 않는다
	s := r.Body.(map[string]any)["settings"].(map[string]any)
	s["mode"], s["nickname"], s["allowedApps"] = "spicy", "지훈", []string{"com.kakao.talk"}
	s["targets"] = map[string]any{"kcal": 1800, "carbs": 200, "protein": 130, "fat": 50}
	s["sex"], s["age"], s["activity"] = "male", 30, "low"
	// 헬스장 위치(없앤 기능)를 보내도 받기만 하고 저장하지 않는다(예전 앱 호환)
	s["gym"].(map[string]any)["name"] = "바디핏짐 성수점"
	s["gym"].(map[string]any)["lat"] = 37.5446
	s["gym"].(map[string]any)["radiusM"] = 10
	s["photoKeepDays"] = 90
	r = c.put("/v1/health/settings", js(s)).is(200)
	r.eq("settings.targets.kcal", 1800).size("settings.targetHistory", 0).eq("settings.sex", "male").eq("settings.age", 30)
	if strings.Contains(r.Raw, "spicy") || strings.Contains(r.Raw, "지훈") || strings.Contains(r.Raw, "바디핏짐") || strings.Contains(r.Raw, `"lat"`) {
		t.Fatalf("뺀 필드가 저장됨: %s", r.Raw)
	}
	c.get("/v1/health/settings").is(200).eq("settings.photoKeepDays", 90).eq("settings.targets.protein", 130)

	// 예전에 저장된 문서에 위치 키가 남아 있어도 읽힌다(무시)
	if _, err := c.pool.Exec(context.Background(),
		`UPDATE health_settings SET doc = jsonb_set(doc, '{gym}', doc->'gym' || '{"name":"옛 헬스장","lat":37.5,"lng":127.0,"radiusM":150}')`); err != nil {
		t.Fatal(err)
	}
	if r := c.get("/v1/health/settings").is(200); strings.Contains(r.Raw, "옛 헬스장") || strings.Contains(r.Raw, `"radiusM"`) {
		t.Fatalf("예전 위치 키가 응답에 나옴: %s", r.Raw)
	}

	// 보낸 키만 바뀐다: 알림만 끄면 나머지(목표·헬스 요일·나이)는 그대로. gym 안도 같다
	c.put("/v1/health/settings", `{"notifications":false,"gym":{"arriveBy":"20:00"}}`).is(200).
		eq("settings.notifications", false).eq("settings.targets.kcal", 1800).eq("settings.age", 30).
		eq("settings.gym.arriveBy", "20:00").eq("settings.gym.weeklyTarget", 5)
	// null은 지운다
	c.put("/v1/health/settings", `{"age":null}`).is(200).eq("settings.age", nil).eq("settings.sex", "male")

	// 목표를 바꾸면 오늘(KST)부터 이력에 남고, 지난 날은 그때 목표다
	r = c.put("/v1/health/settings", `{"targets":{"kcal":1600,"carbs":180,"protein":100,"fat":53}}`).is(200)
	r.size("settings.targetHistory", 2).eq("settings.targetHistory.0.from", "2000-01-01").eq("settings.targetHistory.0.kcal", 1800).
		eq("settings.targetHistory.1.from", "2026-09-26").eq("settings.targetHistory.1.kcal", 1600)
	c.get("/v1/health/records/day?date=2026-09-25").is(200).eq("targets.kcal", 1800)
	c.get("/v1/health/records/day?date=2026-09-26").is(200).eq("targets.kcal", 1600)
	c.get("/v1/health/today?date=2026-09-26").is(200).eq("targets.kcal", 1600)

	// 검증
	c.put("/v1/health/settings", `{"targets":{"kcal":1000,"carbs":100,"protein":60,"fat":40}}`).code(400, "TARGET_BELOW_MINIMUM")
	c.put("/v1/health/settings", `{"meals":[{"index":1,"deadline":"08:00"},{"index":2,"deadline":"12:00"}]}`).code(400, "INVALID")
	c.put("/v1/health/settings", `{"goal":"nope"}`).code(400, "INVALID")
	c.put("/v1/health/settings", `{"photoKeepDays":7}`).code(400, "INVALID")
	c.put("/v1/health/settings", `{not json`).code(400, "INVALID")
	// 실패한 저장은 아무것도 바꾸지 않는다
	c.get("/v1/health/settings").is(200).eq("settings.targets.kcal", 1600).eq("settings.goal", "keep")
}

// 핏두잉 TestMealLogTodayAndRecords.
func TestHealthMealTodayAndRecords(t *testing.T) {
	c := newHealthClient(t)
	for i := 1; i <= 2; i++ {
		p := 1.0
		if i == 2 {
			p = 0.5
		}
		c.post("/v1/health/meals", mealBody("2026-09-26", i, p, 400)).is(200)
	}
	c.post("/v1/health/meals", mealBody("2026-09-25", 1, 1, 400)).is(200)

	r := c.get("/v1/health/today?date=2026-09-26").is(200)
	r.eq("streak", 2).eq("loggedCount", 2).eq("mealCount", 4).eq("total.kcal", 600)                          // 400 + 400*0.5
	r.size("rows", 4).eq("rows.2.logged", false).eq("rows.2.target.kcal", 750).eq("rows.3.target.kcal", 750) // (2100-600)/2
	r.eq("rows.0.nutrition.kcal", 400).eq("rows.0.target", nil)
	r.eq("coupons.rest.total", 2).eq("coupons.rest.left", 2).eq("coupons.weekKey", "2026-09-21").eq("gym.planned", 5)

	// 18:00이면 3끼(마감 17:00)는 놓친 끼니, 남은 목표는 4끼 혼자 갖는다. 시각은 time(핏두잉 이름) 또는 now
	for _, q := range []string{"time", "now"} {
		r = c.get("/v1/health/today?date=2026-09-26&" + q + "=18:00").is(200)
		r.eq("rows.2.missed", true).eq("rows.2.target", nil).eq("rows.3.target.kcal", 1500).eq("rows.3.target.carbs", 283-50-25)
	}
	c.get("/v1/health/today?date=2026-09-26&time=25:00").code(400, "INVALID")
	c.get("/v1/health/today").code(400, "INVALID")

	m := c.get("/v1/health/records/month?month=2026-09").is(200)
	m.eq("month", "2026-09").eq("mealCount", 4).size("days", 30).eq("days.25.state", "partial").eq("days.25.loggedMeals", 2).
		eq("days.0.state", "none").eq("summary.fullDays", 0).eq("summary.gymDays", 0)
	c.get("/v1/health/records/month?month=2026-9x").code(400, "INVALID")

	d := c.get("/v1/health/records/day?date=2026-09-26").is(200)
	d.size("meals", 2).eq("total.kcal", 600).eq("targets.kcal", 2100).eq("gym", false).eq("gymVerifiedBy", nil)
	d.eq("meals.1.portion", 0.5).eq("meals.1.base.kcal", 400).eq("meals.1.nutrition.kcal", 200).eq("meals.1.deadline", "14:00")
	if _, ok := at(t, d.Body, "meals.0.id").(string); !ok {
		t.Fatalf("id는 문자열(핏두잉과 같은 타입): %s", d.Raw)
	}

	// 연속 기록: 오늘 기록이 없으면 어제부터 센다
	c.get("/v1/health/today?date=2026-09-27").is(200).eq("streak", 2).eq("loggedCount", 0)
	c.get("/v1/health/today?date=2026-09-28").is(200).eq("streak", 0)
}

// 핏두잉 TestMealOverwriteAndDelete + 검증.
func TestHealthMealOverwriteDeleteAndValidation(t *testing.T) {
	c := newHealthClient(t)
	first := c.post("/v1/health/meals", mealBody("2026-09-26", 1, 1, 400)).is(200)
	second := c.post("/v1/health/meals", mealBody("2026-09-26", 1, 0.5, 800)).is(200)
	if at(t, first.Body, "meal.id") != at(t, second.Body, "meal.id") {
		t.Fatal("같은 날 같은 번호는 같은 기록을 덮어쓴다")
	}
	c.get("/v1/health/meals?date=2026-09-26").is(200).size("", 1).eq("0.nutrition.kcal", 400).eq("0.base.kcal", 800)
	c.get("/v1/health/today?date=2026-09-26").eq("total.kcal", 400)
	c.do("DELETE", "/v1/health/meals?date=2026-09-26&index=1", "").is(204)
	c.get("/v1/health/meals?date=2026-09-26").is(200).size("", 0)
	c.do("DELETE", "/v1/health/meals?date=2026-09-26&index=x", "").code(400, "INVALID")

	c.post("/v1/health/meals", mealBody("2026-09-26", 6, 1, 400)).code(400, "INVALID") // 설정한 끼니는 4개
	c.post("/v1/health/meals", mealBody("2026-09-26", 10, 1, 400)).code(400, "INVALID")
	c.post("/v1/health/meals", mealBody("2026-09-26", 1, 0.7, 400)).code(400, "INVALID")
	c.post("/v1/health/meals", mealBody("2026-09-26", 1, 3, 400)).code(400, "INVALID")
	c.post("/v1/health/meals", `{not json`).code(400, "INVALID")
	c.post("/v1/health/meals", `{"date":"2026-09-26","index":1}`).code(400, "INVALID") // ai 없음
	// 먹은 양을 안 보내면 1(한 접시)
	r := c.post("/v1/health/meals", `{"date":"2026-09-26","index":1,"ai":{"items":[],"kcal":300,"carbs":1,"protein":1,"fat":1,"confidence":0.5}}`).is(200)
	r.eq("meal.portion", 1).eq("meal.nutrition.kcal", 300)
	// 1인분보다 많이(1.5, 2), 나눠 먹으면 절반
	for _, p := range []float64{1.5, 2} {
		c.post("/v1/health/meals", mealBody("2026-09-26", 1, p, 400)).is(200).eq("meal.nutrition.kcal", 400*p)
	}
	c.post("/v1/health/meals", strings.Replace(mealBody("2026-09-26", 1, 1, 400), `"sharedMeal":false`, `"sharedMeal":true`, 1)).
		is(200).eq("meal.nutrition.kcal", 200).eq("meal.sharedMeal", true)
	// 음식 kcal이 음수면 거절
	bad := `{"date":"2026-09-26","index":1,"portion":1,"ai":{"items":[{"name":"밥","amount":"1","kcal":-1}],"kcal":1,"carbs":1,"protein":1,"fat":1,"confidence":0.5}}`
	c.post("/v1/health/meals", bad).code(400, "INVALID")
	// 다른 API의 본문 제한(64KB)은 그대로다(사진 분석만 크게 연다)
	huge := `{"date":"2026-09-26","index":1,"barcode":"` + strings.Repeat("1", 70*1024) + `"}`
	if r := c.post("/v1/health/meals", huge); r.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("64KB 넘는 본문: %d %s", r.Code, r.Raw)
	}
}

// 핏두잉 TestMealPerFoodPortionsRoundTrip + TestMealItemsKeepPerFoodNutrition(공유·치팅 필드는 뺐다).
func TestHealthMealItemsRoundTrip(t *testing.T) {
	c := newHealthClient(t)
	// 밥 300kcal 75% (225) + 닭가슴살 200kcal 100% = 425
	body := `{"date":"2026-09-26","index":2,"deadline":"14:30","portion":1,"sharedMeal":false,"barcode":"8801234567890",
	 "loggedAt":"2026-09-26T05:00:00Z","shareFriends":true,"cheat":false,
	 "ai":{"items":[
	   {"name":"현미밥","amount":"200g","kcal":300,"carbs":64,"protein":6,"fat":2,"portion":0.75},
	   {"name":"닭가슴살","amount":"100g","kcal":200,"carbs":0,"protein":40,"fat":4,"portion":1},
	   {"name":"김치","amount":"조금"}],
	 "kcal":425,"carbs":48,"protein":45,"fat":6,"confidence":0.8}}`
	r := c.post("/v1/health/meals", body).is(200)
	r.eq("meal.items.0.portion", 0.75).eq("meal.deadline", "14:30").eq("meal.barcode", "8801234567890").
		eq("meal.loggedAt", "2026-09-26T05:00:00Z")
	if strings.Contains(r.Raw, "shareFriends") || strings.Contains(r.Raw, "cheat") {
		t.Fatalf("뺀 필드가 응답에 있음: %s", r.Raw)
	}
	l := c.get("/v1/health/meals?date=2026-09-26").is(200).size("", 1)
	for path, want := range map[string]any{
		"0.items.0.portion": 0.75, "0.items.0.kcal": 300.0, "0.items.1.portion": 1.0, "0.items.2.portion": nil,
		"0.items.2.kcal": nil, "0.nutrition.kcal": 425.0, "0.portion": 1.0, "0.date": "2026-09-26", "0.index": 2.0,
	} {
		l.eq(path, want)
	}
	// 다시 저장하면 음식 목록도 바뀐다
	edited := `{"date":"2026-09-26","index":2,"portion":1,
	 "ai":{"items":[{"name":"현미밥","amount":"200g","kcal":300,"carbs":64,"protein":6,"fat":2,"portion":0.5}],
	 "kcal":150,"carbs":32,"protein":3,"fat":1,"confidence":0.8}}`
	c.post("/v1/health/meals", edited).is(200)
	c.get("/v1/health/meals?date=2026-09-26").is(200).size("0.items", 1).eq("0.items.0.portion", 0.5).eq("0.deadline", "14:00")
	// 음식별 먹은 비율은 여섯 단계만
	c.post("/v1/health/meals", strings.Replace(edited, `"portion":0.5`, `"portion":0.3`, 1)).code(400, "INVALID")
}

// 핏두잉 TestExtraMealsCountInTotalsNotInPlan.
func TestHealthExtraMeals(t *testing.T) {
	c := newHealthClient(t)
	c.post("/v1/health/meals", mealBody("2026-09-26", 1, 1, 400)).is(200)
	c.post("/v1/health/meals", mealBody("2026-09-26", 7, 1, 150)).is(200) // 기타 1(간식)
	r := c.get("/v1/health/today?date=2026-09-26").is(200)
	r.eq("total.kcal", 550).eq("loggedCount", 1).size("rows", 4)
	c.get("/v1/health/meals?date=2026-09-26").is(200).eq("1.index", 7).eq("1.deadline", "")
	// "기타"만 있는 날은 다 먹은 날이 아니다
	c.do("DELETE", "/v1/health/meals?date=2026-09-26&index=1", "").is(204)
	c.get("/v1/health/records/month?month=2026-09").is(200).eq("days.25.state", "none").eq("days.25.loggedMeals", 0)
	// 계획한 끼니를 다 먹으면 full
	for i := 1; i <= 4; i++ {
		c.post("/v1/health/meals", mealBody("2026-09-24", i, 1, 400)).is(200)
	}
	c.get("/v1/health/records/month?month=2026-09").is(200).eq("days.23.state", "full").eq("summary.fullDays", 1)
}

// 핏두잉 TestWeeklyStats + TestWeeklyUsesTargetsOfEachDay.
func TestHealthWeekly(t *testing.T) {
	c := newHealthClient(t)
	// 지난주 월·화, 이번 주 수·목, 오늘(토 9/26) 진행 중
	for _, d := range []string{"2026-09-14", "2026-09-15", "2026-09-23", "2026-09-24"} {
		c.post("/v1/health/meals", mealBody(d, 1, 1, 1600)).is(200)
	}
	c.post("/v1/health/meals", mealBody("2026-09-26", 1, 1, 800)).is(200)

	r := c.get("/v1/health/weekly?date=2026-09-26").is(200)
	r.eq("weekStart", "2026-09-21").size("days", 7)
	for i, want := range []string{"missing", "missing", "done", "done", "missing", "today", "future"} {
		r.eq("days."+strconv.Itoa(i)+".state", want)
	}
	r.eq("days.0.last.kcal", 1600).eq("days.2.last", nil).eq("days.5.current.kcal", 800).eq("days.6.current", nil)
	r.eq("avg.kcal", 1600).eq("lastAvg.kcal", 1600).eq("bandPct", 10).eq("targets.kcal", 2100)
	// 수·목 1600 vs 목표 2100(범위 1890~2310): 둘 다 기록, 범위 안 0, 가장 먼 날은 먼저 나온 수(-500)
	r.eq("summary.kcal.completed", 2).eq("summary.kcal.achieved", 0).eq("summary.kcal.furthest.index", 2).
		eq("summary.kcal.furthest.diff", -500)
	c.get("/v1/health/weekly").code(400, "INVALID")

	// 목요일부터 목표 1600: 수는 그때 목표(2100)로, 목은 1600으로 판단한다
	s := `{"targets":{"kcal":1600,"carbs":180,"protein":100,"fat":53},"targetHistory":[
	 {"from":"2000-01-01","kcal":2100,"carbs":283,"protein":112,"fat":58},
	 {"from":"2026-09-24","kcal":1600,"carbs":180,"protein":100,"fat":53}]}`
	c.put("/v1/health/settings", s).is(200).size("settings.targetHistory", 2)
	r = c.get("/v1/health/weekly?date=2026-09-26").is(200)
	r.eq("targets.kcal", 1600).eq("summary.kcal.completed", 2).eq("summary.kcal.achieved", 1).
		eq("summary.kcal.furthest.index", 2).eq("summary.kcal.furthest.diff", -500)
	c.get("/v1/health/records/day?date=2026-09-23").is(200).eq("targets.kcal", 2100)
}

// 핏두잉 TestRestCouponMarksGymAndDoesNotCountAsVisit + 주간 초기화.
func TestHealthRestCouponAndGym(t *testing.T) {
	c := newHealthClient(t)
	c.post("/v1/health/rest?date=2026-09-26", "").is(200).eq("rest.left", 1).eq("rest.total", 2).
		size("uses", 1).eq("uses.0.type", "rest").eq("uses.0.date", "2026-09-26")
	// 같은 날 다시 쉬어도 한 장만 쓴다
	c.post("/v1/health/rest?date=2026-09-26", "").is(200).eq("rest.left", 1)
	c.get("/v1/health/today?date=2026-09-26").is(200).eq("gym.done", 0).eq("coupons.rest.left", 1)
	c.post("/v1/health/gym/check-in", `{"date":"2026-09-25","verifiedBy":"photo"}`).is(200).
		eq("date", "2026-09-25").eq("verifiedBy", "photo")
	// 위치(geofence): 폰이 헬스장 반경에 머문 것을 확인하고 보낸다
	c.post("/v1/health/gym/check-in", `{"date":"2026-09-22","verifiedBy":"geofence"}`).is(200).eq("verifiedBy", "geofence")
	c.post("/v1/health/gym/check-in", `{"date":"2026-09-23","verifiedBy":"gps"}`).code(400, "INVALID")
	r := c.get("/v1/health/today?date=2026-09-26").is(200)
	r.eq("gym.done", 2).eq("gym.planned", 5).eq("gym.scheduledToday", false).eq("gym.enabled", true).eq("gym.arriveBy", "19:00")
	c.post("/v1/health/gym/check-in", `{"date":"2026-09-25","verifiedBy":"rest"}`).code(400, "INVALID")
	c.post("/v1/health/gym/check-in", `{"date":"2026-09-25","verifiedBy":"walk"}`).code(400, "INVALID")
	c.post("/v1/health/gym/check-in", `{"verifiedBy":"photo"}`).code(400, "INVALID")
	c.get("/v1/health/records/day?date=2026-09-26").is(200).eq("gymVerifiedBy", "rest").eq("gym", false)
	c.get("/v1/health/records/day?date=2026-09-25").is(200).eq("gymVerifiedBy", "photo").eq("gym", true)
	c.get("/v1/health/records/month?month=2026-09").is(200).eq("summary.gymDays", 2).eq("days.21.gym", true).eq("days.24.gym", true).eq("days.25.gym", false)
	// DB에 직접 넣은 위치 인증 기록도 방문으로 센다
	if _, err := c.pool.Exec(context.Background(),
		`INSERT INTO gym_logs (gym_date, check_in_at, verified_by) VALUES ('2026-09-01', now(), 'geofence')`); err != nil {
		t.Fatal(err)
	}
	c.get("/v1/health/records/day?date=2026-09-01").is(200).eq("gymVerifiedBy", "geofence").eq("gym", true)
	c.get("/v1/health/records/month?month=2026-09").is(200).eq("summary.gymDays", 3).eq("days.0.gym", true)

	// 이미 인증한 날은 휴식권으로 덮지 않는다
	c.post("/v1/health/rest?date=2026-09-25", "").code(409, "ALREADY_CHECKED_IN")
	// 날짜는 JSON 본문으로도 받는다. 이번 주 두 장을 다 쓰면 409
	c.post("/v1/health/rest", `{"date":"2026-09-24"}`).is(200).eq("rest.left", 0)
	c.post("/v1/health/rest?date=2026-09-23", "").code(409, "COUPON_EMPTY")
	// 날짜를 안 주면 오늘(KST, 2026-09-26)
	c.get("/v1/health/rest").is(200).eq("weekKey", "2026-09-21").eq("rest.left", 0).size("uses", 2)
	// 월요일에 다시 채워진다
	c.get("/v1/health/rest?date=2026-09-28").is(200).eq("weekKey", "2026-09-28").eq("rest.left", 2).size("uses", 0)
	c.post("/v1/health/rest?date=2026-09-28", "").is(200).eq("rest.left", 1)
	// 쉰 날에 헬스를 가면 방문으로 바뀐다(휴식권은 돌려주지 않는다)
	c.post("/v1/health/gym/check-in", `{"date":"2026-09-28","verifiedBy":"photo","checkInAt":"2026-09-28T10:00:00Z"}`).is(200).
		eq("checkInAt", "2026-09-28T10:00:00Z")
	c.get("/v1/health/today?date=2026-09-28").is(200).eq("gym.done", 1).eq("gym.scheduledToday", true).eq("coupons.rest.left", 1)
	// 설정의 휴식권 장수를 따른다
	c.put("/v1/health/settings", `{"coupons":{"rest":0}}`).is(200)
	c.post("/v1/health/rest?date=2026-10-05", "").code(409, "COUPON_EMPTY")
}

// multipartBody는 "photo" 필드에 사진들을 싣는다.
func multipartBody(t *testing.T, field string, contentType string, photos ...[]byte) (*bytes.Buffer, string) {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	for i, p := range photos {
		h := textproto.MIMEHeader{}
		h.Set("Content-Disposition", fmt.Sprintf(`form-data; name=%q; filename="meal%d.jpg"`, field, i))
		h.Set("Content-Type", contentType)
		w, err := mw.CreatePart(h)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write(p)
	}
	_ = mw.Close()
	return &buf, mw.FormDataContentType()
}

func (c *hc) analyze(field, contentType string, photos ...[]byte) hresp {
	c.t.Helper()
	body, ct := multipartBody(c.t, field, contentType, photos...)
	req := httptest.NewRequest("POST", "/v1/health/meals/analyze", body)
	req.Header.Set("Content-Type", ct)
	return c.serve(req, c.tok)
}

// 핏두잉 TestAnalyzeReturnsFixedSchema + 여러 장·크기 제한. 설정이 비어 있으면 mock 분석기다.
func TestHealthAnalyzeWithMock(t *testing.T) {
	c := newHealthClient(t)
	r := c.analyze("photo", "image/jpeg", []byte{1, 2, 3}).is(200)
	if r.Hdr.Get("X-AI-Model") != "mock" {
		t.Fatalf("X-AI-Model %q", r.Hdr.Get("X-AI-Model"))
	}
	if at(t, r.Body, "items.0.name") == nil {
		t.Fatalf("items 없음: %s", r.Raw)
	}
	for _, k := range []string{"kcal", "carbs", "protein", "fat", "confidence"} {
		if _, ok := at(t, r.Body, k).(float64); !ok {
			t.Fatalf("%s가 숫자가 아님: %s", k, r.Raw)
		}
	}
	// 같은 사진이면 같은 결과
	if r2 := c.analyze("photo", "image/jpeg", []byte{1, 2, 3}); r.Raw != r2.Raw {
		t.Fatal("mock 결과가 다름")
	}
	// 한 끼니 사진 여러 장(최대 3장): 1장당 1MB씩 3장 = 기본 본문 제한(64KB)보다 크다
	big := bytes.Repeat([]byte{7}, 1<<20)
	c.analyze("photo", "image/jpeg", big, big, big).is(200)
	c.analyze("photo", "image/jpeg", big, big, big, big).code(400, "TOO_MANY_PHOTOS")
	// 사진 한 장 2MB 초과, 전체 7MB 초과
	c.analyze("photo", "image/jpeg", make([]byte, 2<<20+10)).code(413, "PHOTO_TOO_LARGE")
	huge := make([]byte, 2<<20)
	c.analyze("photo", "image/jpeg", huge, huge, huge, huge).code(413, "PHOTO_TOO_LARGE")
	c.analyze("photo", "text/plain", []byte{1}).code(400, "INVALID_PHOTO")
	c.analyze("other", "image/jpeg", []byte{1}).code(400, "INVALID_PHOTO")
	c.analyze("photo", "image/jpeg", []byte{}).code(400, "INVALID_PHOTO")
	req := httptest.NewRequest("POST", "/v1/health/meals/analyze", strings.NewReader("{}"))
	req.Header.Set("Content-Type", "application/json")
	c.serve(req, c.tok).code(400, "INVALID_PHOTO")
}

// failingAnalyzer는 늘 실패하는 분석기다(Gemini 장애 흉내).
type failingAnalyzer struct{}

func (failingAnalyzer) Analyze(context.Context, []ai.Photo) (ai.Result, error) {
	return ai.Result{}, errors.New("upstream down")
}

// 분석기 실패는 502 AI_FAILED(직접 입력 안내).
func TestHealthAnalyzeFailure(t *testing.T) {
	c := newHealthClient(t, func(d *server.Deps) { d.Analyzer = failingAnalyzer{} })
	c.analyze("photo", "image/jpeg", []byte{1}).code(502, "AI_FAILED")
}

// AI 크레딧이 없으면(Gemini 402) 503 AI_NO_CREDIT, 앱은 다시 알아보기 대신 직접 입력을 안내한다.
type noCreditAnalyzer struct{}

func (noCreditAnalyzer) Analyze(context.Context, []ai.Photo) (ai.Result, error) {
	return ai.Result{}, fmt.Errorf("%w: %w", ai.ErrFailed, &ai.StatusError{Code: http.StatusPaymentRequired})
}

func TestHealthAnalyzeNoCredit(t *testing.T) {
	c := newHealthClient(t, func(d *server.Deps) { d.Analyzer = noCreditAnalyzer{} })
	r := c.analyze("photo", "image/jpeg", []byte{1}).code(503, "AI_NO_CREDIT")
	r.eq("error", "AI 크레딧이 없어요 - 직접 입력해 주세요")
}

// slowAnalyzer는 동시에 몇 개가 돌았는지 센다.
type slowAnalyzer struct {
	mu        sync.Mutex
	now, peak int
}

func (a *slowAnalyzer) Analyze(context.Context, []ai.Photo) (ai.Result, error) {
	a.mu.Lock()
	a.now++
	a.peak = max(a.peak, a.now)
	a.mu.Unlock()
	time.Sleep(30 * time.Millisecond)
	a.mu.Lock()
	a.now--
	a.mu.Unlock()
	return ai.Result{Items: []ai.Item{}}, nil
}

// 분석은 한 번에 하나씩(메모리 상한): 동시에 와도 차례로 돈다.
func TestHealthAnalyzeOneAtATime(t *testing.T) {
	a := &slowAnalyzer{}
	c := newHealthClient(t, func(d *server.Deps) { d.Analyzer = a })
	var wg sync.WaitGroup
	codes := make([]int, 4)
	for i := range codes {
		wg.Add(1)
		go func() {
			defer wg.Done()
			body, ct := multipartBody(t, "photo", "image/jpeg", []byte{1})
			req := httptest.NewRequest("POST", "/v1/health/meals/analyze", body)
			req.Header.Set("Content-Type", ct)
			req.Header.Set("Authorization", "Bearer "+c.tok)
			rec := httptest.NewRecorder()
			c.h.ServeHTTP(rec, req)
			codes[i] = rec.Code
		}()
	}
	wg.Wait()
	if a.peak != 1 || codes[0] != 200 || codes[1] != 200 || codes[2] != 200 || codes[3] != 200 {
		t.Fatalf("peak=%d codes=%v", a.peak, codes)
	}
}

func TestHealthRecommend(t *testing.T) {
	c := newHealthClient(t)
	q := func(s string) hresp { return c.get("/v1/health/targets/recommend?" + s) }
	// goal·weightKg만: 체중 기반 옛 값
	q("goal=lose&weightKg=70").is(200).eq("kcal", 1750)
	q("goal=keep&weightKg=70").is(200).eq("kcal", 2100)
	q("goal=bulk&weightKg=70").code(400, "INVALID")
	q("goal=nope&weightKg=70").code(400, "INVALID")
	// EER: 40세 남 172cm 70kg 저활동 = 2547.21132kcal, 유지 → 2550
	body := "&weightKg=70&heightCm=172&age=40&sex=male&activity=low"
	r := q("goal=keep" + body).is(200)
	r.eq("kcal", 2550).eq("mode", "balanced").eq("carbs", 351).eq("protein", 128).eq("fat", 71).
		eq("formula", "kdri2025-eer-adult").eq("policy", "fitdoing-2026.10").eq("roundedTo", 50)
	if m := at(t, r.Body, "maintenanceKcal").(float64); m < 2547.2 || m > 2547.3 {
		t.Fatalf("maintenanceKcal %v", m)
	}
	q("goal=lose"+body).is(200).eq("kcal", 2050).eq("goalAdjustPct", -20)
	q("goal=gain"+body).is(200).eq("kcal", 2800)
	q("goal=bulk"+body).is(200).eq("kcal", 3050)
	q("goal=keep&mode=training"+body).is(200).eq("protein", 112).eq("carbs", 366)
	q("goal=keep&weightKg=70&heightCm=172&age=40&sex=male&activity=veryActive").is(200).eq("kcal", 3300)
	q("goal=keep&weightKg=70&heightCm=172").code(400, "INVALID")
	q("goal=keep&weightKg=70&heightCm=172&age=17&sex=male&activity=low").code(400, "ADULT_ONLY")
	q("goal=keep&weightKg=70&heightCm=172&age=40&sex=male&activity=LOW").code(400, "INVALID")
	// 추천은 설정을 바꾸지 않는다
	c.get("/v1/health/settings").is(200).eq("settings.targets.kcal", 2100)
}

// 핏두잉 TestBarcodeAPI.
func TestHealthBarcode(t *testing.T) {
	// 식약처 키도 Open Food Facts도 없으면 503
	newHealthClient(t, func(d *server.Deps) { d.Foods = foods.New("", "", "", "") }).
		get("/v1/health/foods/barcode/8801234567890").code(503, "BARCODE_LOOKUP_UNAVAILABLE")

	safety := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "8800000000000") {
			_, _ = w.Write([]byte(`{"C005":{"RESULT":{"CODE":"INFO-200"}}}`))
			return
		}
		_, _ = w.Write([]byte(`{"C005":{"row":[{"PRDLST_NM":"바삭칩","BSSH_NM":"제과","PRDLST_REPORT_NO":"1","END_DT":""}],"RESULT":{"CODE":"INFO-000"}}}`))
	}))
	defer safety.Close()
	data := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"header":{"resultCode":"00"},"body":{"items":[{"SERVING_SIZE":"100g","AMT_NUM1":"500","AMT_NUM3":"5","AMT_NUM4":"25","AMT_NUM6":"60","NUTRI_AMOUNT_SERVING":"30g"}]}}`))
	}))
	defer data.Close()
	c := newHealthClient(t, func(d *server.Deps) { d.Foods = foods.New("k1", "k2", safety.URL, data.URL) })
	r := c.get("/v1/health/foods/barcode/8801234567890").is(200)
	r.eq("product.name", "바삭칩").eq("product.maker", "제과").eq("product.barcode", "8801234567890").
		eq("product.amount", "1회 섭취참고량 30g").eq("product.nutrition.kcal", 150)
	r.eq("result.kcal", 150).eq("result.items.0.name", "바삭칩").eq("result.items.0.amount", "1회 섭취참고량 30g").eq("result.confidence", 1)
	c.get("/v1/health/foods/barcode/8800000000000").code(404, "BARCODE_NOT_FOUND")
	c.get("/v1/health/foods/barcode/12ab").code(400, "INVALID")
	// 바코드 결과를 그대로 끼니로 저장할 수 있다(사진 분석과 같은 모양)
	meal := map[string]any{"date": "2026-09-26", "index": 7, "barcode": "8801234567890", "ai": r.Body.(map[string]any)["result"]}
	c.post("/v1/health/meals", js(meal)).is(200).eq("meal.nutrition.kcal", 150).eq("meal.barcode", "8801234567890")
}

// 끼니 사진 서버 보관: 올리면 파일과 행이 생기고, 다시 받을 수 있고, 끼니를 지우면 같이 지워진다.
func TestHealthMealPhotosStored(t *testing.T) {
	dir := t.TempDir()
	c := newHealthClient(t, func(d *server.Deps) { d.Config.MealPhotoDir = dir })
	jpeg := func(tag byte) []byte { return []byte{0xFF, 0xD8, 0xFF, 0xE0, tag, tag, 0xFF, 0xD9} }
	put := func(photos ...[]byte) hresp {
		body, ct := multipartBody(c.t, "photo", "image/jpeg", photos...)
		req := httptest.NewRequest("PUT", "/v1/health/meals/photos?date=2026-09-26&index=1", body)
		req.Header.Set("Content-Type", ct)
		return c.serve(req, c.tok)
	}
	files := func() []string {
		var out []string
		_ = filepath.WalkDir(dir, func(p string, e os.DirEntry, _ error) error {
			if e != nil && !e.IsDir() {
				out = append(out, filepath.Base(p))
			}
			return nil
		})
		return out
	}
	c.post("/v1/health/meals", mealBody("2026-09-26", 1, 1, 500)).is(200)

	put(jpeg(1), jpeg(2)).is(200).size("photos", 2)
	if got := files(); len(got) != 2 {
		t.Fatalf("파일 %v", got)
	}
	r := c.get("/v1/health/meals/photos/2026-09-26/1/1")
	if r.Code != 200 || r.Raw != string(jpeg(2)) || r.Hdr.Get("Content-Type") != "image/jpeg" {
		t.Fatalf("사진 받기: %d %q", r.Code, r.Raw)
	}
	// 바꾸기: 1장만 남기면 나머지 파일은 지워지고, 같은 사진은 새로 쓰지 않는다
	put(jpeg(2)).is(200).size("photos", 1)
	if got := files(); len(got) != 1 {
		t.Fatalf("바꾼 뒤 파일 %v", got)
	}
	c.get("/v1/health/meals/photos/2026-09-26/1/1").code(404, "NOT_FOUND")
	// JPEG가 아니면 거절
	put([]byte("not a jpeg")).code(400, "INVALID_PHOTO")
	// 쓰다 만 파일(.partial)은 그 끼니 사진을 다시 올릴 때 지운다(다른 끼니 것은 그대로)
	stale := filepath.Join(dir, "2026-09-26", "2026-09-26_1_0_abc.jpg.partial")
	other := filepath.Join(dir, "2026-09-26", "2026-09-26_2_0_abc.jpg.partial")
	for _, f := range []string{stale, other} {
		if err := os.WriteFile(f, []byte{1}, 0o640); err != nil {
			t.Fatal(err)
		}
	}
	put(jpeg(2)).is(200).size("photos", 1)
	if _, err := os.Stat(stale); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("남은 .partial: %v", err)
	}
	if _, err := os.Stat(other); err != nil {
		t.Fatalf("다른 끼니 .partial을 지움: %v", err)
	}
	_ = os.Remove(other)
	// 끼니를 지우면 사진도
	c.do("DELETE", "/v1/health/meals?date=2026-09-26&index=1", "").is(204)
	c.get("/v1/health/meals/photos?date=2026-09-26&index=1").is(200).size("photos", 0)
	if got := files(); len(got) != 0 {
		t.Fatalf("지운 뒤 파일 %v", got)
	}
	// 끼니 기록이 없으면 사진을 받지 않는다(주인 없는 파일이 남지 않게)
	put(jpeg(3)).code(404, "MEAL_NOT_FOUND")
	if got := files(); len(got) != 0 {
		t.Fatalf("끼니 없이 올린 뒤 파일 %v", got)
	}
}

// 사진 저장이 꺼져 있으면(MEAL_PHOTO_DIR 없음) 올리기는 503, 목록은 빈 목록
func TestHealthMealPhotosOff(t *testing.T) {
	c := newHealthClient(t)
	body, ct := multipartBody(c.t, "photo", "image/jpeg", []byte{0xFF, 0xD8, 0xFF, 0xD9})
	req := httptest.NewRequest("PUT", "/v1/health/meals/photos?date=2026-09-26&index=1", body)
	req.Header.Set("Content-Type", ct)
	c.serve(req, c.tok).code(503, "PHOTO_STORAGE_OFF")
	c.get("/v1/health/meals/photos?date=2026-09-26&index=1").is(200).size("photos", 0)
}
