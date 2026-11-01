-- name: InsertRawEvent :one
-- 같은 content_hash가 이미 있으면 아무것도 넣지 않고 빈 결과를 반환한다.
INSERT INTO raw_events (device_id, source_package, title, text, big_text, posted_at, content_hash)
VALUES ($1, $2, $3, $4, $5, $6, $7)
ON CONFLICT (content_hash) DO NOTHING
RETURNING *;

-- name: GetRawEventByHash :one
SELECT *
FROM raw_events
WHERE content_hash = $1;

-- name: GetRawEvent :one
SELECT *
FROM raw_events
WHERE id = $1;
