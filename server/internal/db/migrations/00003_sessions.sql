-- +goose Up

-- 웹 로그인 세션. 쿠키 값 원문은 저장하지 않고 SHA-256 해시만 저장한다.
CREATE TABLE sessions (
    id_hash      bytea PRIMARY KEY,
    kakao_id     bigint      NOT NULL,
    csrf_token   text        NOT NULL,
    created_at   timestamptz NOT NULL DEFAULT now(),
    expires_at   timestamptz NOT NULL,
    last_seen_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX sessions_expires_idx ON sessions (expires_at);

-- 어떤 카카오 계정으로 발급한 기기 토큰인지(CLI 발급은 NULL)
ALTER TABLE devices ADD COLUMN kakao_id bigint;

-- +goose Down
ALTER TABLE devices DROP COLUMN kakao_id;
DROP TABLE sessions;
