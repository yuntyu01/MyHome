package toss

// 토스증권 어댑터 검증 — TQ tests/test_toss.py 이식.
//
// 네트워크 없이 돈다. 가짜 서버(httptest)가 요청을 받아 적고 미리 정한 응답을 돌려준다.
// 여기 있는 값은 전부 토스 규범 스펙(openapi.json, 2026-08-06 확인)에서 온 것이다.
// 필드명을 찍어 쓰다가 실전 주문이 전부 거부된 것이 KIS에서 겪은 일이라,
// 이 파일은 문서에 적힌 모양 그대로 응답을 만들어 놓고 대조한다.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yuntyu01/MyHome/server/internal/invest/broker"
)

// ── 가짜 토스 서버 (TQ의 _Session) ────────────────────────────────────

type seen struct {
	Method, Path string
	Query        url.Values
	Form         url.Values
	JSON         map[string]any
	Header       http.Header
}

// dropConn을 응답으로 주면 요청을 다 읽은 뒤 답 없이 연결을 끊는다 — 접수됐는지 모르는 상황이다.
type dropConn struct{}

type fakeToss struct {
	srv *httptest.Server

	mu          sync.Mutex
	calls       []seen
	routes      map[string]func(seen) (int, any)
	tokenIssues int
}

func newFake(t *testing.T) *fakeToss {
	t.Helper()
	f := &fakeToss{routes: map[string]func(seen) (int, any){}}
	f.srv = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeToss) route(method, path string, status int, body any) {
	f.routeFn(method, path, func(seen) (int, any) { return status, body })
}

// routeFn의 fn은 서버 잠금 안에서 돈다 — 클로저 상태를 따로 잠글 필요가 없다.
func (f *fakeToss) routeFn(method, path string, fn func(seen) (int, any)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.routes[method+" "+path] = fn
}

func (f *fakeToss) callsTo(method, path string) []seen {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []seen
	for _, c := range f.calls {
		if c.Method == method && c.Path == path {
			out = append(out, c)
		}
	}
	return out
}

func (f *fakeToss) issues() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.tokenIssues
}

