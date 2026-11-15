-- name: ListBalanceAnchors :many
-- 잔액이 찍힌 거래(빈 돈 계산용). 상태와 상관없이 다 본다: 카드대금·내 계좌 이체처럼 지출에서 뺀 것도 잔액은 움직인다.
SELECT id, occurred_at, amount, flow, merchant, balance::bigint AS balance, source
FROM transactions
WHERE balance IS NOT NULL AND occurred_at >= sqlc.arg(from_at)
ORDER BY source, occurred_at, id;

-- name: ListBalanceGapMarks :many
SELECT to_transaction_id, gap, resolution FROM balance_gap_marks
WHERE to_transaction_id = ANY(sqlc.arg(ids)::bigint[]);

-- name: UpsertBalanceGapMark :exec
INSERT INTO balance_gap_marks (to_transaction_id, gap, resolution, filled_transaction_id)
VALUES ($1, $2, $3, $4)
ON CONFLICT (to_transaction_id) DO UPDATE
SET gap = EXCLUDED.gap, resolution = EXCLUDED.resolution, filled_transaction_id = EXCLUDED.filled_transaction_id, created_at = now();
