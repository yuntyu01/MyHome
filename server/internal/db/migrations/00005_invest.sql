-- +goose Up

-- 자동매매(TQ 이식). TQ는 state/*.json + data/master.parquet를 git에 커밋해 상태와 감사 추적을
-- 함께 남겼다. 여기서는 같은 내용을 테이블로 두고, git log가 하던 일은 invest_state_history가 한다.
--
-- 금액·가격은 double precision이다. TQ 산술(float64)과 비트 단위로 같게 해서 이식 대조가
-- 성립하게 하려고 — 원 단위 bigint 규칙은 가계부 거래(transactions)에만 적용된다 (DECISIONS.md).

-- 일별 종가 마스터 (1985-10~). 신호는 spx·ndx만 쓴다.
-- 과거 행의 가격은 고치지 않는다 — 장중 값이 한 번 굳으면 앵커 대조가 영영 멈춘다(2026-08-13 사고).
CREATE TABLE invest_prices (
    day        date PRIMARY KEY,
    spx        double precision NOT NULL,
    ndx        double precision NOT NULL,
    qqq        double precision,
    qqqm       double precision,
    qld        double precision,
    tqqq       double precision,
    -- 환율은 컬럼을 나눈다. 둘은 같은 값이 아니다(고시환율 vs 시장 종가, 오차 중앙 0.20%)
    usdkrw     double precision, -- 하나은행 매매기준율. 백테스트가 쓴 값, 2026-07-31에서 동결
    usdkrw_mkt double precision, -- 시장 환율(KRW=X). 평가 전용
    created_at timestamptz NOT NULL DEFAULT now()
);

-- 단일 문서 상태. position=계좌와 그달 판정, orders=진행 중 자동 매매 주문, pending=냉각 중 이탈 요청
CREATE TABLE invest_state (
    key        text PRIMARY KEY CHECK (key IN ('position', 'orders', 'pending')),
    doc        jsonb       NOT NULL,
    updated_at timestamptz NOT NULL DEFAULT now()
);

-- 상태 변경 이력 — 추가만 한다. doc이 NULL이면 그 상태를 지운 것이다
CREATE TABLE invest_state_history (
    id         bigserial PRIMARY KEY,
    key        text        NOT NULL,
    cmd        text        NOT NULL,
    doc        jsonb,
    created_at timestamptz NOT NULL DEFAULT now()
);

-- 원금 투입 내역 (추가 전용). 총액(position.contributed_krw)과 합을 대조한다.
-- amend는 그 달 기록을 덮는다 — 지우지 않고 superseded_at으로 표시한다
CREATE TABLE invest_ledger (
    id            bigserial PRIMARY KEY,
    on_day        date             NOT NULL,
    ym            text             NOT NULL,
    amount_krw    double precision NOT NULL,
    source        text             NOT NULL CHECK (source IN ('trade', 'execute', 'adjust', 'amend')),
    symbol        text,
    shares        integer,
    note          text             NOT NULL DEFAULT '',
    superseded_at timestamptz,
    created_at    timestamptz      NOT NULL DEFAULT now()
);

CREATE INDEX invest_ledger_ym_idx ON invest_ledger (ym) WHERE superseded_at IS NULL;

-- 규칙 이탈 (추가 전용). kind='missed'는 매수 없이 지난 달의 자동 기록이다
CREATE TABLE invest_deviations (
    id            bigserial PRIMARY KEY,
    kind          text,
    ym            text        NOT NULL,
    occurred_on   date        NOT NULL,
    signal_target text        NOT NULL,
    actual_target text,
    reason        text        NOT NULL DEFAULT '',
    locked_until  date        NOT NULL,
    resurface_on  date        NOT NULL,
    created_at    timestamptz NOT NULL DEFAULT now()
);

-- 적립액 계획. 실제 투입이 아니라 계획이다(실제는 invest_ledger)
CREATE TABLE invest_contributions (
    effective_from text PRIMARY KEY CHECK (effective_from ~ '^\d{4}-\d{2}$'),
    amount         bigint      NOT NULL CHECK (amount > 0),
    reason         text        NOT NULL,
    recorded_on    date        NOT NULL,
    created_at     timestamptz NOT NULL DEFAULT now()
);

-- 앱 밖 매수 (추가 전용). 증권사는 원화로 얼마를 넣었는지 모르므로 사람이 알려준 값이다
CREATE TABLE invest_adjustments (
    id         bigserial PRIMARY KEY,
    on_day     date             NOT NULL,
    symbol     text             NOT NULL,
    shares     integer          NOT NULL CHECK (shares > 0),
    paid_krw   double precision NOT NULL CHECK (paid_krw > 0),
    reason     text             NOT NULL,
    created_at timestamptz      NOT NULL DEFAULT now()
);

-- 명령 실행 기록 — 화면의 '마지막 실행', 실패 알림 본문, deadman이 읽는다
CREATE TABLE invest_runs (
    id          bigserial PRIMARY KEY,
    cmd         text        NOT NULL,
    args        text        NOT NULL DEFAULT '',
    started_at  timestamptz NOT NULL DEFAULT now(),
    finished_at timestamptz,
    exit_code   integer,
    output      text        NOT NULL DEFAULT ''
);

CREATE INDEX invest_runs_cmd_idx ON invest_runs (cmd, started_at DESC);

-- +goose Down
DROP TABLE invest_runs;
DROP TABLE invest_adjustments;
DROP TABLE invest_contributions;
DROP TABLE invest_deviations;
DROP TABLE invest_ledger;
DROP TABLE invest_state_history;
DROP TABLE invest_state;
DROP TABLE invest_prices;
