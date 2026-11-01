-- +goose Up

-- 알림을 보내는 기기. 토큰 원문은 저장하지 않고 SHA-256 해시만 저장한다.
CREATE TABLE devices (
    id           bigserial PRIMARY KEY,
    name         text        NOT NULL,
    token_hash   bytea       NOT NULL UNIQUE,
    created_at   timestamptz NOT NULL DEFAULT now(),
    last_seen_at timestamptz,
    revoked_at   timestamptz
);

-- 폰이 보낸 알림 원문. 해석 규칙이 바뀌면 재처리할 수 있도록 영구 보관한다.
CREATE TABLE raw_events (
    id             bigserial PRIMARY KEY,
    device_id      bigint      REFERENCES devices (id),
    source_package text        NOT NULL,
    title          text        NOT NULL DEFAULT '',
    text           text        NOT NULL DEFAULT '',
    big_text       text        NOT NULL DEFAULT '',
    posted_at      timestamptz NOT NULL,
    received_at    timestamptz NOT NULL DEFAULT now(),
    -- 패키지·게시시각·본문으로 서버가 계산한 해시. 재전송 멱등 처리용.
    content_hash   text        NOT NULL UNIQUE,
    processed_at   timestamptz,
    parse_error    text
);

CREATE INDEX raw_events_posted_at_idx ON raw_events (posted_at);
CREATE INDEX raw_events_parse_error_idx ON raw_events (id) WHERE parse_error IS NOT NULL;

-- +goose Down
DROP TABLE raw_events;
DROP TABLE devices;
