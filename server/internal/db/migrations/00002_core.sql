-- +goose Up

-- 분류. 특수 분류는 code로 식별한다(이름은 사용자가 바꿔도 된다).
CREATE TABLE categories (
    id         bigserial PRIMARY KEY,
    name       text        NOT NULL UNIQUE,
    kind       text        NOT NULL CHECK (kind IN ('expense', 'income', 'special')),
    code       text UNIQUE CHECK (code IN ('income', 'settlement', 'own_transfer', 'investment')),
    sort       int         NOT NULL DEFAULT 0,
    archived   boolean     NOT NULL DEFAULT false,
    created_at timestamptz NOT NULL DEFAULT now()
);

INSERT INTO categories (name, kind, code, sort) VALUES
    ('식비', 'expense', NULL, 10),
    ('카페·간식', 'expense', NULL, 20),
    ('교통', 'expense', NULL, 30),
    ('쇼핑', 'expense', NULL, 40),
    ('생활', 'expense', NULL, 50),
    ('주거·통신', 'expense', NULL, 60),
    ('구독', 'expense', NULL, 70),
    ('문화·여가', 'expense', NULL, 80),
    ('의료·건강', 'expense', NULL, 90),
    ('경조사', 'expense', NULL, 100),
    ('기타', 'expense', NULL, 110),
    ('수입', 'income', 'income', 200),
    ('정산', 'special', 'settlement', 210),
    ('내 계좌 이체', 'special', 'own_transfer', 220),
    ('투자·저축', 'special', 'investment', 230);

