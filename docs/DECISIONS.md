# 결정 기록

형식: **무엇을** — 왜 — 나중에 바꾸려면

## Phase 0

- **모노레포 구성 `/server`, `/android`, `/docs`** — CLAUDE.md 지침 그대로 — 바꿀 일 없음.
- **Go 모듈 경로 `github.com/yuntyu01/MyHome/server`** — 실제 저장소 이름과 대소문자까지 맞춤 — `server/go.mod`와 import 경로를 일괄 치환.
- **서버 바이너리 하나 + 서브커맨드(`serve`, `migrate`, `healthcheck` 등)** — Docker 이미지 하나로 서버·마이그레이션·CLI를 모두 돌리기 위해 — `server/cmd/myhome/main.go`.
- **런타임 이미지는 `scratch` + CA 인증서** — 이미지가 가장 작고 공격면이 적다. 헬스체크는 `myhome healthcheck` 서브커맨드로 대체 — 셸이 필요하면 `alpine`이나 `distroless`로 교체.
- **Dockerfile에 선택적 `ca_bundle` 빌드 시크릿** — TLS를 재암호화하는 프록시 뒤에서도 `go mod download`가 되게 하기 위해. 시크릿이 없으면 아무 영향 없음 — 필요 없으면 해당 `--mount` 줄 삭제.
- **Makefile이 `.env`를 읽음(`-include .env`)** — `make run`/`make migrate`에서 환경변수를 따로 export하지 않아도 되게 — `.env` 값에 `$`나 `#`가 있으면 따옴표 없이 쓰지 말 것. 문제가 되면 Makefile 상단 두 줄 삭제.

## Phase 1

- **Go 1.26으로 올림** (Phase 0은 1.24) — 최신 pgx v5.12, goose v3.29, x/time이 Go 1.25~1.26을 요구한다. 1.24는 이미 지원 종료 — `server/go.mod`의 `go` 줄과 Dockerfile `FROM golang:` 태그.
- **`content_hash`는 서버가 계산** (패키지·게시시각(ms)·title·text·big_text, 길이 접두로 경계 구분) — 클라이언트 값을 믿지 않기 위해. 같은 내용이라도 게시시각이 다르면 별개 이벤트(같은 가게 두 번 결제) — `internal/ingest/ingest.go`의 `ContentHash`.
- **`posted_at`은 Unix 밀리초 정수** — Android `StatusBarNotification.postTime`을 그대로 보내면 돼서 앱 쪽 변환 실수가 없다 — 요청 구조체 `ingest.Request`.
- **새 이벤트 201, 중복 200 + `"duplicate":true`** — 앱은 2xx면 성공으로 보고 알림을 지운다 — `ingest.PostEvent`.
- **원문 저장 뒤 처리가 실패해도 201** — 원문은 이미 안전하게 저장됐고 재처리 명령으로 복구할 수 있다. 실패를 돌려주면 앱이 무한 재전송한다 — `ingest.PostEvent`.
- **수집 API는 알 수 없는 JSON 필드를 무시** — 앱이 먼저 업데이트돼도 수집이 끊기지 않게 — `DecodeJSONLenient`.
- **기기 토큰 `mhd_` + 32바이트 난수(base64url), DB에는 SHA-256만** — 고엔트로피 토큰이라 솔트·느린 해시가 필요 없다. 접두사는 유출 시 식별용 — `internal/auth/token.go`.
- **rate limit은 메모리 토큰 버킷(기기별 + IP별)** — 단일 인스턴스 개인용이라 Redis 불필요. 의존성은 `golang.org/x/time/rate` 하나 — 여러 인스턴스로 늘리면 외부 저장소 기반으로 교체.
- **`MIGRATE_ON_START=true` 기본** — kodeploy에서 별도 마이그레이션 단계 없이 배포되게 — 끄고 `/myhome migrate up`을 따로 실행해도 된다.
- **로그에는 메서드·경로·상태·시간·raw_event_id만** — 알림 원문은 개인정보 — `httpx.Logging`.
- **테스트는 `TEST_DATABASE_URL`의 DB에 테스트마다 임의 스키마를 만들어 격리** — 병렬 실행과 정리가 쉽다. 변수가 없으면 DB 테스트는 Skip — `internal/testdb`.
- **로컬 compose에 `myhome_test` DB 자동 생성 스크립트** — 개발 DB와 테스트를 분리 — `deploy/postgres-init/`. 이미 볼륨이 있으면 `CREATE DATABASE myhome_test`를 한 번 직접 실행.

## Phase 2

