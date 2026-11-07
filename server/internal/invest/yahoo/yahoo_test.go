package yahoo

import (
	"context"
	"errors"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

// testdata/*.json은 2026-11-06 02:07 UTC에 받은 실제 응답을 바 몇 개만 남겨 자른 것이다.
//   - qqq.json  뉴욕. 10-28~10-30은 EDT(UTC 13:30), 11-02부터 EST(UTC 14:30).
//     11-05는 마감 5시간 뒤인데도 close가 null이었다(open·volume은 있음).
//     close를 쓰는지 가리려고 10-30 배당 이벤트를 넣고 그 전날들의 adjclose만 낮췄다.
//   - krw.json  런던. 10-25까지 BST라 현지 자정이 UTC 전날 23:00, 10-26부터 UTC 00:00.
//     11-06은 진행 중인 바다(외환은 런던 자정까지가 하루).

// 받은 요청을 기록하고 종목별로 정해 둔 응답을 주는 가짜 야후.
type fakeYahoo struct {
	mu   sync.Mutex
	reqs []*http.Request
	resp map[string]func(w http.ResponseWriter) // 키는 디코딩된 종목명
}

func (f *fakeYahoo) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	f.reqs = append(f.reqs, r)
	f.mu.Unlock()
	sym, ok := strings.CutPrefix(r.URL.Path, "/v8/finance/chart/")
	h := f.resp[sym]
	if !ok || h == nil {
		w.WriteHeader(404)
		_, _ = w.Write([]byte(`{"chart":{"result":null,"error":{"code":"Not Found","description":"No data found, symbol may be delisted"}}}`))
		return
	}
	h(w)
}

func body(s string) func(w http.ResponseWriter) {
	return func(w http.ResponseWriter) {
		w.Header().Set("Content-Type", "application/json;charset=utf-8")
		_, _ = w.Write([]byte(s))
	}
}

func status(code int, s string) func(w http.ResponseWriter) {
	return func(w http.ResponseWriter) {
		w.WriteHeader(code)
		_, _ = w.Write([]byte(s))
	}
}

