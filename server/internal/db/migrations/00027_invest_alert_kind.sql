-- +goose Up

-- 매수 완료 알림 (사용자 요청, 2026-11-20 "그 달 매수가 모두 체결되면 1회만"). 실패 알림(fail)과 같은 표에 kind로 나눈다.
-- 앱은 done을 조용한 채널로 띄운다. 본문은 종목·수량·이번 달 원금만이다(수익률·평가액은 싣지 않는다).
-- 같은 달 완료 알림은 하나뿐이다(ym 부분 유일 색인) — 매매 작업이 다시 돌아도 두 번 가지 않는다.
ALTER TABLE invest_alerts ADD COLUMN kind text NOT NULL DEFAULT 'fail' CHECK (kind IN ('fail', 'done'));
ALTER TABLE invest_alerts ADD COLUMN ym text CHECK (ym ~ '^\d{4}-\d{2}$');
CREATE UNIQUE INDEX invest_alerts_done_once ON invest_alerts (ym) WHERE kind = 'done';

-- +goose Down
DROP INDEX invest_alerts_done_once;
ALTER TABLE invest_alerts DROP COLUMN ym;
ALTER TABLE invest_alerts DROP COLUMN kind;