- **PulseGate를 포크하지 않고 새로 만듦** — PulseGate(MIT)는 SMS 수신기·텔레그램·다중 목적지·기기 내 해석기·Hilt·Retrofit·Moshi를 포함해서 "앱은 해석하지 않는다", "목적지는 내 서버 하나"라는 이 프로젝트 원칙과 맞지 않고 걷어낼 게 더 많다. README만 검토했고 코드는 복사하지 않았다. 참고한 설계 아이디어: 전송 전 Room에 먼저 저장, SHA-256 중복 방지, 재부팅 후 대기열 유지 — 포크하려면 PulseGate의 `LICENSE`를 함께 가져와야 한다.
- **`core` 모듈을 포함 빌드(`includeBuild`)로 분리** — Android SDK 없이도 API 클라이언트·수집 규칙·금액 포맷을 단위 테스트하기 위해. 일반 `include`로 두면 AGP가 SDK를 요구해서 테스트를 못 돌린다 — 합치려면 `settings.gradle.kts`에서 `include(":core")`로 바꾸고 `core/settings.gradle.kts` 삭제.
- **라이브러리 버전은 보수적으로(2024년 말 안정판)**: AGP 8.7.3, Kotlin 2.1.0, KSP 2.1.0-1.0.29, Compose BOM 2024.12.01, Room 2.6.1, WorkManager 2.10.0, Glance 1.1.1, OkHttp 4.12.0 — 작업 환경에서 Google Maven(dl.google.com)이 막혀 androidx·AGP 최신 버전을 확인할 수 없었다. 존재가 확실한 조합을 골랐다(Kotlin·KSP·OkHttp는 Maven Central에서 확인) — `android/gradle/libs.versions.toml`에서 올리면 된다. Android Studio 업그레이드 도우미 권장.
- **Gradle wrapper 8.11.1** — AGP 8.7 호환 범위 — AGP를 올리면 함께 올린다.
- **applicationId `com.yuntyu.myhome`** — 카카오 키 해시 등록에 이 값을 쓴다 — `app/build.gradle.kts`. 바꾸면 카카오 디벨로퍼스 설정도 바꿔야 한다.
- **수집 대상 패키지는 사용자가 "최근 알림" 화면에서 체크** — 패키지명을 코드에 박지 않는다(CLAUDE.md 원칙) — `AppSettings.selectedPackages`.
- **"최근 알림" 목록용으로 모든 앱의 패키지명·앱 이름·제목 앞 60자를 기기 Room에만 저장** — 실제 패키지명을 보여주려면 필요하다. 서버로는 보내지 않는다 — `RecentAppDao`.
- **진행 중(ongoing)·묶음 요약 알림은 수집 안 함** — 결제 정보가 아니고 중복을 만든다 — `CaptureRules.shouldCapture`.
- **InboxStyle 알림은 `EXTRA_TEXT_LINES`를 줄바꿈으로 합쳐 `big_text`로** — 일부 앱이 여러 줄 알림을 이 방식으로 보낸다 — `CaptureService`.
- **전송 작업은 WorkManager 고유 작업 + `REPLACE` + 긴급(expedited)** — 방금 넣은 알림이 실행 중인 작업 끝무렵에 누락되지 않게. 교체로 취소돼 다시 보내져도 서버가 중복 처리한다. 15분 주기 작업은 안전망 — `WorkScheduler`.
- **HTTP 401/403이면 재시도하지 않고 멈춤, 4xx 형식 오류는 `REJECTED`로 보존** — 무한 재시도로 배터리를 쓰지 않게, 그리고 데이터를 지우지 않기 위해. 설정에서 다시 보내기 가능 — `UploadWorker`.
- **토큰은 Keystore AES-256-GCM 키로 직접 암호화** (EncryptedSharedPreferences 미사용) — security-crypto 라이브러리는 지원 중단 예정이고, 직접 구현이 단순하다 — `TokenStore`.
- **테스트 알림은 제목 `[MyHome 테스트]`로 표시하고 서버는 거래로 만들지 않음** — 실제 기기 확인 절차(테스트 알림 → 서버 확인 → 알림 삭제)를 장부 오염 없이 하기 위해 — `CaptureRules.TEST_TITLE_MARKER`, 서버 파이프라인(Phase 3).
- **앱 백업 끔(`allowBackup=false`, 데이터 추출 규칙에서 전부 제외)** — 토큰·알림 원문이 클라우드 백업으로 나가지 않게 — `AndroidManifest.xml`.
- **http 서버 주소는 localhost·10.0.2.2·사설망(192.168.*, 10.*)만 허용** — 토큰 평문 전송 방지. 개발 편의는 유지 — `core/ServerUrl.kt`. Android 쪽 평문 허용은 network security config로: **릴리스는 HTTPS만**(예외는 기기 자신인 localhost·127.0.0.1뿐), 디버그 빌드만 에뮬레이터·사설망 개발 서버 http 허용(`src/debug/res/xml/network_security_config.xml`). 처음엔 릴리스에 10.0.2.2도 열어 뒀으나, 실제 폰에서는 이 주소가 같은 망의 다른 기기일 수 있어 뺐다.

