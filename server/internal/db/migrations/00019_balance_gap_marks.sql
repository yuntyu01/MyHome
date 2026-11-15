-- +goose Up

-- 잔액으로 찾은 빈 돈 처리 기록(사용자 요청, 2026-11-15 "토스뱅크 잔액 기준으로 빈 값은 어디서 놓친 것, 채워 가야").
-- 알림에 찍힌 잔액을 시간순으로 이어서, 앞 잔액 ± 이번 거래가 이번 잔액과 다르면 그 사이 놓친 돈이 있다(빈 돈).
-- 빈 돈은 계산으로 찾고, 사용자가 채웠거나(filled: 확인 필요 거래를 만듦) 무시한 것(ignored)만 여기 남긴다.
-- 빈 돈은 그 뒤 거래(to_transaction_id)로 가리킨다. 금액(gap)이 바뀌면(재처리·금액 수정) 다시 빈 돈으로 보인다.
CREATE TABLE balance_gap_marks (
    to_transaction_id     bigint PRIMARY KEY REFERENCES transactions (id) ON DELETE CASCADE,
    gap                   bigint      NOT NULL,
    resolution            text        NOT NULL CHECK (resolution IN ('filled', 'ignored')),
    filled_transaction_id bigint REFERENCES transactions (id) ON DELETE SET NULL,
    created_at            timestamptz NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE balance_gap_marks;
