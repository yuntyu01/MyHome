-- name: ListPicks :many
-- 위젯에서 한 번 눌러 고를 확인 필요 지출(최근 것부터). 애매한 가게·처음 보는 가게·결제대행사 이름·이름 없음·빈 돈만:
-- 송금·입금·취소·외화는 고를 게 분류만이 아니라서 앱에서 처리한다.
SELECT *
FROM transactions
WHERE status = 'needs_review'
  AND flow = 'out'
  AND cancels_id IS NULL
  AND merged_into IS NULL
  AND review_reason = ANY (@reasons::text[])
ORDER BY occurred_at DESC, id DESC
LIMIT @lim;

-- name: CountPicks :one
SELECT count(*)
FROM transactions
WHERE status = 'needs_review'
  AND flow = 'out'
  AND cancels_id IS NULL
  AND merged_into IS NULL
  AND review_reason = ANY (@reasons::text[]);

-- name: CategoryUsage :many
-- 최근에 확정한 지출의 분류별 건수(고를 동그라미 순서용).
SELECT category_id::bigint AS category_id, count(*)::bigint AS n
FROM transactions
WHERE status = 'confirmed'
  AND direction = 'expense'
  AND category_id IS NOT NULL
  AND occurred_at >= @from_at
GROUP BY category_id;

-- name: LastCategoriesForMerchant :many
-- 같은 가맹점을 전에 확정한 분류(최근 것부터). 위젯 동그라미의 첫 칸과 폴더를 정한다.
SELECT category_id::bigint AS category_id
FROM transactions
WHERE status = 'confirmed'
  AND direction = 'expense'
  AND category_id IS NOT NULL
  AND merchant = @merchant
ORDER BY occurred_at DESC
LIMIT 5;