## Phase 3

- **금액은 항상 양수 + `flow`(out/in) 컬럼 추가** (CLAUDE.md 초안에 없던 컬럼) — 부분 취소·환불을 "같은 분류의 들어온 돈"으로 표현해서 지출 합계에서 빼기 위해. 지출 합계 = Σ(out) − Σ(in) — `transactions.flow`.
- **`settle_category_id` 추가** — "정산 금액은 다시 더한다"를 분류별로 하려면 어느 분류로 돌려놓을지 알아야 한다. 비우면 총계에만 더한다 — 웹 확인 필요 화면에서 정산을 고를 때 지정.
- **`cancels_id`, `review_reason`, `kind`, `installment_months`, `balance` 추가** — 취소 연결 추적, 확인 필요 사유 표시, 재처리·대사용.
- **분류에 `code`(income/settlement/own_transfer/investment) 추가** — 특수 분류를 이름 대신 코드로 찾아서 사용자가 이름을 바꿔도 동작. 특수 분류는 보관(삭제) 불가.
- **분류 삭제(DELETE)는 보관(archived)으로 처리** — 데이터 삭제 금지 원칙. 과거 거래의 분류가 사라지지 않게.
- **"수입"은 kind=income, 정산·내 계좌 이체·투자·저축은 kind=special** — 초안의 "특수 분류: 수입…"을 kind 값과 맞추려고 수입만 income으로 뒀다. 동작상 차이는 없다.
- **예외 규칙 키워드는 settings 테이블(JSON)** — 실제 알림 문구를 모르는 상태라 코드 대신 설정으로 뺐다. 웹·API에서 바로 고친다.
  - 카드사 이름(`card_company_names`)은 **출금·송금 알림에만** 적용: 승인 알림에는 "KB국민카드"가 항상 들어 있어서 승인까지 제외되는 사고를 막기 위해.
  - `own_accounts` 기본값은 `토스증권 → 투자·저축` 하나(CLAUDE.md 예시). 다른 내 계좌는 추가해야 한다.
  - 티머니는 충전식 머니 목록에서 뺐다. CLAUDE.md가 "티머니 충전도 기록"을 요구해서 교통비(seed 규칙)로 잡힌다.
- **중복 합치기 조건: 다른 패키지 + 같은 flow + 같은 금액 + 시간 창(기본 180초)** — 같은 앱에서 같은 금액이 연달아 오면 실제로 두 번 산 것(커피 두 잔)이라 합치지 않는다. 먼저 들어온 거래를 대표로 남기고, 이름이 더 구체적이면 대표 거래 이름을 바꾸고 다시 분류한다.
- **취소 상계: 120일 안의 같은 가게(느슨한 비교)·금액 이상 원거래** — 전액이면 둘 다 제외, 부분이면 취소 건을 같은 분류의 마이너스 지출로 확정, 부분 취소는 가게 이름이 둘 다 있을 때만 매칭(오매칭 방지).
- **친구 송금은 사용자가 만든 "정확 일치" 규칙만 적용** — "자동 판단 금지" 요구와 "학습" 요구를 둘 다 만족시키려고. seed의 contains 규칙(예: '김밥')이 사람 이름에 걸리지 않게.
- **결제대행사 이름은 떼어내고 남은 이름으로 분류, 남는 게 없으면 확인 필요** — "카카오페이_스타벅스"처럼 섞여 오는 경우 대비. 목록은 `review_merchant_patterns`.
- **seed 규칙에 '쿠팡 → 쇼핑' 포함** — CLAUDE.md §5 예시를 따랐다. R6은 쿠팡을 확인 필요 예시로 들지만 R4(자동 분류)가 우선순위가 높다 — 쿠팡을 매번 확인하고 싶으면 웹 규칙 관리에서 해당 seed 규칙을 지운다.
- **seed 규칙은 contains + priority 10, 학습 규칙은 exact + priority 100, 수동 규칙 기본 50** — 사용자가 고친 게 항상 이긴다. 같은 우선순위면 긴 패턴 먼저(쿠팡이츠 > 쿠팡). 'KT', 'SKT', 'CU'처럼 짧은 이름은 exact로 오탐 방지.
- **학습 시 같은 가맹점의 기존 확인 필요 건도 함께 분류** — 큐를 한 번에 줄이기 위해(응답의 `also_updated`).
- **처리 직렬화: `pg_advisory_xact_lock`** — 동시에 두 알림이 와도 중복 판단이 꼬이지 않게. 개인용 규모라 성능 영향 없음.
- **서버 시작 시 미처리 원문 자동 처리 + `process-pending` 명령** — 처리 도중 서버가 죽어도 복구.
- **재처리는 기본이 계획 출력, `--apply`일 때만 적용** — 거래를 지우고 다시 만드는 작업이라 실수 방지.
- **본문 시각을 우선하되 게시 시각과 3일 넘게 차이 나면 게시 시각** — 월말 23:59 결제가 다음 달로 넘어가지 않게, 동시에 잘못 읽은 날짜를 막기 위해.
- **잔액은 거래에 저장하지만 API·웹 목록에는 내보내지 않음** — 대사·디버깅용. 익명화 export에서는 가린다.
- **API 인증(Phase 3 시점)**: `/v1/*` 사용자 API는 기기 토큰으로 접근. Phase 4에서 웹 세션 쿠키도 허용.

