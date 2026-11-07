package server_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
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
	// 웹 첫 화면처럼 시장 숫자(평가액·가격·환율)는 없다
	for _, banned := range []string{"net_krw", "721000", "50.25", "1,400.00"} {
		if strings.Contains(rec.Body.String(), banned) {
			t.Errorf("첫 화면 응답에 %q가 있음", banned)
		}
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
