-- +goose Up

-- "이름으로 시작" 규칙. 정확히 같음만 쓰던 CU·KT·SKT가 "CU 선릉점" 같은 지점명을 놓쳐서 추가한다.
ALTER TABLE merchant_rules DROP CONSTRAINT merchant_rules_match_type_check;
ALTER TABLE merchant_rules ADD CONSTRAINT merchant_rules_match_type_check
    CHECK (match_type IN ('contains', 'exact', 'regex', 'prefix'));

UPDATE merchant_rules SET match_type = 'prefix'
WHERE created_from = 'seed' AND match_type = 'exact' AND pattern IN ('CU', 'KT', 'SKT');

-- +goose Down
UPDATE merchant_rules SET match_type = 'exact'
WHERE created_from = 'seed' AND match_type = 'prefix' AND pattern IN ('CU', 'KT', 'SKT');
UPDATE merchant_rules SET match_type = 'contains' WHERE match_type = 'prefix';

ALTER TABLE merchant_rules DROP CONSTRAINT merchant_rules_match_type_check;
ALTER TABLE merchant_rules ADD CONSTRAINT merchant_rules_match_type_check
    CHECK (match_type IN ('contains', 'exact', 'regex'));
