// Package toss는 토스증권 Open API 어댑터다 (TQ broker/toss.py 이식). broker.Broker 계약을 구현한다.
//
// 🔴 필드명·엔드포인트는 문서에서 확정한 값이다 (2026-08-06).
// 출처는 규범 스펙 하나다 — https://openapi.tossinvest.com/openapi-docs/latest/openapi.json
// KIS 어댑터가 ORD_DVSN 두 개를 찍어 쓰다가 2026-08-05 실전 주문이 전부 거부됐다.
// 그 하루가 "모르면 안 쓴다"의 근거이고, 여기서는 지어낸 값이 없다. 바뀌면 위 JSON을 다시 받아 대조할 것.
//
// 🔴 왜 토스인가. 해외 레버리지 ETP 기본예탁금 1,000만원(2026-05-22 시행)이 KIS에서 걸린다.
// 시행 전 거래 이력이 있으면 면제이고 사용자는 토스에 이력이 있다. 같은 TQQQ가 한 곳에서만 거래된다.
//
// 🔴 호출 IP를 등록해야 한다. "등록된 허용 IP 목록에 없는 IP 에서의 호출은 403 으로 차단됩니다."
// GitHub 호스티드 러너는 IP가 매번 바뀌므로 그대로는 못 쓴다. 고정 IP가 있는 곳에서 돌려야 한다 —
// 이 패키지는 어디서 돌든 같게 동작한다.
//
// 주문 방식은 문서가 조합으로 정의한다 (승격 순서가 그대로 성립한다):
//
//	moc     MARKET + CLS    장마감시장가 — 백테스트가 종가 기준이라 1순위
//	market  MARKET + DAY    시장가
//	sweep   LIMIT  + DAY    시장가 대용 지정가 (밴드는 호출부가 계산해 넘긴다)
//
// 환경변수:
//
//	TOSS_APP_KEY      client_id       WTS → 설정 → Open API
//	TOSS_APP_SECRET   client_secret   같은 화면
//	TOSS_ACCOUNT      계좌번호 또는 accountSeq (둘 다 받는다)
package toss

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"math"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/yuntyu01/MyHome/server/internal/invest/broker"
)

// BaseURL은 토스증권 Open API 주소다.
const BaseURL = "https://openapi.tossinvest.com"

// AccountHeader는 계좌 컨텍스트가 필요한 API에 accountSeq를 싣는 헤더다.
const AccountHeader = "X-Tossinvest-Account"

// EnvKeys는 FromEnv가 읽는 환경변수다.
var EnvKeys = [...]string{"TOSS_APP_KEY", "TOSS_APP_SECRET", "TOSS_ACCOUNT"}

// 주문 방식 — 문서 확정. timeInForce가 orderType과 결합해 방식을 정한다.
// LIMIT + CLS = LOC, MARKET + CLS = MOC. OPG(장 개시)는 현재 미지원.
type orderStyle struct {
	orderType   string
	timeInForce string
	needsPrice  bool
}

var orderStyles = map[broker.Style]orderStyle{
	broker.MOC:    {orderType: "MARKET", timeInForce: "CLS"},
	broker.Market: {orderType: "MARKET", timeInForce: "DAY"},
	broker.Sweep:  {orderType: "LIMIT", timeInForce: "DAY", needsPrice: true},
}

var (
	sideOut = map[broker.Side]string{broker.Buy: "BUY", broker.Sell: "SELL"}
	sideIn  = map[string]broker.Side{"BUY": broker.Buy, "SELL": broker.Sell}
)

// 🔴 자금 부족은 주문 유형을 바꿔도 안 풀린다 — 승격을 멈춰야 한다.
var fundsErrorCodes = []string{"insufficient-buying-power"}

// 시간 때문에 막힌 것은 재시도로 풀린다. 지금은 계약에 그 질문이 없어 승격 경로를 그대로 타지만,
// 진단 메시지에는 실어 보낸다.
var hoursErrorCodes = []string{"order-hours-closed"}

