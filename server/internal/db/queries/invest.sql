-- 자동매매(TQ 이식) 쿼리. 테이블 설명은 migrations/00005_invest.sql.

-- name: ListInvestPrices :many
SELECT day, spx, ndx, qqq, qqqm, qld, tqqq, usdkrw, usdkrw_mkt
FROM invest_prices
ORDER BY day;

-- name: CopyInvestPrices :copyfrom
INSERT INTO invest_prices (day, spx, ndx, qqq, qqqm, qld, tqqq, usdkrw, usdkrw_mkt)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9);

-- name: InsertInvestPrice :execrows
-- 새 거래일만 넣는다. 이미 있는 날짜는 건드리지 않는다(과거 행 보호).
INSERT INTO invest_prices (day, spx, ndx, qqq, qqqm, qld, tqqq, usdkrw, usdkrw_mkt)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
ON CONFLICT (day) DO NOTHING;

-- name: FillInvestFXMkt :execrows
-- 시장 환율은 새 컬럼이라 보호할 과거값이 없다. 비어 있는 날만 채운다.
UPDATE invest_prices
SET usdkrw_mkt = $2
WHERE day = $1 AND usdkrw_mkt IS NULL;

-- name: GetInvestState :one
SELECT doc, updated_at
FROM invest_state
WHERE key = $1;

-- name: UpsertInvestState :exec
INSERT INTO invest_state (key, doc, updated_at)
VALUES ($1, $2, now())
ON CONFLICT (key) DO UPDATE SET doc = excluded.doc, updated_at = now();

-- name: DeleteInvestState :execrows
DELETE FROM invest_state
WHERE key = $1;

-- name: InsertInvestStateHistory :exec
INSERT INTO invest_state_history (key, cmd, doc)
VALUES ($1, $2, $3);

-- name: ListInvestStateHistory :many
SELECT id, key, cmd, doc, created_at
FROM invest_state_history
ORDER BY id DESC
LIMIT $1;

-- name: ListInvestLedger :many
SELECT id, on_day, ym, amount_krw, source, symbol, shares, note, created_at
FROM invest_ledger
WHERE superseded_at IS NULL
ORDER BY on_day, ym, id;

-- name: InsertInvestLedger :one
INSERT INTO invest_ledger (on_day, ym, amount_krw, source, symbol, shares, note)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING id;

-- name: SupersedeInvestLedgerForAmend :execrows
-- 정정은 그 달을 다시 계산하는 것이라 같은 달의 자동·수동·앞선 정정 기록을 덮는다.
UPDATE invest_ledger
SET superseded_at = now()
WHERE ym = $1 AND source IN ('trade', 'execute', 'amend') AND superseded_at IS NULL;

-- name: ListInvestDeviations :many
SELECT id, kind, ym, occurred_on, signal_target, actual_target, reason, locked_until, resurface_on
FROM invest_deviations
ORDER BY id;

-- name: InsertInvestDeviation :one
INSERT INTO invest_deviations (kind, ym, occurred_on, signal_target, actual_target, reason, locked_until, resurface_on)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING id;

-- name: ListInvestContributions :many
SELECT effective_from, amount, reason, recorded_on
FROM invest_contributions
ORDER BY effective_from;

-- name: InsertInvestContribution :exec
INSERT INTO invest_contributions (effective_from, amount, reason, recorded_on)
VALUES ($1, $2, $3, $4);

-- name: ListInvestAdjustments :many
SELECT id, on_day, symbol, shares, paid_krw, reason
FROM invest_adjustments
ORDER BY id;

-- name: InsertInvestAdjustment :one
INSERT INTO invest_adjustments (on_day, symbol, shares, paid_krw, reason)
VALUES ($1, $2, $3, $4, $5)
RETURNING id;

-- name: StartInvestRun :one
INSERT INTO invest_runs (cmd, args)
VALUES ($1, $2)
RETURNING id;

-- name: FinishInvestRun :exec
UPDATE invest_runs
SET finished_at = now(), exit_code = $2, output = $3
WHERE id = $1;

