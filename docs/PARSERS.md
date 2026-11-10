# 파서 보정 절차

지금 파서(KB·토스)는 **실제 알림을 보지 못한 상태에서 만든 잠정 버전**이다. 1~2주 실사용 후 아래 순서로 보정한다.

## 구조

```
server/internal/parser/
  parser.go     Kind(승인/취소/입금/출금/송금/충전), Event, Result, Parser 인터페이스
  common.go     공통 규칙: 금액(12,345원), 종류 판단, 할부, 날짜·시각, 가맹점 추측
  sources.go    소스별 파서: KB, Toss, Generic
  registry.go   패키지명 → 파서 매핑 (환경변수 PARSER_PACKAGES)
  testdata/synthetic/cases.json   합성 픽스처(실제 형식 아님)
```

- 파서 선택 순서: `PARSER_PACKAGES` 매핑 → 내용 감지(`Detect`) → 범용 파서
- 그 전에 결제가 아닌 알림(광고·혜택, 증권 체결·시세, 안내·예정)은 `ignore.go`가 거르고 `raw_events.ignore_reason`만 남긴다(해석 실패 아님). 새로 거를 문구는 `PARSER_IGNORE_PATTERNS`("문구1;문구2")로 더하고 재처리한다
- **패키지명은 코드에 없다.** 앱 "최근 알림" 화면에서 실제 패키지명을 확인해서 환경변수에 넣는다:
  ```
  PARSER_PACKAGES=kb=<KB Pay 패키지>;toss=<토스 패키지>;kakaopay=<카카오페이 패키지>;coupang=<쿠팡 패키지>
  ```
  지금 운영 값(실제 원문으로 확인한 것만): `PARSER_PACKAGES="samsungpay=com.samsung.android.spay;coupang=com.coupang.mobile"`. 값에 `;`가 있어서 따옴표로 감싼다(systemd는 따옴표를 벗기고, 셸로 읽어도 끊기지 않는다).
  내장 파서 이름(`kb`, `toss`)이 아닌 이름은 범용 파서를 쓰고 그 이름이 거래의 `source`가 된다(명세서 대조 화면에서 결제 수단별 합계로 보인다. 화면에 보일 이름은 `web/labels.go`의 `sourceNames`).
- 범용 파서는 본문에서 가맹점을 못 찾으면 알림 제목(보통 앱 이름, 예: 쿠팡)을 가맹점으로 쓴다.
- 해석에 실패해도 데이터는 잃지 않는다. 금액을 못 찾으면 `raw_events.parse_error`에 기록하고 거래를 만들지 않는다. 금액만 찾으면 `needs_review` 거래가 된다.

## 1. 원문 내보내기 (익명화)

```bash
# 전체 (기간은 KST, to 포함)
/myhome export-raw --from 2026-11-01 --to 2026-11-14 --out raw.jsonl
# 해석 실패만
/myhome export-raw --from 2026-11-01 --to 2026-11-14 --failed --out failed.jsonl
```

가리는 것: 이름(`홍*동`, `○○○님`, 송금·입금 상대), 카드번호, 계좌번호, 전화번호, 잔액·누적 금액, 8자리 이상 숫자.
남기는 것: 가게 이름, 거래 금액, 문구 구조(파서에 필요).

> 자동 가리기는 완벽하지 않다. **공유하거나 커밋하기 전에 파일을 직접 훑어본다.** `raw*.jsonl`, `export*.jsonl`은 `.gitignore`에 있다.

각 줄에는 현재 파서가 본 결과(`parsed`)도 들어 있어서 어디가 틀렸는지 바로 비교할 수 있다.

## 2. 픽스처 추가

익명화한 줄을 `server/internal/parser/testdata/anonymized/cases.json`에 추가한다. 형식은 `synthetic/cases.json`과 같다(`_note`에 "익명화한 실제 알림"이라고 적는다). 기대값(`want`)에는 **사람이 판단한 정답**을 적는다.

```json
{
  "name": "KB Pay 승인 (실제 형식, 익명화)",
  "package": "test.kb",
  "title": "...", "text": "...", "big_text": "",
  "posted_at": "2026-11-03T12:34:56+09:00",
  "want": {"source": "kb", "kind": "approval", "amount": 12000, "merchant": "가게이름"}
}
```

`package`는 테스트용 이름(`test.kb`, `test.toss`)으로 바꿔도 된다(테스트는 `kb=test.kb;toss=test.toss` 매핑을 쓴다).
`parser_test.go`의 `loadFixtures`가 `testdata/anonymized/`도 읽도록 경로를 추가한다(파일이 없으면 건너뛴다).

## 3. 파서 수정

```bash
cd server
go test ./internal/parser/ -run TestSyntheticFixtures -v
```

- 공통 패턴 문제면 `common.go` (금액 라벨, 종류 키워드, 가맹점 잡음 단어)
- 특정 소스만의 형식이면 `sources.go`의 해당 파서 `Parse`
- 새 소스는 `Parser`를 구현하고 `registry.go`의 `Builtin()`에 추가

합성 픽스처와 익명화 픽스처가 모두 통과해야 한다.

## 4. 재처리

배포 후 기존 원문에 새 파서를 적용한다.

```bash
# 계획만 보기 (기본)
/myhome reprocess --from 2026-11-01 --to 2026-11-14
# 적용
/myhome reprocess --from 2026-11-01 --to 2026-11-14 --apply
```

- 원문(`raw_events`)은 지우지 않는다. 원문에서 자동으로 만든 거래만 지우고 다시 만든다.
- **사용자가 고친 거래(`manually_edited`)는 그대로 두고** 그 원문은 건너뛴다.
- 중복·취소로 연결된 거래는 기간 밖이어도 함께 다시 처리해서 상태가 어긋나지 않게 한다.

## 처리 파이프라인 (참고)

1. 파싱 (금액·가맹점·종류·시각·잔액·할부)
2. 예외 규칙
   - 카드대금 출금 → 제외 (출금·송금 알림에 `card_payment_keywords` 또는 `card_company_names`)
   - 내 계좌 간 이체 → 확정, 방향 transfer, 분류 "내 계좌 이체" 또는 "투자·저축" (`own_accounts`)
   - 충전식 머니 충전 → 제외 (`prepaid_keywords`, 티머니 같은 교통카드는 해당 없음)
3. 중복 합치기: 다른 앱에서 같은 방향·같은 금액이 `dedupe_window_seconds`(기본 180초) 안에 오면 1건으로. 가맹점명은 더 구체적인 쪽(결제대행사 이름보다 실제 가게 이름)
4. 취소 상계: 같은 가게·같은 금액 원거래를 찾아 둘 다 제외. 부분 취소는 같은 분류의 마이너스 지출. 못 찾으면 확인 필요
5. 규칙 분류: 사용자 규칙(수동·학습) → 결제대행사 이름 떼고 → seed 규칙
6. 못 하면 확인 필요 (`review_reason`: no_rule, payment_gateway, transfer, deposit, no_merchant, unknown_kind, cancel_unmatched)

설정값(키워드 목록 등)은 웹 관리 화면 또는 `PUT /v1/settings/{key}`로 바꾼다. 바꾼 뒤 필요하면 재처리한다.
