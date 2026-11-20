// Package store는 자동매매 상태를 PostgreSQL에 영속한다 (TQ strategy/state.py 이식).
//
// TQ는 state/*.json을 git에 커밋해 git log를 감사 추적으로 썼다. 여기서는 단일 문서 상태
// (position·orders·pending)를 바꿀 때마다 invest_state_history에 새 값을 남긴다.
//
// 실패는 조용히 넘어가지 않는다. 스키마가 어긋나면 에러를 돌려준다.
package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/yuntyu01/MyHome/server/internal/dbq"
	"github.com/yuntyu01/MyHome/server/internal/invest"
)

// lockKey는 상태를 쓰는 명령을 한 번에 하나씩 돌리는 advisory lock 키다.
// 가계부 처리 잠금(7240001)과 분리한다 — 매매가 알림 처리를 막지 않게.
const lockKey = 7240002

// ErrNoPosition은 position 상태가 아직 없을 때다(마이그레이션 전).
var ErrNoPosition = errors.New("position 상태가 없다. `myhome invest import-tq`로 TQ 상태를 먼저 옮길 것")

type querier interface {
	dbq.DBTX
}

// Store는 자동매매 상태 저장소다. Cmd는 이력에 남길 명령 이름이다.
type Store struct {
	pool *pgxpool.Pool
	db   querier
	q    *dbq.Queries
	Cmd  string

	lockID int64 // 0이면 lockKey. 테스트가 다른 키를 써서 같은 DB의 다른 테스트와 부딪치지 않게 한다
}

func (s *Store) lockKeyOf() int64 {
	if s.lockID != 0 {
		return s.lockID
	}
	return lockKey
}

// New는 풀에 붙은 저장소를 만든다.
func New(pool *pgxpool.Pool, cmd string) *Store {
	return &Store{pool: pool, db: pool, q: dbq.New(pool), Cmd: cmd}
}

