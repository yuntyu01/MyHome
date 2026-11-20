-- +goose Up

-- 매일 기록(사용자 요청, 2026-11-20 "매일 단위로 기록하고 판단은 기존처럼 달 단위로").
-- 거래일마다 그날 종가로 본 신호와 계좌 원금·금액을 남긴다. 가격 갱신(refresh)이 새 거래일을 받을 때 쓰고,
-- 웹·앱은 읽기만 한다. 판정·주문은 이 표를 읽지 않는다. 추가 전용(그날 본 대로 남기고 나중에 고치지 않는다).
CREATE TABLE invest_daily (
    day          date             PRIMARY KEY,
    spx          double precision NOT NULL,
    sma185       double precision NOT NULL,
    vol          double precision NOT NULL,
    c1           boolean          NOT NULL,
    c2           boolean          NOT NULL,
    signal       text             NOT NULL, -- 그날 종가로 판정했다면 살 종목(TQQQ·QQQM)
    -- 계좌(그날 장부로 본 값, 세전). 밀린 날을 한꺼번에 받으면 그날 보유를 몰라 마지막 날만 채운다
    symbol       text,
    shares       integer,
    close_usd    double precision,
    fx           double precision,
    stock_krw    bigint,
    cash_krw     bigint,
    invested_krw bigint,
    gain_krw     bigint,
    recorded_at  timestamptz      NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE invest_daily;
