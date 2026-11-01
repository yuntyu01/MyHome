-- name: InsertTransaction :one
INSERT INTO transactions (raw_event_id, occurred_at, amount, flow, merchant, memo, direction, category_id,
                          settle_category_id, status, exclusion_reason, review_reason, merged_into, cancels_id,
                          source, kind, installment_months, balance, manually_edited)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19)
RETURNING *;

-- name: GetTransaction :one
SELECT *
FROM transactions
WHERE id = $1;

-- name: GetTransactionForUpdate :one
SELECT *
FROM transactions
WHERE id = $1
FOR UPDATE;

-- name: ListTransactionsByRawEvent :many
SELECT *
FROM transactions
WHERE raw_event_id = $1
ORDER BY id;

-- name: UpdateTransaction :one
UPDATE transactions
SET occurred_at        = $2,
    amount             = $3,
    flow               = $4,
    merchant           = $5,
    memo               = $6,
    direction          = $7,
    category_id        = $8,
    settle_category_id = $9,
    status             = $10,
    exclusion_reason   = $11,
    review_reason      = $12,
    merged_into        = $13,
    cancels_id         = $14,
    manually_edited    = $15,
    updated_at         = now()
WHERE id = $1
RETURNING *;

-- name: FindDedupeCandidate :one
-- 다른 앱(패키지)에서 같은 방향·같은 금액이 시간 창 안에 들어온 거래. 가장 가까운 것 하나.
SELECT t.*
FROM transactions t
         JOIN raw_events r ON r.id = t.raw_event_id
WHERE t.id <> @exclude_id
  AND r.source_package <> @source_package
  AND t.flow = @flow
  AND t.amount = @amount
  AND t.occurred_at BETWEEN @window_from AND @window_to
  AND t.merged_into IS NULL
  AND t.exclusion_reason IS DISTINCT FROM 'duplicate'
  AND NOT EXISTS (SELECT 1
                  FROM transactions d
                           JOIN raw_events dr ON dr.id = d.raw_event_id
                  WHERE d.merged_into = t.id
                    AND dr.source_package = @source_package)
ORDER BY abs(extract(epoch FROM (t.occurred_at - @occurred_at::timestamptz))), t.id
LIMIT 1;

-- name: FindCancelCandidates :many
-- 취소 알림의 원거래 후보: 취소 금액 이상인 최근 지출(아직 전액 취소되지 않은 것).
SELECT *
FROM transactions
WHERE id <> @exclude_id
  AND direction = 'expense'
  AND flow = 'out'
  AND status IN ('confirmed', 'needs_review')
  AND amount >= @amount
  AND occurred_at BETWEEN @window_from AND @window_to
ORDER BY occurred_at DESC, id DESC
LIMIT 50;

-- name: ListTransactions :many
SELECT t.*, c.name AS category_name
FROM transactions t
         LEFT JOIN categories c ON c.id = t.category_id
WHERE t.occurred_at >= @from_at
  AND t.occurred_at < @to_at
  AND (sqlc.narg(status)::text IS NULL OR t.status = sqlc.narg(status))
  AND (sqlc.narg(category_id)::bigint IS NULL OR t.category_id = sqlc.narg(category_id))
  AND (sqlc.narg(direction)::text IS NULL OR t.direction = sqlc.narg(direction))
  AND (sqlc.narg(q)::text IS NULL OR t.merchant ILIKE '%' || sqlc.narg(q) || '%' OR t.memo ILIKE '%' || sqlc.narg(q) || '%')
ORDER BY t.occurred_at DESC, t.id DESC
LIMIT @lim OFFSET @off;

-- name: ListNeedsReview :many
SELECT t.*, c.name AS category_name
FROM transactions t
         LEFT JOIN categories c ON c.id = t.category_id
WHERE t.status = 'needs_review'
ORDER BY t.occurred_at DESC, t.id DESC
LIMIT $1;

-- name: CountNeedsReview :one
SELECT count(*)
FROM transactions
WHERE status = 'needs_review';