const (
	envReal     = "real" // 토스는 API 모의투자가 없다
	marketUS    = "US"
	currencyUSD = "USD"
	mask        = "***"

	tokenMargin      = 5 * time.Minute
	tokenFallbackTTL = 1800 * time.Second // expires_in이 없으면 짧게 잡는다 (재발급 1회가 손해의 전부)
	tokenMinTTL      = 60 * time.Second
	timeout          = 15 * time.Second
	maxPages         = 20
	connRetries      = 3
)

var connBackoff = []time.Duration{1 * time.Second, 3 * time.Second}

// Error는 토스 API 오류다. Code가 있으면 그대로 들고 다닌다 — 판별의 유일한 열쇠다.
type Error struct {
	Message string
	Code    string // 토스 오류 코드 (예: insufficient-buying-power). 없으면 ""
	Status  int    // HTTP 상태. 응답을 받기 전 실패면 0
	Err     error  // 원인. 메시지에는 싣지 않는다
}

func (e *Error) Error() string { return e.Message }
func (e *Error) Unwrap() error { return e.Err }

// Config는 토스 설정이다. 시크릿은 어떤 경로로도 문자열이 되지 않는다
// (String·GoString·모든 fmt 동사·JSON이 전부 가린 값을 낸다).
type Config struct {
	AppKey    string // client_id
	AppSecret string // client_secret
	Account   string // 계좌번호 또는 accountSeq
}

func (c Config) String() string {
	return fmt.Sprintf("toss.Config{AppKey: %s, AppSecret: %s, Account: %s, Env: %q}",
		mask, mask, maskAccount(c.Account), envReal)
}

func (c Config) GoString() string { return c.String() }

// Format은 %d·%x 같은 동사로 찍어도 필드가 그대로 새지 않게 막는다.
func (c Config) Format(f fmt.State, _ rune) { _, _ = io.WriteString(f, c.String()) }

func (c Config) MarshalJSON() ([]byte, error) { return json.Marshal(c.String()) }

// Scrub은 문자열에서 앱 키·시크릿을 가린다. 로그와 오류 메시지는 전부 이것을 거친다.
func (c Config) Scrub(text string) string {
	for _, secret := range []string{c.AppSecret, c.AppKey} {
		if utf8Len(secret) >= 4 {
			text = strings.ReplaceAll(text, secret, mask)
		}
	}
	return text
}

// FromEnv는 TOSS_APP_KEY·TOSS_APP_SECRET·TOSS_ACCOUNT를 읽는다. 빠진 키를 전부 이름으로 알려 준다.
func FromEnv() (Config, error) {
	var missing []string
	for _, k := range EnvKeys {
		if strings.TrimSpace(os.Getenv(k)) == "" {
			missing = append(missing, k)
		}
	}
	if len(missing) > 0 {
		return Config{}, fmt.Errorf("토스 자격증명 없음: %s\n"+
			"  토스증권 WTS → 설정 → Open API 에서 client_id·client_secret을 발급하고\n"+
			"  같은 화면의 '허용 IP 관리'에 호출할 IP를 등록할 것 (등록 안 된 IP는 403으로 막힌다)",
			strings.Join(missing, ", "))
	}
	return Config{
		AppKey:    strings.TrimSpace(os.Getenv("TOSS_APP_KEY")),
		AppSecret: strings.TrimSpace(os.Getenv("TOSS_APP_SECRET")),
		Account:   strings.TrimSpace(os.Getenv("TOSS_ACCOUNT")),
	}, nil
}

// TokenPath는 계좌별 토큰 캐시 위치다 — 한 파일을 공유하면 서로를 밀어내고 재발급이 는다.
func TokenPath(account string) (string, error) {
	dir, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("토큰 캐시 위치를 정할 수 없다 — Client.TokenFile을 지정할 것: %w", err)
	}
	var safe []rune
	for _, r := range account {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			safe = append(safe, r)
		}
	}
	name := "default"
	if len(safe) > 0 {
		name = string(safe[max(0, len(safe)-8):])
	}
	return filepath.Join(dir, "myhome", "toss-token-"+name+".json"), nil
}

