package store

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/yuntyu01/MyHome/server/internal/db"
	"github.com/yuntyu01/MyHome/server/internal/invest"
	"github.com/yuntyu01/MyHome/server/internal/testdb"
)

// 00023~00025(판정·주문·체결 표) 마이그레이션: up → down → up이 되고, 운영 모양 문서에서 채운 값이 문서와 같다.
//
// 운영 모양(합성 숫자): TQ에서 옮겨 온 position(보유·지난 실현손익), 판정·체결·정정·현금 맞춤으로 쌓인 이력,
// 진행 중인 주문 기록(결과 모름 1건 + 체결 1건 + 만료 1건), 냉각 중인 이탈 요청.

const lastDocMigration = "22"

func reconnect(t *testing.T, url string) *pgxpool.Pool {
	t.Helper()
	p, err := db.Connect(context.Background(), url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(p.Close)
	return p
}

func migrate(t *testing.T, url, cmd string, args ...string) {
	t.Helper()
	if err := db.Migrate(context.Background(), url, cmd, args...); err != nil {
		t.Fatalf("%s %v: %v", cmd, args, err)
	}
}

func prodShapedPosition(ym, lastRun, apply string, sig invest.Judgment, shares int, cost float64) invest.Position {
	return invest.Position{
		Holdings: invest.Account{Symbol: "TQQQ", Shares: shares, CostKRW: cost, CashUSD: 12.5,
			RealizedGainKRW: map[int]float64{2024: 1_234_567.875, 2025: -12_345.5}, TaxPaidKRW: map[int]float64{2024: 0}},
		LastRun: lastRun, YM: ym, Signal: sig, FXRate: 1386.25, ApplyYM: apply, ContributedKRW: 4_398_765.43,
		MonthsInvested: 7, JudgmentCount: 7, CloseUSD: map[string]float64{"TQQQ": 64.62},
		ExecutedOn: invest.StrPtr("2026-07-01"), ExecutedYM: invest.StrPtr("2026-07"),
	}
}

func TestInvestTableMigrationsBackfillAndRoundTrip(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	url := pool.Config().ConnString()

	// 빈 상태에서 up → down → up
	migrate(t, url, "down-to", lastDocMigration)
	migrate(t, url, "up")
	migrate(t, url, "down-to", lastDocMigration)

	// 문서만 있던 때(22)의 운영 모양 상태
	conn := reconnect(t, url)
	put := func(key, cmd string, v any, current bool) {
		t.Helper()
		b, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := conn.Exec(ctx, "INSERT INTO invest_state_history (key, cmd, doc) VALUES ($1, $2, $3)", key, cmd, b); err != nil {
			t.Fatal(err)
		}
		if current {
			if _, err := conn.Exec(ctx, "INSERT INTO invest_state (key, doc) VALUES ($1, $2) ON CONFLICT (key) DO UPDATE SET doc = excluded.doc", key, b); err != nil {
				t.Fatal(err)
			}
		}
	}
	may := invest.Judgment{SPX: 7001.123456789, SMA185: 6800.000000001, C1: true, Vol: 0.21234567890123, C2: true, Target: "TQQQ"}
	june := invest.Judgment{SPX: 7200.5, SMA185: 6900.25, C1: true, Vol: 0.31, C2: false, Target: "QQQ"}
	july := invest.Judgment{SPX: 7489.72021484375, SMA185: 7045.090378167229, C1: true, Vol: 0.23636855957037134, C2: true, Target: "TQQQ"}
	put("position", "invest import-tq", prodShapedPosition("2026-05", "2026-05-29", "2026-06", may, 33, 2_900_000.5), false)
	put("position", "invest execute", prodShapedPosition("2026-05", "2026-05-29", "2026-06", may, 37, 3_412_345.125), false)
	put("position", "invest judge", prodShapedPosition("2026-06", "2026-06-30", "2026-07", june, 37, 3_412_345.125), false)
	put("position", "invest judge", prodShapedPosition("2026-07", "2026-07-31", "2026-08", july, 37, 3_412_345.125), false)
	cur := prodShapedPosition("2026-07", "2026-07-31", "2026-08", july, 40, 4_001_234.5)
	put("position", "invest reconcile", cur, true)
	book := invest.OrderBook{YM: "2026-08", Stage: "buy", StyleIdx: 1, Orders: []invest.BookOrder{
		{OrderID: "", ClientOrderID: "tq-20260803-TQQQ-buy-6-moc", Symbol: "TQQQ", Side: "buy", Qty: 6, Price: 71.08, Style: "moc",
			SubmittedOn: "2026-08-03", SubmittedAt: "2026-08-03T15:17:00Z", Unknown: true},
		{OrderID: "0000012345", ClientOrderID: "tq-20260803-TQQQ-buy-2-market", Symbol: "TQQQ", Side: "buy", Qty: 2, Price: 71.08, Style: "market",
			SubmittedOn: "2026-08-03", SubmittedAt: "2026-08-03T15:18:00Z", Filled: true, FillPrice: 64.8125},
		{OrderID: "777", Symbol: "TQQQ", Side: "buy", Qty: 0, Price: 70, Style: "moc", SubmittedOn: "2026-08-01", Filled: true, Expired: true},
	}}
	gap := 3
	book.LedgerGap = &gap
	put("orders", "invest trade", book, true)
	put("pending", "invest deviate", invest.Pending{Kind: "deviation", YM: "2026-08", Symbol: "QQQM", Reason: "합성", RequestedAt: "2026-08-02T10:00:00"}, true)

	for round := 1; round <= 2; round++ {
		migrate(t, url, "up")
		st := New(reconnect(t, url), "test")

		// 판정: 판정월마다 처음 보인 값(비트 그대로)
		js, err := st.LoadJudgments(ctx)
		if err != nil {
			t.Fatal(err)
		}
		want := []invest.Judged{
			{YM: "2026-05", Kind: invest.JudgedMonthEnd, Day: invest.Date(2026, 5, 29), Judgment: may},
			{YM: "2026-06", Kind: invest.JudgedMonthEnd, Day: invest.Date(2026, 6, 30), Judgment: june},
			{YM: "2026-07", Kind: invest.JudgedMonthEnd, Day: invest.Date(2026, 7, 31), Judgment: july},
		}
		if len(js) != len(want) {
			t.Fatalf("판정 %d행: %v", len(js), js)
		}
		for i := range want {
			if !js[i].Same(want[i]) {
				t.Fatalf("판정 %d: %s ≠ %s", i, js[i], want[i])
			}
		}

		// 체결: 여는 잔고 하나(+ 연도별 실현손익)에서 다시 쌓으면 문서와 같다 — 그림자 대조 통과
		pos, err := st.LoadPosition(ctx)
		if err != nil {
			t.Fatalf("마이그레이션 뒤 대조: %v", err)
		}
		fs, err := st.LoadFills(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if len(fs) != 3 || fs[0].Side != invest.FillOpening || fs[0].Shares != 40 || fs[1].Day.Year() != 2024 || fs[2].Day.Year() != 2025 {
			t.Fatalf("체결 %v", fs)
		}
		if pos.Holdings.Shares != 40 || pos.Holdings.CostKRW != 4_001_234.5 {
			t.Fatalf("%+v", pos.Holdings)
		}

		// 주문: 문서 그대로(그림자 대조 통과), 상태는 unknown·filled·expired
		got, err := st.LoadOrders(ctx)
		if err != nil {
			t.Fatalf("주문 대조: %v", err)
		}
		if got == nil || len(got.Orders) != 3 || got.Orders[0].Unknown != true || got.Orders[1].FillPrice != 64.8125 || *got.LedgerGap != 3 {
			t.Fatalf("주문 %+v", got)
		}
		evs, err := st.OrderEvents(ctx)
		if err != nil || len(evs) != 3 || evs[0].State != "unknown" || evs[2].State != "expired" {
			t.Fatalf("전이 %+v %v", evs, err)
		}

		// 냉각 중 이탈 요청은 문서 그대로
		if p, err := st.LoadPending(ctx); err != nil || p == nil || p.Symbol != "QQQM" {
			t.Fatalf("pending %+v %v", p, err)
		}
		if round == 1 {
			migrate(t, url, "down-to", lastDocMigration) // 문서는 그대로라 다시 올리면 같은 값을 채운다
		}
	}
}
