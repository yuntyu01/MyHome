package invest

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// 체결 장부(invest_fills, refactor/invest-db 2026-11-20).
//
// 보유 종목·수량·취득원가·연도별 실현손익은 체결을 처음부터 다시 쌓아(Replay) 얻는다. position 문서의 같은 칸은
// 그림자로 남아 매 명령이 비트 단위로 대조한다. 현금(cash_usd)과 납부한 세금(tax_paid_krw)은 체결로 정해지지 않아
// (사람이 증권사 현금에 맞추는 reconcile --apply-cash, tax --paid) 문서가 정본이다.
//
// 🔴 비트 단위로 같으려면 계좌 산술이 더한 값을 그대로 적어야 한다. 그래서 KRW는 계산된 결과 float64다:
//   - buy  : 취득원가 증가분 = (수수료 포함 체결액 USD) × 환율. BuyExact·BuyMax가 CostKRW에 더한 바로 그 값
//   - sell : 매도대금(원, 수수료·SEC 수수료 뺌). SellAll이 실현손익 계산에 쓴 바로 그 값. 손익 = KRW − 그때 취득원가
//   - adjust(앱 밖 매수): 사람이 알려 준 지불액. 취득원가에 그대로 더한다
//   - opening : 여는 잔고. 이 줄에서 종목·수량·취득원가를 그대로 놓고 실현손익을 비운다(그 앞 줄은 계산에서 빠진다)
//   - realized: 여는 잔고의 연도별 실현손익(Day의 연도). opening 바로 뒤에 온다
// 실현손익의 연도는 Day의 연도다(계좌 산술의 year 인자와 같게 적는다).

// 체결 장부 줄 종류.
const (
	FillBuy      = "buy"
	FillSell     = "sell"
	FillOpening  = "opening"
	FillRealized = "realized"
)

// 체결 장부 출처.
const (
	FillFromOpening = "opening" // 마이그레이션·처음 시작·정정할 체결이 장부에 없던 달의 다시 놓기
	FillFromTrade   = "trade"
	FillFromExecute = "execute"
	FillFromAmend   = "amend"
	FillFromAdjust  = "adjust"
)

// Fill은 체결 장부 한 줄이다.
type Fill struct {
	ID       int64
	Day      time.Time
	YM       string
	Symbol   string
	Side     string
	Shares   int
	PriceUSD float64 // 평균 체결단가(참고). opening·realized는 0
	FX       float64 // 적용 환율(참고)
	FeeUSD   float64 // 수수료(참고). 다시 쌓을 때는 쓰지 않는다
	KRW      float64 // 위 설명의 원화 값 — 다시 쌓을 때 이것만 쓴다
	Source   string
}

// Books는 체결에서 다시 쌓은 보유다.
type Books struct {
	Symbol          string
	Shares          int
	CostKRW         float64
	RealizedGainKRW map[int]float64
}

// Replay는 체결을 순서대로 다시 쌓는다. 계좌 산술(SellAll·BuyExact·BuyMax·Adjust)과 같은 덧셈을 같은 순서로 한다.
func Replay(fills []Fill) (Books, error) {
	b := Books{RealizedGainKRW: map[int]float64{}}
	for _, f := range fills {
		switch f.Side {
		case FillOpening:
			b = Books{Symbol: f.Symbol, Shares: f.Shares, CostKRW: f.KRW, RealizedGainKRW: map[int]float64{}}
		case FillRealized:
			b.RealizedGainKRW[f.Day.Year()] += f.KRW
		case FillBuy:
			if b.Symbol != "" && b.Symbol != f.Symbol {
				return b, fmt.Errorf("체결 %d: %s 보유 중에 %s 매수 — 전량 교체 규칙 위반", f.ID, b.Symbol, f.Symbol)
			}
			if f.Shares == 0 {
				if b.Shares == 0 {
					b.Symbol = ""
				}
				continue
			}
			b.Symbol = f.Symbol
			b.Shares += f.Shares
			b.CostKRW += f.KRW
		case FillSell:
			if b.Shares == 0 { // SellAll: 팔 것이 없으면 종목 표시만 지운다
				b.Symbol, b.CostKRW = "", 0
				continue
			}
			gain := f.KRW - b.CostKRW
			b.RealizedGainKRW[f.Day.Year()] += gain
			b.Symbol, b.Shares, b.CostKRW = "", 0, 0
		default:
			return b, fmt.Errorf("체결 %d: 모르는 종류 %q", f.ID, f.Side)
		}
	}
	return b, nil
}