// Client는 broker.Broker 계약 구현이다.
//
// 쓰기 경로는 Order 하나다. 취소·정정은 API에 있지만 앱이 안 쓰므로 붙이지 않는다 —
// 계약을 좁게 유지하는 것이 새 증권사를 붙이는 비용을 정한다.
//
// Config 외 필드는 비우면 기본값을 쓴다. 테스트가 바꾼다.
type Client struct {
	Config Config

	BaseURL    string                                           // 비우면 BaseURL
	TokenFile  string                                           // 토큰 캐시 파일. 비우면 TokenPath(Config.Account)
	HTTPClient *http.Client                                     // 비우면 15초 타임아웃
	Now        func() time.Time                                 // 비우면 time.Now
	Sleep      func(ctx context.Context, d time.Duration) error // 연결 재시도 대기. 비우면 ctx를 따르는 타이머
	Log        io.Writer                                        // 경고 출력. 비우면 os.Stderr

	// 발급을 한 줄로 세운다 — 토스는 클라이언트당 유효 토큰이 하나라 동시에 두 번 받으면 앞의 것이 죽는다.
	tokenMu    sync.Mutex
	forceIssue bool

	seqMu sync.Mutex
	seq   int64
	seqOK bool
}

var _ broker.Broker = (*Client)(nil)

func (c *Client) baseURL() string {
	if c.BaseURL != "" {
		return strings.TrimRight(c.BaseURL, "/")
	}
	return BaseURL
}

func (c *Client) httpClient() *http.Client {
	if c.HTTPClient != nil {
		return c.HTTPClient
	}
	return &http.Client{Timeout: timeout}
}

func (c *Client) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

