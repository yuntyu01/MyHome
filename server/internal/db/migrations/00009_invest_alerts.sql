-- +goose Up

-- 자동매매 실패 알림 (2026-11-08). 메일 대신 앱 알림으로 받는다.
-- systemd가 실패한 invest 유닛의 OnFailure=로 `myhome invest alert --unit <유닛>`을 부르면 매매 역할이 한 줄을 쓰고,
-- 앱은 기기 토큰으로 GET /v1/invest/alerts?after=<id>를 읽어 새 줄마다 폰 알림을 띄운다. 웹 역할은 읽기만 한다(invest_ 접두사).
-- VM이 통째로 죽으면 이 줄도 못 쓴다. 그건 외부 하트비트(INVEST_HEARTBEAT_URL)가 잡는다.
CREATE TABLE invest_alerts (
    id         bigserial PRIMARY KEY,
    created_at timestamptz NOT NULL DEFAULT now(),
    -- 실패한 systemd 유닛 이름(myhome-invest-trade.service 등)과 거기서 꺼낸 명령(trade 등)
    unit       text        NOT NULL DEFAULT '',
    cmd        text        NOT NULL DEFAULT '',
    -- 실행 기록(invest_runs)에서 찾은 실패 실행. DB에 닿기 전에 죽었으면 NULL
    run_id     bigint      REFERENCES invest_runs (id) ON DELETE SET NULL,
    exit_code  integer,
    title      text        NOT NULL,
    body       text        NOT NULL
);

CREATE INDEX invest_alerts_created_idx ON invest_alerts (created_at);

-- +goose Down
DROP TABLE invest_alerts;
