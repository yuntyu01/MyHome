-- name: CreateDevice :one
INSERT INTO devices (name, token_hash, kakao_id)
VALUES ($1, $2, $3)
RETURNING *;

-- name: GetActiveDeviceByTokenHash :one
SELECT *
FROM devices
WHERE token_hash = $1
  AND revoked_at IS NULL;

-- name: TouchDevice :exec
UPDATE devices
SET last_seen_at = now()
WHERE id = $1
  AND (last_seen_at IS NULL OR last_seen_at < now() - interval '1 minute');

-- name: ListDevices :many
SELECT *
FROM devices
ORDER BY id;

-- name: RevokeDevice :one
UPDATE devices
SET revoked_at = coalesce(revoked_at, now())
WHERE id = $1
RETURNING *;