// InTx는 fn을 한 트랜잭션 안에서 실행한다. 여러 상태를 함께 바꾸는 명령은 이것을 쓴다 —
// TQ는 파일을 하나씩 썼기 때문에 중간에 죽으면 position과 ledger가 어긋날 수 있었다.
func (s *Store) InTx(ctx context.Context, fn func(*Store) error) error {
	if s.pool == nil {
		return fn(s) // 이미 트랜잭션 안
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	inner := &Store{db: tx, q: dbq.New(tx), Cmd: s.Cmd}
	if err := fn(inner); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// Lock은 상태를 쓰는 명령의 전역 잠금을 잡는다. 반환된 함수로 푼다.
//
// TQ는 GitHub Actions의 concurrency 그룹(tq-state-write)으로 직렬화했다. systemd timer는
// 같은 유닛만 겹치지 않게 막으므로, 서로 다른 명령·손으로 친 명령은 여기서 막는다.
func (s *Store) Lock(ctx context.Context, wait time.Duration) (func(), error) {
	if s.pool == nil {
		return nil, errors.New("트랜잭션 안에서는 잠금을 잡지 않는다")
	}
	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return nil, err
	}
	lctx, cancel := context.WithTimeout(ctx, wait)
	defer cancel()
	key := s.lockKeyOf()
	if _, err := conn.Exec(lctx, "SELECT pg_advisory_lock($1)", key); err != nil {
		conn.Release()
		return nil, fmt.Errorf("다른 자동매매 명령이 실행 중이다 (%s 기다림): %w", wait, err)
	}
	return func() {
		_, _ = conn.Exec(context.Background(), "SELECT pg_advisory_unlock($1)", key)
		conn.Release()
	}, nil
}

// Held는 잡은 전역 잠금이다. 기다리는 동안 잠깐 놓았다가 다시 잡을 수 있다.
//
// refresh는 야후가 늦으면 재시도 사이에 15분씩(최대 90분) 기다린다. 그동안 잠금을 쥐고 있으면 그 사이
// 도는 judge·주간 갱신이 10분 기다리다 75로 끝나 실패 알림이 간다. 기다리는 동안은 상태를 쓰지 않으므로
// 놓아도 된다 — 다시 잡은 뒤 상태를 처음부터 다시 읽는다(refresh는 매 시도에 마스터를 새로 읽는다).
type Held struct {
	s      *Store
	wait   time.Duration
	unlock func()
}

// Hold는 Lock과 같고 Held를 돌려준다.
func (s *Store) Hold(ctx context.Context, wait time.Duration) (*Held, error) {
	u, err := s.Lock(ctx, wait)
	if err != nil {
		return nil, err
	}
	return &Held{s: s, wait: wait, unlock: u}, nil
}

// Release는 잠금을 푼다. 두 번 불러도 된다.
func (h *Held) Release() {
	if h.unlock != nil {
		h.unlock()
		h.unlock = nil
	}
}

// Pause는 잠금을 놓고 d만큼 기다린 뒤 다시 잡는다. 다시 못 잡으면 에러다(잠금은 풀린 채다).
func (h *Held) Pause(ctx context.Context, d time.Duration, sleep func(time.Duration)) error {
	h.Release()
	sleep(d)
	if err := ctx.Err(); err != nil {
		return err
	}
	u, err := h.s.Lock(ctx, h.wait)
	if err != nil {
		return err
	}
	h.unlock = u
	return nil
}

// ── 가격 ─────────────────────────────────────────────────────────────

func f64(p *float64) float64 {
	if p == nil {
		return math.NaN()
	}
	return *p
}

func ptr(v float64) *float64 {
	if math.IsNaN(v) {
		return nil
	}
	return &v
}

// LoadPrices는 마스터 전체를 날짜순으로 읽는다.
func (s *Store) LoadPrices(ctx context.Context) (invest.Prices, error) {
	rows, err := s.q.ListInvestPrices(ctx)
	if err != nil {
		return nil, err
	}
	out := make(invest.Prices, len(rows))
	for i, r := range rows {
		out[i] = invest.PriceRow{
			Day: invest.DayOf(r.Day), SPX: r.Spx, NDX: r.Ndx,
			QQQ: f64(r.Qqq), QQQM: f64(r.Qqqm), QLD: f64(r.Qld), TQQQ: f64(r.Tqqq),
			USDKRW: f64(r.Usdkrw), USDKRWMkt: f64(r.UsdkrwMkt),
		}
	}
	return out, nil
}

func priceParams(r invest.PriceRow) dbq.InsertInvestPriceParams {
	return dbq.InsertInvestPriceParams{
		Day: r.Day, Spx: r.SPX, Ndx: r.NDX, Qqq: ptr(r.QQQ), Qqqm: ptr(r.QQQM), Qld: ptr(r.QLD),
		Tqqq: ptr(r.TQQQ), Usdkrw: ptr(r.USDKRW), UsdkrwMkt: ptr(r.USDKRWMkt),
	}
}

// InsertPrice는 새 거래일 1행을 넣는다. 이미 있으면 false (과거 행은 건드리지 않는다).
func (s *Store) InsertPrice(ctx context.Context, r invest.PriceRow) (bool, error) {
	if math.IsNaN(r.SPX) || math.IsNaN(r.NDX) {
		return false, fmt.Errorf("%s SPX·NDX 결측 행은 쓰지 않는다", invest.FormatDay(r.Day))
	}
	n, err := s.q.InsertInvestPrice(ctx, priceParams(r))
	return n == 1, err
}

// FillFXMkt는 비어 있는 날의 시장 환율만 채운다.
func (s *Store) FillFXMkt(ctx context.Context, day time.Time, v float64) (bool, error) {
	n, err := s.q.FillInvestFXMkt(ctx, dbq.FillInvestFXMktParams{Day: day, UsdkrwMkt: &v})
	return n == 1, err
}

// CopyPrices는 마이그레이션용 일괄 적재다.
func (s *Store) CopyPrices(ctx context.Context, rows invest.Prices) (int64, error) {
	ps := make([]dbq.CopyInvestPricesParams, len(rows))
	for i, r := range rows {
		p := priceParams(r)
		ps[i] = dbq.CopyInvestPricesParams(p)
	}
	return s.q.CopyInvestPrices(ctx, ps)
}

// ── 단일 문서 상태 ────────────────────────────────────────────────────

func (s *Store) getDoc(ctx context.Context, key string, v any) (bool, error) {
	row, err := s.q.GetInvestState(ctx, key)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if err := json.Unmarshal(row.Doc, v); err != nil {
		return false, fmt.Errorf("%s 상태 파손: %w", key, err)
	}
	return true, nil
}

func (s *Store) putDoc(ctx context.Context, key string, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if err := s.q.UpsertInvestState(ctx, dbq.UpsertInvestStateParams{Key: key, Doc: b}); err != nil {
		return err
	}
	return s.q.InsertInvestStateHistory(ctx, dbq.InsertInvestStateHistoryParams{Key: key, Cmd: s.cmd(), Doc: b})
}

func (s *Store) dropDoc(ctx context.Context, key string) error {
	n, err := s.q.DeleteInvestState(ctx, key)
	if err != nil || n == 0 {
		return err
	}
	return s.q.InsertInvestStateHistory(ctx, dbq.InsertInvestStateHistoryParams{Key: key, Cmd: s.cmd(), Doc: nil})
}

func (s *Store) cmd() string {
	if s.Cmd == "" {
		return "unknown"
	}
	return s.Cmd
}

// LoadPosition은 실제 계좌와 그달 판정이다. 표(판정·체결)와 position 문서를 대조하고 다르면 에러다(ShadowError).
// 매매 명령은 전부 이것을 쓴다 — 어긋난 채로는 아무 명령도 돌지 않고 실패 알림이 간다.
func (s *Store) LoadPosition(ctx context.Context) (invest.Position, error) {
	p, err := s.LoadPositionDoc(ctx)
	if err != nil {
		return p, err
	}
	if err := s.checkShadow(ctx, p); err != nil {
		return p, err
	}
	return p, nil
}

// LoadPositionDoc은 position 문서를 대조 없이 읽는다. 화면(웹·앱)이 쓴다 — 대조가 어긋나도 화면은 떠야 한다.
// 어긋남은 매매 명령이 실패로 알린다.
func (s *Store) LoadPositionDoc(ctx context.Context) (invest.Position, error) {
	var p invest.Position
	ok, err := s.getDoc(ctx, "position", &p)
	if err != nil {
		return p, err
	}
	if !ok {
		return p, ErrNoPosition
	}
	p.Normalize()
	if err := p.Validate(); err != nil {
		return p, fmt.Errorf("position 스키마 이상: %w", err)
	}
	return p, nil
}

// SavePosition은 계좌를 저장하고 이력에 남긴다. 모든 판정은 기록에 남는다.
//
// 🔴 쓴 뒤 표(판정·체결)와 대조한다. 다르면 에러이고 한 트랜잭션이라 문서도 표도 쓰이지 않는다. 그래서 계좌를
// 바꾸는 명령은 체결(AddFills)·판정(AddJudgment)을 먼저 쓰고 마지막에 이것을 부른다.
func (s *Store) SavePosition(ctx context.Context, p invest.Position) error {
	p.Normalize()
	if err := p.Validate(); err != nil {
		return err
	}
	return s.InTx(ctx, func(s *Store) error {
		if err := s.putDoc(ctx, "position", p); err != nil {
			return err
		}
		return s.checkShadow(ctx, p)
	})
}

// ShadowError는 표에서 다시 계산한 값이 position 문서와 다를 때다(refactor/invest-db의 그림자 대조).
// 문서를 지우기 전 석 달 동안 매 명령이 확인한다.
type ShadowError struct{ Diffs []string }

func (e *ShadowError) Error() string {
	return "🚨 표와 position 문서가 다르다(그림자 대조) — 아무것도 하지 않는다:\n  " + strings.Join(e.Diffs, "\n  ")
}

// checkShadow는 표(판정)와 position 문서를 비트 단위로 대조한다.
func (s *Store) checkShadow(ctx context.Context, p invest.Position) error {
	var diffs []string
	if p.Signal.Target != "" {
		want, err := invest.JudgedOf(p)
		if err != nil {
			return err
		}
		got, ok, err := s.Judgment(ctx, p.YM, invest.JudgedMonthEnd)
		switch {
		case err != nil:
			return err
		case !ok:
			diffs = append(diffs, fmt.Sprintf("판정 표에 %s 행이 없다 · 문서 %s", p.YM, want))
		case !got.Same(want):
			diffs = append(diffs, fmt.Sprintf("판정 표 %s · 문서 %s", got, want))
		}
	}
	if len(diffs) > 0 {
		return &ShadowError{Diffs: diffs}
	}
	return nil
}

// ── 판정 ─────────────────────────────────────────────────────────────

// AddJudgment는 판정 1회를 남긴다. 같은 달·종류가 이미 있으면 같은 값이어야 한다(다르면 에러, 새 행은 없다).
func (s *Store) AddJudgment(ctx context.Context, j invest.Judged) error {
	if _, err := s.q.InsertInvestJudgment(ctx, dbq.InsertInvestJudgmentParams{
		Ym: j.YM, Kind: j.Kind, Day: j.Day, Spx: j.SPX, Sma185: j.SMA185, Vol: j.Vol, C1: j.C1, C2: j.C2, Target: j.Target,
	}); err != nil {
		return err
	}
	got, ok, err := s.Judgment(ctx, j.YM, j.Kind)
	if err != nil {
		return err
	}
	if !ok || !got.Same(j) {
		return fmt.Errorf("%s 판정이 이미 다른 값으로 기록돼 있다 — 표 %s · 지금 %s", j.YM, got, j)
	}
	return nil
}

// Judgment는 그 달·종류의 판정이다. 없으면 ok=false.
func (s *Store) Judgment(ctx context.Context, ym, kind string) (invest.Judged, bool, error) {
	r, err := s.q.GetInvestJudgment(ctx, dbq.GetInvestJudgmentParams{Ym: ym, Kind: kind})
	if errors.Is(err, pgx.ErrNoRows) {
		return invest.Judged{}, false, nil
	}
	if err != nil {
		return invest.Judged{}, false, err
	}
	return judgedOf(r), true, nil
}

// LoadJudgments는 판정 기록 전체다(달 순).
func (s *Store) LoadJudgments(ctx context.Context) ([]invest.Judged, error) {
	rows, err := s.q.ListInvestJudgments(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]invest.Judged, len(rows))
	for i, r := range rows {
		out[i] = judgedOf(r)
	}
	return out, nil
}

