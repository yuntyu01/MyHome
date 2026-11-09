-- +goose Up

-- 분류를 폴더처럼 묶는다(최대 3단계). 결제는 어느 단계에든 넣을 수 있고, 위 폴더 합계는 아래를 모두 더한 값이다.
-- 깊이·종류·순환 검사는 서버 코드(api.UpdateCategory)가 한다. 특수 분류(code 있음)는 늘 맨 위에 있다.
ALTER TABLE categories ADD COLUMN parent_id bigint REFERENCES categories (id);

-- 사용자가 적는 분류 설명. AI 분류 추천이 가맹점 이름과 함께 참고한다(분류 이름·설명만 보내고 금액·원문은 보내지 않는다).
ALTER TABLE categories ADD COLUMN memo text NOT NULL DEFAULT '';

-- 폴더가 다르면 같은 이름을 쓸 수 있다(예: 폴더마다 '기타').
ALTER TABLE categories DROP CONSTRAINT categories_name_key;
CREATE UNIQUE INDEX categories_parent_name_key ON categories (coalesce(parent_id, 0), name);
CREATE INDEX categories_parent_idx ON categories (parent_id) WHERE parent_id IS NOT NULL;

-- AI에 보낸 분류 목록(경로·설명)의 지문. 목록이 바뀌면 같은 가맹점을 다시 묻는다(suggest.Worker).
ALTER TABLE merchant_suggestions ADD COLUMN catalog text NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE merchant_suggestions DROP COLUMN catalog;
DROP INDEX categories_parent_idx;
DROP INDEX categories_parent_name_key;
ALTER TABLE categories ADD CONSTRAINT categories_name_key UNIQUE (name);
ALTER TABLE categories DROP COLUMN memo;
ALTER TABLE categories DROP COLUMN parent_id;
