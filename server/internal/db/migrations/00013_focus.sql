-- +goose Up

-- 집중(공부 타이머, 열품타 비슷한 것, 2026-11-09). 사용자 1명이라 user_id가 없다.
-- 앱이 먼저 자기 DB(Room)에 저장하고 서버로 올린다. 서버는 백업(재설치 복원)이자 나중에 웹 통계용이다.
-- id는 앱이 만든 UUID다. 같은 id를 다시 PUT하면 updated_at이 같거나 더 새로울 때만 덮어쓴다(last-write-wins).
-- 지우기는 deleted 표시로 한다(복원할 때 지운 것도 알 수 있게). 길이·형식 검사는 서버 코드(focus 패키지)가 먼저 한다.

-- 과목. 숨긴 과목(archived)도 지난 기록 때문에 남겨 둔다.
CREATE TABLE focus_subjects (
    id         text        PRIMARY KEY CHECK (id ~ '^[A-Za-z0-9-]{1,64}$'),
    name       text        NOT NULL CHECK (char_length(name) BETWEEN 1 AND 40),
    color      text        NOT NULL CHECK (color ~ '^#[0-9A-Fa-f]{6}$'),
    sort       integer     NOT NULL DEFAULT 0,
    archived   boolean     NOT NULL DEFAULT false,
    updated_at timestamptz NOT NULL
);

-- 공부 세션 하나(타이머를 켠 시각 ~ 끈 시각). 한 세션은 24시간까지다.
-- 하루는 05:00 KST에 바뀐다. 날짜 경계를 넘는 세션은 집계할 때 나눈다(focus.SplitByDay).
CREATE TABLE focus_sessions (
    id         text        PRIMARY KEY CHECK (id ~ '^[A-Za-z0-9-]{1,64}$'),
    subject_id text        NOT NULL REFERENCES focus_subjects (id),
    started_at timestamptz NOT NULL,
    ended_at   timestamptz NOT NULL,
    deleted    boolean     NOT NULL DEFAULT false,
    updated_at timestamptz NOT NULL,
    CHECK (ended_at > started_at),
    CHECK (ended_at - started_at <= interval '24 hours')
);

CREATE INDEX focus_sessions_started_idx ON focus_sessions (started_at);

-- 장소에 머문 기록(도서관·독서실 등). 장소 이름만 받는다: 좌표는 폰 밖으로 나가지 않는다.
-- 앱은 장소를 떠난 뒤에만 보낸다(exited_at 필수). 한 기록은 24시간까지다.
CREATE TABLE focus_stays (
    id         text        PRIMARY KEY CHECK (id ~ '^[A-Za-z0-9-]{1,64}$'),
    place      text        NOT NULL CHECK (char_length(place) BETWEEN 1 AND 40),
    entered_at timestamptz NOT NULL,
    exited_at  timestamptz NOT NULL,
    deleted    boolean     NOT NULL DEFAULT false,
    updated_at timestamptz NOT NULL,
    CHECK (exited_at > entered_at),
    CHECK (exited_at - entered_at <= interval '24 hours')
);

CREATE INDEX focus_stays_entered_idx ON focus_stays (entered_at);

-- +goose Down
DROP TABLE focus_stays;
DROP TABLE focus_sessions;
DROP TABLE focus_subjects;
