-- +goose Up

-- 전환 달 적립금 추가 매수 (사용자 요청, 2026-11-20 "전환 달에도 매도대금 매수 후 늦게 들어온 적립금은 같은 달 안에
-- 추가 매수"). 전환 매수를 낼 때 아직 들어오지 않은 적립금(원)을 주문 묶음에 남긴다. 체결을 반영하면 position 문서의
-- top_up_krw가 되고, 들어오면 같은 달 매매 창 안에 더 산다. 0이면 모자란 적립금이 없었다.
ALTER TABLE invest_order_books ADD COLUMN top_up_krw double precision NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE invest_order_books DROP COLUMN top_up_krw;
