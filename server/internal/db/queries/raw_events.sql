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

-- name: GetRawEventForUpdate :one
SELECT *
FROM raw_events
WHERE id = $1
FOR UPDATE;

-- name: MarkRawEventProcessed :exec
UPDATE raw_events
SET processed_at = now(),
    parse_error  = sqlc.narg(parse_error)
WHERE id = $1;

-- name: ListFailedRawEvents :many
SELECT *
FROM raw_events
WHERE parse_error IS NOT NULL
ORDER BY posted_at DESC
LIMIT $1;

-- name: ListRawEvents :many
SELECT *
FROM raw_events
ORDER BY posted_at DESC
LIMIT $1;

-- name: ListRawEventsInRange :many
SELECT *
FROM raw_events
WHERE posted_at >= @from_at
  AND posted_at < @to_at
ORDER BY posted_at, id;

-- name: ListUnprocessedRawEvents :many
SELECT *
FROM raw_events
WHERE processed_at IS NULL
ORDER BY posted_at, id
LIMIT $1;

-- name: ResetRawEventProcessing :exec
UPDATE raw_events
SET processed_at = NULL,
    parse_error  = NULL
WHERE id = ANY (@ids::bigint[]);
