-- 집중(공부 타이머) 쿼리. 테이블 설명은 migrations/00013_focus.sql.
-- Upsert는 저장된 updated_at이 더 새로우면 아무것도 바꾸지 않는다(last-write-wins, 같으면 덮어쓴다).
-- 그래서 Upsert 뒤에 Get으로 저장된 값을 다시 읽어 돌려준다.

-- name: UpsertFocusSubject :exec
INSERT INTO focus_subjects (id, name, color, sort, archived, updated_at)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (id) DO UPDATE SET
    name = excluded.name, color = excluded.color, sort = excluded.sort,
    archived = excluded.archived, updated_at = excluded.updated_at
WHERE focus_subjects.updated_at <= excluded.updated_at;

-- name: GetFocusSubject :one
SELECT id, name, color, sort, archived, updated_at
FROM focus_subjects
WHERE id = $1;

-- name: FocusSubjectExists :one
SELECT EXISTS (SELECT 1 FROM focus_subjects WHERE id = $1);

-- name: ListFocusSubjects :many
SELECT id, name, color, sort, archived, updated_at
FROM focus_subjects
ORDER BY sort, name, id;

-- name: UpsertFocusSession :exec
INSERT INTO focus_sessions (id, subject_id, started_at, ended_at, deleted, updated_at)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (id) DO UPDATE SET
    subject_id = excluded.subject_id, started_at = excluded.started_at, ended_at = excluded.ended_at,
    deleted = excluded.deleted, updated_at = excluded.updated_at
WHERE focus_sessions.updated_at <= excluded.updated_at;

-- name: GetFocusSession :one
SELECT id, subject_id, started_at, ended_at, deleted, updated_at
FROM focus_sessions
WHERE id = $1;

-- name: ListFocusSessions :many
-- 지운 것까지 모두(앱 복원용).
SELECT id, subject_id, started_at, ended_at, deleted, updated_at
FROM focus_sessions
ORDER BY started_at, id;

-- name: ListFocusSessionsOverlapping :many
-- [window_start, window_end)에 걸치는 지우지 않은 세션. 한 세션은 24시간까지라 since(window_start - 24시간)
-- 뒤에 시작한 것만 보면 된다(started_at 인덱스를 쓴다).
SELECT subject_id, started_at, ended_at
FROM focus_sessions
WHERE NOT deleted
  AND started_at >= sqlc.arg(since)
  AND started_at < sqlc.arg(window_end)
  AND ended_at > sqlc.arg(window_start)
ORDER BY started_at, id;

-- name: UpsertFocusStay :exec
INSERT INTO focus_stays (id, place, entered_at, exited_at, deleted, updated_at)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (id) DO UPDATE SET
    place = excluded.place, entered_at = excluded.entered_at, exited_at = excluded.exited_at,
    deleted = excluded.deleted, updated_at = excluded.updated_at
WHERE focus_stays.updated_at <= excluded.updated_at;

-- name: GetFocusStay :one
SELECT id, place, entered_at, exited_at, deleted, updated_at
FROM focus_stays
WHERE id = $1;

-- name: ListFocusStays :many
-- 지운 것까지 모두(앱 복원용).
SELECT id, place, entered_at, exited_at, deleted, updated_at
FROM focus_stays
ORDER BY entered_at, id;

-- name: ListFocusStaysOverlapping :many
-- ListFocusSessionsOverlapping과 같다(머무름도 24시간까지).
SELECT entered_at, exited_at
FROM focus_stays
WHERE NOT deleted
  AND entered_at >= sqlc.arg(since)
  AND entered_at < sqlc.arg(window_end)
  AND exited_at > sqlc.arg(window_start)
ORDER BY entered_at, id;
