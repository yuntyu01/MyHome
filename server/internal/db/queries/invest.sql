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
