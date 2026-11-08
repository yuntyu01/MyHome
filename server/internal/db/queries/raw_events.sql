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
SET processed_at  = now(),
    parse_error   = sqlc.narg(parse_error),
    ignore_reason = sqlc.narg(ignore_reason)
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
SET processed_at  = NULL,
    parse_error   = NULL,
    ignore_reason = NULL
WHERE id = ANY (@ids::bigint[]);

-- name: CountAttentionRawEvents :one
-- 확인이 필요한 원문: 해석 실패, 또는 받은 지 오래됐는데 아직 처리되지 않은 것(처리 오류). @since 이후 받은 것만.
SELECT count(*)
FROM raw_events
WHERE received_at >= @since
  AND (parse_error IS NOT NULL OR (processed_at IS NULL AND received_at < @stale_before));
