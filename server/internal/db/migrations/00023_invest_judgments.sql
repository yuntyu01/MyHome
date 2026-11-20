-- +goose Up

-- 판정 1회 = 1행 (refactor/invest-db, 2026-11-20). 예전에는 position 문서의 판정 칸이 매달 덮였고
-- 지난 판정은 invest_state_history 문서 속에만 있었다. judge가 여기에 남기고, position 문서의 판정 칸과 대조한다.
-- 추가만 한다(고치거나 지우지 않는다). 같은 달을 다시 판정하면 같은 값이어야 하고 새 행은 생기지 않는다.
--
-- kind: month_end = 달 마지막 거래일 판정(매매가 따르는 것). mid_month는 자리만 있다(쓰는 코드 없음).
-- target은 역할 라벨(TQQQ = 위험 선호, QQQ = 위험 회피)이다 — 실제로 사는 종목은 invest.Ticker가 정한다.
-- 숫자는 double precision이다. 문서의 JSON 숫자를 그대로 읽어 비트 단위로 같다.
CREATE TABLE invest_judgments (
    ym         text             NOT NULL CHECK (ym ~ '^\d{4}-\d{2}$'),
    kind       text             NOT NULL CHECK (kind IN ('month_end', 'mid_month')),
    day        date             NOT NULL,
    spx        double precision NOT NULL,
    sma185     double precision NOT NULL,
    vol        double precision NOT NULL,
    c1         boolean          NOT NULL,
    c2         boolean          NOT NULL,
    target     text             NOT NULL,
    created_at timestamptz      NOT NULL DEFAULT now(),
    PRIMARY KEY (ym, kind)
);

-- 지난 판정을 position 변경 이력에서 채운다. 판정월마다 그 판정이 처음 보인 문서(판정하거나 옮겨 온 때)를 쓴다.
-- 판정 근거가 빈 문서(판정 전)는 건너뛴다.
INSERT INTO invest_judgments (ym, kind, day, spx, sma185, vol, c1, c2, target, created_at)
SELECT DISTINCT ON (doc->>'ym')
       doc->>'ym', 'month_end', (doc->>'last_run')::date,
       (doc->'signal'->>'spx')::double precision, (doc->'signal'->>'sma185')::double precision,
       (doc->'signal'->>'vol')::double precision, (doc->'signal'->>'c1')::boolean, (doc->'signal'->>'c2')::boolean,
       doc->'signal'->>'target', created_at
FROM invest_state_history
WHERE key = 'position' AND doc IS NOT NULL AND coalesce(doc->'signal'->>'target', '') <> ''
ORDER BY doc->>'ym', id;

-- +goose Down
DROP TABLE invest_judgments;
