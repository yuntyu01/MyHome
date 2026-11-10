-- name: ListSuggestionMerchants :many
-- AI 분류 추천 대상: 규칙이 없어서(no_rule) 확인 필요로 온 나간 지출의 가맹점 이름. 결제대행사 이름뿐인 건(payment_gateway)·송금(transfer)·
-- 입금 등 다른 사유는 AI도 알 수 없어서 넣지 않는다. 가맹점 이름만 돌려준다(금액·시각·원문은 AI 쪽으로 가지 않는다).
SELECT merchant, max(occurred_at)::timestamptz AS last_at
FROM transactions
WHERE status = 'needs_review'
  AND review_reason = 'no_rule'
  AND direction = 'expense'
  AND flow = 'out'
  AND cancels_id IS NULL
  AND btrim(merchant) <> ''
GROUP BY merchant
ORDER BY last_at DESC
LIMIT @lim;

-- name: ListMerchantSuggestions :many
-- 가맹점 키로 저장된 추천. 분류가 보관됐는지·지출 분류인지도 같이 본다.
SELECT s.merchant_key,
       s.category_id,
       s.confidence,
       s.model,
       s.catalog,
       c.name     AS category_name,
       c.kind     AS category_kind,
       c.archived AS category_archived
FROM merchant_suggestions s
         LEFT JOIN categories c ON c.id = s.category_id
WHERE s.merchant_key = ANY (@keys::text[]);

-- name: UpsertMerchantSuggestion :exec
INSERT INTO merchant_suggestions (merchant_key, category_id, confidence, model, catalog)
VALUES (@merchant_key, sqlc.narg(category_id), @confidence, @model, @catalog)
ON CONFLICT (merchant_key) DO UPDATE
    SET category_id = excluded.category_id,
        confidence  = excluded.confidence,
        model       = excluded.model,
        catalog     = excluded.catalog,
        created_at  = now();

-- name: DeleteMerchantSuggestion :exec
DELETE
FROM merchant_suggestions
WHERE merchant_key = $1;

-- name: ListAutoFileTargets :many
-- AI가 확신하는 분류로 바로 넣을 후보 거래(ListSuggestionMerchants와 같은 조건, 거래마다).
SELECT id, merchant
FROM transactions
WHERE status = 'needs_review'
  AND review_reason = 'no_rule'
  AND direction = 'expense'
  AND flow = 'out'
  AND cancels_id IS NULL
  AND btrim(merchant) <> ''
ORDER BY id
LIMIT @lim;

-- name: FileWithAICategory :execrows
-- AI 분류로 확정한다. 그 사이에 사용자가 분류했거나 다른 사유로 바뀌었으면(확인 필요·no_rule이 아니면) 건드리지 않는다.
-- 학습 규칙은 만들지 않는다: 같은 가맹점은 저장된 AI 답(merchant_suggestions)으로 다시 넣고, 사용자가 고치면 그때 규칙이 생긴다.
UPDATE transactions
SET category_id   = @category_id,
    status        = 'confirmed',
    review_reason = NULL,
    updated_at    = now()
WHERE id = @id
  AND status = 'needs_review'
  AND review_reason = 'no_rule';
