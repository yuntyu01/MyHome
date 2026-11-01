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