func judgedOf(r dbq.InvestJudgment) invest.Judged {
	return invest.Judged{YM: r.Ym, Kind: r.Kind, Day: invest.DayOf(r.Day),
		Judgment: invest.Judgment{SPX: r.Spx, SMA185: r.Sma185, C1: r.C1, Vol: r.Vol, C2: r.C2, Target: r.Target}}
}

// Bootstrap은 처음 상태를 놓는다: 판정 행, position 문서(그리고 여는 체결). 운영 계좌는 마이그레이션이 이 일을
// 했다(00023·00025) — 새로 시작하는 계좌와 테스트가 쓴다. position이 이미 있으면 에러다.
func (s *Store) Bootstrap(ctx context.Context, p invest.Position) error {
	return s.InTx(ctx, func(s *Store) error {
		if _, err := s.LoadPositionDoc(ctx); err == nil {
			return errors.New("position이 이미 있다. Bootstrap은 처음 한 번만")
		} else if !errors.Is(err, ErrNoPosition) {
			return err
		}
		if p.Signal.Target != "" {
			j, err := invest.JudgedOf(p)
			if err != nil {
				return err
			}
			if err := s.AddJudgment(ctx, j); err != nil {
				return err
			}
		}
		return s.SavePosition(ctx, p)
	})
}

