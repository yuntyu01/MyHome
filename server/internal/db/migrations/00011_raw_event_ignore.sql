-- +goose Up

-- 결제가 아닌 알림(광고·혜택, 증권, 안내)은 거래를 만들지 않고 무시 사유만 남긴다. 해석 실패(parse_error)와 따로 센다.
ALTER TABLE raw_events ADD COLUMN ignore_reason text;

-- 같은 앱이 같은 알림을 다시 올렸는지 찾을 때(패키지 + 게시 시각 창)
CREATE INDEX raw_events_package_posted_idx ON raw_events (source_package, posted_at);

-- 확인이 필요한 원문(해석 실패, 처리 안 됨) 개수를 셀 때
CREATE INDEX raw_events_attention_idx ON raw_events (received_at) WHERE parse_error IS NOT NULL OR processed_at IS NULL;

-- +goose Down
DROP INDEX raw_events_attention_idx;
DROP INDEX raw_events_package_posted_idx;
ALTER TABLE raw_events DROP COLUMN ignore_reason;