-- 거래. 금액은 항상 양수이고 돈의 방향은 flow(out: 나감, in: 들어옴)로 표시한다.
-- direction은 회계상 성격이다. 지출 분류에 flow=in인 거래(부분 취소·환불)는 지출을 줄인다.
CREATE TABLE transactions (
    id                 bigserial PRIMARY KEY,
    raw_event_id       bigint REFERENCES raw_events (id),
    occurred_at        timestamptz NOT NULL,
    amount             bigint      NOT NULL CHECK (amount >= 0),
    flow               text        NOT NULL CHECK (flow IN ('out', 'in')),
    merchant           text        NOT NULL DEFAULT '',
    memo               text        NOT NULL DEFAULT '',
    direction          text        NOT NULL CHECK (direction IN ('expense', 'income', 'transfer')),
    category_id        bigint REFERENCES categories (id),
    -- 정산(더치페이 회수) 금액을 되돌려 놓을 지출 분류. NULL이면 총계에만 더한다.
    settle_category_id bigint REFERENCES categories (id),
    status             text        NOT NULL CHECK (status IN ('confirmed', 'needs_review', 'excluded')),
    exclusion_reason   text,
    review_reason      text,
    merged_into        bigint REFERENCES transactions (id),
    cancels_id         bigint REFERENCES transactions (id),
    source             text        NOT NULL,
    kind               text        NOT NULL DEFAULT '',
    installment_months int         NOT NULL DEFAULT 0,
    balance            bigint,
    manually_edited    boolean     NOT NULL DEFAULT false,
    created_at         timestamptz NOT NULL DEFAULT now(),
    updated_at         timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX transactions_occurred_at_idx ON transactions (occurred_at);
CREATE INDEX transactions_status_idx ON transactions (status) WHERE status = 'needs_review';
CREATE INDEX transactions_raw_event_idx ON transactions (raw_event_id);
CREATE INDEX transactions_amount_time_idx ON transactions (amount, occurred_at);
CREATE INDEX transactions_merged_into_idx ON transactions (merged_into) WHERE merged_into IS NOT NULL;
CREATE INDEX transactions_cancels_idx ON transactions (cancels_id) WHERE cancels_id IS NOT NULL;

-- 분류별 기간 예산. period_start는 예산 기간 시작일(KST 날짜).
CREATE TABLE budgets (
    period_start date   NOT NULL,
    category_id  bigint NOT NULL REFERENCES categories (id),
    amount       bigint NOT NULL CHECK (amount >= 0),
    PRIMARY KEY (period_start, category_id)
);

-- 가맹점 → 분류 규칙.
CREATE TABLE merchant_rules (
    id           bigserial PRIMARY KEY,
    pattern      text        NOT NULL,
    match_type   text        NOT NULL CHECK (match_type IN ('contains', 'exact', 'regex')),
    category_id  bigint      NOT NULL REFERENCES categories (id),
    -- NULL이면 들어온 돈·나간 돈 모두에 적용
    flow         text CHECK (flow IN ('out', 'in')),
    priority     int         NOT NULL DEFAULT 0,
    created_from text        NOT NULL CHECK (created_from IN ('manual', 'learned', 'seed')),
    created_at   timestamptz NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX merchant_rules_unique_idx ON merchant_rules (pattern, match_type, coalesce(flow, ''));

-- 설정(키-값). 값은 JSON.
CREATE TABLE settings (
    key        text PRIMARY KEY,
    value      jsonb       NOT NULL,
    updated_at timestamptz NOT NULL DEFAULT now()
);

INSERT INTO settings (key, value) VALUES
    -- 예산 기간 시작일(1~28). 월급날 등으로 바꿀 수 있다.
    ('budget_start_day', '1'),
    -- 같은 금액이 이 시간(초) 안에 다른 앱에서 또 오면 1건으로 합친다.
    ('dedupe_window_seconds', '180'),
    -- 내 계좌 간 이체로 볼 상대 이름. target: own_transfer(내 계좌 이체) 또는 investment(투자·저축)
    ('own_accounts', '[{"pattern": "토스증권", "target": "investment"}]'),
    -- 카드대금 출금으로 볼 문구(출금·이체 알림에만 적용). 승인 시점에 이미 기록했으므로 지출에서 제외.
    ('card_payment_keywords', '["카드대금", "카드 대금", "결제대금", "카드값"]'),
    -- 출금 알림에서 이 이름이 보이면 카드대금으로 본다(승인 알림에는 적용 안 함).
    ('card_company_names', '["KB국민카드", "국민카드", "삼성카드", "신한카드", "현대카드", "롯데카드", "하나카드", "우리카드", "BC카드", "비씨카드", "NH카드", "농협카드"]'),
    -- 충전식 머니. "충전" 알림에 이 이름이 있으면 지출에서 제외(실제 결제 때 기록됨). 티머니는 교통비라 넣지 않음.
    ('prepaid_keywords', '["카카오페이머니", "페이머니", "쿠팡캐시", "쿠페이머니", "토스머니", "네이버페이머니", "네이버페이 포인트"]'),
    -- 결제대행사·간편결제 이름. 가맹점이 이것뿐이면 무엇을 샀는지 몰라서 확인 필요로 보낸다.
    ('review_merchant_patterns', '["KG이니시스", "이니시스", "토스페이먼츠", "NHN KCP", "KCP", "나이스페이", "나이스정보통신", "다날", "KICC", "한국정보통신", "스마트로", "페이레터", "카카오페이", "네이버페이", "토스페이", "페이코", "PAYCO"]');

-- seed 분류 규칙: 흔한 가맹점 키워드. 추측값이므로 쓰면서 고치면 된다(웹 > 관리 > 규칙).
INSERT INTO merchant_rules (pattern, match_type, category_id, flow, priority, created_from)
SELECT r.pattern, r.match_type, c.id, r.flow, r.priority, 'seed'
FROM (VALUES
    -- 카페·간식
    ('스타벅스', 'contains', '카페·간식', 'out', 10), ('STARBUCKS', 'contains', '카페·간식', 'out', 10),
    ('투썸', 'contains', '카페·간식', 'out', 10), ('메가커피', 'contains', '카페·간식', 'out', 10),
    ('메가MGC', 'contains', '카페·간식', 'out', 10), ('컴포즈커피', 'contains', '카페·간식', 'out', 10),
    ('빽다방', 'contains', '카페·간식', 'out', 10), ('이디야', 'contains', '카페·간식', 'out', 10),
    ('할리스', 'contains', '카페·간식', 'out', 10), ('폴바셋', 'contains', '카페·간식', 'out', 10),
    ('커피빈', 'contains', '카페·간식', 'out', 10), ('파스쿠찌', 'contains', '카페·간식', 'out', 10),
    ('공차', 'contains', '카페·간식', 'out', 10), ('배스킨라빈스', 'contains', '카페·간식', 'out', 10),
    ('던킨', 'contains', '카페·간식', 'out', 10), ('파리바게뜨', 'contains', '카페·간식', 'out', 10),
    ('뚜레쥬르', 'contains', '카페·간식', 'out', 10), ('설빙', 'contains', '카페·간식', 'out', 10),
    -- 교통
    ('티머니', 'contains', '교통', 'out', 10), ('카카오T', 'contains', '교통', 'out', 10),
    ('카카오모빌리티', 'contains', '교통', 'out', 10), ('택시', 'contains', '교통', 'out', 10),
    ('코레일', 'contains', '교통', 'out', 10), ('KTX', 'contains', '교통', 'out', 10),
    ('SRT', 'contains', '교통', 'out', 10), ('고속버스', 'contains', '교통', 'out', 10),
    ('주유소', 'contains', '교통', 'out', 10), ('쏘카', 'contains', '교통', 'out', 10),
    -- 쇼핑
    ('쿠팡', 'contains', '쇼핑', 'out', 10), ('11번가', 'contains', '쇼핑', 'out', 10),
    ('G마켓', 'contains', '쇼핑', 'out', 10), ('지마켓', 'contains', '쇼핑', 'out', 10),
    ('옥션', 'contains', '쇼핑', 'out', 10), ('무신사', 'contains', '쇼핑', 'out', 10),
    ('29CM', 'contains', '쇼핑', 'out', 10), ('올리브영', 'contains', '쇼핑', 'out', 10),
    ('에이블리', 'contains', '쇼핑', 'out', 10), ('지그재그', 'contains', '쇼핑', 'out', 10),
    ('신세계백화점', 'contains', '쇼핑', 'out', 10), ('롯데백화점', 'contains', '쇼핑', 'out', 10),
    ('현대백화점', 'contains', '쇼핑', 'out', 10),
    -- 식비
    ('쿠팡이츠', 'contains', '식비', 'out', 10), ('배달의민족', 'contains', '식비', 'out', 10),
    ('배민', 'contains', '식비', 'out', 10), ('요기요', 'contains', '식비', 'out', 10),
    ('맥도날드', 'contains', '식비', 'out', 10), ('버거킹', 'contains', '식비', 'out', 10),
    ('롯데리아', 'contains', '식비', 'out', 10), ('맘스터치', 'contains', '식비', 'out', 10),
    ('서브웨이', 'contains', '식비', 'out', 10), ('김밥', 'contains', '식비', 'out', 10),
    ('도미노피자', 'contains', '식비', 'out', 10),
    -- 생활
    ('이마트', 'contains', '생활', 'out', 10), ('홈플러스', 'contains', '생활', 'out', 10),
    ('롯데마트', 'contains', '생활', 'out', 10), ('코스트코', 'contains', '생활', 'out', 10),
    ('다이소', 'contains', '생활', 'out', 10), ('노브랜드', 'contains', '생활', 'out', 10),
    ('GS25', 'contains', '생활', 'out', 10), ('CU', 'exact', '생활', 'out', 10),
    ('씨유', 'contains', '생활', 'out', 10), ('세븐일레븐', 'contains', '생활', 'out', 10),
    ('이마트24', 'contains', '생활', 'out', 10),
    -- 주거·통신
    ('SK텔레콤', 'contains', '주거·통신', 'out', 10), ('SKT', 'exact', '주거·통신', 'out', 10),
    ('KT', 'exact', '주거·통신', 'out', 10), ('LGU+', 'contains', '주거·통신', 'out', 10),
    ('LG유플러스', 'contains', '주거·통신', 'out', 10), ('관리비', 'contains', '주거·통신', 'out', 10),
    ('도시가스', 'contains', '주거·통신', 'out', 10), ('한국전력', 'contains', '주거·통신', 'out', 10),
    -- 구독
    ('넷플릭스', 'contains', '구독', 'out', 10), ('NETFLIX', 'contains', '구독', 'out', 10),
    ('유튜브', 'contains', '구독', 'out', 10), ('YOUTUBE', 'contains', '구독', 'out', 10),
    ('스포티파이', 'contains', '구독', 'out', 10), ('멜론', 'contains', '구독', 'out', 10),
    ('디즈니플러스', 'contains', '구독', 'out', 10), ('티빙', 'contains', '구독', 'out', 10),
    ('왓챠', 'contains', '구독', 'out', 10), ('쿠팡와우', 'contains', '구독', 'out', 10),
    ('네이버플러스', 'contains', '구독', 'out', 10),
    -- 문화·여가
    ('CGV', 'contains', '문화·여가', 'out', 10), ('메가박스', 'contains', '문화·여가', 'out', 10),
    ('롯데시네마', 'contains', '문화·여가', 'out', 10), ('인터파크', 'contains', '문화·여가', 'out', 10),
    ('예스24', 'contains', '문화·여가', 'out', 10), ('교보문고', 'contains', '문화·여가', 'out', 10),
    ('야놀자', 'contains', '문화·여가', 'out', 10), ('여기어때', 'contains', '문화·여가', 'out', 10),
    -- 의료·건강
    ('약국', 'contains', '의료·건강', 'out', 10), ('병원', 'contains', '의료·건강', 'out', 10),
    ('의원', 'contains', '의료·건강', 'out', 10), ('치과', 'contains', '의료·건강', 'out', 10),
    ('한의원', 'contains', '의료·건강', 'out', 10),
    -- 수입
    ('급여', 'contains', '수입', 'in', 10), ('월급', 'contains', '수입', 'in', 10),
    ('이자', 'contains', '수입', 'in', 10)
) AS r(pattern, match_type, category, flow, priority)
JOIN categories c ON c.name = r.category;

-- +goose Down
DROP TABLE settings;
DROP TABLE merchant_rules;
DROP TABLE budgets;
DROP TABLE transactions;
DROP TABLE categories;
