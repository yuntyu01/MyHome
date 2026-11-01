-- name: ListSettings :many
SELECT *
FROM settings
ORDER BY key;

-- name: UpsertSetting :exec
INSERT INTO settings (key, value, updated_at)
VALUES ($1, $2, now())
ON CONFLICT (key) DO UPDATE SET value = excluded.value, updated_at = now();

-- name: AcquireProcessingLock :exec
-- 이벤트 처리를 한 번에 하나씩 하도록 트랜잭션 단위 잠금(중복 판단 경쟁 방지).
SELECT pg_advisory_xact_lock(7240001);