// LoadOrders는 진행 중인 주문이다. 없으면 nil.
func (s *Store) LoadOrders(ctx context.Context) (*invest.OrderBook, error) {
	var b invest.OrderBook
	ok, err := s.getDoc(ctx, "orders", &b)
	if err != nil || !ok {
		return nil, err
	}
	if b.Orders == nil {
		b.Orders = []invest.BookOrder{}
	}
	return &b, nil
}

// SaveOrders는 주문 진행 상태를 저장한다.
func (s *Store) SaveOrders(ctx context.Context, b invest.OrderBook) error {
	if b.Orders == nil {
		b.Orders = []invest.BookOrder{}
	}
	return s.putDoc(ctx, "orders", b)
}

// ClearOrders는 사이클이 끝난 주문을 지운다(이력에는 남는다).
func (s *Store) ClearOrders(ctx context.Context) error { return s.dropDoc(ctx, "orders") }

// LoadPending은 냉각 중인 이탈 요청이다. 없으면 nil.
func (s *Store) LoadPending(ctx context.Context) (*invest.Pending, error) {
	var p invest.Pending
	ok, err := s.getDoc(ctx, "pending", &p)
	if err != nil || !ok {
		return nil, err
	}
	return &p, nil
}

// SavePending은 이탈 요청을 기록한다.
func (s *Store) SavePending(ctx context.Context, p invest.Pending) error {
	return s.putDoc(ctx, "pending", p)
}

// ClearPending은 이탈 요청을 지운다.
func (s *Store) ClearPending(ctx context.Context) error { return s.dropDoc(ctx, "pending") }

// ── 원금 내역 ────────────────────────────────────────────────────────

func optStr(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func nilIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// LoadLedger는 원금이 들어간 사건들이다. 시간순. 정정으로 덮인 행은 빠진다.
func (s *Store) LoadLedger(ctx context.Context) ([]invest.LedgerEntry, error) {
	rows, err := s.q.ListInvestLedger(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]invest.LedgerEntry, len(rows))
	for i, r := range rows {
		e := invest.LedgerEntry{
			ID: r.ID, On: invest.FormatDay(r.OnDay), YM: r.Ym, AmountKRW: r.AmountKrw, Source: r.Source,
			Symbol: optStr(r.Symbol), Note: r.Note, CreatedAt: r.CreatedAt,
		}
		if r.Shares != nil {
			n := int(*r.Shares)
			e.Shares = &n
		}
		out[i] = e
	}
	return out, nil
}

// round2는 Python round(x, 2)와 같다 — 이진 값 그대로를 소수 둘째 자리로 올바르게 반올림한다.
func round2(x float64) float64 {
	v, _ := strconv.ParseFloat(strconv.FormatFloat(x, 'f', 2, 64), 64)
	return v
}

// AppendLedger는 원금 투입 1건을 기록한다.
//
// 🔴 amend는 더하지 않고 덮는다. 정정은 그 달을 다시 계산하는 것이므로 같은 달의 자동·수동
// 기록과 앞선 정정을 덮고 자기 것을 남긴다 — 안 그러면 한 달치가 두 번 세어진다
// (실측 2026-09: 2회차 정정 뒤 −694,510원 불일치).
func (s *Store) AppendLedger(ctx context.Context, e invest.LedgerEntry) error {
	switch e.Source {
	case "trade", "execute", "adjust", "amend":
	default:
		return fmt.Errorf("source는 trade·execute·adjust·amend 중 하나여야 한다: %q", e.Source)
	}
	on, err := invest.ParseDay(e.On)
	if err != nil {
		return fmt.Errorf("on 형식 이상: %q", e.On)
	}
	if e.Source == "amend" {
		if _, err := s.q.SupersedeInvestLedgerForAmend(ctx, e.YM); err != nil {
			return err
		}
	}
	var shares *int32
	if e.Shares != nil {
		n := int32(*e.Shares)
		shares = &n
	}
	_, err = s.q.InsertInvestLedger(ctx, dbq.InsertInvestLedgerParams{
		OnDay: on, Ym: e.YM, AmountKrw: round2(e.AmountKRW), Source: e.Source,
		Symbol: nilIfEmpty(e.Symbol), Shares: shares, Note: e.Note,
	})
	return err
}

