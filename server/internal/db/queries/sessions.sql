-- name: CreateSession :one
INSERT INTO sessions (id_hash, email, csrf_token, expires_at)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: GetActiveSession :one
SELECT *
FROM sessions
WHERE id_hash = $1
  AND expires_at > now();

-- name: TouchSession :exec
UPDATE sessions
SET last_seen_at = now()
WHERE id_hash = $1
  AND last_seen_at < now() - interval '5 minutes';

-- name: DeleteSession :exec
-- 로그아웃: 세션 행을 지운다(로그인 상태 정보일 뿐 가계부 데이터가 아님).
DELETE
FROM sessions
WHERE id_hash = $1;

-- name: DeleteExpiredSessions :execrows
DELETE
FROM sessions
WHERE expires_at <= now();