-- name: RecentTransactions :many
SELECT t.*, c.name AS category_name
FROM transactions t
         LEFT JOIN categories c ON c.id = t.category_id
WHERE t.status <> 'excluded'
ORDER BY t.occurred_at DESC, t.id DESC
LIMIT $1;

-- name: CategorySummary :many
-- 분류별 남은 돈 = 예산 - 확정 지출(취소·환불 차감) + 그 분류로 돌려놓은 정산.
WITH spent AS (SELECT x.category_id,
                      sum(CASE WHEN x.flow = 'out' THEN x.amount ELSE -x.amount END)::bigint AS spent
               FROM transactions x
               WHERE x.status = 'confirmed'
                 AND x.direction = 'expense'
                 AND x.occurred_at >= @from_at
                 AND x.occurred_at < @to_at
               GROUP BY x.category_id),
     settled AS (SELECT t.settle_category_id AS category_id, sum(t.amount)::bigint AS settled
                 FROM transactions t
                          JOIN categories sc ON sc.id = t.category_id
                 WHERE t.status = 'confirmed'
                   AND sc.code = 'settlement'
                   AND t.settle_category_id IS NOT NULL
                   AND t.occurred_at >= @from_at
                   AND t.occurred_at < @to_at
                 GROUP BY t.settle_category_id)
SELECT c.id,
       c.name,
       c.sort,
       coalesce(b.amount, 0)::bigint                                                 AS budget,
       coalesce(s.spent, 0)::bigint                                                  AS spent,
       coalesce(st.settled, 0)::bigint                                               AS settled,
       (coalesce(b.amount, 0) - coalesce(s.spent, 0) + coalesce(st.settled, 0))::bigint AS remaining
FROM categories c
         LEFT JOIN budgets b ON b.category_id = c.id AND b.period_start = @period_start
         LEFT JOIN spent s ON s.category_id = c.id
         LEFT JOIN settled st ON st.category_id = c.id
WHERE c.kind = 'expense'
  AND (NOT c.archived OR b.amount IS NOT NULL OR s.spent IS NOT NULL)
ORDER BY c.sort, c.id;

-- name: PeriodTotals :one
-- 기간 총계: 수입, 정산(분류 미지정분), 투자·저축 순이동, 내 계좌 이체.
SELECT coalesce(sum(t.amount) FILTER (WHERE c.code = 'income' AND t.flow = 'in'), 0)::bigint AS income,
       coalesce(sum(t.amount) FILTER (WHERE c.code = 'settlement' AND t.settle_category_id IS NULL), 0)::bigint AS settled_unassigned,
       coalesce(sum(t.amount) FILTER (WHERE c.code = 'settlement'), 0)::bigint AS settled_total,
       coalesce(sum(CASE WHEN t.flow = 'out' THEN t.amount ELSE -t.amount END) FILTER (WHERE c.code = 'investment'), 0)::bigint AS investment,
       coalesce(sum(CASE WHEN t.flow = 'out' THEN t.amount ELSE -t.amount END) FILTER (WHERE c.code = 'own_transfer'), 0)::bigint AS own_transfer
FROM transactions t
         JOIN categories c ON c.id = t.category_id
WHERE t.status = 'confirmed'
  AND t.occurred_at >= @from_at
  AND t.occurred_at < @to_at;

-- name: DailyExpense :many
-- 일별 확정 지출(KST 날짜 기준).
SELECT (occurred_at AT TIME ZONE 'Asia/Seoul')::date AS day,
       sum(CASE WHEN flow = 'out' THEN amount ELSE -amount END)::bigint AS spent
FROM transactions
WHERE status = 'confirmed'
  AND direction = 'expense'
  AND occurred_at >= @from_at
  AND occurred_at < @to_at
GROUP BY day
ORDER BY day;

-- name: SourceTotals :many
-- 대사 도우미: 출처별 확정 지출 합계와 건수.
SELECT source,
       count(*)::bigint AS cnt,
       sum(CASE WHEN flow = 'out' THEN amount ELSE -amount END)::bigint AS spent
