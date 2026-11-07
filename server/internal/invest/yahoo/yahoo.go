// Package yahoo는 야후 파이낸스 차트 API에서 일봉 종가를 받는다 (TQ의 yf.download 대체).
//
// TQ는 마스터 갱신에 아래 호출을 쓴다. 이 패키지는 그 호출의 "Close" 열만 그대로 재현한다.
//
//	yf.download([...], start=마지막_보유일, auto_adjust=False, threads=False)["Close"]
//
// 🔴 종가는 indicators.quote[0].close(원종가, 분할만 반영)다. adjclose(배당 조정)가 아니다.
// 조정가를 섞으면 배당락일마다 계단이 생기고 스플라이스 지점에 가짜 수익률이 생긴다(TQ claude.md §10).
//
// 장중 행 제외·앵커 대조·결측 검사는 호출부 몫이다. 여기서는 야후가 준 값을 날짜만 붙여 돌려준다.
package yahoo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// ErrTemporary는 기다리면 풀릴 수 있는 실패다(네트워크·429·5xx·빈 응답).
// 호출부는 errors.Is로 가려 "나중에 다시"(종료 코드 75)로 바꾼다. 그 밖의 오류는 영구 실패다.
var ErrTemporary = errors.New("야후 일시 장애")

const (
	// DefaultBaseURL은 야후 차트 API 주소다.
	DefaultBaseURL = "https://query1.finance.yahoo.com"
	// DefaultUserAgent는 일반 브라우저 UA다. UA가 비어 있으면 야후가 429를 준다.
	DefaultUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0.0.0 Safari/537.36"

	maxBody = 16 << 20 // 일봉 수십 년치도 수 MB다. 넘으면 정상 응답이 아니다
)

// Bar는 일봉 1개다.
type Bar struct {
	// Day는 거래소 현지 달력 날짜를 UTC 자정으로 담는다 (invest.Date와 같은 규칙).
	Day time.Time
	// Close는 원종가다. 야후가 null을 주면 NaN — OK()로 확인한다.
	Close float64
}

// OK는 종가가 있는지다.
func (b Bar) OK() bool { return !math.IsNaN(b.Close) }

// Client는 야후 차트 API 클라이언트다. 영값 그대로 쓸 수 있다.
//
// 사용하는 엔드포인트(주소는 BaseURL로 바꿀 수 있다):
//   - GET {base}/v8/finance/chart/{symbol}?period1=..&period2=..&interval=1d   일봉
//
// 쿠키·crumb 없이 호출한다. 야후가 crumb을 요구하기 시작하면 401이 오고, 영구 실패로 분류된다.
type Client struct {
	BaseURL   string           // 비우면 DefaultBaseURL
	UserAgent string           // 비우면 DefaultUserAgent
	HTTP      *http.Client     // 비우면 15초 타임아웃 기본 클라이언트
	Now       func() time.Time // 조회 끝(period2). 비우면 time.Now — 테스트가 고정한다
}

func (c *Client) client() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: 15 * time.Second}
}

func (c *Client) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

// DailyCloses는 종목별 일봉 종가를 start의 달력 날짜부터 지금까지 받는다.
//
// 종목은 순서대로 하나씩 받는다(TQ의 threads=False). 하나라도 실패하면 거기서 멈추고
// 오류를 돌려준다 — 일부만 채운 결과로 마스터를 쓰면 구멍이 생기기 때문이다.
//
// start는 그 값 자체의 시간대 기준 달력 날짜만 본다(시각은 버린다). 바는 날짜 오름차순이며,
// 장이 열려 있으면 오늘의 장중 바도 들어 있다.
//
// 실측(2026-11-06 02:07 UTC, 뉴욕 마감 5시간 뒤):
//   - ETF(QQQ·QQQM·QLD·TQQQ)는 그날 바의 close가 null이었다. 지수(^GSPC·^NDX)는 채워져 있었다
//   - KRW=X는 런던 자정에 하루가 바뀌어 11-06 바가 이미 진행 중이었다. 뉴욕 마감 기준
//     컷오프로는 이 바가 걸러지지 않는다
func (c *Client) DailyCloses(ctx context.Context, symbols []string, start time.Time) (map[string][]Bar, error) {
	from := time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, time.UTC)
	out := make(map[string][]Bar, len(symbols))
	for _, sym := range symbols {
		bars, err := c.daily(ctx, sym, from)
		if err != nil {
			return nil, fmt.Errorf("야후 %s: %w", sym, err)
		}
		out[sym] = bars
	}
	return out, nil
}

// chartResponse는 차트 응답 중 쓰는 칸만 담는다.
type chartResponse struct {
	Chart struct {
		Result []struct {
			Meta struct {
				Symbol               string `json:"symbol"`
				ExchangeTimezoneName string `json:"exchangeTimezoneName"`
			} `json:"meta"`
			Timestamp  []int64 `json:"timestamp"`
			Indicators struct {
				Quote []struct {
					Close []*float64 `json:"close"` // null이 섞여 온다
				} `json:"quote"`
			} `json:"indicators"`
		} `json:"result"`
		Error *chartError `json:"error"`
	} `json:"chart"`
}

type chartError struct {
	Code        string `json:"code"`
	Description string `json:"description"`
}