// ImportLedger는 마이그레이션용 그대로 적재다. amend 덮어쓰기를 다시 적용하지 않는다 —
// TQ 장부에는 이미 덮인 결과만 남아 있다.
func (s *Store) ImportLedger(ctx context.Context, e invest.LedgerEntry) error {
	on, err := invest.ParseDay(e.On)
	if err != nil {
		return fmt.Errorf("on 형식 이상: %q", e.On)
	}
	var shares *int32
	if e.Shares != nil {
		n := int32(*e.Shares)
		shares = &n
	}
	_, err = s.q.InsertInvestLedger(ctx, dbq.InsertInvestLedgerParams{
		OnDay: on, Ym: e.YM, AmountKrw: e.AmountKRW, Source: e.Source,
		Symbol: nilIfEmpty(e.Symbol), Shares: shares, Note: e.Note,
	})
	return err
}

// ── 이탈 ─────────────────────────────────────────────────────────────

// LoadDeviations는 이탈 기록 전체다(기록 순).
func (s *Store) LoadDeviations(ctx context.Context) ([]invest.Deviation, error) {
	rows, err := s.q.ListInvestDeviations(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]invest.Deviation, len(rows))
	for i, r := range rows {
		out[i] = invest.Deviation{
			ID: r.ID, Kind: optStr(r.Kind), YM: r.Ym, OccurredOn: invest.FormatDay(r.OccurredOn),
			SignalTarget: r.SignalTarget, ActualTarget: r.ActualTarget, Reason: r.Reason,
			LockedUntil: invest.FormatDay(r.LockedUntil), ResurfaceOn: invest.FormatDay(r.ResurfaceOn),
		}
	}
	return out, nil
}

func (s *Store) insertDeviation(ctx context.Context, d invest.Deviation) error {
	occ, err := invest.ParseDay(d.OccurredOn)
	if err != nil {
		return err
	}
	lock, err := invest.ParseDay(d.LockedUntil)
	if err != nil {
		return err
	}
	res, err := invest.ParseDay(d.ResurfaceOn)
	if err != nil {
		return err
	}
	_, err = s.q.InsertInvestDeviation(ctx, dbq.InsertInvestDeviationParams{
		Kind: nilIfEmpty(d.Kind), Ym: d.YM, OccurredOn: occ, SignalTarget: d.SignalTarget,
		ActualTarget: d.ActualTarget, Reason: d.Reason, LockedUntil: lock, ResurfaceOn: res,
	})
	return err
}

// AppendDeviation은 규칙 이탈 1건을 기록한다. 사유는 선택이고, 비용은 저장하지 않는다
// (이탈의 대가는 시점마다 달라지므로 숫자로 박아두면 거짓말이 된다).
func (s *Store) AppendDeviation(ctx context.Context, ym string, occurredOn time.Time, signalTarget, actualTarget, reason string, lockedUntil time.Time) (invest.Deviation, error) {
	if signalTarget == actualTarget {
		return invest.Deviation{}, errors.New("신호와 실제 포지션이 같다. 이탈이 아니다")
	}
	if !lockedUntil.After(occurredOn) {
		return invest.Deviation{}, fmt.Errorf("잠금 해제일(%s)이 이탈일(%s) 이전", invest.FormatDay(lockedUntil), invest.FormatDay(occurredOn))
	}
	d := invest.Deviation{
		YM: ym, OccurredOn: invest.FormatDay(occurredOn), SignalTarget: signalTarget,
		ActualTarget: invest.StrPtr(actualTarget), Reason: strings.TrimSpace(reason),
		LockedUntil: invest.FormatDay(lockedUntil),
		ResurfaceOn: invest.FormatDay(invest.PlusMonths(occurredOn, invest.ResurfaceMonths)),
	}
	return d, s.insertDeviation(ctx, d)
}

// RecordMissedMonth는 그 달에 매수를 안 했음을 기록한다. 이탈의 한 종류다.
//
// 안 사는 것은 아무것도 입력하지 않는 행위라 사유가 있을 수 없다. 그래서 더 위험하다 —
// 24시간 냉각조차 우회된다. 기록만이라도 남겨 준수 카운트에 반영한다. 이미 그 달 기록이 있으면 nil.
func (s *Store) RecordMissedMonth(ctx context.Context, ym, signalTarget string, detectedOn time.Time) (*invest.Deviation, error) {
	devs, err := s.LoadDeviations(ctx)
	if err != nil {
		return nil, err
	}
	for _, d := range devs {
		if d.YM == ym {
			return nil, nil
		}
	}
	d := invest.Deviation{
		Kind: "missed", YM: ym, OccurredOn: invest.FormatDay(detectedOn), SignalTarget: signalTarget,
		Reason:      "(미실행 — 사유 없음. 매수하지 않아 자동 기록됨)",
		LockedUntil: invest.FormatDay(detectedOn),
		ResurfaceOn: invest.FormatDay(invest.PlusMonths(detectedOn, invest.ResurfaceMonths)),
	}
	return &d, s.insertDeviation(ctx, d)
}

// ImportDeviation은 마이그레이션용 그대로 적재다.
func (s *Store) ImportDeviation(ctx context.Context, d invest.Deviation) error {
	return s.insertDeviation(ctx, d)
}

