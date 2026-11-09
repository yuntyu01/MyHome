package server_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// 백테스트 결과(TQ 뷰어 이식): 웹 하위 화면과 앱 API. 고정 표라 DB 상태와 상관없이 나온다.

func TestWebInvestBacktest(t *testing.T) {
	c := newWebClient(t)
	c.seedInvest(investSeed{})
	if body := c.page("/invest"); !strings.Contains(body, `href="/invest/backtest"`) {
		t.Fatal("투자 화면에서 백테스트 결과로 가는 길이 없다")
	}
	body := c.page("/invest/backtest")
	for _, want := range []string{
		"TQQQ·QLD를 그냥 들고 있는 것과 비교", "★ SPX185+vol30", "QLD 보유", "TQQQ 보유",
		"닷컴 버블을 뺀 표본", "10년 적립", "읽기 전에", "TQQQ·QLD를 그냥 보유하는 것보다 안전",
		"−98%", // 7년 전체 TQQQ 보유 MDD최악 — 맨 위 요약은 표본과 상관없이 전체 표본이다
	} {
		if !strings.Contains(body, want) {
			t.Errorf("백테스트 화면에 %q가 없다", want)
		}
	}
	all := c.page("/invest/backtest?s=all")
	if !strings.Contains(all, "지금 보는 것은 <strong>전체 표본</strong>") || strings.Contains(all, "닷컴 버블을 뺀 표본</strong>이다") {
		t.Error("?s=all이면 전체 표본 안내로 바뀌어야 한다")
	}
	if rec := c.post("/invest/backtest", nil, false); rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST status %d, want 405", rec.Code)
	}
}

func TestAppInvestBacktestAPI(t *testing.T) {
	c := newWebClient(t)
	if rec := serve(c.h, httptest.NewRequest("GET", "/v1/invest/backtest", nil)); rec.Code != http.StatusUnauthorized {
		t.Errorf("인증 없이 status %d, want 401", rec.Code)
	}
	rec := c.deviceGet("/v1/invest/backtest")
	var b struct {
		Columns []struct {
			Key            string `json:"key"`
			HigherIsBetter bool   `json:"higher_is_better"`
		} `json:"columns"`
		Key []struct {
			Name        string   `json:"name"`
			Star        bool     `json:"star"`
			MDDWorstPct float64  `json:"mdd_worst_pct"`
			Tops        []string `json:"tops"`
		} `json:"key"`
		Horizons []struct {
			Label    string            `json:"label"`
			ExDotcom []json.RawMessage `json:"ex_dotcom"`
			All      []json.RawMessage `json:"all"`
		} `json:"horizons"`
		Notes []string `json:"notes"`
	}
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &b) != nil {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	if len(b.Columns) != 9 || b.Columns[0].Key != "worst" || len(b.Horizons) != 6 || len(b.Horizons[2].All) != 9 || len(b.Notes) < 5 {
		t.Fatalf("모양이 다르다: %+v", b)
	}
	if len(b.Key) != 4 || !b.Key[0].Star || b.Key[1].Name != "TQQQ 보유" || b.Key[2].Name != "QLD 보유" || b.Key[1].MDDWorstPct != -97.93 {
		t.Fatalf("요약 비교: %+v", b.Key)
	}
	for _, k := range b.Key {
		if k.Tops == nil {
			t.Fatal("tops는 비어도 배열이어야 한다(앱 파서)")
		}
	}
}
