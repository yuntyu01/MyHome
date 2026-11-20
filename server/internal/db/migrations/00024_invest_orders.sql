-- +goose Up

-- 자동 매매 주문 기록을 표로 (refactor/invest-db, 2026-11-20). 예전에는 invest_state의 orders 문서 하나였다.
-- 뜻은 그대로다: 진행 중인 주문 묶음(book)은 하나뿐이고, trade가 날마다 이어받아 체결을 확인하며,
-- 다 끝나면(체결 반영)·비면(거절·없음)·사람이 치우면(orders --clear-stale) 닫는다. 닫힌 묶음과 주문은 지우지 않는다.
-- orders 문서는 되돌리기용 그림자로 계속 쓰고, 읽을 때 표와 대조한다(다르면 trade·orders가 실패).

-- 주문 묶음 = 한 적용월의 매매 한 차례. 같은 달에 다시 열릴 수 있어(거절로 비었다가 다시 냄) 달이 키가 아니다
CREATE TABLE invest_order_books (
    id         bigserial   PRIMARY KEY,
    ym         text        NOT NULL CHECK (ym ~ '^\d{4}-\d{2}$'),
    stage      text        NOT NULL CHECK (stage IN ('sell', 'buy')),
    style_idx  integer     NOT NULL DEFAULT 0,
    ledger_gap integer,                -- 매도 때 증권사 보유 − 장부(앱 밖 매매 흔적). 감사용
    opened_at  timestamptz NOT NULL DEFAULT now(),
    closed_at  timestamptz,
    closed_by  text        CHECK (closed_by IN ('done', 'empty', 'clear-stale')),
    CHECK ((closed_at IS NULL) = (closed_by IS NULL))
);
-- 열린 묶음은 하나뿐이다
CREATE UNIQUE INDEX invest_order_books_one_open ON invest_order_books ((true)) WHERE closed_at IS NULL;

-- 주문 1건 = 1행. clientOrderId(tq-<뉴욕 주문일>-<종목>-<방향>-<수량>-<유형>)가 키다 — 같은 날 같은 주문을 다시
-- 보내면 같은 키라 같은 행을 다시 쓴다(증권사도 같은 주문으로 본다). 키가 없던 옛 주문은 NULL.
-- state: unknown(보냈지만 접수 여부 모름) → open(접수, 체결 대기) → filled | expired(채워진 만큼만 인정하고 닫음).
-- dropped는 묶음에서 빠진 것이다(명시적 거절, 또는 결과 모름이었는데 증권사에 없음).
CREATE TABLE invest_orders (
    id              bigserial        PRIMARY KEY,
    book_id         bigint           NOT NULL REFERENCES invest_order_books (id),
    seq             integer          NOT NULL,
    client_order_id text             UNIQUE,
    order_id        text             NOT NULL DEFAULT '', -- 증권사 주문번호. 결과 모름이면 ''
    symbol          text             NOT NULL,
    side            text             NOT NULL CHECK (side IN ('buy', 'sell')),
    qty             integer          NOT NULL,            -- 낸 수량. 만료로 닫으면 채워진 수량
    price           double precision NOT NULL,            -- 지정가(시장가 계열이면 밴드 기준값)
    style           text             NOT NULL,
    submitted_on    date             NOT NULL,            -- 뉴욕 날짜
    submitted_at    timestamptz,
    state           text             NOT NULL CHECK (state IN ('unknown', 'open', 'filled', 'expired', 'dropped')),
    fill_price      double precision NOT NULL DEFAULT 0,
    updated_at      timestamptz      NOT NULL DEFAULT now()
);
CREATE INDEX invest_orders_book_idx ON invest_orders (book_id, seq);

-- 상태 전이 기록(추가만 한다)
CREATE TABLE invest_order_events (
    id        bigserial   PRIMARY KEY,
    order_ref bigint      NOT NULL REFERENCES invest_orders (id),
    state     text        NOT NULL,
    qty       integer     NOT NULL,
    note      text        NOT NULL DEFAULT '',
    at        timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX invest_order_events_order_idx ON invest_order_events (order_ref, id);

-- 지금 orders 문서가 있으면 옮긴다(문서는 그대로 둔다 — 그림자)
INSERT INTO invest_order_books (ym, stage, style_idx, ledger_gap)
SELECT doc->>'ym', doc->>'stage', coalesce((doc->>'style_idx')::integer, 0), (doc->>'ledger_gap')::integer
FROM invest_state WHERE key = 'orders';

INSERT INTO invest_orders (book_id, seq, client_order_id, order_id, symbol, side, qty, price, style,
                           submitted_on, submitted_at, state, fill_price)
SELECT b.id, (o.ord - 1)::integer, nullif(o.v->>'client_order_id', ''), coalesce(o.v->>'odno', ''),
       o.v->>'symbol', o.v->>'side', (o.v->>'qty')::integer, (o.v->>'price')::double precision, o.v->>'style',
       (o.v->>'submitted_on')::date, nullif(o.v->>'submitted_at', '')::timestamptz,
       CASE WHEN coalesce((o.v->>'unknown')::boolean, false) THEN 'unknown'
            WHEN coalesce((o.v->>'expired')::boolean, false) THEN 'expired'
            WHEN coalesce((o.v->>'filled')::boolean, false) THEN 'filled'
            ELSE 'open' END,
       coalesce((o.v->>'fill_price')::double precision, 0)
FROM invest_order_books b
JOIN invest_state s ON s.key = 'orders'
CROSS JOIN LATERAL jsonb_array_elements(coalesce(s.doc->'orders', '[]'::jsonb)) WITH ORDINALITY AS o(v, ord)
WHERE b.closed_at IS NULL;

INSERT INTO invest_order_events (order_ref, state, qty, note)
SELECT id, state, qty, '00024: orders 문서에서 옮김' FROM invest_orders;

-- +goose Down
DROP TABLE invest_order_events;
DROP TABLE invest_orders;
DROP TABLE invest_order_books;