func (c *Client) sleep(ctx context.Context, d time.Duration) error {
	if c.Sleep != nil {
		return c.Sleep(ctx, d)
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

func (c *Client) tokenFile() (string, error) {
	if c.TokenFile != "" {
		return c.TokenFile, nil
	}
	return TokenPath(c.Config.Account)
}

func (c *Client) warn(msg string) {
	w := c.Log
	if w == nil {
		w = os.Stderr
	}
	_, _ = fmt.Fprintf(w, "[toss] %s\n", c.Config.Scrub(msg))
}

// Env는 실전/모의 구분이다. 토스는 API 모의투자가 없어 항상 real이다.
func (c *Client) Env() string { return envReal }

// AccountMasked는 로그용 계좌 표시(앞 4자리 + ****)다.
func (c *Client) AccountMasked() string { return maskAccount(c.Config.Account) }

// ── 계좌 ──────────────────────────────────────────────────────────────

// AccountSeq는 X-Tossinvest-Account에 실을 accountSeq다.
//
// TOSS_ACCOUNT는 계좌번호와 accountSeq를 둘 다 받는다. 화면에 보이는 것은 계좌번호이고
// API가 요구하는 것은 seq라, 사람이 어느 쪽을 넣었는지로 실패하게 두지 않는다.
func (c *Client) AccountSeq(ctx context.Context) (int64, error) {
	c.seqMu.Lock()
	defer c.seqMu.Unlock()
	if c.seqOK {
		return c.seq, nil
	}
	const where = "GET /api/v1/accounts"
	want := strings.TrimSpace(c.Config.Account)
	digits := onlyDigits(want)
	raw, err := c.request(ctx, http.MethodGet, "/api/v1/accounts", reqOpts{noAccount: true})
	if err != nil {
		return 0, err
	}
	var v any
	if err := decode(raw, where, &v); err != nil {
		return 0, err
	}
	if m, ok := v.(map[string]any); ok {
		v = m["result"]
	}
	rows, err := rowsOf(v, where)
	if err != nil {
		return 0, err
	}
	pick := func(row map[string]any) (int64, error) {
		seq, err := toInt64(row["accountSeq"])
		if err != nil {
			return 0, &Error{Message: "accountSeq를 정수로 읽을 수 없다 — 규격 변경 의심"}
		}
		c.seq, c.seqOK = seq, true
		return seq, nil
	}
	for _, row := range rows {
		if no := onlyDigits(text(row["accountNo"])); no != "" && no == digits {
			return pick(row)
		}
	}
	for _, row := range rows {
		if row["accountSeq"] != nil && text(row["accountSeq"]) == want {
			return pick(row)
		}
	}
	return 0, &Error{Message: fmt.Sprintf(
		"TOSS_ACCOUNT=%s 와 맞는 계좌를 찾지 못했다 (조회된 계좌 %d개). 계좌번호나 accountSeq를 확인할 것",
		maskAccount(want), len(rows))}
}

// ── 계약 ① 잔고 ───────────────────────────────────────────────────────

// Balance는 미국 주식 보유와 USD 매수가능금액이다. 읽기 전용.
//
// CashUSD는 매수가능금액(현금 기준)이다. 외화예수금과 완전히 같은 개념은 아니지만 앱이 쓰는 곳
// (대조·사이징)에서는 이 값이 맞다 — 주문을 낼 수 있는 돈이 얼마인지가 질문이기 때문이다.
func (c *Client) Balance(ctx context.Context) (broker.Balance, error) {
	const where = "GET /api/v1/holdings"
	raw, err := c.request(ctx, http.MethodGet, "/api/v1/holdings", reqOpts{})
	if err != nil {
		return broker.Balance{}, err
	}
	var res struct {
		Items []map[string]any `json:"items"`
	}
	if err := decode(raw, where, &res); err != nil {
		return broker.Balance{}, err
	}
	var holdings []broker.Holding
	for _, item := range res.Items {
		if text(item["marketCountry"]) != marketUS {
			continue // 국내 주식은 이 전략과 무관하다
		}
		qty, err := num(item["quantity"], "보유 수량")
		if err != nil {
			return broker.Balance{}, err
		}
		if qty <= 0 {
			continue
		}
		if qty != math.Floor(qty) {
			// 소수 수량은 미국 시장가 매도에서만 생긴다. 장부는 정수주라 버림이 안전하지만 조용히 버리지는 않는다
			c.warn(fmt.Sprintf("%s 보유 %s — 소수 수량이라 버림 처리", text(item["symbol"]), floatText(qty)))
		}
		avg, err := numOr0(item["averagePurchasePrice"], "매입평균가")
		if err != nil {
			return broker.Balance{}, err
		}
		var amount any
		if mv, ok := item["marketValue"].(map[string]any); ok {
			amount = mv["amount"]
		}
		value, err := numOr0(amount, "평가금액")
		if err != nil {
			return broker.Balance{}, err
		}
		holdings = append(holdings, broker.Holding{
			Symbol:         strings.TrimSpace(text(item["symbol"])),
			Shares:         int(math.Floor(qty)),
			AvgPriceUSD:    avg,
			MarketValueUSD: value,
		})
	}
	cash, err := c.cashUSD(ctx)
	if err != nil {
		return broker.Balance{}, err
	}
	return broker.Balance{Holdings: holdings, CashUSD: cash, FetchedAt: c.now().UTC().Truncate(time.Second)}, nil
}

func (c *Client) cashUSD(ctx context.Context) (float64, error) {
	const where = "GET /api/v1/buying-power"
	raw, err := c.request(ctx, http.MethodGet, "/api/v1/buying-power",
		reqOpts{query: url.Values{"currency": {currencyUSD}}})
	if err != nil {
		return 0, err
	}
	var res struct {
		CashBuyingPower any `json:"cashBuyingPower"`
	}
	if err := decode(raw, where, &res); err != nil {
		return 0, err
	}
	return num(res.CashBuyingPower, "매수가능금액")
}

// ── 계약 ② 매수 한도 ──────────────────────────────────────────────────

// Buyable은 그 단가로 지금 몇 주까지 살 수 있는가다.
//
// 토스는 금액만 준다 — 수량은 여기서 낸다. 호출부가 예상단가에 안전폭을 이미 얹어 넘기므로
// 수수료는 그 안에 들어간다. ⚠️ 이 값은 거래 자격을 검증하지 않는다 (broker.Buyable 참고).
func (c *Client) Buyable(ctx context.Context, symbol string, priceUSD float64) (broker.Buyable, error) {
	if !(priceUSD > 0) {
		return broker.Buyable{}, fmt.Errorf("예상단가가 0 이하다: %s", floatText(priceUSD))
	}
	cash, err := c.cashUSD(ctx)
	if err != nil {
		return broker.Buyable{}, err
	}
	qty := int(math.Floor(cash / priceUSD))
	if qty <= 0 {
		c.warn(fmt.Sprintf("매수가능 0주 — 주문가능 $%s · 예상단가 $%s", money(cash), money(priceUSD)))
	}
	return broker.Buyable{Symbol: symbol, PriceUSD: priceUSD, CashUSD: cash, MaxQty: qty}, nil
}

// ── 계약 ③ 주문 ───────────────────────────────────────────────────────

// Order는 주문 제출이다. 쓰기다 — 실제 돈이 움직인다.
//
// 🔴 clientOrderId가 멱등성 키다. 같은 값으로 다시 보내면 토스가 이전 주문을 그대로 돌려준다.
// 자동화에서 가장 비싼 실수가 이중 주문인데, 그것을 API가 직접 막아 준다. 그래서 키를
// (날짜·종목·방향·수량·유형)으로 결정적으로 만든다 — 같은 날 같은 주문을 두 번 내면 한 건이 되고,
// 유형을 승격하면 다른 건이 된다.
//
// 🔴 주문 POST는 연결이 끊겨도 다시 보내지 않는다 — 접수 여부를 모르는 채로 다시 보내지 않는다.
func (c *Client) Order(ctx context.Context, symbol string, side broker.Side, qty int, price float64, style broker.Style) (broker.OrderResult, error) {
	out, ok := sideOut[side]
	if !ok {
		return broker.OrderResult{}, fmt.Errorf("side는 'buy' 또는 'sell': %q", side)
	}
	st, ok := orderStyles[style]
	if !ok {
		return broker.OrderResult{}, fmt.Errorf("주문 유형은 %v 중 하나: %q", slices.Sorted(maps.Keys(orderStyles)), style)
	}
	qtyS, err := formatDecimal(float64(qty), 0, "주문 수량")
	if err != nil {
		return broker.OrderResult{}, err
	}
	body := map[string]any{
		"symbol":        symbol,
		"side":          out,
		"quantity":      qtyS,
		"clientOrderId": fmt.Sprintf("tq-%s-%s-%s-%s-%s", broker.Today(c.now()).Format("20060102"), symbol, side, qtyS, style),
		"orderType":     st.orderType,
		"timeInForce":   st.timeInForce,
	}
	var sent float64
	if st.needsPrice {
		priceS, err := formatDecimal(price, 4, "주문 단가")
		if err != nil {
			return broker.OrderResult{}, err
		}
		body["price"] = priceS
		sent, _ = strconv.ParseFloat(priceS, 64)
	}

	c.warn(fmt.Sprintf("주문 제출 — %s %s %s주 (%s: %s+%s). 실제 돈이 움직인다",
		symbol, side, qtyS, style, st.orderType, st.timeInForce))
	raw, err := c.request(ctx, http.MethodPost, "/api/v1/orders", reqOpts{json: body, noRetry: true})
	if err != nil {
		return broker.OrderResult{}, err
	}
	var res map[string]any
	if err := decode(raw, "POST /api/v1/orders", &res); err != nil {
		return broker.OrderResult{}, err
	}
	odno := strings.TrimSpace(orText(res["orderId"]))
	if odno == "" {
		return broker.OrderResult{}, &Error{Message: fmt.Sprintf(
			"주문 응답에 orderId 없음 — 접수 여부를 앱에서 직접 확인할 것 (실제 키=%v)",
			slices.Sorted(maps.Keys(res)))}
	}
	c.warn("주문 접수 — orderId=" + odno)
	return broker.OrderResult{OrderID: odno, Symbol: symbol, Side: side, Qty: qty, Price: sent,
		SubmittedAt: c.now().UTC()}, nil
}

// ── 계약 ④ 체결 ───────────────────────────────────────────────────────

// Fills는 기간·종목의 체결이다. 진행 중 주문도 본다 — 부분 체결이 거기 있다.
//
// 토스는 체결을 주문 단위로 집계해 준다(execution). 호출부는 주문번호로 대조하므로 주문당 한 건으로
// 만들어 돌려주면 그대로 맞는다. 날짜는 KST로 읽는다. to가 비면 from 하루다.
func (c *Client) Fills(ctx context.Context, symbol string, from, to time.Time) ([]broker.Fill, error) {
	if from.IsZero() {
		return nil, errors.New("조회 시작일이 비었다")
	}
	if to.IsZero() {
		to = from
	}
	start, end := ymd(from), ymd(to)
	if end < start {
		return nil, fmt.Errorf("조회 종료일이 시작일보다 앞선다: %s → %s", start, end)
	}
	closed, err := c.orders(ctx, "CLOSED", symbol, start, end)
	if err != nil {
		return nil, err
	}
	open, err := c.orders(ctx, "OPEN", symbol, "", "")
	if err != nil {
		return nil, err
	}

	var out []broker.Fill
	for _, row := range append(closed, open...) {
		ex, _ := row["execution"].(map[string]any)
		qty, err := numOr0(ex["filledQuantity"], "체결 수량")
		if err != nil {
			return nil, err
		}
		if qty <= 0 {
			continue // 미체결은 Fill로 만들지 않는다
		}
		raw := ex["averageFilledPrice"]
		if raw == nil || raw == "" {
			return nil, &Error{Message: fmt.Sprintf(
				"체결 수량은 %s인데 평균 체결가가 비어 있다 (orderId=%s) — 장부를 0으로 쓰지 않는다",
				floatText(qty), text(row["orderId"]))}
		}
		price, err := num(raw, "체결단가")
		if err != nil {
			return nil, err
		}
		sym := orText(row["symbol"])
		if sym == "" {
			sym = symbol
		}
		side, ok := sideIn[text(row["side"])]
		if !ok {
			side = broker.Buy
		}
		out = append(out, broker.Fill{OrderID: orText(row["orderId"]), Symbol: sym, Side: side,
			Qty: int(qty), Price: price})
	}
	return out, nil
}

// orders는 주문 목록이다. CLOSED만 페이징된다 (OPEN은 전량 반환).
func (c *Client) orders(ctx context.Context, status, symbol, start, end string) ([]map[string]any, error) {
	q := url.Values{"status": {status}}
	if symbol != "" {
		q.Set("symbol", symbol)
	}
	if status == "CLOSED" {
		if start != "" {
			q.Set("from", start)
		}
		if end != "" {
			q.Set("to", end)
		}
		q.Set("limit", "100")
	}
	rows := []map[string]any{}
	cursor := ""
	for range maxPages {
		if cursor != "" {
			q.Set("cursor", cursor)
		}
		raw, err := c.request(ctx, http.MethodGet, "/api/v1/orders", reqOpts{query: q})
		if err != nil {
			return nil, err
		}
		var res struct {
			Orders     []map[string]any `json:"orders"`
			HasNext    any              `json:"hasNext"`
			NextCursor any              `json:"nextCursor"`
		}
		if err := decode(raw, "GET /api/v1/orders", &res); err != nil {
			return nil, err
		}
		rows = append(rows, res.Orders...)
		if !truthy(res.HasNext) {
			return rows, nil
		}
		if cursor = orText(res.NextCursor); cursor == "" {
			return rows, nil
		}
	}
	return nil, &Error{Message: fmt.Sprintf("주문 조회 %d페이지 초과 — 절단된 결과를 돌려주느니 멈춘다", maxPages)}
}

// ── 계약 ⑤ 미체결 ─────────────────────────────────────────────────────

// OpenOrders는 진행 중 주문 원본 행이다. 승격 전에 옛 주문이 죽었는지 확인하는 용도다.
// 필드를 가정하지 않는다. 숫자는 json.Number로 남긴다 (주문번호 같은 큰 정수가 float로 뭉개지지 않게).
func (c *Client) OpenOrders(ctx context.Context) ([]map[string]any, error) {
	return c.orders(ctx, "OPEN", "", "", "")
}

// ── 계약 ⑥ 자금 부족 판별 ─────────────────────────────────────────────

// IsFundsError는 자금 부족인가다. 주문 유형을 바꿔도 안 풀리므로 승격을 멈춰야 한다.
func (c *Client) IsFundsError(err error) bool {
	if err == nil {
		return false
	}
	var te *Error
	if errors.As(err, &te) && te.Code != "" {
		return slices.Contains(fundsErrorCodes, te.Code)
	}
	msg := err.Error()
	return slices.ContainsFunc(fundsErrorCodes, func(code string) bool { return strings.Contains(msg, code) })
}
