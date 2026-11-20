-- +goose Up

-- 체결 1건 = 1행 (refactor/invest-db, 2026-11-20). 보유 종목·수량·취득원가·연도별 실현손익은 이 표를 처음부터
-- 다시 쌓아 얻는다(invest.Replay). position 문서의 같은 칸은 그림자로 남아 매 명령이 비트 단위로 대조한다.
-- 현금(cash_usd)·납부 세금(tax_paid_krw)은 체결로 정해지지 않아 문서가 정본이다.
--
-- krw는 계좌 산술이 실제로 더한 원화 값 그대로다(double precision, 비트 보존):
--   buy  = 취득원가 증가분((수수료 포함 체결액 USD) × 환율), adjust(앱 밖 매수)는 사람이 알려 준 지불액
--   sell = 매도대금(원, 수수료·SEC 수수료 뺌) — 손익 = krw − 그때 취득원가
--   opening = 여는 잔고의 취득원가(종목·수량을 그대로 놓고 실현손익을 비운다), realized = 그 잔고의 연도별 실현손익
-- price_usd·fx·fee_usd는 참고값이다(다시 쌓을 때 쓰지 않는다). 실현손익 연도는 day의 연도다.
-- 정정(amend)은 그 달 체결 이후 줄을 지우지 않고 superseded_at으로 덮는다(invest_ledger와 같은 방식).
CREATE TABLE invest_fills (
    id            bigserial        PRIMARY KEY,
    day           date             NOT NULL,
    ym            text             NOT NULL CHECK (ym ~ '^\d{4}-\d{2}$'),
    symbol        text             NOT NULL DEFAULT '',
    side          text             NOT NULL CHECK (side IN ('buy', 'sell', 'opening', 'realized')),
    shares        integer          NOT NULL CHECK (shares >= 0),
    price_usd     double precision NOT NULL DEFAULT 0,
    fx            double precision NOT NULL DEFAULT 0,
    fee_usd       double precision NOT NULL DEFAULT 0,
    krw           double precision NOT NULL,
    source        text             NOT NULL CHECK (source IN ('opening', 'trade', 'execute', 'amend', 'adjust')),
    superseded_at timestamptz,
    created_at    timestamptz      NOT NULL DEFAULT now()
);
CREATE INDEX invest_fills_live_idx ON invest_fills (id) WHERE superseded_at IS NULL;
CREATE INDEX invest_fills_ym_idx ON invest_fills (ym) WHERE superseded_at IS NULL;

-- 지금 계좌를 여는 잔고로 놓는다(backfill). 그 앞의 매매는 원금 내역(invest_ledger)과 문서 이력에 그대로 있다 —
-- TQ 시절 체결은 단가·환율이 남아 있지 않아 줄마다 되살릴 수 없다. 한 문장으로: 지금부터 체결이 쌓인다.
INSERT INTO invest_fills (day, ym, symbol, side, shares, krw, source)
SELECT current_date, doc->>'apply_ym', coalesce(doc->'holdings'->>'symbol', ''), 'opening',
       coalesce((doc->'holdings'->>'shares')::integer, 0), coalesce((doc->'holdings'->>'cost_krw')::double precision, 0), 'opening'
FROM invest_state WHERE key = 'position';

INSERT INTO invest_fills (day, ym, side, shares, krw, source)
SELECT make_date(g.key::integer, 1, 1), g.key || '-01', 'realized', 0, g.value::double precision, 'opening'
FROM invest_state s, jsonb_each_text(coalesce(s.doc->'holdings'->'realized_gain_krw', '{}'::jsonb)) AS g(key, value)
WHERE s.key = 'position'
ORDER BY g.key::integer;

-- +goose Down
DROP TABLE invest_fills;
