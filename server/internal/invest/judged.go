package invest

import (
	"fmt"
	"math"
	"time"
)

// 판정 기록 종류(invest_judgments.kind).
const (
	JudgedMonthEnd = "month_end" // 달 마지막 거래일 판정 — 매매가 따르는 것
	JudgedMidMonth = "mid_month" // 자리만 있다. 쓰는 코드는 없다
)

// Judged는 판정 1회다(invest_judgments 한 줄). position 문서의 판정 칸(ym·last_run·signal)과 같은 값이어야 한다.
type Judged struct {
	YM   string
	Kind string
	Day  time.Time
	Judgment
}

// JudgedOf는 position 문서의 판정 칸을 판정 기록 모양으로 읽는다.
func JudgedOf(p Position) (Judged, error) {
	d, err := p.SignalDate()
	if err != nil {
		return Judged{}, err
	}
	return Judged{YM: p.YM, Kind: JudgedMonthEnd, Day: d, Judgment: p.Signal}, nil
}

// Same은 두 판정이 비트 단위로 같은가.
func (a Judged) Same(b Judged) bool {
	return a.YM == b.YM && a.Kind == b.Kind && FormatDay(a.Day) == FormatDay(b.Day) &&
		sameBits(a.SPX, b.SPX) && sameBits(a.SMA185, b.SMA185) && sameBits(a.Vol, b.Vol) &&
		a.C1 == b.C1 && a.C2 == b.C2 && a.Target == b.Target
}

func (a Judged) String() string {
	return fmt.Sprintf("%s %s(%s) SPX %v · SMA185 %v · vol %v · %v/%v → %s", a.YM, a.Kind, FormatDay(a.Day), a.SPX, a.SMA185, a.Vol, a.C1, a.C2, a.Target)
}

// sameBits는 float64를 비트 그대로 비교한다(0과 -0을 가르고, NaN끼리도 같은 비트면 같다).
func sameBits(a, b float64) bool { return math.Float64bits(a) == math.Float64bits(b) }