func fixture(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// 실측 시각 — 픽스처를 받은 순간
var fetchedAt = time.Date(2026, 11, 6, 2, 7, 50, 0, time.UTC)

func newClient(t *testing.T, f *fakeYahoo) *Client {
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	return &Client{BaseURL: srv.URL, Now: func() time.Time { return fetchedAt }}
}

func day(y int, m time.Month, d int) time.Time { return time.Date(y, m, d, 0, 0, 0, 0, time.UTC) }

type want struct {
	day   time.Time
	close float64 // NaN이면 null
}

func checkBars(t *testing.T, sym string, got []Bar, ws []want) {
	t.Helper()
	if len(got) != len(ws) {
		t.Fatalf("%s 바 %d개, 기대 %d개: %+v", sym, len(got), len(ws), got)
	}
	for i, w := range ws {
		g := got[i]
		if !g.Day.Equal(w.day) || g.Day.Location() != time.UTC {
			t.Errorf("%s[%d] 날짜 %v, 기대 %v", sym, i, g.Day, w.day)
		}
		if math.IsNaN(w.close) {
			if g.OK() {
				t.Errorf("%s[%d] %s null이어야 하는데 %v", sym, i, w.day.Format("2006-01-02"), g.Close)
			}
			continue
		}
		if !g.OK() || g.Close != w.close {
			t.Errorf("%s[%d] %s 종가 %v, 기대 %v", sym, i, w.day.Format("2006-01-02"), g.Close, w.close)
		}
	}
}

// 날짜 변환·null·close 선택·시작일 필터·요청 형식을 실제 응답 모양으로 확인한다.
func TestDailyCloses(t *testing.T) {
	f := &fakeYahoo{resp: map[string]func(http.ResponseWriter){
		"QQQ":   body(fixture(t, "qqq.json")),
		"KRW=X": body(fixture(t, "krw.json")),
	}}
	c := newClient(t, f)

	got, err := c.DailyCloses(context.Background(), []string{"QQQ", "KRW=X"}, day(2026, 10, 23))
	if err != nil {
		t.Fatal(err)
	}
	// KRW=X 10-22 바는 period1을 하루 앞당긴 탓에 딸려 온 것이라 버려져야 한다.
	// QQQ 10-28·10-29는 adjclose(724.8844·714.6247)가 아니라 close여야 한다.
	checkBars(t, "QQQ", got["QQQ"], []want{
		{day(2026, 10, 28), 725.6099853515625},
		{day(2026, 10, 29), 715.3400268554688},
		{day(2026, 10, 30), 690.489990234375},
		{day(2026, 11, 2), 681.0900268554688}, // EST 첫날 — UTC 14:30
		{day(2026, 11, 5), math.NaN()},        // 마감 뒤에도 null
	})
	// UTC로 날짜를 자르면 10-23이 10-22로, 10-22가 10-21로 밀린다
	checkBars(t, "KRW=X", got["KRW=X"], []want{
		{day(2026, 10, 23), 1325.989990234375}, // BST — UTC 10-22 23:00
		{day(2026, 10, 26), 1321.760009765625}, // GMT — UTC 10-26 00:00
		{day(2026, 11, 5), 1340.0699462890625},
		{day(2026, 11, 6), 1356.1500244140625}, // 진행 중인 바도 그대로 준다(거르는 건 호출부)
	})

	if len(f.reqs) != 2 {
		t.Fatalf("요청 %d번", len(f.reqs))
	}
	reqs := f.reqs
	for i, sym := range []string{"QQQ", "KRW=X"} {
		r := reqs[i]
		q := r.URL.Query()
		if !strings.HasSuffix(r.URL.Path, "/"+sym) {
			t.Errorf("요청 %d 순서: %s", i, r.URL.Path)
		}
		// period1은 하루 앞당긴 UTC 자정, period2는 지금
		if q.Get("period1") != "1792627200" || q.Get("period2") != "1793930870" {
			t.Errorf("기간 %s~%s", q.Get("period1"), q.Get("period2"))
		}
		if q.Get("interval") != "1d" || q.Get("includeAdjustedClose") != "true" || q.Get("events") != "div,split" {
			t.Errorf("쿼리 %s", r.URL.RawQuery)
		}
		if ua := r.Header.Get("User-Agent"); !strings.HasPrefix(ua, "Mozilla/5.0") {
			t.Errorf("UA %q", ua)
		}
	}

	// 시작일 전날 바(QQQ 10-28)는 버린다
	got, err = c.DailyCloses(context.Background(), []string{"QQQ"}, day(2026, 10, 29))
	if err != nil {
		t.Fatal(err)
	}
	if b := got["QQQ"]; len(b) != 4 || !b[0].Day.Equal(day(2026, 10, 29)) {
		t.Errorf("시작일 필터: %+v", b)
	}
}

// ^GSPC의 ^는 경로에서 이스케이프돼야 한다.
func TestSymbolEscaped(t *testing.T) {
	f := &fakeYahoo{resp: map[string]func(http.ResponseWriter){
		"^GSPC": body(strings.ReplaceAll(fixture(t, "qqq.json"), `"symbol":"QQQ"`, `"symbol":"^GSPC"`)),
	}}
	c := newClient(t, f)
	if _, err := c.DailyCloses(context.Background(), []string{"^GSPC"}, day(2026, 10, 28)); err != nil {
		t.Fatal(err)
	}
	if uri := f.reqs[0].RequestURI; !strings.HasPrefix(uri, "/v8/finance/chart/%5EGSPC?") {
		t.Errorf("요청 경로 %s", uri)
	}
}

// 오류 분류 — 일시(ErrTemporary)만 호출부가 "나중에 다시"로 바꾼다.
func TestErrorClassification(t *testing.T) {
	empty := `{"chart":{"result":[{"meta":{"currency":"USD","symbol":"QQQ","exchangeTimezoneName":"America/New_York","instrumentType":"ETF","dataGranularity":"1d","range":""},"indicators":{"quote":[{}],"adjclose":[{}]}}],"error":null}}`
	cases := []struct {
		name string
		h    func(http.ResponseWriter)
		temp bool
	}{
		{"429", status(429, "Too Many Requests\r\n"), true},
		{"500", status(500, ""), true},
		{"503", status(503, "<html>Service Unavailable</html>"), true},
		{"빈 결과(timestamp 없음)", body(empty), true},
		{"result null", body(`{"chart":{"result":null,"error":null}}`), true},
		{"시작일 이후 바 없음", body(fixture(t, "qqq.json")), true}, // start를 11-06으로 준다
		{"404", nil, false}, // fakeYahoo 기본 응답
		{"crumb 거부", status(401, `{"finance":{"result":null,"error":{"code":"Unauthorized","description":"Invalid Crumb"}}}`), false},
		{"JSON 깨짐", body(`{"chart":{"result":[`), false},
		{"HTML", body(`<!doctype html><html>consent</html>`), false},
		{"chart.error", body(`{"chart":{"result":null,"error":{"code":"Bad Request","description":"Invalid input - start date cannot be after end date"}}}`), false},
		{"길이 불일치", body(`{"chart":{"result":[{"meta":{"exchangeTimezoneName":"America/New_York"},"timestamp":[1793889000],"indicators":{"quote":[{"close":[]}]}}],"error":null}}`), false},
		{"시간대 없음", body(`{"chart":{"result":[{"meta":{"exchangeTimezoneName":"Mars/Olympus"},"timestamp":[1793889000],"indicators":{"quote":[{"close":[1.0]}]}}],"error":null}}`), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := &fakeYahoo{resp: map[string]func(http.ResponseWriter){"QQQ": tc.h}}
			c := newClient(t, f)
			start := day(2026, 10, 28)
			if tc.name == "시작일 이후 바 없음" {
				start = day(2026, 11, 6)
			}
			got, err := c.DailyCloses(context.Background(), []string{"QQQ"}, start)
			if err == nil {
				t.Fatalf("오류가 없다: %+v", got)
			}
			if errors.Is(err, ErrTemporary) != tc.temp {
				t.Errorf("일시=%v 기대 %v: %v", errors.Is(err, ErrTemporary), tc.temp, err)
			}
			if !strings.Contains(err.Error(), "QQQ") {
				t.Errorf("종목명이 없다: %v", err)
			}
		})
	}
}