-- name: LatestInvestRuns :many
-- 명령별 마지막 실행 1건씩.
SELECT DISTINCT ON (cmd) id, cmd, args, started_at, finished_at, exit_code, output
FROM invest_runs
ORDER BY cmd, started_at DESC;

-- name: GetInvestRun :one
SELECT id, cmd, args, started_at, finished_at, exit_code, output
FROM invest_runs
WHERE id = $1;

-- name: LastFailedInvestRun :one
SELECT id, cmd, args, started_at, finished_at, exit_code, output
FROM invest_runs
WHERE cmd = $1 AND exit_code IS NOT NULL AND exit_code <> 0
ORDER BY started_at DESC
LIMIT 1;

-- name: InsertInvestAlert :one
-- 실패 알림 한 줄. 매매 역할만 쓴다.
INSERT INTO invest_alerts (unit, cmd, run_id, exit_code, title, body)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING id, created_at, unit, cmd, run_id, exit_code, title, body;

-- name: ListInvestAlertsAfter :many
-- 앱이 마지막으로 본 id 다음 알림. since보다 오래된 것은 주지 않는다(처음 연결한 기기에 옛 알림을 쏟지 않게).
SELECT id, created_at, unit, cmd, run_id, exit_code, title, body
FROM invest_alerts
WHERE id > sqlc.arg(after_id) AND created_at >= sqlc.arg(since)
ORDER BY id
LIMIT sqlc.arg(max_rows);


-- name: InvestHealth :one
-- 외부 하트비트용 신선도(GET /healthz/invest). 금액·출력은 읽지 않는다.
-- 매매를 아직 시작하지 않았으면(position 없음) has_position이 false다.
SELECT
    EXISTS (SELECT 1 FROM invest_state WHERE key = 'position') AS has_position,
    COALESCE((SELECT max(started_at) FROM invest_runs WHERE cmd = 'status' AND exit_code = 0),
             'epoch'::timestamptz)::timestamptz AS last_status_ok,
    COALESCE((SELECT exit_code FROM invest_runs WHERE cmd = 'judge' AND finished_at IS NOT NULL
              ORDER BY started_at DESC LIMIT 1), 0)::integer AS judge_exit,
    COALESCE((SELECT exit_code FROM invest_runs WHERE cmd = 'trade' AND finished_at IS NOT NULL
              ORDER BY started_at DESC LIMIT 1), 0)::integer AS trade_exit;

-- name: InsertBrokerCash :exec
INSERT INTO invest_broker_cash (cash_usd, cash_krw, cmd, fetched_at) VALUES ($1, $2, $3, $4);

-- name: LastBrokerCash :one
-- 증권사가 마지막으로 알려 준 주문가능 현금(웹·앱 투자 화면).
SELECT cash_usd, cmd, fetched_at FROM invest_broker_cash ORDER BY fetched_at DESC, id DESC LIMIT 1;

-- name: LastBrokerCashKRW :one
-- 증권사가 마지막으로 알려 준 원화 주문가능 금액. 원화를 같이 읽은 작업만 남긴다.
SELECT cash_krw::double precision AS cash_krw, fetched_at FROM invest_broker_cash
WHERE cash_krw IS NOT NULL ORDER BY fetched_at DESC, id DESC LIMIT 1;

-- name: InsertInvestFlow :execrows
-- 같은 사건(step, key)은 한 번만 남는다. 0이면 이미 있던 것이다.
INSERT INTO invest_flow (ym, step, key, at, data) VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (step, key) DO NOTHING;

-- name: ListInvestFlow :many
SELECT id, ym, step, key, at, data, recorded_at FROM invest_flow WHERE ym = $1 ORDER BY at, id;

-- name: GetInvestDepositDay :one
SELECT deposit_day FROM invest_prefs WHERE id = 1;

