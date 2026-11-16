-- +goose Up
-- AI가 "여러 종류를 파는 곳(편의점·마트·쿠팡 등)이라 산 물건에 따라 분류가 갈린다"고 본 가맹점.
-- 이런 가맹점은 AI 분류로 바로 넣지 않고 위젯에서 고르게 한다(사용자 요청, 2026-11-16).
ALTER TABLE merchant_suggestions ADD COLUMN mixed boolean NOT NULL DEFAULT false;

-- +goose Down
ALTER TABLE merchant_suggestions DROP COLUMN mixed;
