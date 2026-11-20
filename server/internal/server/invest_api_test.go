package server_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/yuntyu01/MyHome/server/internal/auth"
)

// 앱 투자 탭(GET /v1/invest, /v1/invest/value). 상태는 합성 데이터다.

// deviceGet은 기기 토큰으로 GET한다(앱과 같은 인증).
func (c *webClient) deviceGet(path string) *httptest.ResponseRecorder {
	c.t.Helper()
	tok, _, err := auth.IssueDeviceToken(context.Background(), c.q, "phone")
	if err != nil {
		c.t.Fatal(err)
	}
	req := httptest.NewRequest("GET", path, nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	return serve(c.h, req)
}

// devicePut은 기기 토큰으로 JSON을 PUT한다.
func (c *webClient) devicePut(path, body string) *httptest.ResponseRecorder {
	c.t.Helper()
	tok, _, err := auth.IssueDeviceToken(context.Background(), c.q, "phone")
	if err != nil {
		c.t.Fatal(err)
	}
	req := httptest.NewRequest("PUT", path, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Content-Type", "application/json")
	return serve(c.h, req)
}

// 앱 설정의 입금일: 저장하면 첫 화면이 그 값을 돌려주고, 1~20 밖이면 거부한다. 0은 지우기다.
func TestAppInvestDepositDay(t *testing.T) {
	c := newWebClient(t)
	c.seedInvest(investSeed{})
	if rec := c.devicePut("/v1/invest/prefs", `{"deposit_day": 5}`); rec.Code != http.StatusOK {
		t.Fatalf("저장: %d %s", rec.Code, rec.Body.String())
	}
	var s struct {
		DepositDay int `json:"deposit_day"`
	}
	if err := json.Unmarshal(c.deviceGet("/v1/invest").Body.Bytes(), &s); err != nil || s.DepositDay != 5 {
		t.Fatalf("첫 화면 입금일 %d %v", s.DepositDay, err)
	}
	for _, bad := range []string{`{"deposit_day": 21}`, `{"deposit_day": -1}`, `{}`, `{"deposit_day": 5, "x": 1}`} {
		if rec := c.devicePut("/v1/invest/prefs", bad); rec.Code != http.StatusBadRequest {
			t.Fatalf("%s → %d", bad, rec.Code)
		}
	}
	if rec := c.devicePut("/v1/invest/prefs", `{"deposit_day": 0}`); rec.Code != http.StatusOK {
		t.Fatalf("지우기: %d", rec.Code)
	}
	_ = json.Unmarshal(c.deviceGet("/v1/invest").Body.Bytes(), &s)
	if s.DepositDay != 0 {
		t.Fatalf("지운 뒤 %d", s.DepositDay)
	}
}

func TestAppInvestAPI(t *testing.T) {
	c := newWebClient(t)

	// 인증 없이는 못 읽는다
	if rec := serve(c.h, httptest.NewRequest("GET", "/v1/invest", nil)); rec.Code != http.StatusUnauthorized {
		t.Errorf("인증 없이 status %d, want 401", rec.Code)
	}
	// 옮기기 전: migrated=false
	var empty map[string]any
	rec := c.deviceGet("/v1/invest")
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &empty) != nil || empty["migrated"] != false {
		t.Fatalf("옮기기 전 응답: %d %s", rec.Code, rec.Body.String())
	}

	c.seedInvest(investSeed{})
	rec = c.deviceGet("/v1/invest")
	var s struct {
		Migrated bool   `json:"migrated"`
		Symbol   string `json:"symbol"`
		Shares   int    `json:"shares"`
		Problems int    `json:"problems"`
		Month    struct {
			Title, Value string
		} `json:"month"`
		Schedule []struct{ Title string } `json:"schedule"`
		Checks   []struct {
			Title string
			Bad   bool
		} `json:"checks"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &s); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("status %d: %v %s", rec.Code, err, rec.Body.String())
	}
	if !s.Migrated || s.Symbol != "TQQQ" || s.Shares != 10 || s.Problems != 0 || !strings.HasPrefix(s.Month.Value, "예정 ") || len(s.Schedule) != 2 || len(s.Checks) == 0 {
		t.Errorf("첫 화면 값이 웹과 다름: %+v", s)
	}
	// 맨 위 요약에 종목·증권 총액·넣은 돈이 있다(사용자 요청, 2026-11-20: TQ는 첫 화면에 시장 숫자를 두지 않았다).
	// 세후 평가액(net_krw)은 여전히 평가 화면에만 있다
	var top struct {
		Summary struct {
			Symbol      string `json:"symbol"`
			CanValue    bool   `json:"can_value"`
			TotalKRW    int64  `json:"total_krw"`
			InvestedKRW int64  `json:"invested_krw"`
		} `json:"summary"`
		Flow struct {
			Steps []struct{ Key, State string } `json:"steps"`
		} `json:"flow"`
		// 매일 기록은 가격 갱신이 남긴다. 아직 없으면 빈 목록(앱이 null을 받지 않게)
		Daily *struct {
			Has  bool              `json:"has"`
			Rows []json.RawMessage `json:"rows"`
		} `json:"daily"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &top); err != nil {
		t.Fatal(err)
	}
	if top.Summary.Symbol != "TQQQ" || !top.Summary.CanValue || top.Summary.TotalKRW <= 0 || top.Summary.InvestedKRW <= 0 || len(top.Flow.Steps) != 5 ||
		top.Daily == nil || top.Daily.Has || top.Daily.Rows == nil {
		t.Errorf("요약·단계: %+v", top)
	}
	if strings.Contains(rec.Body.String(), "net_krw") {
		t.Error("첫 화면 응답에 세후 평가액이 있음")
	}

	var v struct {
		CanValue       bool  `json:"can_value"`
		NetKRW         int64 `json:"net_krw"`
		ContributedKRW int64 `json:"contributed_krw"`
		GainKRW        int64 `json:"gain_krw"`
		Ledger         []struct {
			Source string `json:"source"`
			Amount int64  `json:"amount"`
		} `json:"ledger"`
		LedgerBad bool `json:"ledger_bad"`
	}
	rec = c.deviceGet("/v1/invest/value")
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("value status %d: %v", rec.Code, err)
	}
	if !v.CanValue || v.NetKRW != 721_000 || v.ContributedKRW != 600_000 || v.GainKRW != 121_000 || len(v.Ledger) != 1 || v.Ledger[0].Source != "자동매수" || v.LedgerBad {
		t.Errorf("평가 값이 웹과 다름: %+v", v)
	}
	// 읽기 전용: 쓰기 메서드가 없다
	req := httptest.NewRequest("POST", "/v1/invest", nil)
	if rec := serve(c.h, req); rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST /v1/invest status %d, want 405", rec.Code)
	}
}