func (c *Client) daily(ctx context.Context, symbol string, from time.Time) ([]Bar, error) {
	// 조회 시작은 하루 앞당긴다. 거래소 현지 자정이 UTC로는 전날일 수 있다
	// (KRW=X는 런던 자정 = 서머타임 중 UTC 23:00). 범위 밖 날짜는 아래에서 날짜로 거른다.
	end := c.now()
	q := url.Values{}
	q.Set("period1", strconv.FormatInt(from.AddDate(0, 0, -1).Unix(), 10))
	q.Set("period2", strconv.FormatInt(end.Unix(), 10))
	q.Set("interval", "1d")
	q.Set("includePrePost", "false")
	q.Set("includeAdjustedClose", "true")
	q.Set("events", "div,split")
	base := c.BaseURL
	if base == "" {
		base = DefaultBaseURL
	}
	u := strings.TrimRight(base, "/") + "/v8/finance/chart/" + url.PathEscape(symbol) + "?" + q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	ua := c.UserAgent
	if ua == "" {
		ua = DefaultUserAgent
	}
	req.Header.Set("User-Agent", ua)
	req.Header.Set("Accept", "application/json")

	resp, err := c.client().Do(req)
	if err != nil {
		return nil, fmt.Errorf("호출 실패: %w (%w)", ErrTemporary, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
	if err != nil {
		return nil, fmt.Errorf("응답 읽기 실패: %w (%w)", ErrTemporary, err)
	}
	if len(body) > maxBody {
		return nil, fmt.Errorf("응답이 %dMB를 넘는다", maxBody>>20)
	}

	switch {
	case resp.StatusCode == http.StatusTooManyRequests:
		return nil, fmt.Errorf("요청 제한(429): %w", ErrTemporary)
	case resp.StatusCode >= 500:
		return nil, fmt.Errorf("서버 오류(%d): %w", resp.StatusCode, ErrTemporary)
	case resp.StatusCode != http.StatusOK:
		// 404는 없는 종목, 401은 crumb 요구 — 기다려도 안 풀린다
		return nil, fmt.Errorf("응답 %d%s", resp.StatusCode, describe(body))
	}

	var cr chartResponse
	if err := json.Unmarshal(body, &cr); err != nil {
		return nil, fmt.Errorf("응답 형식 오류: %w", err)
	}
	if e := cr.Chart.Error; e != nil {
		return nil, fmt.Errorf("차트 오류 %s: %s", e.Code, e.Description)
	}
	if len(cr.Chart.Result) == 0 {
		return nil, fmt.Errorf("결과 없음: %w", ErrTemporary)
	}
	r := cr.Chart.Result[0]
	if len(r.Timestamp) == 0 {
		// 범위 안에 거래가 아직 없다 — 데이터 반영 지연으로 본다
		return nil, fmt.Errorf("빈 응답: %w", ErrTemporary)
	}
	if len(r.Indicators.Quote) == 0 || len(r.Indicators.Quote[0].Close) != len(r.Timestamp) {
		return nil, fmt.Errorf("응답 형식 오류: 시각 %d개와 종가 개수가 다르다", len(r.Timestamp))
	}
	loc, err := time.LoadLocation(r.Meta.ExchangeTimezoneName)
	if err != nil || r.Meta.ExchangeTimezoneName == "" {
		return nil, fmt.Errorf("거래소 시간대 %q를 읽지 못했다", r.Meta.ExchangeTimezoneName)
	}

	bars := make([]Bar, 0, len(r.Timestamp))
	for i, ts := range r.Timestamp {
		// 야후가 조회 끝 뒤의 최신가를 덧붙이는 경우가 있다 — yfinance도 버린다
		if ts >= end.Unix() {
			continue
		}
		day := localDay(ts, loc)
		if day.Before(from) {
			continue
		}
		cl := math.NaN()
		if p := r.Indicators.Quote[0].Close[i]; p != nil {
			cl = *p
		}
		// 같은 날짜가 두 번 오면(장중 최신가를 별도 행으로 붙이는 야후 버그) 나중 것이 최신이다
		if n := len(bars); n > 0 && bars[n-1].Day.Equal(day) {
			bars[n-1].Close = cl
			continue
		}
		bars = append(bars, Bar{Day: day, Close: cl})
	}
	if len(bars) == 0 {
		return nil, fmt.Errorf("%s 이후 바가 없다: %w", from.Format("2006-01-02"), ErrTemporary)
	}
	return bars, nil
}

// localDay는 바 시각을 거래소 현지 달력 날짜로 바꾼다 (yfinance와 같은 방식).
//
// 일봉 시각은 현지 장 시작(뉴욕 09:30) 또는 현지 자정(런던 00:00)이다. 그런데 야후가
// 서머타임 경계 근처에서 자정을 1~2시간 이르게(전날 22:00·23:00 정각) 찍는 경우가 있어,
// 그 모양이면 다음 날 자정으로 올린다 (yfinance fix_Yahoo_dst_issue).
func localDay(ts int64, loc *time.Location) time.Time {
	t := time.Unix(ts, 0).In(loc)
	if t.Minute() == 0 && t.Second() == 0 && (t.Hour() == 22 || t.Hour() == 23) {
		t = t.Add(time.Duration(24-t.Hour()) * time.Hour)
	}
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

// describe는 오류 응답 본문의 error를 덧붙일 문구로 만든다.
// 404는 chart.error, crumb 거부(401)는 finance.error에 담겨 온다.
func describe(body []byte) string {
	var v struct {
		Chart   struct{ Error *chartError } `json:"chart"`
		Finance struct{ Error *chartError } `json:"finance"`
	}
	if json.Unmarshal(body, &v) != nil {
		return ""
	}
	for _, e := range []*chartError{v.Chart.Error, v.Finance.Error} {
		if e != nil {
			return fmt.Sprintf(" — %s: %s", e.Code, e.Description)
		}
	}
	return ""
}
