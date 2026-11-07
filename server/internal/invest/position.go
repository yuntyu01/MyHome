package invest

import (
	"fmt"
	"time"
)

// Position은 실제 계좌와 그달 판정 근거다. JSON 모양은 TQ state/position.json과 같다 —
// 마이그레이션 때 그 파일을 그대로 읽고, 화면도 같은 이름을 쓴다.
type Position struct {
	Holdings       Account            `json:"holdings"`
	LastRun        string             `json:"last_run"` // 판정일 YYYY-MM-DD
	YM             string             `json:"ym"`       // 판정월
	Signal         Judgment           `json:"signal"`
	FXRate         float64            `json:"fx_rate"`
	FXEstimated    bool               `json:"fx_estimated"` // 시장 환율로 추정한 값인가. amend --fx가 끈다
	ApplyYM        string             `json:"apply_ym"`
	ContributedKRW float64            `json:"contributed_krw"`
	MonthsInvested int                `json:"months_invested"`
	JudgmentCount  int                `json:"judgment_count"` // 준수 카운트의 분모
	CloseUSD       map[string]float64 `json:"close_usd"`
	// 어느 적용월의 체결인지 함께 남긴다 — 이 키가 없으면 다음 판정에서 체결일만 이월돼
	// 미체결 감지가 죽는다(§1.5)
	ExecutedOn *string   `json:"executed_on"`
	ExecutedYM *string   `json:"executed_ym"`
	LateDays   *int      `json:"late_days,omitempty"`
	Before     *Snapshot `json:"before,omitempty"` // 체결 직전 상태. amend가 여기서부터 다시 계산한다
	AmendedOn  *string   `json:"amended_on,omitempty"`
	AmendCount int       `json:"amend_count,omitempty"`
}

// Judgment는 판정 근거 전체다. 모든 판정은 기록에 남긴다.
type Judgment struct {
	SPX    float64 `json:"spx"`
	SMA185 float64 `json:"sma185"`
	C1     bool    `json:"c1"`
	Vol    float64 `json:"vol"`
	C2     bool    `json:"c2"`
	Target string  `json:"target"` // 역할 라벨
}

// Snapshot은 체결 직전 계좌·누적치다.
type Snapshot struct {
	Holdings       Account `json:"holdings"`
	ContributedKRW float64 `json:"contributed_krw"`
	MonthsInvested int     `json:"months_invested"`
}

// SignalDate는 판정일이다.
func (p Position) SignalDate() (time.Time, error) {
	d, err := ParseDay(p.LastRun)
	if err != nil {
		return d, fmt.Errorf("position.last_run 형식 이상: %q", p.LastRun)
	}
	return d, nil
}

// ExecutedThisMonth는 적용월 체결이 기록됐는가.
func (p Position) ExecutedThisMonth() bool {
	return p.ExecutedYM != nil && *p.ExecutedYM == p.ApplyYM
}

// Normalize는 비어 있는 맵을 채운다(JSON null로 들어온 값).
func (p *Position) Normalize() {
	if p.Holdings.RealizedGainKRW == nil {
		p.Holdings.RealizedGainKRW = map[int]float64{}
	}
	if p.Holdings.TaxPaidKRW == nil {
		p.Holdings.TaxPaidKRW = map[int]float64{}
	}
	if p.CloseUSD == nil {
		p.CloseUSD = map[string]float64{}
	}
	if p.Before != nil {
		if p.Before.Holdings.RealizedGainKRW == nil {
			p.Before.Holdings.RealizedGainKRW = map[int]float64{}
		}
		if p.Before.Holdings.TaxPaidKRW == nil {
			p.Before.Holdings.TaxPaidKRW = map[int]float64{}
		}
	}
}

// Validate는 저장 전 최소 검사다. 스키마가 어긋나면 조용히 넘어가지 않는다.
func (p Position) Validate() error {
	if _, err := ParseDay(p.LastRun); err != nil {
		return fmt.Errorf("last_run 형식 이상: %q", p.LastRun)
	}
	for _, ym := range []string{p.YM, p.ApplyYM} {
		if _, err := time.Parse("2006-01", ym); err != nil {
			return fmt.Errorf("YYYY-MM 형식 이상: %q", ym)
		}
	}
	if p.Holdings.Shares < 0 {
		return fmt.Errorf("보유 수량이 음수: %d", p.Holdings.Shares)
	}
	if p.Holdings.Symbol != "" && !IsTradable(p.Holdings.Symbol) {
		return fmt.Errorf("보유 종목 %q는 이 전략의 종목이 아니다", p.Holdings.Symbol)
	}
	return nil
}

