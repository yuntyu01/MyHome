-- name: ListFixedDeductions :many
SELECT *
FROM fixed_deductions
WHERE NOT archived
ORDER BY sort, id;

-- name: FixedDeductionTotal :one
SELECT coalesce(sum(amount), 0)::bigint AS total
FROM fixed_deductions
WHERE NOT archived;

-- name: CreateFixedDeduction :one
INSERT INTO fixed_deductions (name, amount, sort)
VALUES (@name, @amount, coalesce((SELECT max(sort) + 10 FROM fixed_deductions), 10))
RETURNING *;

-- name: UpdateFixedDeduction :one
UPDATE fixed_deductions
SET name     = @name,
    amount   = @amount,
    archived = @archived
WHERE id = @id
RETURNING *;

-- name: GetFixedDeduction :one
SELECT *
FROM fixed_deductions
WHERE id = $1;