## Phase 4

- **카카오 회원번호 확인은 `GET /v1/user/access_token_info` 하나로** — 회원번호(`id`)와 앱 ID(`app_id`)를 같이 준다. 앱 ID 확인으로 다른 카카오 앱에서 받은 토큰을 거부할 수 있다(`KAKAO_APP_ID` 설정 시) — `auth.KakaoHTTP.TokenInfo`. 엔드포인트 주소는 기본값이고 설정으로 바꿀 수 있다(실제 API로 확인 못 함).
- **카카오 API는 `auth.Kakao` 인터페이스로 분리, 테스트는 가짜 구현** — 키가 없는 환경에서도 로그인 흐름 전체를 테스트하기 위해. 실제 구현의 요청 형식은 httptest 서버로 따로 확인.
- **웹 세션은 DB 테이블(`sessions`) + 쿠키 원문은 해시만 저장** — 서버 재시작에도 로그인 유지, 쿠키 유출 시 DB만으로는 재사용 불가 — `migrations/00003_sessions.sql`.
- **세션 쿠키 SameSite=Lax** (Strict 아님) — 카카오에서 돌아오는 리다이렉트 직후 첫 화면에서 쿠키가 빠지는 문제를 피하기 위해. 대신 상태 변경 요청은 CSRF 토큰(헤더 `X-CSRF-Token` 또는 폼 `csrf_token`)을 요구한다.
- **기기 토큰(Bearer) 요청은 CSRF 검사 안 함** — 쿠키를 쓰지 않아 CSRF 대상이 아니다.
- **OAuth state는 10분짜리 HttpOnly 쿠키로 검증** — 서버 저장소 없이 로그인 위조(CSRF) 방지.
- **허용 안 된 계정 화면에 본인 회원번호를 보여줌** — 처음 설정할 때 `ALLOWED_KAKAO_IDS`에 넣을 값을 알아내는 가장 쉬운 방법. 본인 정보라 노출 문제 없음.
- **개발용 로그인(`/auth/dev-login`)은 `APP_ENV=development` + `DEV_LOGIN_KAKAO_ID` + 허용 목록에 있을 때만 등록** — 운영 설정에서 `DEV_LOGIN_KAKAO_ID`를 넣으면 서버가 시작을 거부한다.
- **기기 토큰에 발급한 카카오 회원번호 기록(`devices.kakao_id`)** — 기기 목록에서 CLI 발급과 구분.
- **로그아웃은 세션 행 삭제** — 로그인 상태 정보라 "데이터 삭제 금지" 대상이 아니라고 판단. 만료 세션도 로그인 때 정리.
- **앱: 카카오 액세스 토큰은 서버에 한 번 보내고 저장하지 않음** — 앱에 남는 비밀은 기기 토큰 하나(Keystore 암호화).
- **앱: 카카오톡 로그인 → 실패 시 카카오계정 로그인, 사용자가 취소하면 중단** — 카카오 SDK 권장 흐름.
- **카카오 SDK 버전 2.20.6, 저장소 `devrepo.kakao.com`** — 작업 환경에서 접속·확인하지 못했다. 빌드 오류가 나면 카카오 문서의 최신 버전으로 바꾼다 — `android/gradle/libs.versions.toml`.
- **네이티브 앱 키는 `local.properties`(`kakao.nativeAppKey`)로 주입, 없으면 카카오 버튼 비활성** — 키 없이도 앱이 빌드·동작하게(토큰 직접 입력).

## Phase 5

