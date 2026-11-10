-- +goose Up

-- 몸무게 기록(사용자 요청, 2026-11-10). 하루에 하나(나중에 넣은 값이 이긴다). 저울 사진은 서버에 남기지 않는다(숫자만 읽는다).
-- 몸 사진은 폰에만 둔다(서버로 보내지 않는다) - docs/DECISIONS.md 「몸 기록」
CREATE TABLE body_weights (
    day        date             PRIMARY KEY,
    weight_kg  double precision NOT NULL CHECK (weight_kg >= 20 AND weight_kg <= 400),
    updated_at timestamptz      NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE body_weights;