// Diff는 체결에서 쌓은 보유와 계좌(position 문서)를 비트 단위로 대조한 차이다. 같으면 nil.
func (b Books) Diff(a Account) []string {
	var out []string
	if b.Symbol != a.Symbol {
		out = append(out, fmt.Sprintf("종목 체결 %q · 문서 %q", b.Symbol, a.Symbol))
	}
	if b.Shares != a.Shares {
		out = append(out, fmt.Sprintf("수량 체결 %d · 문서 %d", b.Shares, a.Shares))
	}
	if !sameBits(b.CostKRW, a.CostKRW) {
		out = append(out, fmt.Sprintf("취득원가 체결 %v · 문서 %v", b.CostKRW, a.CostKRW))
	}
	years := map[int]bool{}
	for y := range b.RealizedGainKRW {
		years[y] = true
	}
	for y := range a.RealizedGainKRW {
		years[y] = true
	}
	ys := make([]int, 0, len(years))
	for y := range years {
		ys = append(ys, y)
	}
	sort.Ints(ys)
	for _, y := range ys {
		bv, bok := b.RealizedGainKRW[y]
		av, aok := a.RealizedGainKRW[y]
		if bok != aok || !sameBits(bv, av) {
			out = append(out, fmt.Sprintf("%d년 실현손익 체결 %s · 문서 %s", y, optF(bv, bok), optF(av, aok)))
		}
	}
	return out
}

func optF(v float64, ok bool) string {
	if !ok {
		return "없음"
	}
	return fmt.Sprint(v)
}

// OpeningFills는 계좌를 여는 잔고 줄로 적는다(opening 한 줄 + 연도별 realized). 다시 쌓으면 계좌의 네 칸과 같다.
func OpeningFills(a Account, day time.Time, ym, source string) []Fill {
	out := []Fill{{Day: day, YM: ym, Symbol: a.Symbol, Side: FillOpening, Shares: a.Shares, KRW: a.CostKRW, Source: source}}
	ys := make([]int, 0, len(a.RealizedGainKRW))
	for y := range a.RealizedGainKRW {
		ys = append(ys, y)
	}
	sort.Ints(ys)
	for _, y := range ys {
		out = append(out, Fill{Day: Date(y, time.January, 1), YM: fmt.Sprintf("%04d-01", y), Side: FillRealized, KRW: a.RealizedGainKRW[y], Source: source})
	}
	return out
}

// Traded는 계좌 산술 한 번이 체결 장부에 남길 값이다(SellAllT·BuyExactT·BuyMaxT가 돌려준다).
type Traded struct {
	Side     string
	Symbol   string
	Shares   int
	PriceUSD float64
	FX       float64
	FeeUSD   float64
	KRW      float64
}

// Fill은 체결 장부 줄로 바꾼다. day의 연도가 실현손익 연도와 같아야 한다.
func (t Traded) Fill(day time.Time, ym, source string) Fill {
	return Fill{Day: day, YM: ym, Symbol: t.Symbol, Side: t.Side, Shares: t.Shares, PriceUSD: t.PriceUSD, FX: t.FX,
		FeeUSD: t.FeeUSD, KRW: t.KRW, Source: source}
}

// FillsOf는 여러 산술 결과를 장부 줄로 바꾼다. 수량 0인 매수는 계좌를 바꾸지 않아 빼고, 매도는 남긴다(종목 표시를 지운다).
func FillsOf(ts []Traded, day time.Time, ym, source string) []Fill {
	var out []Fill
	for _, t := range ts {
		if t.Side == FillBuy && t.Shares == 0 {
			continue
		}
		out = append(out, t.Fill(day, ym, source))
	}
	return out
}

func (f Fill) String() string {
	return strings.TrimSpace(fmt.Sprintf("%s %s %s %s %d주 krw %v (%s)", FormatDay(f.Day), f.YM, f.Side, f.Symbol, f.Shares, f.KRW, f.Source))
}
