-- name: ListRules :many
SELECT r.*, c.name AS category_name
FROM merchant_rules r
         JOIN categories c ON c.id = r.category_id
ORDER BY r.priority DESC, length(r.pattern) DESC, r.id DESC;

-- name: UpsertRule :one
INSERT INTO merchant_rules (pattern, match_type, category_id, flow, priority, created_from)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (pattern, match_type, coalesce(flow, '')) DO UPDATE
    SET category_id  = excluded.category_id,
        priority     = excluded.priority,
        created_from = excluded.created_from
RETURNING *;

-- name: DeleteRule :execrows
DELETE
FROM merchant_rules
WHERE id = $1;
