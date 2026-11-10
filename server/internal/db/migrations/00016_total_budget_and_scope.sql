-- +goose Up

-- 한 달 전체 예산(앱 홈 "이번 달 쓸 수 있는 돈"을 눌러 고친다, 2026-11-10 사용자 요청).
-- 정한 기간부터 다음에 바꿀 때까지 이어진다(그 기간 이하에서 가장 최근 줄). amount가 NULL이면 "분류 예산 합계를 쓴다"로 되돌린 것.
-- 줄이 하나도 없으면 예전처럼 분류 예산 합계다. 분류 예산과 남은 돈 계산(분류별)은 그대로다.
CREATE TABLE period_total_budgets (
    period_start date PRIMARY KEY,
    amount       bigint CHECK (amount IS NULL OR amount >= 0),
    updated_at   timestamptz NOT NULL DEFAULT now()
);

-- 분류 예산 방식(사용자 요청, 2026-11-10): 한도(예산 > 0) / 자유(예산 0, 한 달 예산에서는 깎임) / 예산 없음(in_budget = false,
-- 한 달 예산에서 깎이지 않음). 지금까지 예산 0인 분류는 한 달 예산에서 깎여 왔으므로 기본값 true(= 자유)로 둔다.
-- 폴더를 예산 없음으로 하면 그 아래도 예산 없음으로 본다(api.BuildSummary).
ALTER TABLE categories ADD COLUMN in_budget boolean NOT NULL DEFAULT true;

-- +goose Down
ALTER TABLE categories DROP COLUMN in_budget;
DROP TABLE period_total_budgets;
