-- +goose Up

-- 증권사(토스)가 준 주문가능 현금(USD) 기록(사용자 요청, 2026-11-15 "증권 계좌 돈도 투자에서 보이게").
-- 매매 작업(status·trade·reconcile)이 증권사를 부를 때마다 한 줄씩 남기고, 웹·앱은 마지막 값을 보여 준다.
-- 웹은 토스를 부르지 않는다(토큰 1개, 키 분리) — 그래서 값은 마지막 작업 시각 기준이다. 추가 전용.
-- invest_* 이름이라 postgres-roles.sql이 매매 역할에 쓰기, 웹 역할에 읽기를 준다.
CREATE TABLE invest_broker_cash (
    id         bigserial PRIMARY KEY,
    cash_usd   double precision NOT NULL,
    cmd        text             NOT NULL,
    fetched_at timestamptz      NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE invest_broker_cash;
