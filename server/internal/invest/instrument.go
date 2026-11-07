package invest

import (
	"fmt"
	"sort"
)

// 체결 종목 매핑 — 신호의 역할을 실제 매수 종목으로 바꾼다 (TQ strategy/instrument.py).
//
// 신호는 역할만 정한다(RiskOn·RiskOff). 그 역할을 어느 상품으로 채울지는 증권사 선택과
// 같은 층위의 결정이며 전략 파라미터가 아니다.
//
// 🔴 RiskOff를 직접 바꾸지 말 것. 바꾸면 픽스처 336행 중 113행의 pos 라벨이 어긋난다.
// 그런데 실제로 바뀌는 것은 라벨뿐이다. 그래서 신호 출력은 역할 라벨 그대로 두고 여기서 갈아끼운다.
//
// 🔴 같은 지수를 추종하는 상품 간 교체만 허용한다. 다른 지수·배수는 전략 변경이다.
//
// RiskOff를 QQQM으로 두는 이유 (2026-08 결정, 실측 근거)
//   - 단가 $283 vs QQQ $688 — 60만원 적립 시 QQQ는 0주가 나온다
//   - 같은 나스닥100 추종, 일간 상관 0.9992, 보수 0.15% vs 0.20%
//
// QNDX는 상장 27거래일·거래대금 $14M이라 펀드 폐지·합병 위험 때문에 채택하지 않았다.
var Instrument = map[string]string{
	RiskOn:  "TQQQ",
	RiskOff: "QQQM",
}

// Tickers는 실제로 계좌에 들어올 수 있는 종목이다(정렬됨).
var Tickers = func() []string {
	out := make([]string, 0, len(Instrument))
	for _, v := range Instrument {
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}()

var byTicker = func() map[string]string {
	m := map[string]string{}
	for k, v := range Instrument {
		m[v] = k
	}
	return m
}()

// Ticker는 역할 라벨을 실제 종목으로 바꾼다. 이미 실제 종목이면 그대로 돌려준다.
//
// 호출부가 신호 출력(QQQ)과 계좌 보유(QQQM)를 섞어 넘겨도 같은 답이 나와야 비교가 어긋나지 않는다.
func Ticker(roleOrTicker string) (string, error) {
	if t, ok := Instrument[roleOrTicker]; ok {
		return t, nil
	}
	if _, ok := byTicker[roleOrTicker]; ok {
		return roleOrTicker, nil
	}
	return "", fmt.Errorf("%q는 이 전략의 종목이 아니다. 역할 [%s %s] / 종목 %v", roleOrTicker, RiskOff, RiskOn, Tickers)
}

// MustTicker는 이미 검증된 역할·종목에만 쓴다.
func MustTicker(roleOrTicker string) string {
	t, err := Ticker(roleOrTicker)
	if err != nil {
		panic(err)
	}
	return t
}

// Role은 실제 종목을 역할 라벨로 바꾼다. 신호·픽스처와 대조할 때 쓴다.
func Role(roleOrTicker string) (string, error) {
	if r, ok := byTicker[roleOrTicker]; ok {
		return r, nil
	}
	if _, ok := Instrument[roleOrTicker]; ok {
		return roleOrTicker, nil
	}
	return "", fmt.Errorf("%q는 이 전략의 종목이 아니다", roleOrTicker)
}

// IsTradable은 이 전략의 역할이나 종목인가.
func IsTradable(name string) bool {
	_, a := Instrument[name]
	_, b := byTicker[name]
	return a || b
}