// 휴대폰 하단바는 홈·거래·확인·투자·더보기 5칸이고, 분석은 더보기 안에 있다.
func TestWebBottomNavFiveTabs(t *testing.T) {
	c := newWebClient(t)
	nav := regexp.MustCompile(`(?s)<nav class="bottom-nav".*?</nav>`).FindString(c.page("/"))
	got := regexp.MustCompile(`<span>([^<]+)</span>`).FindAllStringSubmatch(nav, -1)
	var names []string
	for _, m := range got {
		names = append(names, m[1])
	}
	if strings.Join(names, ",") != "홈,거래,확인,투자,더보기" {
		t.Errorf("하단바 %v", names)
	}
	if !strings.Contains(c.page("/more"), `href="/analytics"`) {
		t.Error("더보기에 분석이 없음")
	}
	// 분석 화면에서는 더보기 칸이 선택된다
	nav = regexp.MustCompile(`(?s)<nav class="bottom-nav".*?</nav>`).FindString(c.page("/analytics"))
	if !strings.Contains(nav, `href="/more" aria-current="page"`) {
		t.Error("분석 화면에서 더보기가 선택돼야 함")
	}
}

// 매매 실패 알림(GET /v1/invest/alerts): 앱은 마지막으로 본 id 다음만 받고, 7일 넘은 것은 받지 않는다.
func TestAppInvestAlertsAPI(t *testing.T) {
	c := newWebClient(t)
	if rec := serve(c.h, httptest.NewRequest("GET", "/v1/invest/alerts", nil)); rec.Code != http.StatusUnauthorized {
		t.Errorf("인증 없이 status %d, want 401", rec.Code)
	}
	ctx := context.Background()
	if _, err := c.pool.Exec(ctx, `INSERT INTO invest_alerts (unit, cmd, title, body, created_at) VALUES
		('myhome-invest-trade.service', 'trade', '옛 실패', '8일 전', now() - interval '8 days'),
		('myhome-invest-trade.service', 'trade', '자동매매 주문 실패 (exit 1)', '✗ 증권사 거부', now() - interval '1 hour'),
		('myhome-invest-judge.service', 'judge', '자동매매 월말 판정 실패 (exit 75)', '가격 없음', now())`); err != nil {
		t.Fatal(err)
	}
	type resp struct {
		Alerts []struct {
			ID    int64  `json:"id"`
			Title string `json:"title"`
			Body  string `json:"body"`
		} `json:"alerts"`
	}
	var all resp
	rec := c.deviceGet("/v1/invest/alerts")
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &all) != nil || len(all.Alerts) != 2 || all.Alerts[0].Body != "✗ 증권사 거부" {
		t.Fatalf("처음: %d %s", rec.Code, rec.Body.String())
	}
	var next resp
	rec = c.deviceGet("/v1/invest/alerts?after=" + strconv.FormatInt(all.Alerts[0].ID, 10))
	if json.Unmarshal(rec.Body.Bytes(), &next) != nil || len(next.Alerts) != 1 || !strings.Contains(next.Alerts[0].Title, "월말 판정") {
		t.Fatalf("after: %s", rec.Body.String())
	}
	rec = c.deviceGet("/v1/invest/alerts?after=" + strconv.FormatInt(all.Alerts[1].ID, 10))
	if rec.Code != http.StatusOK || strings.TrimSpace(rec.Body.String()) != `{"alerts":[]}` {
		t.Fatalf("다 본 뒤: %s", rec.Body.String())
	}
	if rec := c.deviceGet("/v1/invest/alerts?after=x"); rec.Code != http.StatusBadRequest {
		t.Errorf("after=x status %d", rec.Code)
	}
}