// ── 적립액 ───────────────────────────────────────────────────────────

// LoadContributions는 적립액 변경 이력이다. effective_from 오름차순.
func (s *Store) LoadContributions(ctx context.Context) ([]invest.Contribution, error) {
	rows, err := s.q.ListInvestContributions(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]invest.Contribution, len(rows))
	for i, r := range rows {
		out[i] = invest.Contribution{EffectiveFrom: r.EffectiveFrom, Amount: r.Amount, Reason: r.Reason, RecordedOn: invest.FormatDay(r.RecordedOn)}
	}
	return out, nil
}

// ContributionFor는 그 달에 적용될 적립액(원)이다. 변경 이력이 있어도 그 달에 유효했던 금액이다.
func (s *Store) ContributionFor(ctx context.Context, ym string) (int64, error) {
	hist, err := s.LoadContributions(ctx)
	if err != nil {
		return 0, err
	}
	var amt int64
	found := false
	for _, r := range hist {
		if r.EffectiveFrom <= ym {
			amt, found = r.Amount, true
		}
	}
	if !found {
		return 0, fmt.Errorf("%s에 적용할 적립액이 없다. 최초 금액을 먼저 넣을 것", ym)
	}
	return amt, nil
}

// SetContribution은 적립액을 바꾼다. 이탈이 아니다 — 소득이 바뀌면 바꾸는 게 정상이다.
// 다만 당월 이하 소급, 신호를 아는 달(signalKnownFor) 이하, 사유 없는 변경을 막는다.
func (s *Store) SetContribution(ctx context.Context, amount int64, effectiveFrom, reason string, recordedOn time.Time, signalKnownFor string) (invest.Contribution, error) {
	reason = strings.TrimSpace(reason)
	if amount <= 0 {
		return invest.Contribution{}, fmt.Errorf("적립액은 0보다 커야 한다: %d", amount)
	}
	if n := len([]rune(reason)); n < invest.MinContribReasonChars {
		return invest.Contribution{}, fmt.Errorf("변경 사유는 %d자 이상 (현재 %d자)", invest.MinContribReasonChars, n)
	}
	if _, err := time.Parse("2006-01", effectiveFrom); err != nil {
		return invest.Contribution{}, fmt.Errorf("--from은 YYYY-MM: %q", effectiveFrom)
	}
	thisYM := invest.YM(recordedOn)
	if effectiveFrom <= thisYM {
		return invest.Contribution{}, fmt.Errorf("적용월은 다음 달(%s) 이후여야 한다. 이번 달(%s)은 이미 집행됐거나 집행 중이다", invest.NextYM(thisYM), thisYM)
	}
	if signalKnownFor != "" && effectiveFrom <= signalKnownFor {
		return invest.Contribution{}, fmt.Errorf("%s 신호는 이미 결정됐다. %s부터는 바꿀 수 없다 — %s 이후로 지정할 것. 신호를 보고 적립액을 조절하는 것은 마켓 타이밍이다",
			signalKnownFor, effectiveFrom, invest.NextYM(signalKnownFor))
	}
	hist, err := s.LoadContributions(ctx)
	if err != nil {
		return invest.Contribution{}, err
	}
	for _, r := range hist {
		if r.EffectiveFrom == effectiveFrom {
			return invest.Contribution{}, fmt.Errorf("%s에 이미 변경 기록이 있다", effectiveFrom)
		}
	}
	c := invest.Contribution{EffectiveFrom: effectiveFrom, Amount: amount, Reason: reason, RecordedOn: invest.FormatDay(recordedOn)}
	return c, s.ImportContribution(ctx, c)
}

// ImportContribution은 검사 없이 넣는다(최초 개시·마이그레이션용).
func (s *Store) ImportContribution(ctx context.Context, c invest.Contribution) error {
	on, err := invest.ParseDay(c.RecordedOn)
	if err != nil {
		return err
	}
	return s.q.InsertInvestContribution(ctx, dbq.InsertInvestContributionParams{
		EffectiveFrom: c.EffectiveFrom, Amount: c.Amount, Reason: c.Reason, RecordedOn: on,
	})
}

// ── 앱 밖 매수 ───────────────────────────────────────────────────────

// LoadAdjustments는 앱 밖에서 산 것들이다.
func (s *Store) LoadAdjustments(ctx context.Context) ([]invest.Adjustment, error) {
	rows, err := s.q.ListInvestAdjustments(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]invest.Adjustment, len(rows))
	for i, r := range rows {
		out[i] = invest.Adjustment{ID: r.ID, On: invest.FormatDay(r.OnDay), Symbol: r.Symbol, Shares: int(r.Shares), PaidKRW: r.PaidKrw, Reason: r.Reason}
	}
	return out, nil
}

