-- +goose Up

-- AI 분류 추천 캐시 (2026-11-08). 규칙으로 분류하지 못한(no_rule) 지출의 가맹점 이름을 Gemini에 물어 본 답을 가맹점마다 하나 둔다.
-- 가맹점 하나에 AI 호출은 한 번뿐이다(이 행을 지우면 다시 묻는다). 추천은 확인 필요 화면에서 한 번 눌러야 확정되고, 그때 학습 규칙이 생긴다.
CREATE TABLE merchant_suggestions (
    -- classify.Normalize(가맹점 이름). 결제대행사 이름(review_merchant_patterns)은 떼어낸 뒤의 이름이다 - suggest.Target
    merchant_key text PRIMARY KEY CHECK (merchant_key <> ''),
    -- NULL = AI도 모른다("모름"). 분류를 지우면 행도 지워서 다시 물을 수 있게 한다(분류는 보통 지우지 않고 보관한다).
    category_id  bigint REFERENCES categories (id) ON DELETE CASCADE,
    -- 0~1. 낮은 값도 저장하고, 보여줄지는 서버가 정한다(suggest.MinConfidence).
    confidence   double precision NOT NULL CHECK (confidence >= 0 AND confidence <= 1),
    model        text        NOT NULL,
    created_at   timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX merchant_suggestions_category_idx ON merchant_suggestions (category_id) WHERE category_id IS NOT NULL;

-- +goose Down
DROP TABLE merchant_suggestions;