- **웹 스택: Go `html/template` + htmx + Chart.js, 서버가 직접 서빙** (◆ 기본값) — 별도 프론트엔드 빌드 없음.
- **htmx 2.0.11, Chart.js 4.5.1을 저장소에 내장(`internal/web/static/vendor/`)** — CDN에 의존하지 않고 CSP를 `script-src 'self'`로 엄격하게 두기 위해. npm 레지스트리에서 받아 `dist.integrity`(sha512)로 검증했고 라이선스 파일(0BSD, MIT)을 같이 뒀다. Chart.js는 출시 열흘 된 4.6.1 대신 1년 넘은 4.5.1을 골랐다 — 올리려면 같은 폴더의 파일과 템플릿의 파일명을 바꾼다.
- **인라인 스크립트 금지(CSP), 차트 데이터는 `<script type="application/json">`으로 전달** — XSS 방어. 테마는 `theme.js`를 `<head>`에서 먼저 실행해 깜빡임 방지. 인라인 style은 진행 막대 너비 때문에 허용(`style-src 'unsafe-inline'`).
- **htmx 요청의 CSRF는 `<body hx-headers>`로 헤더에 싣고, 일반 폼은 숨김 필드 `csrf_token`** — Phase 4의 세션 CSRF 검사와 같은 토큰.
- **POST 후 303 리다이렉트 + `?ok=`/`?err=` 안내** — 새로고침 때 재전송 방지.
- **분류 삭제 대신 "보관"** — Phase 3 결정과 같음(관리 화면).
- **설정 목록 입력은 줄바꿈 또는 쉼표 구분 텍스트** — 별도 편집 UI 없이 빠르게 고칠 수 있게.
- **확인 필요 큐는 오래된 것부터 한 건씩, 분류 버튼 = 저장 + 다음 건** — "한 건씩 빠르게" 요구. 가맹점이 있으면 "항상" 체크가 기본 켜짐. 들어온 돈이면 특수 분류(수입·정산 등)를 먼저 보여주고 정산이면 되돌릴 분류를 고를 수 있다.
- **거래 목록의 "항상" 체크박스는 분류를 고를 때만 전송(`hx-trigger="change from:find select"`)** — 체크만 했을 때 빈 분류로 저장 시도가 나가지 않게.
- **분석 일별 추이에서 이번 기간의 미래 날짜는 비움** — 0으로 그리면 "지출 없음"처럼 보여서.
- **다크 모드: 시스템 설정을 따르고, 상단 버튼으로 바꾸면 브라우저에 기억(localStorage)** — 저장소를 못 쓰면 시스템 설정.
- **보안 헤더**: CSP, `X-Frame-Options: DENY`, `nosniff`, `Referrer-Policy: same-origin`, 화면은 `Cache-Control: no-store`.

## Phase 6

- **위젯은 캐시만 읽어서 그리고, 갱신은 별도(`WidgetRefresher`)** — Glance의 `provideGlance`에서 네트워크를 쓰면 느리고 실패 시 빈 위젯이 된다. 마지막 요약을 SharedPreferences에 JSON으로 저장(앱 백업 제외 대상).
- **갱신 시점: 이벤트 전송 성공 직후(`AfterUpload`) + WorkManager 30분 주기 + 앱 홈 진입 + 앱에서 분류한 직후** — "결제 직후 즉시 갱신"(R2). 시스템 `updatePeriodMillis`는 0(배터리).
- **위젯 크기 3단계(SizeMode.Responsive)**: 작게(총 남은 돈 + 분류 2개, 금액은 "12만"처럼 줄임), 중간(분류 3 + 최근 결제 3), 크게(분류 5 + 최근 결제 4) — `core/WidgetModel.kt`. 기준 크기는 `WidgetLayout.forSize`.
- **위젯 "상위 분류" = 예산이 있는 분류 중 사용률 높은 순** — 가장 신경 써야 할 분류가 먼저 보이게. 지출이 많은 순으로 바꾸려면 `WidgetModel.topCategories`.
- **위젯 "최근 결제" = 나간 돈 + 확인 필요 건** — 확정된 입금(급여 등)은 "결제"가 아니라서 뺀다.
- **위젯 탭 동작은 명시적 Intent + 항목별 data URI** — 여러 클릭 영역의 PendingIntent가 섞이지 않게. 확인 필요 결제는 해당 거래 분류 대화상자를 바로 연다.
- **앱 화면은 요약·확인 필요·최근 거래 위주, 세부 수정은 PC 웹** — CLAUDE.md "앱 화면: 이번 달 요약, 확인 필요 처리, 최근 거래". 기존 상태 화면은 "수집 상태"로 옮겼다.
- **ViewModel·DI 없이 화면에서 직접 API 호출(Dispatchers.IO) + 저장소 객체** — 개인용 소규모 앱이라 구조를 단순하게 유지.

## Phase 7