FROM transactions
WHERE status = 'confirmed'
  AND direction = 'expense'
  AND occurred_at >= @from_at
  AND occurred_at < @to_at
GROUP BY source
ORDER BY source;

-- name: ListTransactionsForReprocess :many
-- 재처리 대상: 원문에서 만들어졌고 사용자가 고치지 않은 거래.
SELECT *
FROM transactions
WHERE raw_event_id = ANY (@raw_ids::bigint[])
  AND NOT manually_edited;

-- name: ListLinkedTransactions :many
-- 주어진 거래와 중복·취소로 연결된 거래(양방향).
SELECT *
FROM transactions
WHERE merged_into = ANY (@ids::bigint[])
   OR cancels_id = ANY (@ids::bigint[])
   OR id IN (SELECT merged_into FROM transactions x WHERE x.id = ANY (@ids::bigint[]) AND x.merged_into IS NOT NULL)
   OR id IN (SELECT cancels_id FROM transactions y WHERE y.id = ANY (@ids::bigint[]) AND y.cancels_id IS NOT NULL);

-- name: DetachLinks :exec
UPDATE transactions
SET merged_into = CASE WHEN merged_into = ANY (@ids::bigint[]) THEN NULL ELSE merged_into END,
    cancels_id  = CASE WHEN cancels_id = ANY (@ids::bigint[]) THEN NULL ELSE cancels_id END
WHERE merged_into = ANY (@ids::bigint[])
   OR cancels_id = ANY (@ids::bigint[]);

-- name: DeleteDerivedTransactions :execrows
-- 재처리 전용: 원문에서 다시 만들 수 있는(수동 수정 안 된) 거래만 지운다. 원문은 그대로 둔다.
DELETE
FROM transactions
WHERE id = ANY (@ids::bigint[])
  AND raw_event_id IS NOT NULL
  AND NOT manually_edited;

-- name: TopMerchants :many
-- 월간 리포트: 확정 지출이 큰 가맹점.
SELECT merchant,
       count(*)::bigint AS cnt,
       sum(CASE WHEN flow = 'out' THEN amount ELSE -amount END)::bigint AS spent
FROM transactions
WHERE status = 'confirmed'
  AND direction = 'expense'
  AND merchant <> ''
  AND occurred_at >= @from_at
  AND occurred_at < @to_at
GROUP BY merchant
ORDER BY spent DESC
LIMIT @lim;

-- name: StatusCounts :one
-- 기간 거래 건수(상태별).
SELECT count(*) FILTER (WHERE status = 'confirmed' AND direction = 'expense')::bigint AS expense_count,
       count(*) FILTER (WHERE status = 'needs_review')::bigint                        AS review_count,
       count(*) FILTER (WHERE status = 'excluded')::bigint                            AS excluded_count
FROM transactions
WHERE occurred_at >= @from_at
  AND occurred_at < @to_at;

-- name: ExcludedTotals :many
-- 대사 도우미: 제외된 거래를 사유·출처별로(카드 명세서와 차이 설명용).
SELECT coalesce(exclusion_reason, '')::text AS reason,
       source,
       count(*)::bigint                     AS cnt,
       sum(amount)::bigint                  AS amount
FROM transactions
WHERE status = 'excluded'
  AND occurred_at >= @from_at
  AND occurred_at < @to_at
GROUP BY reason, source
ORDER BY reason, source;

-- name: SourceFlowTotals :many
-- 대사 도우미: 출처별 확정 거래를 방향(나감/들어옴)·성격별로.
SELECT source,
       direction,
       flow,
       count(*)::bigint    AS cnt,
       sum(amount)::bigint AS amount
FROM transactions
WHERE status = 'confirmed'
  AND occurred_at >= @from_at
  AND occurred_at < @to_at
GROUP BY source, direction, flow
ORDER BY source, direction, flow;
