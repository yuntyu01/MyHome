-- +goose Up

-- 고정으로 빠지는 돈(사용자 요청, 2026-11-10 "주식 같은 것도 고정 -값"): 매달 이번 달 쓸 수 있는 돈에서 미리 뺀다.
-- 지출로 잡히지 않는 돈(주식 적립, 적금 등)에 쓴다. 카드로 내서 지출로 잡히는 돈을 여기 또 넣으면 두 번 빠진다.
-- 지우기는 보관(archived)으로 한다(데이터를 지우지 않는다). 목록은 기간과 상관없이 지금 값이 모든 달에 적용된다.
CREATE TABLE fixed_deductions (
    id         bigserial PRIMARY KEY,
    name       text        NOT NULL CHECK (length(name) BETWEEN 1 AND 40),
    amount     bigint      NOT NULL CHECK (amount > 0 AND amount <= 100000000000),
    sort       integer     NOT NULL DEFAULT 0,
    archived   boolean     NOT NULL DEFAULT false,
    created_at timestamptz NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE fixed_deductions;
