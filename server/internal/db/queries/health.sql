-- 건강(핏두잉 이식) 쿼리. 테이블 설명은 migrations/00006_health.sql.

-- name: AcquireHealthLock :exec
-- 휴식권 차감처럼 "남은 장수 확인 후 쓰기"를 한 번에 하나씩 한다(가계부 7240001, 자동매매 7240002와 분리).
SELECT pg_advisory_xact_lock(7240003);

-- name: GetHealthSettings :one
SELECT doc
FROM health_settings
WHERE id = 1;

-- name: UpsertHealthSettings :exec
INSERT INTO health_settings (id, doc, updated_at)
VALUES (1, $1, now())
ON CONFLICT (id) DO UPDATE SET doc = excluded.doc, updated_at = now();

-- name: ListTargetHistory :many
SELECT from_date, kcal, carbs, protein, fat
FROM health_target_history
ORDER BY from_date;

-- name: DeleteTargetHistory :exec
DELETE FROM health_target_history;

-- name: InsertTargetHistory :exec
INSERT INTO health_target_history (from_date, kcal, carbs, protein, fat)
VALUES ($1, $2, $3, $4, $5);

-- name: UpsertMealLog :one
-- 같은 날 같은 번호는 덮어쓴다(id는 그대로).
INSERT INTO meal_logs (meal_date, meal_index, deadline, barcode, kcal, carbs, protein, fat, confidence, portion, shared_meal, logged_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
ON CONFLICT (meal_date, meal_index) DO UPDATE SET
    deadline = excluded.deadline, barcode = excluded.barcode,
    kcal = excluded.kcal, carbs = excluded.carbs, protein = excluded.protein, fat = excluded.fat,
    confidence = excluded.confidence, portion = excluded.portion, shared_meal = excluded.shared_meal,
    logged_at = excluded.logged_at, updated_at = now()
RETURNING id;

-- name: DeleteMealItems :exec
DELETE FROM meal_items
WHERE meal_log_id = $1;

-- name: InsertMealItem :exec
INSERT INTO meal_items (meal_log_id, position, name, amount, kcal, carbs, protein, fat, portion)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9);

-- name: ListMealLogsBetween :many
SELECT id, meal_date, meal_index, deadline, barcode, kcal, carbs, protein, fat, confidence, portion, shared_meal, logged_at
FROM meal_logs
WHERE meal_date BETWEEN sqlc.arg(from_date) AND sqlc.arg(to_date)
ORDER BY meal_date, meal_index;

-- name: ListMealItemsBetween :many
SELECT i.meal_log_id, i.position, i.name, i.amount, i.kcal, i.carbs, i.protein, i.fat, i.portion
FROM meal_items i
JOIN meal_logs m ON m.id = i.meal_log_id
WHERE m.meal_date BETWEEN sqlc.arg(from_date) AND sqlc.arg(to_date)
ORDER BY i.meal_log_id, i.position;

-- name: DeleteMealLog :execrows
DELETE FROM meal_logs
WHERE meal_date = $1 AND meal_index = $2;

-- name: ListMealDatesUpTo :many
-- 연속 기록 일수(streak)용. 기록이 있는 날짜를 최근 것부터.
SELECT DISTINCT meal_date
FROM meal_logs
WHERE meal_date <= $1
ORDER BY meal_date DESC;

-- name: CountPlannedMealsByDay :many
-- 달력용. 계획한 끼니(1~6)만 센다. "기타"만 있는 날은 다 먹은 날이 아니다.
SELECT meal_date, count(*)::integer AS n
FROM meal_logs
WHERE meal_date BETWEEN sqlc.arg(from_date) AND sqlc.arg(to_date) AND meal_index < 7
GROUP BY meal_date;

-- name: GetGymLog :one
SELECT gym_date, check_in_at, verified_by
FROM gym_logs
WHERE gym_date = $1;

-- name: UpsertGymCheckIn :one
INSERT INTO gym_logs (gym_date, check_in_at, verified_by)
VALUES ($1, $2, $3)
ON CONFLICT (gym_date) DO UPDATE SET check_in_at = excluded.check_in_at, verified_by = excluded.verified_by, updated_at = now()
RETURNING gym_date, check_in_at, verified_by;

-- name: InsertGymRest :exec
-- 휴식은 기록이 없는 날에만 넣는다(이미 인증한 날은 핸들러가 먼저 거절한다).
INSERT INTO gym_logs (gym_date, verified_by)
VALUES ($1, 'rest')
ON CONFLICT (gym_date) DO NOTHING;

-- name: ListGymVisitDatesBetween :many
-- 휴식(rest)은 방문으로 세지 않는다.
SELECT gym_date
FROM gym_logs
WHERE gym_date BETWEEN sqlc.arg(from_date) AND sqlc.arg(to_date) AND verified_by <> 'rest'
ORDER BY gym_date;

-- name: ListRestUses :many
SELECT used_on, used_at
FROM rest_coupon_uses
WHERE week_key = $1
ORDER BY used_at, id;

-- name: InsertRestUse :exec
INSERT INTO rest_coupon_uses (used_on, week_key)
VALUES ($1, $2);

-- name: LockMealLog :one
-- 사진 바꾸기는 끼니 행을 잠근 채 한다: 끼니 지우기·다른 사진 올리기와 겹치지 않게(행이 없으면 끼니가 없는 것).
SELECT id
FROM meal_logs
WHERE meal_date = $1 AND meal_index = $2
FOR UPDATE;

-- name: ListMealPhotos :many
SELECT meal_date, meal_index, n, file, bytes, sha256, created_at
FROM meal_photos
WHERE meal_date = $1 AND meal_index = $2
ORDER BY n;

-- name: GetMealPhoto :one
SELECT meal_date, meal_index, n, file, bytes, sha256, created_at
FROM meal_photos
WHERE meal_date = $1 AND meal_index = $2 AND n = $3;

-- name: InsertMealPhoto :exec
INSERT INTO meal_photos (meal_date, meal_index, n, file, bytes, sha256)
VALUES ($1, $2, $3, $4, $5, $6);

-- name: DeleteMealPhotos :many
-- 지운 행의 파일 이름을 돌려준다(파일은 커밋 뒤에 지운다).
DELETE FROM meal_photos
WHERE meal_date = $1 AND meal_index = $2
RETURNING file;

-- name: UpsertBodyWeight :one
INSERT INTO body_weights (day, weight_kg)
VALUES (@day, @weight_kg)
ON CONFLICT (day) DO UPDATE SET weight_kg = excluded.weight_kg, updated_at = now()
RETURNING *;

-- name: DeleteBodyWeight :execrows
DELETE FROM body_weights WHERE day = @day;

-- name: ListBodyWeights :many
-- 기간 몸무게(날짜 순)
SELECT * FROM body_weights WHERE day >= @from_day AND day <= @to_day ORDER BY day;
