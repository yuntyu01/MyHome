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
       s.mixed,
       c.name     AS category_name,
       c.kind     AS category_kind,
       c.archived AS category_archived
FROM merchant_suggestions s
         LEFT JOIN categories c ON c.id = s.category_id
WHERE s.merchant_key = ANY (@keys::text[]);

-- name: UpsertMerchantSuggestion :exec
INSERT INTO merchant_suggestions (merchant_key, category_id, confidence, model, catalog, mixed)
VALUES (@merchant_key, sqlc.narg(category_id), @confidence, @model, @catalog, @mixed)
ON CONFLICT (merchant_key) DO UPDATE
    SET category_id = excluded.category_id,
        confidence  = excluded.confidence,
        model       = excluded.model,
        catalog     = excluded.catalog,
        mixed       = excluded.mixed,
        created_at  = now();

-- name: ListMixedMerchantKeys :many
-- AI가 "산 물건에 따라 분류가 갈린다"고 본 가맹점 키. 파이프라인이 이 가맹점을 위젯에서 고르게 확인 필요로 둔다.
SELECT merchant_key
FROM merchant_suggestions
WHERE mixed;

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

-- name: ListRecentAutoMerchants :many
-- 규칙(기본 규칙 포함)이나 AI로 바로 넣은 최근 지출의 가맹점. AI에 "여러 분류 가게(편의점·마트·쿠팡 등)인지"를 한 번 묻는다
-- (사용자 요청, 2026-11-16 "하드코딩 말고 AI가 검사"). 가맹점 이름만 돌려준다.
SELECT merchant, max(occurred_at)::timestamptz AS last_at
FROM transactions
WHERE status = 'confirmed'
  AND NOT manually_edited
  AND direction = 'expense'
  AND flow = 'out'
  AND raw_event_id IS NOT NULL
  AND merged_into IS NULL
  AND cancels_id IS NULL
  AND btrim(merchant) <> ''
  AND created_at >= @since
GROUP BY merchant
ORDER BY last_at DESC
LIMIT @lim;

-- name: ListRecentAutoConfirmed :many
-- 사람이 손대지 않고 자동으로 확정된 최근 지출(AI가 여러 분류 가게라고 보면 위젯에서 고르게 돌린다).
SELECT id, merchant
FROM transactions
WHERE status = 'confirmed'
  AND NOT manually_edited
  AND direction = 'expense'
  AND flow = 'out'
  AND raw_event_id IS NOT NULL
  AND merged_into IS NULL
  AND cancels_id IS NULL
  AND created_at >= @since
ORDER BY id;

-- name: ReviewAsAmbiguous :execrows
-- 자동 확정된 지출을 위젯에서 고르게(확인 필요, ambiguous) 돌린다. 그 사이 사람이 고쳤으면 건드리지 않는다.
UPDATE transactions
SET status        = 'needs_review',
    review_reason = 'ambiguous',
    category_id   = NULL,
    updated_at    = now()
WHERE id = @id
  AND status = 'confirmed'
  AND NOT manually_edited;
