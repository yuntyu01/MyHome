-- +goose Up

-- 메가커피는 카드 승인·삼성페이 알림에 정식 이름 "메가엠지씨커피"로 찍힌다(seed는 "메가커피"·"메가MGC"뿐이라 확인 필요로 갔다).
-- 이미 같은 규칙이 있으면(사용자가 학습시킨 것 포함) 건드리지 않는다.
INSERT INTO merchant_rules (pattern, match_type, category_id, flow, priority, created_from)
SELECT '메가엠지씨', 'contains', c.id, 'out', 10, 'seed'
FROM categories c
WHERE c.name = '카페·간식'
  AND NOT EXISTS (SELECT 1 FROM merchant_rules r WHERE r.pattern = '메가엠지씨' AND r.match_type = 'contains' AND coalesce(r.flow, '') = 'out');

-- +goose Down
DELETE FROM merchant_rules WHERE pattern = '메가엠지씨' AND match_type = 'contains' AND created_from = 'seed';
