-- +goose Up

-- 건강(핏두잉 이식, 2026-11-08). 사용자 1명이라 user_id가 없다.
-- 날짜(date)는 사용자 달력 날짜(KST)다. 끼니 사진은 서버에 저장하지 않는다. 분석 요청에만 쓰고 버린다.

-- 설정: 한 행짜리 jsonb 문서(핏두잉 앱 설정 모양에서 뺀 필드를 지운 것). 날짜별 목표는 아래 표에 따로 둔다.
CREATE TABLE health_settings (
    id         integer     PRIMARY KEY DEFAULT 1 CHECK (id = 1),
    doc        jsonb       NOT NULL,
    updated_at timestamptz NOT NULL DEFAULT now()
);

-- 날짜별 목표: from_date부터 이 목표를 쓴다. 목표를 바꿔도 지난 날은 그때 목표로 판단한다.
CREATE TABLE health_target_history (
    from_date date    PRIMARY KEY,
    kcal      integer NOT NULL CHECK (kcal >= 0),
    carbs     integer NOT NULL CHECK (carbs >= 0),
    protein   integer NOT NULL CHECK (protein >= 0),
    fat       integer NOT NULL CHECK (fat >= 0)
);

-- 끼니 기록. 같은 날 같은 번호는 덮어쓴다. 1~6은 계획한 끼니, 7~9는 "기타"(간식 등, 마감 없음).
-- kcal·탄단지는 인식한 한 접시 기준이고, 먹은 양(portion)과 나눠 먹음(shared_meal = 절반)은 읽을 때 곱한다.
CREATE TABLE meal_logs (
    id          bigserial        PRIMARY KEY,
    meal_date   date             NOT NULL,
    meal_index  integer          NOT NULL CHECK (meal_index BETWEEN 1 AND 9),
    deadline    text             NOT NULL DEFAULT '',
    barcode     text,
    kcal        integer          NOT NULL CHECK (kcal BETWEEN 0 AND 10000),
    carbs       integer          NOT NULL CHECK (carbs BETWEEN 0 AND 1000),
    protein     integer          NOT NULL CHECK (protein BETWEEN 0 AND 1000),
    fat         integer          NOT NULL CHECK (fat BETWEEN 0 AND 1000),
    confidence  double precision NOT NULL CHECK (confidence BETWEEN 0 AND 1),
    portion     double precision NOT NULL CHECK (portion IN (0.25, 0.5, 1, 1.5, 2)),
    shared_meal boolean          NOT NULL DEFAULT false,
    logged_at   timestamptz      NOT NULL,
    updated_at  timestamptz      NOT NULL DEFAULT now(),
    UNIQUE (meal_date, meal_index)
);

-- 끼니의 음식(표시 순서 position). 음식별 숫자는 그 음식 전체 기준이고, 직접 입력한 음식이면 비어 있다.
-- portion은 그 음식을 먹은 비율(비어 있으면 다 먹음). 끼니 합계(meal_logs)에는 이미 반영돼 있다.
CREATE TABLE meal_items (
    meal_log_id bigint           NOT NULL REFERENCES meal_logs (id) ON DELETE CASCADE,
    position    integer          NOT NULL,
    name        text             NOT NULL,
    amount      text             NOT NULL,
    kcal        integer,
    carbs       integer,
    protein     integer,
    fat         integer,
    portion     double precision CHECK (portion IN (0.25, 0.5, 0.75, 1, 1.5, 2)),
    PRIMARY KEY (meal_log_id, position)
);

-- 헬스: 하루 한 줄. geofence(위치)·photo(사진)는 방문, rest는 휴식권을 쓴 날(방문으로 세지 않음).
CREATE TABLE gym_logs (
    gym_date    date        PRIMARY KEY,
    check_in_at timestamptz,
    verified_by text        NOT NULL CHECK (verified_by IN ('geofence', 'photo', 'rest')),
    updated_at  timestamptz NOT NULL DEFAULT now()
);

-- 휴식권 사용 기록(추가만). 매주 월요일(week_key)에 다시 채워진다. 같은 날에는 한 장만 쓴다.
CREATE TABLE rest_coupon_uses (
    id       bigserial   PRIMARY KEY,
    used_on  date        NOT NULL UNIQUE,
    week_key date        NOT NULL,
    used_at  timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX rest_coupon_uses_week_idx ON rest_coupon_uses (week_key);

-- +goose Down
DROP TABLE rest_coupon_uses;
DROP TABLE gym_logs;
DROP TABLE meal_items;
DROP TABLE meal_logs;
DROP TABLE health_target_history;
DROP TABLE health_settings;
