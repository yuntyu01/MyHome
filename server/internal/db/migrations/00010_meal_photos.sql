-- +goose Up

-- 끼니 사진 서버 보관 (2026-11-08 사용자 결정). 파일은 VM 디스크(MEAL_PHOTO_DIR, 운영은 /var/lib/myhome/meal_photos)에 두고
-- 여기에는 어느 끼니의 몇 번째 사진이 어느 파일인지만 둔다. 끼니를 지우면 사진 행과 파일도 지운다(health.Service.DeleteMeal).
-- 폰의 사진은 보기용 캐시이고, 폰 보관 기간(photoKeepDays)이 지나 지워져도 서버에서 다시 받는다.
-- 백업: deploy/home/backup.sh가 파일을 Object Storage photos/에 한 번씩 올린다.
CREATE TABLE meal_photos (
    meal_date  date        NOT NULL,
    meal_index integer     NOT NULL CHECK (meal_index BETWEEN 1 AND 9),
    n          integer     NOT NULL CHECK (n BETWEEN 0 AND 2),
    -- MEAL_PHOTO_DIR 기준 상대 경로: 2026-11-08/2026-11-08_1_0_<sha256 앞 12자>.jpg
    file       text        NOT NULL UNIQUE,
    bytes      integer     NOT NULL CHECK (bytes > 0),
    sha256     bytea       NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (meal_date, meal_index, n)
);

-- +goose Down
DROP TABLE meal_photos;