-- name: SetInvestDepositDay :exec
INSERT INTO invest_prefs (id, deposit_day, updated_at) VALUES (1, $1, now())
ON CONFLICT (id) DO UPDATE SET deposit_day = EXCLUDED.deposit_day, updated_at = now();

-- name: InsertInvestDaily :execrows
-- 그날 줄이 이미 있으면 건드리지 않는다(그날 본 대로 남긴다). 0이면 이미 있던 것이다.
INSERT INTO invest_daily (day, spx, sma185, vol, c1, c2, signal,
    symbol, shares, close_usd, fx, stock_krw, cash_krw, invested_krw, gain_krw)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
ON CONFLICT (day) DO NOTHING;

-- name: LastInvestDailyDay :one
SELECT day FROM invest_daily ORDER BY day DESC LIMIT 1;

-- name: ListInvestDaily :many
-- 최근 거래일부터.
SELECT * FROM invest_daily ORDER BY day DESC LIMIT sqlc.arg(max_rows)::integer;

-- name: InsertInvestJudgment :execrows
-- 판정 1회 = 1행. 같은 달을 다시 판정하면 새 행을 만들지 않는다(호출부가 같은 값인지 대조한다).
INSERT INTO invest_judgments (ym, kind, day, spx, sma185, vol, c1, c2, target)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
ON CONFLICT (ym, kind) DO NOTHING;

-- name: GetInvestJudgment :one
SELECT ym, kind, day, spx, sma185, vol, c1, c2, target, created_at FROM invest_judgments WHERE ym = $1 AND kind = $2;

-- name: ListInvestJudgments :many
SELECT ym, kind, day, spx, sma185, vol, c1, c2, target, created_at FROM invest_judgments ORDER BY ym, kind;

-- name: GetOpenInvestOrderBook :one
-- 열린 주문 묶음(많아야 하나).
SELECT id, ym, stage, style_idx, ledger_gap FROM invest_order_books WHERE closed_at IS NULL;

-- name: InsertInvestOrderBook :one
INSERT INTO invest_order_books (ym, stage, style_idx, ledger_gap) VALUES ($1, $2, $3, $4) RETURNING id;

-- name: UpdateInvestOrderBook :exec
UPDATE invest_order_books SET stage = $2, style_idx = $3, ledger_gap = $4 WHERE id = $1 AND closed_at IS NULL;

-- name: CloseInvestOrderBook :execrows
UPDATE invest_order_books SET closed_at = now(), closed_by = $2 WHERE id = $1 AND closed_at IS NULL;

-- name: ListInvestOrdersOfBook :many
-- 묶음의 주문 전부(빠진 것 포함). 화면·trade는 dropped를 뺀다.
SELECT id, book_id, seq, client_order_id, order_id, symbol, side, qty, price, style, submitted_on, submitted_at, state, fill_price
FROM invest_orders WHERE book_id = $1 ORDER BY seq, id;

-- name: GetInvestOrderByClientID :one
SELECT id, book_id, seq, client_order_id, order_id, symbol, side, qty, price, style, submitted_on, submitted_at, state, fill_price
FROM invest_orders WHERE client_order_id = $1;

-- name: InsertInvestOrder :one
INSERT INTO invest_orders (book_id, seq, client_order_id, order_id, symbol, side, qty, price, style, submitted_on, submitted_at, state, fill_price)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13) RETURNING id;

-- name: UpdateInvestOrder :exec
UPDATE invest_orders SET book_id = $2, seq = $3, order_id = $4, symbol = $5, side = $6, qty = $7, price = $8, style = $9,
    submitted_on = $10, submitted_at = $11, state = $12, fill_price = $13, updated_at = now()
WHERE id = $1;

-- name: InsertInvestOrderEvent :exec
INSERT INTO invest_order_events (order_ref, state, qty, note) VALUES ($1, $2, $3, $4);

-- name: ListInvestOrderEvents :many
SELECT e.id, o.client_order_id, o.order_id, e.state, e.qty, e.note, e.at
FROM invest_order_events e JOIN invest_orders o ON o.id = e.order_ref ORDER BY e.id;
