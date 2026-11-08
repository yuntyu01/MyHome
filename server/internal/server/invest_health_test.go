package server_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/yuntyu01/MyHome/server/internal/invest/store"
)

// GET /healthz/invest — 밖의 하트비트가 로그인 없이 부르는 자동매매 신선도. 상태는 합성 데이터다.
func TestInvestHealthz(t *testing.T) {
	c := newWebClient(t)
	ctx := context.Background()
	get := func() (int, map[string]any, string) {
		t.Helper()
		rec := serve(c.h, httptest.NewRequest("GET", "/healthz/invest", nil)) // 로그인·기기 토큰 없이
		var body map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("JSON 아님: %d %s", rec.Code, rec.Body.String())
		}
		return rec.Code, body, rec.Body.String()
	}
	st := store.New(c.pool, "test")
	run := func(cmd string, exit int) {
		t.Helper()
		id, err := st.StartRun(ctx, cmd, "")
		if err != nil {
			t.Fatal(err)
		}
		if err := st.FinishRun(ctx, id, exit, "출력 600,000원 TQQQ 10주"); err != nil {
			t.Fatal(err)
		}
	}

	// 매매를 아직 시작하지 않았다 — 볼 것이 없으니 200 (하트비트가 "ok"를 찾는다)
	if code, body, _ := get(); code != http.StatusOK || body["status"] != "ok" || body["trading"] != "not_started" {
		t.Fatalf("시작 전: %d %v", code, body)
	}

	// 정상 운용 중(정기 명령이 전부 방금 성공)
	c.seedInvest(investSeed{})
	if code, body, _ := get(); code != http.StatusOK || body["status"] != "ok" || body["trading"] != nil {
		t.Fatalf("정상: %d %v", code, body)
	}

	// position은 있는데 성공한 status가 없다 — deadman이 한 번도 안 돌았다
	if _, err := c.pool.Exec(ctx, "DELETE FROM invest_runs WHERE cmd = 'status'"); err != nil {
		t.Fatal(err)
	}
	if code, body, _ := get(); code != http.StatusServiceUnavailable || fmtList(body["problems"]) != "status_stale" {
		t.Fatalf("status 없음: %d %v", code, body)
	}
	run("status", 0)
	if code, _, _ := get(); code != http.StatusOK {
		t.Fatalf("status 성공 뒤: %d", code)
	}

	// 8일 넘게 성공한 status가 없다
	if _, err := c.pool.Exec(ctx, "UPDATE invest_runs SET started_at = now() - interval '8 days 1 hour' WHERE cmd = 'status'"); err != nil {
		t.Fatal(err)
	}
	if code, body, _ := get(); code != http.StatusServiceUnavailable || fmtList(body["problems"]) != "status_stale" {
		t.Fatalf("낡은 status: %d %v", code, body)
	}
	run("status", 1) // 실패한 status는 신선도가 아니다
	if code, _, _ := get(); code != http.StatusServiceUnavailable {
		t.Fatalf("실패한 status로 살아났다: %d", code)
	}
	run("status", 0)

	// 마지막 judge·trade가 실패했다
	run("judge", 1)
	run("trade", 75)
	code, body, raw := get()
	if code != http.StatusServiceUnavailable || fmtList(body["problems"]) != "judge_failed,trade_failed" {
		t.Fatalf("실패: %d %v", code, body)
	}
	// 🔴 공개 경로다 — 금액·보유·종목·출력이 실리면 안 된다
	for _, leak := range []string{"600", "TQQQ", "10주", "shares", "krw", "출력"} {
		if strings.Contains(raw, leak) {
			t.Fatalf("공개 응답에 %q가 실렸다: %s", leak, raw)
		}
	}
	// 다음 실행이 성공하면 풀린다
	run("judge", 0)
	run("trade", 0)
	if code, body, _ := get(); code != http.StatusOK {
		t.Fatalf("회복: %d %v", code, body)
	}
}

func fmtList(v any) string {
	xs, _ := v.([]any)
	out := make([]string, 0, len(xs))
	for _, x := range xs {
		s, _ := x.(string)
		out = append(out, s)
	}
	return strings.Join(out, ",")
}