// 접속 실패는 일시 장애다. 원인 오류도 꺼낼 수 있어야 한다.
func TestNetworkErrorIsTemporary(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	srv.Close() // 닫힌 포트 → connection refused
	c := &Client{BaseURL: srv.URL}
	_, err := c.DailyCloses(context.Background(), []string{"QQQ"}, day(2026, 10, 28))
	if !errors.Is(err, ErrTemporary) {
		t.Fatalf("일시 장애여야 한다: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = (&Client{BaseURL: "http://127.0.0.1:1"}).DailyCloses(ctx, []string{"QQQ"}, day(2026, 10, 28))
	if !errors.Is(err, ErrTemporary) || !errors.Is(err, context.Canceled) {
		t.Fatalf("취소: %v", err)
	}
}

// 하나가 실패하면 뒤 종목은 부르지 않는다 — 일부만 채운 결과를 돌려주지 않는다.
func TestStopsAtFirstFailure(t *testing.T) {
	f := &fakeYahoo{resp: map[string]func(http.ResponseWriter){
		"QQQ":  body(fixture(t, "qqq.json")),
		"TQQQ": body(fixture(t, "qqq.json")),
	}}
	c := newClient(t, f)
	got, err := c.DailyCloses(context.Background(), []string{"QQQ", "NOPE", "TQQQ"}, day(2026, 10, 28))
	if err == nil || errors.Is(err, ErrTemporary) || !strings.Contains(err.Error(), "NOPE") {
		t.Fatalf("NOPE에서 영구 실패해야 한다: %v", err)
	}
	if got != nil || len(f.reqs) != 2 {
		t.Errorf("결과 %v, 요청 %d번", got, len(f.reqs))
	}
}

// 같은 날짜 바가 두 번 오면 나중 것, 조회 끝 이후 시각의 바는 버린다.
func TestDuplicateAndFutureBars(t *testing.T) {
	// 11-05 장중에 받은 모양: 09:30 바 뒤에 같은 날 최신가 바가 따로 붙고, 조회 끝 이후 바도 하나 있다
	js := `{"chart":{"result":[{"meta":{"currency":"USD","symbol":"TQQQ","exchangeTimezoneName":"America/New_York","instrumentType":"ETF","dataGranularity":"1d","range":""},` +
		`"timestamp":[1793802600,1793889000,1793900000,1793930871],` +
		`"indicators":{"quote":[{"close":[62.2400016784668,55.1,54.9,99.0]}],"adjclose":[{"adjclose":[62.2400016784668,55.1,54.9,99.0]}]}}],"error":null}}`
	f := &fakeYahoo{resp: map[string]func(http.ResponseWriter){"TQQQ": body(js)}}
	got, err := newClient(t, f).DailyCloses(context.Background(), []string{"TQQQ"}, day(2026, 11, 4))
	if err != nil {
		t.Fatal(err)
	}
	checkBars(t, "TQQQ", got["TQQQ"], []want{
		{day(2026, 11, 4), 62.2400016784668},
		{day(2026, 11, 5), 54.9},
	})
}

// 일봉 시각 → 현지 날짜. 야후가 자정을 전날 22:00·23:00 정각으로 찍는 버그도 다음 날로 올린다.
func TestLocalDay(t *testing.T) {
	ny, _ := time.LoadLocation("America/New_York")
	ldn, _ := time.LoadLocation("Europe/London")
	akl, _ := time.LoadLocation("Pacific/Auckland")
	cases := []struct {
		at   time.Time
		loc  *time.Location
		want time.Time
	}{
		{time.Date(2026, 10, 30, 13, 30, 0, 0, time.UTC), ny, day(2026, 10, 30)}, // EDT 09:30
		{time.Date(2026, 11, 2, 14, 30, 0, 0, time.UTC), ny, day(2026, 11, 2)},   // EST 09:30
		{time.Date(2026, 10, 22, 23, 0, 0, 0, time.UTC), ldn, day(2026, 10, 23)}, // BST 자정
		{time.Date(2026, 10, 26, 0, 0, 0, 0, time.UTC), ldn, day(2026, 10, 26)},  // GMT 자정
		{time.Date(2026, 11, 4, 23, 0, 0, 0, time.UTC), ldn, day(2026, 11, 5)},   // GMT 23:00 정각 — 버그 보정
		{time.Date(2026, 11, 4, 22, 0, 0, 0, time.UTC), ldn, day(2026, 11, 5)},   // 22:00 정각도
		{time.Date(2026, 11, 4, 23, 30, 0, 0, time.UTC), ldn, day(2026, 11, 4)},  // 정각이 아니면 그대로
		// 위 보정 때문에 런던 BST는 UTC로 잘라도 우연히 맞는다. 현지 변환을 빼먹으면 여기서 틀린다
		{time.Date(2026, 11, 5, 21, 0, 0, 0, time.UTC), akl, day(2026, 11, 6)}, // NZDT 10:00
	}
	for _, tc := range cases {
		if got := localDay(tc.at.Unix(), tc.loc); !got.Equal(tc.want) {
			t.Errorf("%v (%s) → %v, 기대 %v", tc.at, tc.loc, got, tc.want)
		}
	}
}
