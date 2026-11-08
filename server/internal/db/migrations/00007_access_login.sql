-- +goose Up

-- 로그인을 카카오에서 Cloudflare Access(구글)로 바꾼다 (2026-11-08).

-- 웹 세션은 이메일에 묶는다. 예전 카카오 세션은 지운다(로그인 상태일 뿐 가계부 데이터가 아니다. 다시 열면 새로 만들어진다).
DELETE FROM sessions;
ALTER TABLE sessions DROP COLUMN kakao_id;
ALTER TABLE sessions ADD COLUMN email text NOT NULL;

-- 기기 토큰을 발급한 계정(앱 연결). 예전 카카오로 발급한 기기는 kakao_id를 기록으로 남기고 토큰도 그대로 쓴다.
-- CLI로 발급한 기기는 둘 다 NULL.
ALTER TABLE devices ADD COLUMN email text;

-- 앱 연결용 1회용 코드(PKCE S256). 코드 원문은 저장하지 않고 SHA-256 해시만 둔다.
CREATE TABLE app_link_codes (
    code_hash  bytea       PRIMARY KEY,
    challenge  text        NOT NULL, -- base64url(sha256(verifier)), 43자
    email      text        NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz NOT NULL,
    used_at    timestamptz
);

CREATE INDEX app_link_codes_expires_idx ON app_link_codes (expires_at);

-- +goose Down
DROP TABLE app_link_codes;
ALTER TABLE devices DROP COLUMN email;
DELETE FROM sessions;
ALTER TABLE sessions DROP COLUMN email;
ALTER TABLE sessions ADD COLUMN kakao_id bigint NOT NULL;
