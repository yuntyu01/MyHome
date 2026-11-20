package run

import (
	"math"
	"strings"
	"testing"
	"time"

	"github.com/yuntyu01/MyHome/server/internal/invest"
)

// 가격 갱신이 새 거래일마다 매일 기록을 남긴다. 신호는 밀린 날도, 계좌(원금·금액)는 마지막 날만.
func TestRefreshRecordsEveryTradingDay(t *testing.T) {
	a, b := d(8, 3), d(8, 4)
	r := newRefreshRig(t, stageRefresh(a, b))
	p := r.pos()
	p.Holdings = qqqm3()
	p.Holdings.CashUSD = 10
	p.ContributedKRW = 1_200_000
	if err := r.st.SavePosition(r.ctx, p); err != nil {
		t.Fatal(err)
	}
	if err := r.st.SaveBrokerCashBoth(r.ctx, 10, 5000, time.Now()); err != nil {
		t.Fatal(err)
	}
	// 7/30까지는 이미 남겼다 → 7/31부터 이어 쓴다
	if _, err := r.st.AddDaily(r.ctx, invest.DailyRow{Day: d(7, 30), SPX: 1, SMA185: 1, Vol: 0.1, Signal: "TQQQ"}); err != nil {
		t.Fatal(err)
	}

	if rc := r.refresh(0, 0); rc != 0 {
		t.Fatalf("rc %d %s", rc, r.err)
	}
	rows, err := r.st.LoadDaily(r.ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, x := range rows {
		got = append(got, invest.FormatDay(x.Day))
	}
	if strings.Join(got, ",") != "2026-08-04,2026-08-03,2026-07-31,2026-07-30" {
		t.Fatalf("남긴 날 %v", got)
	}
	if !strings.Contains(r.out.String(), "매일 기록 +3일") {
		t.Fatalf("출력에 매일 기록이 없다:\n%s", r.out)
	}

	// 신호는 그날 종가로 다시 계산한 값이다(판정 함수와 같은 식)
	sigs, err := invest.DailySignals(r.master().SignalInputs(), d(8, 3))
	if err != nil {
		t.Fatal(err)
	}
	for i, s := range sigs {
		x := rows[1-i] // 8/3, 8/4 순
		if x.SPX != s.SPX || x.SMA185 != s.SMA185 || x.Vol != s.Vol || x.Signal != invest.MustTicker(s.Pos) {
			t.Fatalf("%s 기록 %+v ≠ 계산 %+v", invest.FormatDay(s.Day), x, s)
		}
	}

	// 계좌는 마지막 날(8/4)만: 3주 × 302.5달러 × 그날 환율
	last, prev := rows[0], rows[1]
	if prev.HasAccount || rows[2].HasAccount {
		t.Fatal("밀린 날에 지금 장부를 적었다")
	}
	if !last.HasAccount {
		t.Fatal("마지막 날 계좌가 비었다")
	}
	row, _ := r.master().AsOf(b)
	fx := row.USDKRWMkt
	if math.IsNaN(fx) || fx <= 0 {
		t.Fatalf("8/4 환율 %v", fx)
	}
	stock := int64(math.Round(3 * 302.5 * fx))
	if last.Symbol != "QQQM" || last.Shares != 3 || last.CloseUSD != 302.5 || last.StockKRW != stock ||
		last.CashKRW != int64(math.Round(10*fx+5000)) || last.InvestedKRW != 1_200_000 ||
		last.GainKRW != int64(math.Round(3*302.5*fx+10*fx-1_200_000)) {
		t.Fatalf("계좌 %+v (환율 %v)", last, fx)
	}

	// 다시 돌려도 더 남기지 않는다(그날 본 대로 둔다)
	r.out.Reset()
	if rc := r.refresh(0, 0); rc != 0 {
		t.Fatalf("rc %d %s", rc, r.err)
	}
	if again, _ := r.st.LoadDaily(r.ctx, 10); len(again) != 4 {
		t.Fatalf("두 번째 갱신 뒤 %d줄", len(again))
	}
	if strings.Contains(r.out.String(), "매일 기록") {
		t.Fatalf("새 날이 없는데 기록했다:\n%s", r.out)
	}
}

// 표가 비어 있으면 그달 1일부터 신호를 채운다(배포 뒤 첫 갱신에도 이번 달 기록이 보이게).
func TestFirstDailyRecordStartsAtTheMonth(t *testing.T) {
	a, b := d(8, 3), d(8, 4)
	r := newRefreshRig(t, stageRefresh(a, b))
	if rc := r.refresh(0, 0); rc != 0 {
		t.Fatalf("rc %d %s", rc, r.err)
	}
	rows, err := r.st.LoadDaily(r.ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || !rows[0].Day.Equal(b) || !rows[1].Day.Equal(a) {
		t.Fatalf("%+v", rows)
	}
}
