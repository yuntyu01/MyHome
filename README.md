# MyHome 가계부

개인용(사용자 1명) 가계부. 폰이 결제 알림을 자동으로 모아 서버로 보내면, 서버가 해석·분류해서 분류별 월 예산에서 차감한다. 안드로이드 홈 위젯과 PC 웹에서 실시간으로 확인하고 분석한다.

```
[Android 앱] --알림 원문(HTTPS, Bearer)--> [Go 서버] --> [PostgreSQL]
     ^  위젯·앱 화면 <---- 요약 API ----------+
[PC 웹 브라우저] ---------------------------> [Go 서버] (카카오 로그인)
```

- 앱은 알림을 **해석하지 않고 원문 그대로** 보낸다. 해석·분류·중복 처리는 전부 서버가 한다(알림 문구가 바뀌어도 서버만 고치면 됨).
- 서버는 원문을 영구 보관한다. 해석 규칙이 바뀌면 재처리할 수 있다.

## 문서

| 문서 | 내용 |
|---|---|
| [`CLAUDE.md`](CLAUDE.md) | 요구사항과 작업 지침 |
| [`docs/PROGRESS.md`](docs/PROGRESS.md) | 진행 상황, 막힌 것 |
| [`docs/DECISIONS.md`](docs/DECISIONS.md) | 설계 결정과 바꾸는 방법 |
| [`docs/DEPLOY.md`](docs/DEPLOY.md) | kodeploy 배포, 환경변수, 운영 명령 |
| [`docs/KAKAO_SETUP.md`](docs/KAKAO_SETUP.md) | 카카오 디벨로퍼스 설정 |
| [`docs/ANDROID_SETUP.md`](docs/ANDROID_SETUP.md) | 앱 빌드·설치·권한·위젯 |
| [`docs/PARSERS.md`](docs/PARSERS.md) | 실데이터로 파서 보정하는 절차 |
| [`docs/BACKUP.md`](docs/BACKUP.md) | 백업·복구 |

## 구성

```
server/                 Go 서버 (net/http, pgx, sqlc, goose)
  cmd/myhome/           바이너리: serve, migrate, token, reprocess, process-pending, export-raw, healthcheck
  internal/
    ingest/             POST /v1/events (원문 저장, 멱등)
    parser/             소스별 파서(KB·토스 잠정, 범용) + 합성 픽스처
    pipeline/           처리: 예외 규칙 → 중복 → 취소 상계 → 분류 → 확인 필요, 재처리, 사용자 수정
    api/                JSON API (/v1/summary, transactions, review, budgets, analytics, reports, reconcile …)
    auth/               기기 토큰, 카카오 로그인, 세션·CSRF
    web/                PC 웹 (html/template + htmx + Chart.js 내장)
    db/                 마이그레이션(goose), 쿼리(sqlc → internal/dbq)
android/
  app/                  수집기(NotificationListenerService) → Room 대기열 → WorkManager 전송, Glance 위젯, Compose 화면
  core/                 순수 Kotlin 로직(API 클라이언트, 위젯·예산 알림 판단) — SDK 없이 테스트 가능
scripts/                backup.sh, restore.sh
docs/
```

## 기능

- **수집**: 선택한 앱(KB Pay, 토스, 카카오페이, 쿠팡 등)의 결제 알림을 모아 전송. 실패하면 재시도, 재부팅 후에도 대기열 유지. 전송에 성공하면 원본 알림 삭제(설정)
- **해석·분류**: 금액·가맹점·승인/취소/입금/출금 판단 → 가맹점 규칙으로 자동 분류 → 못 하면 "확인 필요"
- **예외 처리**: 카드대금 출금·충전식 머니 충전 제외, 내 계좌 이체는 투자·저축으로 따로 집계, 토스페이+KB 중복 1건으로 합치기, 취소 상계, 할부는 승인 시점 전액, 친구 송금은 확인 필요
- **학습**: 확인 필요 건을 분류할 때 "이 가맹점은 항상"을 켜면 다음부터 자동 분류
- **예산**: 분류별 월 예산(시작일 변경 가능), 남은 돈 = 예산 − 확정 지출 + 정산
- **PC 웹**: 대시보드, 거래(검색·수정·제외), 확인 필요 큐, 예산(지난달 복사), 분석(비중·일별 추이·전월 대비), 월간 리포트, 대사 도우미, 관리(현금 입력·분류·규칙·설정·해석 실패 원문·기기), 다크 모드, 모바일
- **앱·위젯**: 홈 위젯(크기별 3단계, 결제 직후 즉시 갱신 + 30분), 이번 달 요약, 확인 필요 처리, 최근 거래, 예산 80%·초과 알림(무음, 끌 수 있음)
- **보안**: 카카오 로그인 + 허용 회원번호, 기기 토큰(DB엔 해시만, 앱은 Keystore 암호화), 세션 쿠키 HttpOnly·Secure·SameSite + CSRF, CSP, rate limit

## 로컬 실행

필요한 것: Docker, Go 1.26+

```bash
cp .env.example .env        # 값 채우기 (.env는 커밋 금지)
make up                     # postgres + server (docker compose)
curl localhost:8080/healthz # {"status":"ok"}
```

카카오 키 없이 웹을 보려면 `.env`에 개발용 로그인을 켠다(운영 금지):

```
APP_ENV=development
DEV_LOGIN_KAKAO_ID=1
ALLOWED_KAKAO_IDS=1
COOKIE_SECURE=false
```

→ http://localhost:8080/login 의 "개발용 로그인"

Docker 없이 서버만:

```bash
make db     # postgres만 docker로
make run    # go run ./cmd/myhome serve (.env 읽음)
```

## 자주 쓰는 명령

| 명령 | 설명 |
|---|---|
| `make test` | 서버 전체 테스트 (`TEST_DATABASE_URL` DB 사용, 없으면 DB 테스트 건너뜀) |
| `make android-core-test` | Android core 단위 테스트 |
| `make migrate` | DB 마이그레이션 적용 (`make migrate ARGS=status`) |
| `make backup` | 로컬 DB 백업 |
| `make docker-build` | 서버 이미지 빌드 |
| `cd android && ./gradlew assembleDebug` | APK 빌드 (Android SDK 필요) |

서버 관리 명령(배포된 컨테이너에서 `/myhome …`): `token issue|list|revoke`, `reprocess --from --to [--apply]`, `process-pending`, `export-raw`. 자세한 건 [`docs/DEPLOY.md`](docs/DEPLOY.md).

## 디자인

웹·앱·위젯은 [modo](https://github.com/yuntyu01/modo)의 디자인 언어를 따른다: 다크는 검정 바탕 + 위쪽 남색 그라데이션, 라이트는 옅은 하늘빛, 테두리 없는 둥근 카드, 강조색 `#85ADE2`, Pretendard 글꼴, 휴대폰에서는 떠 있는 알약 하단바. 자세한 값은 `docs/DECISIONS.md` "디자인".

## 라이선스 고지

- htmx (0BSD), Chart.js (MIT): `server/internal/web/static/vendor/`에 라이선스 파일 포함
- Pretendard 글꼴 (SIL OFL 1.1, Kil Hyung-jin): 웹 `server/internal/web/static/fonts/`, 앱 `android/app/src/main/res/font/` (라이선스 `android/app/src/main/assets/licenses/`)
