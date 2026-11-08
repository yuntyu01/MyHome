-- name: CreateLinkCode :exec
INSERT INTO app_link_codes (code_hash, challenge, email, expires_at)
VALUES ($1, $2, $3, $4);

-- name: ConsumeLinkCode :one
-- 한 번만 쓸 수 있게 쓰는 순간 표시한다. 이미 썼거나 만료됐으면 행이 없다.
UPDATE app_link_codes
SET used_at = now()
WHERE code_hash = $1
  AND used_at IS NULL
  AND expires_at > now()
RETURNING *;

-- name: DeleteExpiredLinkCodes :execrows
DELETE
FROM app_link_codes
WHERE expires_at <= now();
