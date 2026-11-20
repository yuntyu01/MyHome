-- +goose Up

-- 이번 달 자동 매수 단계(입금 → 환전 → 주문 → 체결)를 투자 화면에 보이려는 기록(사용자 요청, 2026-11-20
-- "각 단계를 GitHub Actions처럼 체크"). 매매 작업이 증권사에서 본 사실을 그대로 남기고, 웹·앱은 읽기만 한다.
-- 매매 판단에 쓰는 것은 입금(deposit)뿐이다(입금일 게이트). 같은 사건은 (step, key)로 한 번만 남는다. 추가 전용.
CREATE TABLE invest_flow (
    id          bigserial   PRIMARY KEY,
    ym          text        NOT NULL,
    step        text        NOT NULL CHECK (step IN ('deposit', 'exchange', 'order', 'fill')),
    key         text        NOT NULL,
    at          timestamptz NOT NULL,
    data        jsonb       NOT NULL DEFAULT '{}'::jsonb,
    recorded_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (step, key)
);
CREATE INDEX invest_flow_ym_idx ON invest_flow (ym, at);

-- 입금일(한국 날짜, 1~20). 앱 설정에서 바꾸므로 이 표만 웹 역할도 쓸 수 있다(postgres-roles.sql).
-- 비어 있으면 예전처럼 적용월 첫 거래일에 산다. 한 줄짜리 표다.
CREATE TABLE invest_prefs (
    id          integer     PRIMARY KEY DEFAULT 1 CHECK (id = 1),
    deposit_day integer     CHECK (deposit_day BETWEEN 1 AND 20),
    updated_at  timestamptz NOT NULL DEFAULT now()
);

-- 원화 주문가능 금액도 남긴다(입금 확인·증권 총액 표시). 예전 줄은 비어 있다.
ALTER TABLE invest_broker_cash ADD COLUMN cash_krw double precision;

-- +goose Down
ALTER TABLE invest_broker_cash DROP COLUMN cash_krw;
DROP TABLE invest_prefs;
DROP TABLE invest_flow;