// AppendAdjustment는 앱 밖 매수 1건을 기록한다. 적립액이 바뀌는 일이라 사유를 받는다.
func (s *Store) AppendAdjustment(ctx context.Context, a invest.Adjustment) error {
	a.Reason = strings.TrimSpace(a.Reason)
	if a.Shares <= 0 {
		return fmt.Errorf("수량은 1 이상이어야 한다: %d", a.Shares)
	}
	if a.PaidKRW <= 0 {
		return fmt.Errorf("지불액은 0보다 커야 한다: %v", a.PaidKRW)
	}
	if n := len([]rune(a.Reason)); n < invest.MinContribReasonChars {
		return fmt.Errorf("사유는 %d자 이상 (현재 %d자) — 적립액이 바뀌는 일이므로 나중에 패턴이 보이게 남긴다", invest.MinContribReasonChars, n)
	}
	on, err := invest.ParseDay(a.On)
	if err != nil {
		return err
	}
	_, err = s.q.InsertInvestAdjustment(ctx, dbq.InsertInvestAdjustmentParams{OnDay: on, Symbol: a.Symbol, Shares: int32(a.Shares), PaidKrw: a.PaidKRW, Reason: a.Reason})
	return err
}

// ── 실행 기록 ────────────────────────────────────────────────────────

// StartRun은 명령 실행을 기록한다.
func (s *Store) StartRun(ctx context.Context, cmd, args string) (int64, error) {
	return s.q.StartInvestRun(ctx, dbq.StartInvestRunParams{Cmd: cmd, Args: args})
}

// FinishRun은 종료 코드와 출력을 남긴다.
func (s *Store) FinishRun(ctx context.Context, id int64, exit int, output string) error {
	return s.q.FinishInvestRun(ctx, dbq.FinishInvestRunParams{ID: id, ExitCode: ptrInt32(int32(exit)), Output: output})
}

func ptrInt32(v int32) *int32 { return &v }

// LatestRuns는 명령별 마지막 실행이다.
func (s *Store) LatestRuns(ctx context.Context) ([]dbq.InvestRun, error) {
	return s.q.LatestInvestRuns(ctx)
}

// GetRun은 실행 1건이다.
func (s *Store) GetRun(ctx context.Context, id int64) (dbq.InvestRun, error) {
	return s.q.GetInvestRun(ctx, id)
}

// Health는 외부 하트비트용 신선도다(position 유무, 마지막 성공 status, 마지막 judge·trade 종료 코드).
// 질의 하나이고 금액·출력은 읽지 않는다.
func (s *Store) Health(ctx context.Context) (dbq.InvestHealthRow, error) {
	return s.q.InvestHealth(ctx)
}

// LastFailedRun은 그 명령의 마지막 실패다.
func (s *Store) LastFailedRun(ctx context.Context, cmd string) (dbq.InvestRun, error) {
	return s.q.LastFailedInvestRun(ctx, cmd)
}

// ── 실패 알림 ────────────────────────────────────────────────────────

// AddAlert는 실패 알림 한 줄을 남긴다. 앱이 GET /v1/invest/alerts로 읽어 폰 알림을 띄운다.
func (s *Store) AddAlert(ctx context.Context, p dbq.InsertInvestAlertParams) (dbq.InvestAlert, error) {
	return s.q.InsertInvestAlert(ctx, p)
}

// AlertsAfter는 afterID 다음의 알림이다. since보다 오래된 것과 max를 넘는 것은 뺀다.
func (s *Store) AlertsAfter(ctx context.Context, afterID int64, since time.Time, max int) ([]dbq.InvestAlert, error) {
	return s.q.ListInvestAlertsAfter(ctx, dbq.ListInvestAlertsAfterParams{AfterID: afterID, Since: since, MaxRows: int32(max)})
}

// SaveBrokerCash는 증권사가 준 주문가능 현금(USD)을 남긴다. 웹·앱 투자 화면 표시용이고 매매 판단에는 쓰지 않는다.
func (s *Store) SaveBrokerCash(ctx context.Context, cashUSD float64, at time.Time) error {
	return s.q.InsertBrokerCash(ctx, dbq.InsertBrokerCashParams{CashUsd: cashUSD, Cmd: s.cmd(), FetchedAt: at})
}

// SaveBrokerCashBoth는 달러와 원화 주문가능 금액을 함께 남긴다(입금 확인 작업).
func (s *Store) SaveBrokerCashBoth(ctx context.Context, cashUSD, cashKRW float64, at time.Time) error {
	return s.q.InsertBrokerCash(ctx, dbq.InsertBrokerCashParams{CashUsd: cashUSD, CashKrw: &cashKRW, Cmd: s.cmd(), FetchedAt: at})
}

// LastBrokerCashKRW는 증권사가 마지막으로 알려 준 원화 주문가능 금액이다. 아직 없으면 ok=false.
func (s *Store) LastBrokerCashKRW(ctx context.Context) (cashKRW float64, at time.Time, ok bool, err error) {
	r, err := s.q.LastBrokerCashKRW(ctx)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, time.Time{}, false, nil
	}
	if err != nil {
		return 0, time.Time{}, false, err
	}
	return r.CashKrw, r.FetchedAt, true, nil
}