func (f *fakeToss) serve(w http.ResponseWriter, r *http.Request) {
	raw, _ := io.ReadAll(r.Body)
	s := seen{Method: r.Method, Path: r.URL.Path, Query: r.URL.Query(), Header: r.Header.Clone()}
	switch ct := r.Header.Get("Content-Type"); {
	case strings.HasPrefix(ct, "application/x-www-form-urlencoded"):
		s.Form, _ = url.ParseQuery(string(raw))
	case strings.HasPrefix(ct, "application/json"):
		_ = json.Unmarshal(raw, &s.JSON)
	}

	f.mu.Lock()
	f.calls = append(f.calls, s)
	if s.Path == "/oauth2/token" {
		f.tokenIssues++
	}
	status, body := http.StatusNotFound, any(apiErr("not-found", ""))
	if fn := f.routes[s.Method+" "+s.Path]; fn != nil {
		status, body = fn(s)
	}
	f.mu.Unlock()

	if _, ok := body.(dropConn); ok {
		if conn, _, err := w.(http.Hijacker).Hijack(); err == nil {
			_ = conn.Close()
		}
		return
	}
	if body == nil { // 파이썬 _Resp.json()이 실패하는 경우 — JSON이 아닌 빈 본문
		w.WriteHeader(status)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func ok(result any) map[string]any { return map[string]any{"result": result} }

func apiErr(code, msg string) map[string]any {
	return map[string]any{"error": map[string]any{"requestId": "r", "code": code, "message": msg}}
}

// ── 픽스처 ────────────────────────────────────────────────────────────

// fixedNow는 미국 장중(UTC 15:17)이다 — 그 시각의 한국 날짜는 다음 날이다.
var fixedNow = time.Date(2026, 8, 6, 15, 17, 0, 0, time.UTC)

type fixture struct {
	*fakeToss
	c      *Client
	log    *bytes.Buffer
	sleeps []time.Duration
}

var ctx = context.Background()

// newClient는 가짜 서버에 붙은 클라이언트다. 연결을 재사용하지 않는다 — http.Transport가
// 재사용 연결에서 조용히 한 번 더 보내는 것을 막아 서버가 받은 횟수를 정확히 센다.
func newClient(t *testing.T, f *fakeToss, cfg Config) *fixture {
	t.Helper()
	fx := &fixture{fakeToss: f, log: &bytes.Buffer{}}
	fx.c = &Client{
		Config:     cfg,
		BaseURL:    f.srv.URL,
		TokenFile:  filepath.Join(t.TempDir(), "tok.json"),
		HTTPClient: &http.Client{Transport: &http.Transport{DisableKeepAlives: true}, Timeout: 5 * time.Second},
		Now:        func() time.Time { return fixedNow },
		Sleep: func(_ context.Context, d time.Duration) error {
			fx.sleeps = append(fx.sleeps, d)
			return nil
		},
		Log: fx.log,
	}
	return fx
}

// client는 TQ 테스트의 client 픽스처다.
func client(t *testing.T) *fixture {
	t.Helper()
	f := newFake(t)
	f.route("POST", "/oauth2/token", 200,
		map[string]any{"access_token": "TOKEN-1", "token_type": "Bearer", "expires_in": 1800})
	f.route("GET", "/api/v1/accounts", 200,
		ok([]any{map[string]any{"accountNo": "12345678901", "accountSeq": 42, "accountType": "BROKERAGE"}}))
	return newClient(t, f, Config{AppKey: "KEY-abcdef", AppSecret: "SECRET-uvwxyz", Account: "123-456-78901"})
}

func tossErr(t *testing.T, err error) *Error {
	t.Helper()
	var te *Error
	if !errors.As(err, &te) {
		t.Fatalf("toss.Error가 아니다: %T %v", err, err)
	}
	return te
}

func mustContain(t *testing.T, err error, want string) {
	t.Helper()
	if err == nil {
		t.Fatalf("에러가 나야 한다 (%q)", want)
	}
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("에러에 %q가 없다: %v", want, err)
	}
}

// ── 1. 토큰 ───────────────────────────────────────────────────────────

func TestTheTokenIsIssuedOnceAndCached(t *testing.T) {
	fx := client(t)
	for range 2 {
		if tok, err := fx.c.Token(ctx); err != nil || tok != "TOKEN-1" {
			t.Fatalf("Token = %q, %v", tok, err)
		}
	}
	if n := fx.issues(); n != 1 {
		t.Fatalf("캐시가 있는데 다시 발급했다 (%d회)", n)
	}
	call := fx.callsTo("POST", "/oauth2/token")[0]
	if call.Form.Get("grant_type") != "client_credentials" { // 문서 확정 값
		t.Errorf("grant_type = %q", call.Form.Get("grant_type"))
	}
	if call.Form.Get("client_id") != "KEY-abcdef" {
		t.Errorf("client_id = %q", call.Form.Get("client_id"))
	}
	if ct := call.Header.Get("Content-Type"); ct != "application/x-www-form-urlencoded" {
		t.Errorf("토큰 발급은 폼이어야 한다: %q", ct)
	}
}

func TestTheTokenFileIsNotWorldReadable(t *testing.T) {
	fx := client(t)
	if _, err := fx.c.Token(ctx); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(fx.c.TokenFile)
	if err != nil {
		t.Fatal(err)
	}
	var saved map[string]any
	if err := json.Unmarshal(b, &saved); err != nil {
		t.Fatal(err)
	}
	if saved["token"] != "TOKEN-1" || saved["expires_at"] == nil {
		t.Fatalf("캐시 내용: %v", saved)
	}
	if runtime.GOOS != "windows" {
		st, _ := os.Stat(fx.c.TokenFile)
		if st.Mode().Perm() != 0o600 {
			t.Fatalf("권한 = %o", st.Mode().Perm())
		}
	}
}

func TestA401DropsTheTokenAndReissuesOnce(t *testing.T) {
	fx := client(t)
	fx.route("GET", "/api/v1/holdings", 200, ok(map[string]any{"items": []any{}}))
	if _, err := fx.c.Token(ctx); err != nil { // 캐시 채움
		t.Fatal(err)
	}
	// 첫 호출만 오류 본문이고, 둘째도 401 라우트라 결국 실패한다
	n := 0
	fx.routeFn("GET", "/api/v1/buying-power", func(seen) (int, any) {
		n++
		if n == 1 {
			return 401, apiErr("expired-token", "")
		}
		return 401, ok(map[string]any{"cashBuyingPower": "100.00", "currency": "USD"})
	})

	_, err := fx.c.cashUSD(ctx)
	if te := tossErr(t, err); te.Status != 401 {
		t.Fatalf("Status = %d", te.Status)
	}
	if got := fx.issues(); got != 2 {
		t.Fatalf("토큰을 정확히 한 번 다시 받아야 한다 (발급 %d회)", got)
	}
	if got := len(fx.callsTo("GET", "/api/v1/buying-power")); got != 2 {
		t.Fatalf("401 재시도는 1회뿐이다 (호출 %d회)", got)
	}
}

// ── 2. 계좌 ───────────────────────────────────────────────────────────

// 화면에는 하이픈이 보이고 API는 seq를 요구한다 — 사람이 어느 쪽을 넣든 통해야 한다.
func TestTheAccountNumberIsMatchedWithHyphensStripped(t *testing.T) {
	fx := client(t)
	if seq, err := fx.c.AccountSeq(ctx); err != nil || seq != 42 {
		t.Fatalf("AccountSeq = %d, %v", seq, err)
	}
}

func TestAnAccountSeqIsAcceptedDirectly(t *testing.T) {
	f := newFake(t)
	f.route("POST", "/oauth2/token", 200, map[string]any{"access_token": "T", "expires_in": 900})
	f.route("GET", "/api/v1/accounts", 200,
		ok([]any{map[string]any{"accountNo": "99999999999", "accountSeq": 7, "accountType": "BROKERAGE"}}))
	fx := newClient(t, f, Config{AppKey: "k", AppSecret: "s", Account: "7"})
	if seq, err := fx.c.AccountSeq(ctx); err != nil || seq != 7 {
		t.Fatalf("AccountSeq = %d, %v", seq, err)
	}
}

func TestAnUnknownAccountStopsInsteadOfGuessing(t *testing.T) {
	f := newFake(t)
	f.route("POST", "/oauth2/token", 200, map[string]any{"access_token": "T", "expires_in": 900})
	f.route("GET", "/api/v1/accounts", 200,
		ok([]any{map[string]any{"accountNo": "111", "accountSeq": 1, "accountType": "BROKERAGE"}}))
	fx := newClient(t, f, Config{AppKey: "k", AppSecret: "s", Account: "22222222"})
	_, err := fx.c.AccountSeq(ctx)
	tossErr(t, err)
	mustContain(t, err, "맞는 계좌를 찾지 못했다")
}

func TestTheAccountHeaderRidesOnEveryAccountAPI(t *testing.T) {
	fx := client(t)
	fx.route("GET", "/api/v1/buying-power", 200, ok(map[string]any{"currency": "USD", "cashBuyingPower": "10"}))
	if _, err := fx.c.cashUSD(ctx); err != nil {
		t.Fatal(err)
	}
	call := fx.callsTo("GET", "/api/v1/buying-power")[0]
	if got := call.Header.Get("X-Tossinvest-Account"); got != "42" {
		t.Fatalf("계좌 헤더 = %q", got)
	}
}

// ── 3. 잔고 ───────────────────────────────────────────────────────────

func TestBalanceKeepsUSHoldingsAndDropsTheRest(t *testing.T) {
	fx := client(t)
	fx.route("GET", "/api/v1/holdings", 200, ok(map[string]any{"items": []any{
		map[string]any{"symbol": "TQQQ", "marketCountry": "US", "currency": "USD",
			"quantity": "5", "averagePurchasePrice": "70.5", "marketValue": map[string]any{"amount": "355.4"}},
		map[string]any{"symbol": "005930", "marketCountry": "KR", "currency": "KRW",
			"quantity": "10", "averagePurchasePrice": "70000", "marketValue": map[string]any{"amount": "700000"}},
		map[string]any{"symbol": "QQQM", "marketCountry": "US", "currency": "USD",
			"quantity": "0", "averagePurchasePrice": "0", "marketValue": map[string]any{"amount": "0"}},
	}}))
	fx.route("GET", "/api/v1/buying-power", 200, ok(map[string]any{"currency": "USD", "cashBuyingPower": "395.89"}))

	bal, err := fx.c.Balance(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(bal.Holdings) != 1 || bal.Holdings[0].Symbol != "TQQQ" { // 국내·0주는 뺀다
		t.Fatalf("Holdings = %+v", bal.Holdings)
	}
	if bal.SharesOf("TQQQ") != 5 {
		t.Errorf("SharesOf = %d", bal.SharesOf("TQQQ"))
	}
	if !approx(bal.Holdings[0].AvgPriceUSD, 70.5) || !approx(bal.CashUSD, 395.89) {
		t.Errorf("평균가·현금 = %v, %v", bal.Holdings[0].AvgPriceUSD, bal.CashUSD)
	}
}

// 소수 수량은 미국 시장가 매도에서만 생긴다. 장부는 정수주라 버리되 남긴다.
func TestAFractionalHoldingIsFlooredButNotSilently(t *testing.T) {
	fx := client(t)
	fx.route("GET", "/api/v1/holdings", 200, ok(map[string]any{"items": []any{
		map[string]any{"symbol": "TQQQ", "marketCountry": "US", "currency": "USD",
			"quantity": "5.4", "averagePurchasePrice": "70", "marketValue": map[string]any{"amount": "378"}},
	}}))
	fx.route("GET", "/api/v1/buying-power", 200, ok(map[string]any{"currency": "USD", "cashBuyingPower": "0"}))
	bal, err := fx.c.Balance(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if bal.SharesOf("TQQQ") != 5 {
		t.Errorf("SharesOf = %d", bal.SharesOf("TQQQ"))
	}
	if !strings.Contains(fx.log.String(), "[toss] ") || !strings.Contains(fx.log.String(), "소수 수량") {
		t.Errorf("경고가 없다: %q", fx.log.String())
	}
}

// ── 4. 매수 한도 ──────────────────────────────────────────────────────

// 토스는 금액만 준다 — 수량은 어댑터가 낸다.
func TestBuyableTurnsMoneyIntoShares(t *testing.T) {
	fx := client(t)
	fx.route("GET", "/api/v1/buying-power", 200, ok(map[string]any{"currency": "USD", "cashBuyingPower": "395.89"}))
	b, err := fx.c.Buyable(ctx, "TQQQ", 71.08)
	if err != nil {
		t.Fatal(err)
	}
	if !approx(b.CashUSD, 395.89) || b.MaxQty != 5 || b.Symbol != "TQQQ" { // floor(395.89 / 71.08)
		t.Fatalf("Buyable = %+v", b)
	}
}

func TestAnEmptyAccountReportsZeroNotAnError(t *testing.T) {
	fx := client(t)
	fx.route("GET", "/api/v1/buying-power", 200, ok(map[string]any{"currency": "USD", "cashBuyingPower": "0"}))
	b, err := fx.c.Buyable(ctx, "TQQQ", 71.08)
	if err != nil || b.MaxQty != 0 {
		t.Fatalf("Buyable = %+v, %v", b, err)
	}
	if !strings.Contains(fx.log.String(), "매수가능 0주") {
		t.Errorf("경고가 없다: %q", fx.log.String())
	}
}

// ── 5. 주문 (승격 순서) ───────────────────────────────────────────────

// 🔴 KIS는 이 값을 찍어 쓰다가 실전 주문이 전부 거부됐다.
// 토스는 문서가 orderType + timeInForce 조합으로 정의한다 — MARKET+CLS = MOC, LIMIT+CLS = LOC.
// 그 표를 그대로 옮겼는지 고정한다.
func TestEachStyleMapsToTheDocumentedCombination(t *testing.T) {
	for _, tc := range []struct {
		style          broker.Style
		orderType, tif string
		hasPrice       bool
	}{
		{broker.MOC, "MARKET", "CLS", false}, // 장마감시장가 — 백테스트가 종가 기준
		{broker.Market, "MARKET", "DAY", false},
		{broker.Sweep, "LIMIT", "DAY", true},
	} {
		t.Run(string(tc.style), func(t *testing.T) {
			fx := client(t)
			fx.route("POST", "/api/v1/orders", 200, ok(map[string]any{"orderId": "ORD-1", "clientOrderId": nil}))
			res, err := fx.c.Order(ctx, "TQQQ", broker.Buy, 5, 71.08, tc.style)
			if err != nil {
				t.Fatal(err)
			}
			sent := fx.callsTo("POST", "/api/v1/orders")[0].JSON
			if sent["orderType"] != tc.orderType || sent["timeInForce"] != tc.tif {
				t.Errorf("조합 = %v + %v", sent["orderType"], sent["timeInForce"])
			}
			if sent["side"] != "BUY" || sent["symbol"] != "TQQQ" {
				t.Errorf("side·symbol = %v, %v", sent["side"], sent["symbol"])
			}
			if sent["quantity"] != "5" { // 문자열로 보낸다
				t.Errorf("quantity = %#v", sent["quantity"])
			}
			price, has := sent["price"]
			if has != tc.hasPrice {
				t.Errorf("price 유무 = %v", has)
			}
			if tc.hasPrice && price != "71.08" {
				t.Errorf("price = %#v", price)
			}
			if res.OrderID != "ORD-1" {
				t.Errorf("OrderID = %q", res.OrderID)
			}
		})
	}
}

// 🔴 멱등성 키다 — 같은 값으로 다시 보내면 토스가 이전 주문을 돌려준다.
// 승격하면 유형이 달라지므로 새 주문이 되고, 같은 유형 재실행은 한 건이 된다.
func TestTheClientOrderIDIsDeterministicPerStyle(t *testing.T) {
	fx := client(t)
	fx.route("POST", "/api/v1/orders", 200, ok(map[string]any{"orderId": "ORD-1"}))
	for _, st := range []broker.Style{broker.MOC, broker.MOC, broker.Market} {
		if _, err := fx.c.Order(ctx, "TQQQ", broker.Buy, 5, 71.08, st); err != nil {
			t.Fatal(err)
		}
	}
	var keys []string
	for _, c := range fx.callsTo("POST", "/api/v1/orders") {
		keys = append(keys, fmt.Sprint(c.JSON["clientOrderId"]))
	}
	if keys[0] != keys[1] {
		t.Errorf("같은 주문인데 키가 달라 이중 주문이 된다: %v", keys)
	}
	if keys[2] == keys[0] {
		t.Errorf("승격했는데 같은 키라 새 주문이 안 나간다: %v", keys)
	}
	// UTC 08-06 15:17은 KST 08-07이다 — 증권사 달력으로 찍는다
	if keys[0] != "tq-20260807-TQQQ-buy-5-moc" {
		t.Errorf("키 = %q", keys[0])
	}
}

func TestAnOrderWithoutAnIDIsNotTreatedAsAccepted(t *testing.T) {
	fx := client(t)
	fx.route("POST", "/api/v1/orders", 200, ok(map[string]any{"clientOrderId": "x"}))
	_, err := fx.c.Order(ctx, "TQQQ", broker.Buy, 1, 70.0, broker.Sweep)
	tossErr(t, err)
	mustContain(t, err, "orderId 없음")
}

// 주문은 멱등 호출이 아니다 — 접수 여부를 모르는 채로 다시 보내지 않는다.
func TestARejectedOrderIsNeverRetried(t *testing.T) {
	fx := client(t)
	fx.route("POST", "/api/v1/orders", 422, apiErr("insufficient-buying-power", "주문 가능 금액이 부족합니다."))
	if _, err := fx.c.Order(ctx, "TQQQ", broker.Buy, 5, 71.08, broker.Sweep); err == nil {
		t.Fatal("거부된 주문이 성공했다")
	}
	if n := len(fx.callsTo("POST", "/api/v1/orders")); n != 1 {
		t.Fatalf("주문 POST %d회", n)
	}
}

// 🔴 응답 전에 연결이 끊기면 토스가 받았는지 모른다. 다시 보내면 이중 주문일 수 있다.
// 연결을 재사용하는 기본 Transport로 돌린다 — Transport가 몰래 다시 보내지도 않는지까지 본다.
func TestTheOrderPostIsNotRetriedOnConnectionError(t *testing.T) {
	fx := client(t)
	fx.c.HTTPClient = fx.srv.Client()
	fx.route("POST", "/api/v1/orders", 0, dropConn{})
	_, err := fx.c.Order(ctx, "TQQQ", broker.Buy, 5, 71.08, broker.Sweep)
	if err == nil {
		t.Fatal("끊긴 주문이 성공했다")
	}
	if n := len(fx.callsTo("POST", "/api/v1/orders")); n != 1 {
		t.Fatalf("주문 POST %d회 — 다시 보냈다", n)
	}
	if len(fx.sleeps) != 0 {
		t.Fatalf("재시도 대기를 했다: %v", fx.sleeps)
	}
}

// 조회는 연결이 끊기면 1초·3초 쉬고 다시 보낸다 (3회까지).
func TestReadsAreRetriedOnConnectionError(t *testing.T) {
	t.Run("셋째에 붙음", func(t *testing.T) {
		fx := client(t)
		n := 0
		fx.routeFn("GET", "/api/v1/buying-power", func(seen) (int, any) {
			if n++; n < 3 {
				return 0, dropConn{}
			}
			return 200, ok(map[string]any{"cashBuyingPower": "12.5"})
		})
		cash, err := fx.c.cashUSD(ctx)
		if err != nil || cash != 12.5 {
			t.Fatalf("cashUSD = %v, %v", cash, err)
		}
		if !slices.Equal(fx.sleeps, []time.Duration{time.Second, 3 * time.Second}) {
			t.Errorf("대기 = %v", fx.sleeps)
		}
		if !strings.Contains(fx.log.String(), "연결 끊김") {
			t.Errorf("경고가 없다: %q", fx.log.String())
		}
	})
	t.Run("끝내 끊김", func(t *testing.T) {
		fx := client(t)
		fx.route("GET", "/api/v1/buying-power", 0, dropConn{})
		_, err := fx.c.cashUSD(ctx)
		tossErr(t, err)
		mustContain(t, err, "연결이 3회 끊겼다")
		if n := len(fx.callsTo("GET", "/api/v1/buying-power")); n != 3 {
			t.Errorf("시도 %d회", n)
		}
	})
}

// ── 6. 오류 판별 ──────────────────────────────────────────────────────

// 자금 부족은 주문 유형을 바꿔도 안 풀린다.
func TestInsufficientFundsStopsThePromotion(t *testing.T) {
	fx := client(t)
	fx.route("POST", "/api/v1/orders", 422, apiErr("insufficient-buying-power", "주문 가능 금액이 부족합니다."))
	_, err := fx.c.Order(ctx, "TQQQ", broker.Buy, 5, 71.08, broker.Sweep)
	te := tossErr(t, err)
	if te.Code != "insufficient-buying-power" || te.Status != 422 {
		t.Errorf("Code·Status = %q, %d", te.Code, te.Status)
	}
	if !fx.c.IsFundsError(err) {
		t.Error("자금 부족을 못 알아봤다")
	}
	mustContain(t, err, "주문가능금액 부족")
}

// 🔴 시간 때문에 막힌 것은 재시도로 풀린다 — 자금 부족과 섞으면 엉뚱한 데를 뒤진다.
func TestClosedHoursIsNotAFundsError(t *testing.T) {
	fx := client(t)
	fx.route("POST", "/api/v1/orders", 422, map[string]any{"error": map[string]any{
		"requestId": "r", "code": "order-hours-closed",
		"message": "현재 해당 주문을 접수할 수 없는 시간입니다.",
		"data":    map[string]any{"retryAfterAt": "2026-08-06T22:30:00+09:00"}}})
	_, err := fx.c.Order(ctx, "TQQQ", broker.Buy, 5, 71.08, broker.Sweep)
	if fx.c.IsFundsError(err) {
		t.Error("시간 제한을 자금 부족으로 봤다")
	}
	mustContain(t, err, "2026-08-06T22:30:00+09:00")
}

// 등록 안 된 IP는 403이다. 이유를 모르면 자격증명을 의심하게 된다.
func TestABlockedIPSaysWhatToDo(t *testing.T) {
	fx := client(t)
	fx.route("POST", "/api/v1/orders", 403, apiErr("forbidden", ""))
	_, err := fx.c.Order(ctx, "TQQQ", broker.Buy, 1, 70.0, broker.Sweep)
	mustContain(t, err, "허용 IP")
}

func TestPrerequisitesAreNamed(t *testing.T) {
	fx := client(t)
	fx.route("POST", "/api/v1/orders", 422, apiErr("prerequisite-required", "사전 자격 요건 미충족"))
	_, err := fx.c.Order(ctx, "TQQQ", broker.Buy, 1, 70.0, broker.Sweep)
	mustContain(t, err, "약관 동의")
}

// ── 7. 체결 ───────────────────────────────────────────────────────────

func orderRow(oid string, qty, filled int, price, status string) map[string]any {
	var avg any
	if filled > 0 {
		avg = price
	}
	return map[string]any{"orderId": oid, "symbol": "TQQQ", "side": "BUY", "orderType": "LIMIT",
		"timeInForce": "DAY", "status": status, "price": "71.08",
		"quantity": fmt.Sprint(qty), "currency": "USD",
		"orderedAt": "2026-08-06T23:39:00+09:00",
		"execution": map[string]any{"filledQuantity": fmt.Sprint(filled), "averageFilledPrice": avg,
			"filledAmount": nil, "commission": nil, "tax": nil, "filledAt": nil, "settlementDate": nil}}
}

func page(rows ...any) map[string]any {
	if rows == nil {
		rows = []any{}
	}
	return ok(map[string]any{"orders": rows, "nextCursor": nil, "hasNext": false})
}

func day(y int, m time.Month, d int) time.Time { return time.Date(y, m, d, 0, 0, 0, 0, broker.KST) }

// 🔴 부분 체결은 진행 중 주문에 들어 있다 — CLOSED만 보면 못 본다.
func TestFillsIncludePartialsStillOpen(t *testing.T) {
	fx := client(t)
	fx.routeFn("GET", "/api/v1/orders", func(c seen) (int, any) {
		if c.Query.Get("status") == "CLOSED" {
			return 200, page(orderRow("DONE", 5, 5, "71.0", "FILLED"))
		}
		return 200, page(orderRow("LIVE", 5, 2, "70.5", "PARTIAL_FILLED"))
	})
	got, err := fx.c.Fills(ctx, "TQQQ", day(2026, 8, 6), day(2026, 8, 6))
	if err != nil {
		t.Fatal(err)
	}
	qty := map[string]int{}
	for _, f := range got {
		qty[f.OrderID] = f.Qty
		if f.Side != broker.Buy {
			t.Errorf("Side = %q", f.Side)
		}
	}
	if len(qty) != 2 || qty["DONE"] != 5 || qty["LIVE"] != 2 {
		t.Fatalf("체결 = %v", qty)
	}
}

func TestUnfilledOrdersDoNotBecomeFills(t *testing.T) {
	fx := client(t)
	fx.route("GET", "/api/v1/orders", 200, page(orderRow("WAIT", 5, 0, "", "PENDING")))
	got, err := fx.c.Fills(ctx, "TQQQ", day(2026, 8, 6), day(2026, 8, 6))
	if err != nil || len(got) != 0 {
		t.Fatalf("Fills = %+v, %v", got, err)
	}
}

// 체결은 있는데 단가가 비면 장부에 0을 쓰게 된다 — 그 전에 멈춘다.
func TestAFilledOrderWithoutAPriceStops(t *testing.T) {
	fx := client(t)
	row := orderRow("BAD", 5, 5, "", "FILLED")
	row["execution"].(map[string]any)["averageFilledPrice"] = nil
	fx.route("GET", "/api/v1/orders", 200, page(row))
	_, err := fx.c.Fills(ctx, "TQQQ", day(2026, 8, 6), day(2026, 8, 6))
	tossErr(t, err)
	mustContain(t, err, "평균 체결가")
}

func TestTheQueryWindowIsISODates(t *testing.T) {
	fx := client(t)
	fx.route("GET", "/api/v1/orders", 200, page())
	if _, err := fx.c.Fills(ctx, "TQQQ", day(2026, 8, 5), day(2026, 8, 7)); err != nil {
		t.Fatal(err)
	}
	var closed *seen
	for _, c := range fx.callsTo("GET", "/api/v1/orders") {
		if c.Query.Get("status") == "CLOSED" {
			closed = &c
			break
		}
	}
	if closed == nil {
		t.Fatal("CLOSED 조회가 없다")
	}
	if closed.Query.Get("from") != "2026-08-05" || closed.Query.Get("to") != "2026-08-07" {
		t.Fatalf("구간 = %s ~ %s", closed.Query.Get("from"), closed.Query.Get("to"))
	}
	if closed.Query.Get("limit") != "100" || closed.Query.Get("symbol") != "TQQQ" {
		t.Errorf("limit·symbol = %s, %s", closed.Query.Get("limit"), closed.Query.Get("symbol"))
	}
}

func TestClosedOrdersArePagedToTheEnd(t *testing.T) {
	fx := client(t)
	pages := 0
	fx.routeFn("GET", "/api/v1/orders", func(c seen) (int, any) {
		pages++
		if c.Query.Get("status") == "OPEN" {
			return 200, page()
		}
		if pages == 1 {
			return 200, ok(map[string]any{"orders": []any{orderRow("A", 1, 1, "70.0", "FILLED")},
				"nextCursor": "c2", "hasNext": true})
		}
		if c.Query.Get("cursor") != "c2" {
			return 400, apiErr("bad-cursor", "")
		}
		return 200, page(orderRow("B", 1, 1, "71.0", "FILLED"))
	})
	got, err := fx.c.Fills(ctx, "TQQQ", day(2026, 8, 6), day(2026, 8, 6))
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, f := range got {
		ids = append(ids, f.OrderID)
	}
	slices.Sort(ids)
	if !slices.Equal(ids, []string{"A", "B"}) {
		t.Fatalf("체결 = %v", ids)
	}
}

// 필드를 가정하지 않는다 — 호출부가 값 어딘가에서 주문번호를 찾는다.
func TestOpenOrdersAreReturnedRaw(t *testing.T) {
	fx := client(t)
	fx.route("GET", "/api/v1/orders", 200, page(orderRow("LIVE", 5, 0, "", "PENDING")))
	rows, err := fx.c.OpenOrders(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) == 0 || rows[0]["orderId"] != "LIVE" {
		t.Fatalf("OpenOrders = %v", rows)
	}
}

// ── 8. 시크릿 ─────────────────────────────────────────────────────────

func TestSecretsNeverBecomeAString(t *testing.T) {
	cfg := Config{AppKey: "KEY-abcdef", AppSecret: "SECRET-uvwxyz", Account: "12345678901"}
	j, _ := json.Marshal(cfg)
	for _, text := range []string{
		cfg.String(), cfg.GoString(), string(j),
		fmt.Sprintf("%v", cfg), fmt.Sprintf("%+v", cfg), fmt.Sprintf("%#v", cfg),
		fmt.Sprintf("%s", cfg), fmt.Sprintf("%d", cfg), fmt.Sprintf("%v", &cfg),
		fmt.Sprintf("%+v", &Client{Config: cfg}),
	} {
		if strings.Contains(text, "KEY-abcdef") || strings.Contains(text, "SECRET-uvwxyz") ||
			strings.Contains(text, "12345678901") {
			t.Errorf("시크릿이 문자열이 됐다: %s", text)
		}
	}
	if got := cfg.Scrub("보낸 값: SECRET-uvwxyz"); got != "보낸 값: ***" {
		t.Errorf("Scrub = %q", got)
	}
}

func TestTheErrorMessageIsScrubbed(t *testing.T) {
	fx := client(t)
	fx.route("POST", "/api/v1/orders", 400, apiErr("invalid-request", "bad key SECRET-uvwxyz"))
	_, err := fx.c.Order(ctx, "TQQQ", broker.Buy, 1, 70.0, broker.Sweep)
	if err == nil || strings.Contains(err.Error(), "SECRET-uvwxyz") {
		t.Fatalf("시크릿이 에러에 실렸다: %v", err)
	}
}

// ── 9. 계약 ───────────────────────────────────────────────────────────

// 국경에 서는 타입은 broker 소유다 — 어댑터가 다른 타입을 돌려주면 호출부가 증권사를 알아야 한다.
// Go에서는 시그니처가 타입을 고정하므로, 계약을 만족하는지와 세 호출이 실제로 통하는지를 본다.
func TestTheAdapterReturnsTheSharedBorderTypes(t *testing.T) {
	fx := client(t)
	fx.route("GET", "/api/v1/holdings", 200, ok(map[string]any{"items": []any{}}))
	fx.route("GET", "/api/v1/buying-power", 200, ok(map[string]any{"currency": "USD", "cashBuyingPower": "1"}))
	fx.route("POST", "/api/v1/orders", 200, ok(map[string]any{"orderId": "O"}))

	var b broker.Broker = fx.c
	if _, err := b.Balance(ctx); err != nil {
		t.Error(err)
	}
	if _, err := b.Buyable(ctx, "TQQQ", 70.0); err != nil {
		t.Error(err)
	}
	if res, err := b.Order(ctx, "TQQQ", broker.Buy, 1, 70.0, broker.Sweep); err != nil || res.Price != 70 {
		t.Errorf("Order = %+v, %v", res, err)
	}
	if b.Env() != "real" || b.AccountMasked() != "123-****" {
		t.Errorf("Env·AccountMasked = %q, %q", b.Env(), b.AccountMasked())
	}
}

// ── 이식 추가분 ───────────────────────────────────────────────────────

// 🔴 토스는 클라이언트당 유효 토큰이 하나다 — 프로세스가 새로 떠도 캐시를 이어 써야 한다.
func TestTheTokenCacheOutlivesTheClient(t *testing.T) {
	fx := client(t)
	if _, err := fx.c.Token(ctx); err != nil {
		t.Fatal(err)
	}
	next := newClient(t, fx.fakeToss, fx.c.Config)
	next.c.TokenFile = fx.c.TokenFile
	if _, err := next.c.Token(ctx); err != nil {
		t.Fatal(err)
	}
	if n := fx.issues(); n != 1 {
		t.Fatalf("새 클라이언트가 다시 발급했다 (%d회)", n)
	}
	// 만료 5분 전부터는 새로 받는다 (1800초 - 5분 = 25분)
	next.c.Now = func() time.Time { return fixedNow.Add(25*time.Minute + time.Second) }
	if _, err := next.c.Token(ctx); err != nil {
		t.Fatal(err)
	}
	if n := fx.issues(); n != 2 {
		t.Fatalf("만료 직전 토큰을 그대로 썼다 (%d회)", n)
	}
}

// 기댓값은 TQ _fmt를 파이썬 3.11로 돌린 결과다.
func TestFormatDecimalMatchesPython(t *testing.T) {
	for _, tc := range []struct {
		v      float64
		places int
		want   string // "" = 에러
	}{
		{5, 0, "5"}, {71.08, 4, "71.08"}, {1000, 4, "1000"}, {70, 4, "70"},
		{0.5, 0, "1"}, {2.5, 0, "3"}, {1.00005, 4, "1.0001"}, {1.00015, 4, "1.0002"},
		{2.675, 2, "2.68"}, {1.005, 2, "1.01"}, {71.085, 2, "71.09"}, {12345.6789, 2, "12345.68"},
		{0.1 + 0.2, 4, "0.3"}, {71.08499999, 4, "71.085"}, {99.99995, 4, "100"},
		{5e-5, 4, "0.0001"}, {0.000125, 4, "0.0001"}, {0.0003, 4, "0.0003"},
		{1e16, 4, "10000000000000000"}, {1e23, 4, "100000000000000000000000"},
		{4e-5, 4, ""}, {1.5e-7, 4, ""}, {0.49, 0, ""}, {0, 0, ""}, {-1, 4, ""}, {-0.0, 4, ""},
		{1e24, 4, ""}, // 29자리 — decimal 정밀도(28) 초과
	} {
		got, err := formatDecimal(tc.v, tc.places, "값")
		if tc.want == "" {
			if err == nil {
				t.Errorf("formatDecimal(%v, %d) = %q, 에러여야 한다", tc.v, tc.places, got)
			}
			continue
		}
		if err != nil || got != tc.want {
			t.Errorf("formatDecimal(%v, %d) = %q, %v; want %q", tc.v, tc.places, got, err, tc.want)
		}
	}
}

func TestFromEnvNamesMissingKeys(t *testing.T) {
	t.Setenv("TOSS_APP_KEY", " key ")
	t.Setenv("TOSS_APP_SECRET", "")
	t.Setenv("TOSS_ACCOUNT", "  ")
	_, err := FromEnv()
	for _, want := range []string{"TOSS_APP_SECRET, TOSS_ACCOUNT", "WTS → 설정 → Open API", "허용 IP"} {
		mustContain(t, err, want)
	}
	if strings.Contains(err.Error(), "TOSS_APP_KEY") {
		t.Errorf("있는 키까지 빠졌다고 한다: %v", err)
	}

	t.Setenv("TOSS_APP_SECRET", "sec")
	t.Setenv("TOSS_ACCOUNT", " 123-45 ")
	cfg, err := FromEnv()
	if err != nil || cfg != (Config{AppKey: "key", AppSecret: "sec", Account: "123-45"}) {
		t.Fatalf("FromEnv = %v, %v", cfg.Account, err)
	}
}

func approx(a, b float64) bool { return a-b < 1e-9 && b-a < 1e-9 }