func strPtr(s string) *string { return &s }

// StrPtr는 *string을 만든다.
func StrPtr(s string) *string { return strPtr(s) }

// LedgerEntry는 원금 투입 1건이다(추가 전용). Source는 trade·execute·adjust·amend.
type LedgerEntry struct {
	ID        int64     `json:"-"`
	On        string    `json:"on"`
	YM        string    `json:"ym"`
	AmountKRW float64   `json:"amount_krw"`
	Source    string    `json:"source"`
	Symbol    string    `json:"symbol,omitempty"`
	Shares    *int      `json:"shares,omitempty"`
	Note      string    `json:"note,omitempty"`
	CreatedAt time.Time `json:"-"`
}

// Deviation은 규칙 이탈 1건이다. Kind가 "missed"면 미실행 자동 기록이고 ActualTarget이 없다.
type Deviation struct {
	ID           int64   `json:"-"`
	Kind         string  `json:"kind,omitempty"`
	YM           string  `json:"ym"`
	OccurredOn   string  `json:"occurred_on"`
	SignalTarget string  `json:"signal_target"`
	ActualTarget *string `json:"actual_target"`
	Reason       string  `json:"reason"`
	LockedUntil  string  `json:"locked_until"`
	ResurfaceOn  string  `json:"resurface_on"`
}

// Contribution은 적립액 계획 1건이다. 실제 투입이 아니라 계획이다.
type Contribution struct {
	EffectiveFrom string `json:"effective_from"` // YYYY-MM
	Amount        int64  `json:"amount"`
	Reason        string `json:"reason"`
	RecordedOn    string `json:"recorded_on"`
}

// Adjustment는 앱 밖 매수 1건이다. 증권사는 원화로 얼마를 넣었는지 모르므로 사람이 알려준 값이다.
type Adjustment struct {
	ID      int64   `json:"-"`
	On      string  `json:"on"`
	Symbol  string  `json:"symbol"`
	Shares  int     `json:"shares"`
	PaidKRW float64 `json:"paid_krw"`
	Reason  string  `json:"reason"`
}

// Pending은 24시간 냉각 중인 이탈 요청이다.
type Pending struct {
	Kind        string `json:"kind"`
	YM          string `json:"ym"`
	Symbol      string `json:"symbol"`
	Reason      string `json:"reason"`
	RequestedAt string `json:"requested_at"` // YYYY-MM-DDTHH:MM:SS (KST, 시간대 표시 없음 — TQ와 같다)
}

// OrderBook은 진행 중인 자동 매매 주문이다. 사이클이 끝나면 지운다.
//
// 지정가 주문은 즉시 체결이 보장되지 않고, 전환하는 달은 매도가 체결돼야 매수를 낼 수 있다.
// 그래서 한 번에 끝내지 않고 날마다 이어받는다.
type OrderBook struct {
	YM        string      `json:"ym"`
	Stage     string      `json:"stage"` // sell | buy
	StyleIdx  int         `json:"style_idx"`
	LedgerGap *int        `json:"ledger_gap,omitempty"` // 증권사 보유 - 장부 (앱 밖 매매 흔적)
	Orders    []BookOrder `json:"orders"`
}

// BookOrder는 제출한 주문 1건이다.
type BookOrder struct {
	OrderID     string  `json:"odno"`
	Symbol      string  `json:"symbol"`
	Side        string  `json:"side"`
	Qty         int     `json:"qty"`
	Price       float64 `json:"price"`
	Style       string  `json:"style"`
	SubmittedOn string  `json:"submitted_on"`
	Filled      bool    `json:"filled"`
	Expired     bool    `json:"expired,omitempty"`
	FillPrice   float64 `json:"fill_price,omitempty"`
}

// PlusMonths는 d에서 months개월 뒤 같은 날(말일 보정).
func PlusMonths(d time.Time, months int) time.Time {
	m := int(d.Month()) - 1 + months
	y := d.Year() + m/12
	mm := m%12 + 1
	last := Date(y, time.Month(mm), 1).AddDate(0, 1, -1).Day()
	day := d.Day()
	if day > last {
		day = last
	}
	return Date(y, time.Month(mm), day)
}

// ResurfaceMonths는 이탈 사유를 다시 보여줄 간격이다 (SPEC §3.2).
const ResurfaceMonths = 6

// MinContribReasonChars는 적립액 변경·앱 밖 매수 사유의 최소 길이다(이탈 사유는 선택).
const MinContribReasonChars = 20
