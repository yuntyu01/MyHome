-- name: ListBudgets :many
SELECT b.*, c.name AS category_name
FROM budgets b
         JOIN categories c ON c.id = b.category_id
WHERE b.period_start = $1
ORDER BY c.sort, c.id;

-- name: UpsertBudget :exec
INSERT INTO budgets (period_start, category_id, amount)
VALUES ($1, $2, $3)
ON CONFLICT (period_start, category_id) DO UPDATE SET amount = excluded.amount;

-- name: CopyBudgets :execrows
-- 이전 기간 예산을 새 기간으로 복사한다. 이미 있는 분류는 덮어쓰지 않는다.
INSERT INTO budgets (period_start, category_id, amount)
SELECT @to_period::date, category_id, amount
FROM budgets
WHERE period_start = @from_period::date
ON CONFLICT (period_start, category_id) DO NOTHING;

-- name: HasBudgets :one
SELECT EXISTS (SELECT 1 FROM budgets WHERE period_start = $1);

-- name: LatestBudgetPeriodBefore :one
-- 이 기간보다 앞서 예산이 있는 가장 최근 기간. 없으면 결과 없음.
SELECT period_start
FROM budgets
WHERE period_start < $1
ORDER BY period_start DESC
LIMIT 1;

-- name: TotalBudgetAt :one
-- 이 기간에 쓰는 한 달 전체 예산: 이 기간 이하에서 가장 최근에 정한 값(NULL = 분류 합계). 없으면 결과 없음.
SELECT amount
FROM period_total_budgets
WHERE period_start <= $1
ORDER BY period_start DESC
LIMIT 1;

-- name: SetTotalBudget :exec
INSERT INTO period_total_budgets (period_start, amount)
VALUES ($1, $2)
ON CONFLICT (period_start) DO UPDATE SET amount = excluded.amount, updated_at = now();