- **예산 알림은 앱이 판단(서버 푸시 없음)** — 요약을 받을 때마다(전송 직후, 30분 주기, 앱 홈) 80% 도달·초과를 확인한다. FCM 같은 외부 푸시 서비스와 키가 필요 없다 — `core/BudgetAlerts.kt`, `notify/BudgetAlertNotifier.kt`.
- **알림은 기간·분류·단계(80/100)마다 한 번, 초과를 처음 보면 80%는 건너뜀** — 같은 알림 반복 방지. 판단 기준은 정산을 뺀 사용률(서버의 `ratio`)과 남은 돈 < 0.
- **예산 알림 채널은 IMPORTANCE_LOW + 소리·진동 없음 + `setSilent(true)`, 설정에서 끄기** — "기본은 무음이고 끌 수 있게". 알림 채널 설정에서 사용자가 소리를 켤 수도 있다.
- **월간 리포트 기본 = 지난 기간** — "지난달 요약과 분류별 증감". 화살표로 다른 달도 본다. 내용: 지출·예산 사용률·수입·일평균, 예산 초과 분류, 분류별 증감, 많이 쓴 가맹점 7곳 — `api/reports.go`, `/report`, `GET /v1/reports/monthly`.
- **대사 도우미 = 출처별 확정 지출 + 제외 내역(사유·출처별) + 최근 6개 기간 출처별 표 + 출처·성격별 합계** — 카드 명세서와 차이가 나면 "뺀 거래" 표(중복·취소·카드대금·충전)로 설명할 수 있게 — `/reconcile`, `GET /v1/reconcile`.
- **백업은 `pg_dump --format=custom`, 기본으로 아무것도 지우지 않음(`KEEP_DAYS`를 줄 때만 정리)** — 데이터 삭제 금지 원칙. 백업 직후 `pg_restore --list`로 검증.
- **복구 스크립트는 `--yes` 없으면 계획만 출력, 새 빈 DB에 복구 후 전환 권장** — 덮어쓰기 사고 방지.

## 최종 검토 (Android 코드 리뷰 반영)

앱 모듈을 빌드할 수 없어서 별도 검토를 한 번 돌렸다(Glance 1.1.0 API 시그니처 대조, Kotlin 2.1 문법 확인). 컴파일 실패 확정 항목은 없었고 아래를 고쳤다.

- **위젯이 갱신 직후 옛 숫자를 그리는 문제** — Glance는 실행 중인 `provideGlance`를 `updateAll`로 다시 시작하지 않는다. 캐시 상태를 `collectAsState`로 구독하게 바꿨다.
- **큰 위젯에서 행이 잘리는 문제** — Glance 컨테이너는 직접 자식 10개까지만 그린다. 분류·최근 결제를 각각 `Column`으로 묶었다.
- **평문 HTTP 개발 서버 접속 불가** — network security config 추가(위 Phase 2 항목).
- **"최근 알림"에 앱 이름 대신 패키지명이 보이는 문제** — Android 11+ 패키지 가시성. 런처 앱 `<queries>` 선언 추가.
- 회전 후 위젯 인텐트 재처리 방지, 코루틴 취소 예외를 삼키지 않기, Room Flow `remember`, `material-icons-core`·`lifecycle-runtime-compose` 의존성 명시, `LocalLifecycleOwner`를 새 위치로.
- **카카오 SDK API(`ClientError.reason/msg`, `loginWithKakaoTalk(context, callback=)` 등)는 저장소 접근이 막혀 확인하지 못했다**(검토자 확신도 85~90%). 첫 빌드에서 오류가 나면 이 부분부터 본다.

## 디자인: modo 스타일 적용 (사용자 요청)