// AddFlow는 자동 매수 단계 사건을 남긴다. 이미 있던 사건이면 false다.
func (s *Store) AddFlow(ctx context.Context, e invest.FlowEvent) (bool, error) {
	data, err := json.Marshal(e)
	if err != nil {
		return false, err
	}
	n, err := s.q.InsertInvestFlow(ctx, dbq.InsertInvestFlowParams{Ym: e.YM, Step: e.Step, Key: e.Key, At: e.At, Data: data})
	return n > 0, err
}

// LoadFlow는 그 적용월의 단계 사건이다(시각 순).
func (s *Store) LoadFlow(ctx context.Context, ym string) ([]invest.FlowEvent, error) {
	rows, err := s.q.ListInvestFlow(ctx, ym)
	if err != nil {
		return nil, err
	}
	out := make([]invest.FlowEvent, 0, len(rows))
	for _, r := range rows {
		var e invest.FlowEvent
		if err := json.Unmarshal(r.Data, &e); err != nil {
			return nil, fmt.Errorf("invest_flow %d: %w", r.ID, err)
		}
		e.YM, e.Step, e.Key, e.At = r.Ym, r.Step, r.Key, r.At
		out = append(out, e)
	}
	return out, nil
}

// DepositDay는 입금일(한국 날짜)이다. 정하지 않았으면 0.
func (s *Store) DepositDay(ctx context.Context) (int, error) {
	d, err := s.q.GetInvestDepositDay(ctx)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && d == nil) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	return int(*d), nil
}

// SetDepositDay는 입금일을 바꾼다. 0이면 지운다(첫 거래일에 산다).
func (s *Store) SetDepositDay(ctx context.Context, day int) error {
	if day < 0 || day > invest.MaxDepositDay {
		return fmt.Errorf("입금일은 1~%d일 (지우려면 0): %d", invest.MaxDepositDay, day)
	}
	var v *int32
	if day > 0 {
		d := int32(day)
		v = &d
	}
	return s.q.SetInvestDepositDay(ctx, v)
}

// LastBrokerCash는 증권사가 마지막으로 알려 준 주문가능 현금이다. 아직 없으면 ok=false.
func (s *Store) LastBrokerCash(ctx context.Context) (cashUSD float64, at time.Time, ok bool, err error) {
	r, err := s.q.LastBrokerCash(ctx)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, time.Time{}, false, nil
	}
	if err != nil {
		return 0, time.Time{}, false, err
	}
	return r.CashUsd, r.FetchedAt, true, nil
}

// AddDaily는 매일 기록 한 줄을 남긴다. 그날 줄이 이미 있으면 false(고치지 않는다).
func (s *Store) AddDaily(ctx context.Context, r invest.DailyRow) (bool, error) {
	p := dbq.InsertInvestDailyParams{Day: r.Day, Spx: r.SPX, Sma185: r.SMA185, Vol: r.Vol, C1: r.C1, C2: r.C2, Signal: r.Signal}
	if r.HasAccount {
		shares := int32(r.Shares)
		p.Symbol, p.Shares, p.CloseUsd, p.Fx = &r.Symbol, &shares, &r.CloseUSD, &r.FX
		p.StockKrw, p.CashKrw, p.InvestedKrw, p.GainKrw = &r.StockKRW, &r.CashKRW, &r.InvestedKRW, &r.GainKRW
	}
	n, err := s.q.InsertInvestDaily(ctx, p)
	return n > 0, err
}

// LastDailyDay는 매일 기록의 마지막 날이다. 아직 없으면 ok=false.
func (s *Store) LastDailyDay(ctx context.Context) (day time.Time, ok bool, err error) {
	d, err := s.q.LastInvestDailyDay(ctx)
	if errors.Is(err, pgx.ErrNoRows) {
		return time.Time{}, false, nil
	}
	if err != nil {
		return time.Time{}, false, err
	}
	return invest.DayOf(d), true, nil
}

// LoadDaily는 최근 매일 기록 max줄이다(최근 날부터).
func (s *Store) LoadDaily(ctx context.Context, max int) ([]invest.DailyRow, error) {
	rows, err := s.q.ListInvestDaily(ctx, int32(max))
	if err != nil {
		return nil, err
	}
	out := make([]invest.DailyRow, 0, len(rows))
	for _, r := range rows {
		d := invest.DailyRow{Day: invest.DayOf(r.Day), SPX: r.Spx, SMA185: r.Sma185, Vol: r.Vol, C1: r.C1, C2: r.C2,
			Signal: r.Signal, RecordedAt: r.RecordedAt}
		if r.Symbol != nil && r.Shares != nil && r.StockKrw != nil && r.CashKrw != nil && r.InvestedKrw != nil && r.GainKrw != nil {
			d.HasAccount, d.Symbol, d.Shares = true, *r.Symbol, int(*r.Shares)
			d.StockKRW, d.CashKRW, d.InvestedKRW, d.GainKRW = *r.StockKrw, *r.CashKrw, *r.InvestedKrw, *r.GainKrw
			if r.CloseUsd != nil {
				d.CloseUSD = *r.CloseUsd
			}
			if r.Fx != nil {
				d.FX = *r.Fx
			}
		}
		out = append(out, d)
	}
	return out, nil
}
