-- name: ListCategories :many
SELECT *
FROM categories
ORDER BY sort, id;

-- name: GetCategory :one
SELECT *
FROM categories
WHERE id = $1;

-- name: GetCategoryByCode :one
SELECT *
FROM categories
WHERE code = $1;

-- name: CreateCategory :one
INSERT INTO categories (name, kind, sort)
VALUES ($1, $2, $3)
RETURNING *;

-- name: UpdateCategory :one
UPDATE categories
SET name     = $2,
    sort     = $3,
    archived = $4
WHERE id = $1
RETURNING *;