사용자의 다른 앱 [modo](https://github.com/yuntyu01/modo)의 디자인 언어를 웹·앱·위젯에 맞췄다. 기준값은 modo의 `app/src/ui/theme.ts`(테마 토큰), `primitives.tsx`·`BottomNav.tsx`(모서리·하단바), `docs/screenshots`에서 가져왔다.

- **색**: 강조색 modo 대표색 `#85ADE2`. 다크는 검정 바탕 + 위쪽 남색 그라데이션(`#051324` → `#020910` → 검정, 화면 32%·68% 지점), 카드 `#1A1E25`(80% 불투명), 하단바 `#161A20`. 라이트는 `#F7F9FC` 바탕 + 옅은 하늘빛 그라데이션(`#E6F0FD`, 18%·42%), 흰 카드, 입력창 `#EEF2F8`. 글자·보조 글자색도 modo 값 그대로 — 웹 `static/app.css`의 `:root` 토큰, 앱 `ui/Theme.kt`의 `ModoColors`.
- **모양**: 테두리·그림자 없이 바탕색 차이로만 구분, 카드 16, 묶음 목록 14, 입력칸 12, 버튼·검색창·하단바는 알약 모양.
- **글꼴**: Pretendard(SIL OFL 1.1). 웹은 modo처럼 글자 범위별 woff2(400·600·700, 약 4MB, 필요한 글자만 내려받음), 앱은 OTF 3종(약 4.7MB). 라이선스 파일을 함께 넣었다 — 웹 `static/fonts/Pretendard-LICENSE.txt`, 앱 `assets/licenses/`. 용량이 부담되면 굵기를 줄이거나 시스템 글꼴로 되돌린다(`--font`, `Pretendard` FontFamily).
- **구성**: 큰 굵은 제목("내 가계부"), 글자 탭(선택: 굵은 본문색, 나머지: 흐린 회색), 휴대폰 폭(웹 640px 이하·앱)에서는 화면 아래 떠 있는 알약 하단바(선택 칸은 옅은 강조색 알약). 웹 휴대폰 폭에서 탭에 없는 화면은 "더보기"(`/more`) 묶음 목록으로.
- **modo와 다르게 한 것**
  - 강조색 위 글자는 어두운 색(`#0B0C0E`): modo는 체크 아이콘만 흰색으로 올리지만, 이 앱은 글자 버튼이 많아 흰 글자는 대비가 2.3:1로 낮다.
  - 라이트 모드에서 글자로 쓰는 강조색은 한 단계 진하게(`#3F78C2`), 빨강도 `#E5484D`: 흰 바탕 대비 확보. 채우기(막대·스위치)는 `#85ADE2` 그대로.
  - 예산 경고색(80%)은 modo에 없어서 modo 테마 팔레트의 노랑(`#F5C451`)을 썼다.
  - 앱의 동적 색(Material You)은 끄고 modo 색을 고정했다.
  - modo 로고·아이콘은 가져오지 않았다(다른 앱이라).
- **차트 색**: modo 테마색 목록(`#85ADE2`, `#FF7F6B`, `#5FD38D`, `#F5C451`, `#B79CFF` …) 순서.
- **좁은 칸 금액**: 100만 원 이상만 "320만"처럼 줄인다(웹·앱 같은 규칙).

## 화면 단순화와 버그 수정 (사용자 요청, 2026-11-02)

- **거래 행은 접어 두고 눌러서 수정** — 모든 행에 수정 항목이 펼쳐져 있으면 목록을 읽기 어렵다. 방금 고친 행만 펼친 채로 돌아온다 — `tx-row`의 `<details>`를 없애면 예전처럼 펼쳐진다.
- **거래 목록은 한 페이지 50건** — 200건을 한 번에 그리면 스크롤이 너무 길다 — `web/pages.go`의 `f.Limit`.
- **홈 분류는 초과한 것부터, 사용률 순. 5개까지 펼치고 나머지는 접음** — 신경 쓸 분류를 먼저 보고 최근 거래까지 금방 닿게 한다. 초과·80% 이상은 5개가 넘어도 항상 보이고, 안 쓴 분류는 항상 접는다. 웹과 앱이 같은 규칙 — 개수는 `dashboardTopCategories`(웹), `WidgetModel.homeCategories`(앱).
- **관리 화면은 탭으로 나눔** — 한 페이지에 다 있으면 개발 도구처럼 보인다. 분류의 표시 순서, 규칙의 비교 방식·우선순위, 설정의 세부 값은 접어 뒀다 — `web/templates/admin.html`.
- **화면에는 쉬운 말** — 대사 → 명세서 대조, 출처 → 결제 수단, 규칙 없음 → 처음 보는 가맹점, `kb` → KB국민카드, `2026-10` → 10월(올해가 아니면 연도 포함), 내 계좌 설정 값은 `투자`·`이체`(`investment`·`own_transfer`도 계속 받음). API·DB 값은 그대로 — 이름은 `web/labels.go`, 앱은 `ReviewScreen.kt`.
- **휴대폰 하단바에 글자 표시** — 아이콘만으로는 뜻을 알기 어렵다 — `layout.html`의 `.bottom-nav`, 앱은 `ModoBottomBar`.
- **규칙 방식 `prefix`(이름으로 시작) 추가, 기본 규칙 CU·KT·SKT에 적용** — `exact`라서 "CU 선릉점"을 놓쳤고, `contains`는 CUBE 같은 다른 이름까지 잡는다. 바로 뒤에 영문·숫자·`&`가 오면 다른 이름으로 본다(KTX, KT&G). "KT위즈"처럼 한글이 이어지면 맞는 것으로 친다 — 마이그레이션 00004. 이미 "확인 필요"로 들어온 거래는 `myhome reprocess`로 다시 처리.
- **범용 파서는 가맹점을 못 찾으면 알림 제목을 씀, 문장 조각은 가맹점으로 안 씀** — "주문하신 상품의 가" 같은 이름이 만들어졌다. 실제 쿠팡 알림 문구는 아직 확인하지 못했다 — 실데이터 보정 때 다시 본다(`docs/PARSERS.md`).
- **DB 헬스체크를 TCP로** — postgres 이미지의 초기화용 임시 서버는 유닉스 소켓만 열어서, 소켓으로 확인하면 초기화가 끝나기 전에 healthy가 되고 서버가 연결에 실패했다 — `docker-compose.yml`.
- **템플릿은 버퍼에 그린 뒤 보냄** — 중간에 실패하면 잘린 화면이 200으로 나갔다. 실제로 규칙 표의 `*string` 비교 오류 때문에 관리 화면의 설정·해석 실패·기기 섹션이 보이지 않았는데 테스트가 잡지 못했다. 이제 500으로 알리고 테스트에 걸린다 — `web.go`의 `writeTemplate`.
- **앱 Scaffold에 글자색을 직접 줌** — 바탕을 투명하게 두면 Scaffold가 글자색을 정하지 못해 검정이 되고, 다크 모드에서 글자가 안 보였다 — `MainActivity.kt`의 `contentColor`.
- **위젯 중간 크기는 최근 결제 2건** — 기본 크기(세로 2칸)에 3건을 넣으면 마지막 줄이 잘렸다. 앱 홈의 최근 거래도 웹과 같이 5건 — `WidgetLayout.MEDIUM`, `HomeScreen.kt`.
- **최근 알림 목록은 묶음 요약을 빼고, 게시 시각이 마지막보다 늦은 알림만 셈** — 알림이 쌓이면 안드로이드가 제목 없는 묶음 요약을 따로 만들고 이미 받은 알림을 다시 보낸다. 그대로 세면 건수가 부풀고 마지막 제목이 빈칸이 됐다. 마지막 시각도 받은 시각 대신 알림의 게시 시각으로 바꿨다 — `CaptureRules.shouldList`, `RecentAppDao.record`.
- **무채색 바탕에 색은 주의가 필요한 금액에만(사용자 요청, modo와 다른 점)** — 색이 따로 놀고 빨간 막대가 화면을 경고처럼 보이게 했고, 파랑·빨강 조합도 사용자가 원하지 않았다. 그라데이션을 없애고 바탕 `#151515`/`#F6F6F5`, 카드 `#222222`/`#FFFFFF`. 평소 막대·버튼·선택 상태·수입은 무채색 `#CFCFCF`/`#5E5E5E`. 예산 80% 이상과 확인 필요는 라벤더 `#AFA5D4`/`#6F6698`(라이트 막대 `#8F85B9`), 초과는 살구 `#D89777`/`#A15A36`(라이트 막대 `#C07B58`). 처음 지침의 "초과면 빨강"을 대신한다. 차트만 분류 구분을 위해 여러 색을 쓰되 빨강·파랑 계열은 뺐다. 위 "디자인" 항목의 색·그라데이션 값을 대신한다 — 웹 `app.css`, 앱 `Theme.kt`, 위젯 `SummaryWidget.kt`.
- **KoDeploy 배포용 Dockerfile을 저장소 루트에 따로 둠** — KoDeploy는 저장소의 Dockerfile을 감지하고 없으면 nixpacks로 생성한다. 하위 폴더 지정이 가능한지 확인하지 못해 루트에 둔다. `server/Dockerfile`은 로컬 compose용(캐시 마운트, CA 시크릿)으로 유지한다. Go 버전은 두 파일을 같이 올린다 — 합치려면 compose의 `context`와 Makefile을 루트로 옮기고 `server/Dockerfile`, `server/.dockerignore`를 삭제.
- **배포 이미지는 `$BUILDPLATFORM`에서 크로스 컴파일** — KoDeploy 클러스터가 OCI ARM이다. 빌더 아키텍처와 달라도 에뮬레이션 없이 빌드된다 — 루트 `Dockerfile`의 `FROM --platform`, `TARGETARCH`.
- **배포 Dockerfile에는 `# syntax=`와 캐시 마운트를 쓰지 않음** — 프런트엔드 이미지를 받다가 Docker Hub 요청 제한에 걸릴 일을 줄이고, 다른 사용자와 빌더를 공유할 경우 모듈 캐시가 오염될 위험을 피한다. 그만큼 빌드는 느리다 — 루트 `Dockerfile`.
- **배포 이미지도 `scratch` 유지** — 위 런타임 이미지 결정과 같다. KoDeploy 터미널이 셸을 요구하는지 확인하지 못했다. 필요하면 `FROM alpine:3`으로 교체 — `docs/DEPLOY.md`.
