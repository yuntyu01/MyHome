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
INSERT INTO categories (name, kind, sort, parent_id, memo)
VALUES (@name, @kind, @sort, sqlc.narg(parent_id), @memo)
RETURNING *;

-- name: UpdateCategory :one
UPDATE categories
SET name      = @name,
    sort      = @sort,
    archived  = @archived,
    parent_id = sqlc.narg(parent_id),
    memo      = @memo
WHERE id = @id
RETURNING *;

-- name: SetCategoriesArchived :exec
-- 폴더를 숨기거나 되살릴 때 아래 분류(또는 위 폴더)까지 한 번에 바꾼다.
UPDATE categories
SET archived = @archived
WHERE id = ANY (@ids::bigint[])
  AND code IS NULL;
