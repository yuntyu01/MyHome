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

> 2026-11-08에 카카오 로그인을 없애고 Cloudflare Access + 앱 연결 코드로 바꿨다. 아래는 그때까지의 기록이다. 지금 방식은 맨 아래 「로그인: Cloudflare Access」.

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
- **VM은 매일 04:30 백업, 버킷에는 쓰기 전용 주소만 (2026-11-08)** - `myhome-backup.timer`가 덤프를 VM 디스크(14일)와 OCI Object Storage(`db/` 60일, 버전 관리로 덮어쓴 옛 버전 30일)에, 끼니 사진은 `photos/`에 한 번씩 올린다. VM에는 쓰기 전용 PAR만 둬서 VM이 털려도 백업을 읽을 수 없다. 실패하면 앱에 "서버 백업 실패" 알림. 버킷은 kodeploy `terraform/oci-infra/home_backup.tf`(사람이 apply) - `docs/BACKUP.md` 「VM」.
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

## 자동매매 (TQ 이식, 2026-11-06)

- **TQ(Python)를 Go로 옮겨 같은 바이너리의 `myhome invest` 서브커맨드로** — 언어·DB·배포를 가계부와 하나로. 신호·계좌·주문·장부는 TQ와 같은 동작이고, 336행 판정 픽스처와 핵심 매매 시나리오 테스트를 그대로 옮겼다 — `server/internal/invest/`.
- **격리는 프로세스·OS 사용자·DB 역할로** (CLAUDE.md 「나중에」의 "가계부 서버에 매매 키를 두지 않는다"를 이렇게 지킨다) — 웹(`myhome serve`, 사용자 `myhome-web`)에는 토스 키가 없고 `invest_*`는 읽기만 한다. 매매(`myhome invest`, 사용자 `myhome-trader`)만 키를 갖는다. 같은 VM이라 루트 권한을 뺏기면 둘 다 노출된다 — 더 떼려면 매매를 별도 VM으로 옮기고 웹이 조회 API로 읽게 한다 — `deploy/home/`.
- **실행 위치는 OCI E2.1.Micro + 예약 공인 IP** (kodeploy 아님) — 토스는 등록한 IP에서만 호출을 받는다. 클러스터 NAT IP는 모든 테넌트가 같이 써서 허용목록이 의미가 없다. 예약 IP라 인스턴스를 다시 만들어도 유지된다 — kodeploy 저장소 `terraform/oci-infra/home.tf`.
- **예약 실행은 systemd timer** — kodeploy에는 사용자 CronJob이 없고, TQ의 명령 단위 구조(refresh·judge·trade·status)를 그대로 옮길 수 있다. 같은 유닛은 겹쳐 돌지 않고, 서로 다른 명령·손으로 친 명령은 advisory lock(7240002, 가계부 처리 잠금과 분리)이 막는다 — `deploy/home/systemd/`, `invest/store.Lock`.
- **판정 타이머에 1일을 넣음** (TQ는 25~31일) — 판정이 뉴욕 자정을 넘기면 다음 날 만회하는데, 월말이 31일이면 만회 날이 1일이다. 판정할 날인지는 `JudgmentDay`가 정한다 — 타이머만 바꾸면 된다.
- **금액·가격은 float64 / double precision** — 원 단위 bigint 규칙은 가계부 거래에만. TQ 산술과 비트 단위로 같아야 이식 대조(판정 336행, 원금 장부)가 성립한다 — `invest/account.go`, `00005_invest.sql`.
- **position·orders·pending은 jsonb 단일 문서 + 변경 이력 테이블** — TQ의 `state/*.json` 모양 그대로라 마이그레이션이 손실 없고, git log가 하던 감사 추적은 `invest_state_history`가 한다. 원금·이탈·적립·앱 밖 매수는 추가 전용 테이블 — 정정은 지우지 않고 `superseded_at`.
- **336행 픽스처를 실행 관문으로도** — TQ는 정기 작업 앞에 회귀 테스트를 돌렸다. 여기서는 judge·trade·execute가 DB 가격으로 직접 대조하고 어긋나면 멈춘다(마스터 오염 방어) — `invest.CheckFixture`.
- **시세는 야후 chart API를 표준 라이브러리로 직접 호출** — yfinance에 해당하는 Go 라이브러리가 없고, 토스 시세 API에는 신호 입력(S&P500·NDX)이 없다. 원종가(`quote.close`)와 거래소 시간대 날짜를 쓴다. 2026-11-06 실측 대조에서 SPX·ETF 오차 0, NDX 1.4e-7 — `invest/yahoo`.
- **토스 클라이언트는 표준 라이브러리만** — 토큰은 클라이언트당 1개만 유효해 0600 파일로 캐시해 재사용한다. 주문 POST는 재시도하지 않고, 결정적 `clientOrderId`가 이중 주문을 막는다 — `invest/toss`.
- **웹은 토스를 부르지 않는다** — 토큰이 1개뿐이라 두 프로세스가 각자 받으면 서로를 무효화한다. 화면의 보유 현황은 매매 작업이 DB에 남긴 상태를 보여준다.
- **매매 실패는 메일 대신 앱 알림 (사용자 결정, 2026-11-08)** - Gmail 앱 비밀번호를 VM에 두지 않으려고 메일을 뺐다(`MAIL_*`·`GMAIL_APP_PASSWORD`·`SMTP_*` 없음). invest 유닛이 실패하면 `OnFailure=`가 `myhome-alert@.service`(`myhome invest alert --unit`)를 불러 `invest_alerts`에 한 줄을 남기고, 앱이 위젯 갱신 주기(30분·결제 전송 직후)에 `GET /v1/invest/alerts?after=<id>`로 읽어 "자동매매 실패" 채널(소리 있음)로 띄운다. 그래서 알림은 최대 30분쯤 늦을 수 있고, 서버는 7일 안의 것만 준다. 푸시(FCM)는 Firebase 키를 하나 더 두게 돼서 안 썼다. VM 자체가 죽은 것은 `status` 성공 때 보내는 `INVEST_HEARTBEAT_URL` 신호가 끊기는 것으로 외부가 잡는다. 메일을 다시 쓰려면 git 기록에서 메일판 `invest/run/alert.go`를 본다.
- **버린 것** — X·Threads 공개 기록과 차트 발행(pipeline/, x/), KIS 어댑터(교훈만 주석으로), Cloudflare Worker 뷰어와 GitHub Actions 우회책(러너 IP 게이트, workflow_run). **보관한 것** — 백테스트·마스터·환율 원본 xlsx를 참고용으로 `docs/tq/`에. 백테스트 결과는 화면에 넣지 않는다(TQ 규칙: 평균 하나로 판단하게 만든다).
- **마이그레이션은 `myhome invest import-tq` 1회** — 빈 DB에만 쓰고, 쓰기 전에 336행 대조와 원금 장부 대조, 쓴 뒤에 다시 읽어 원본 대조. 가격은 `scripts/tq_export.py`로 parquet → CSV.

## 하단바와 투자 칸 (사용자 요청, 2026-11-08)

- **홈은 예산 요약 그대로, 투자는 칸을 따로** — 이 앱은 알림으로 모아 분류하고 돈을 아껴 쓰려고 만든다. 홈은 남은 돈·분류별 막대·확인 필요·최근 거래를 유지하고, 자동매매는 가계부와 섞지 않고 투자 칸 하나로 뺐다.
- **하단바 5칸: 앱은 홈·거래·확인·투자·설정, 휴대폰 웹은 홈·거래·확인·투자·더보기** — 6칸이면 한 칸이 너무 좁다. 웹의 분석은 가끔 보는 화면이라 더보기 맨 위로 옮겼다(분석 화면에서는 더보기 칸이 선택됨). 앱 칸은 고정 폭 대신 똑같이 나눈다 — 웹 `layout.html`의 `bottom-nav`, `more.html`, 앱 `ModoBottomBar`.
- **앱 투자 칸은 `GET /v1/invest`·`/v1/invest/value`를 읽기만** — 웹 투자 화면과 같은 함수(`loadInvestStatus`·`loadInvestValue`)의 값을 JSON으로 준다. 기기 토큰으로 읽을 수 있게 됐지만 쓰기 라우트는 없고, 토스도 부르지 않는다(위 「웹은 토스를 부르지 않는다」와 같음). 첫 화면에는 시장 숫자가 없고 '평가 보기'에만 있다 — `web/invest.go`의 `apiInvest`, 앱 `InvestScreen.kt`.

## 건강 (핏두잉 이식, 2026-11-08)

- **핏두잉 서버의 개인용 부분을 이 서버로 옮김** - 사용자 1명이 가계부와 식단·헬스를 한 서버, 한 앱으로 쓰려고. 원본은 `/root/study/fitdoing/server`(gorm, 다중 사용자)이고 여기서는 pgx·sqlc·goose, `user_id` 없는 표로 다시 썼다. 새 패키지는 `internal/ai`(Gemini), `internal/health`(설정·목표·끼니·헬스·휴식권·화면 계산), `internal/health/foods`(바코드). 장소 검색(`internal/health/places`)은 2026-11-08에 뺐다(아래 「헬스장 위치 기능 제거」). 다시 떼려면 이 패키지들과 `00006_health.sql`을 따로 서비스로 옮기고 `server.New`의 `newHealth` 한 줄을 지운다.
- **JSON·쿼리 이름은 핏두잉 그대로(camelCase)** - 가계부 API(snake_case)와 다르지만, 앱 쪽이 핏두잉 서버 구조체와 앱 클라이언트를 보고 모델을 만든다. 뺀 필드만 지웠고 이름은 하나도 바꾸지 않았다. 바꾸려면 `internal/health`의 json 태그와 앱 `HealthModels.kt`를 같이 고친다.
- **오류 본문은 가계부 모양 `{"error": 메시지}`에 핏두잉 코드 `"code"`를 더함** - 앱이 `COUPON_EMPTY`·`PHOTO_TOO_LARGE`·`TARGET_BELOW_MINIMUM` 같은 경우를 상태 코드만으로 구분할 수 없다. 메시지는 핏두잉의 한국어 문구를 그대로 써서 화면에 바로 보여줄 수 있다 - `health/errors.go`.
- **뺀 것** - 핏두잉의 로그인·회원·기기(가계부 로그인과 기기 토큰을 쓴다), 친구·피드·댓글·공유 페이지, 관리자 화면과 사용자별 AI 모델 지정, 잠금 엔진·강도(mild/normal/spicy)·치팅권·비상 해제·허용 앱·잠금 시간, 닉네임·테마·강조색·그라데이션·친구 공유 기본값, 사진 백업(R2)과 서버 사진 저장, 수면 기록(`sleep_logs`), 앱 동기화·복원(`/api/sync/*`). 음식 선택 방식·알레르기·단식도 뺐다(서버에서 쓰는 곳이 없고 핏두잉도 저장만 했다). 다시 필요하면 핏두잉 저장소에서 해당 패키지를 옮긴다.
- **응답에서 함께 빠진 필드** - 달력 `locked`·`sleepLocked`·`summary.lockedDays`, 하루 카드 `locked`·`lockCount`, 끼니 `shareFriends`·`cheat`, 끼니 저장 응답 `cheatLeft`, 휴식권 요약 `cheat`·`emergency`, 설정 응답 `pendingMode`·`pendingFrom`. 요청에 이 필드가 와도 무시한다(모르는 필드 무시).
- **모든 `/v1/health` 경로는 `RequireAPI`(기기 토큰 또는 웹 세션 + CSRF)** - 핏두잉에서 공개였던 목표 추천도 인증 뒤로 옮겼다. 가계부 API와 같은 미들웨어라 기기 폐기도 그대로 먹힌다 - `server.go`.
- **사진 분석 경로만 본문 16MB** - 전역 `MAX_BODY_BYTES`(64KB)는 그대로 두고 `httpx.LimitBodyRoutes`로 `POST /v1/health/meals/analyze`만 연다. 사진 한 장 5MB, 한 번에 3장(핏두잉 값). 사진은 메모리에서만 읽고 버린다(배포 이미지에는 `/tmp`도 없다). 바꾸려면 `health.MaxPhotoBytes`, `ai.MaxPhotos`.
- **`AI_PROVIDER` 기본값은 "키가 있으면 gemini, 없으면 mock"** - 핏두잉은 mock이 기본이라 키를 넣고도 `AI_PROVIDER`를 빠뜨리면 가짜 결과가 나왔다. 키 없는 로컬·테스트는 mock으로 그대로 돈다. `gemini`인데 키가 없거나 모르는 값이면 서버가 시작하지 않는다. 모델은 `GEMINI_MODEL` 하나(기본 `gemini-3.5-flash-lite`, 핏두잉 기본값) - `config.go`.
- **`ai` 패키지는 호출부(`Gemini.GenerateJSON`)와 음식 분석을 나눔** - 다음 단계에서 같은 키로 가계부 가맹점 이름 분류를 붙일 수 있게. 프롬프트 조각(`Text`, `Inline`)과 응답 스키마 도우미(`Object`, `Enum` 등)만 쓰면 된다. 키는 헤더로만 보내고 연결 실패 에러에서 요청 주소를 지운다 - `ai/gemini.go`.
- **설정은 jsonb 한 행 + 날짜별 목표 표** - 사용자가 1명이고 핏두잉 앱 설정 모양을 그대로 쓰므로, 필드를 더할 때 스키마를 바꾸지 않아도 된다. 날짜별 목표는 날짜로 찾으니 표(`health_target_history`)로 둔다. 표 이름은 가계부 `settings`와 겹치지 않게 `health_` 접두 - `00006_health.sql`.
- **설정 PUT은 보낸 키만 바꿈(객체 안도 같음), `null`은 지움** - 핏두잉은 설정 전체를 받고 새 필드만 키가 없을 때 보존했다. 여기서는 저장된 값 위에 요청을 풀어서 앱이 일부만 보내도 다른 값이 지워지지 않는다. 핏두잉의 "구버전 앱 보호"(bulk 유지, 오래된 목표 사본 무시)와 강도 하향 지연은 구버전 앱이 없어서 뺐다. 동기화 백업용 `PUT /api/sync/settings`도 없다(서버가 원본) - `health/service.go`의 `PutSettings`.
- **목표 이력은 서버가 남김** - `targetHistory`를 보내면 그대로 쓰고, 안 보내면 목표가 바뀐 경우 오늘(KST)부터 기록한다(첫 변경이면 예전 목표를 2000-01-01부터). 아직 저장한 적 없는 첫 PUT은 기본 목표를 "예전 목표"로 남기지 않는다. 1200kcal 하한은 PUT에서만 검사하고 끼니 기록·조회는 검사하지 않는다(핏두잉 정책 그대로).
- **`photoKeepDays`(폰 사진 보관 일수)를 설정에 둠** - 핏두잉에서는 폰에만 있던 값인데, 여기서는 설정이 서버에 있어서 앱을 다시 깔아도 남게 했다. `null`(계속)·30·90·180. 사진은 서버에도 있어서 폰에서만 지운다.
- **끼니 `id`는 bigserial이지만 JSON에서는 문자열** - 핏두잉은 uuid 문자열이었다. 앱 모델 타입이 바뀌지 않게 문자열로 준다. 같은 날 같은 번호를 다시 저장하면 id가 그대로다.
- **끼니 `portion`을 빼면 1(한 접시)** - 핏두잉은 필수였다. 앱이 기본값을 생략해도 400이 나지 않게. 값이 있으면 0.25·0.5·1·1.5·2만 받는다. `deadline: ""`도 받는다("기타"는 마감이 없다).
- **오늘 화면의 기기 시각은 `time`(핏두잉 이름)과 `now` 둘 다 받음** - 지시서와 핏두잉 이름이 달라서 둘 다 연다. 둘 다 있으면 `time`.
- **휴식권은 같은 날 두 번 차감하지 않고, 이미 인증한 날은 409 `ALREADY_CHECKED_IN`** - 핏두잉은 같은 날 다시 쓰면 또 차감했고, 헬스에 간 날을 휴식으로 덮어 방문 기록이 사라졌다. 쉰 날에 나중에 헬스 인증을 하면 방문으로 바뀌고 휴식권은 돌려주지 않는다(핏두잉과 같음). 차감은 advisory lock 7240003으로 하나씩 한다(가계부 7240001, 자동매매 7240002와 분리) - `health/service.go`의 `UseRest`.
- **휴식권 경로의 날짜는 생략 가능(오늘 KST)** - `GET /v1/health/rest`는 `?date=`, `POST`는 `?date=` 또는 본문 `{"date"}`. 핏두잉(`/api/coupons`)은 `?date=`가 필수였다. 사용 기록(`uses`)에는 쉰 날 `date`를 더했다. 다른 화면 API의 `date`는 핏두잉처럼 필수다.
- (같은 날 바뀜: 아래 「헬스장 위치로 자동 출석」) **헬스장 위치 기능 제거 (사용자 결정, 2026-11-08) - 인증은 사진·휴식권만** - 헬스장 장소 검색(`GET /v1/health/places/search`, `internal/health/places`, `KAKAO_REST_API_KEY`)과 위치 도착 인증을 뺐다. 이제 카카오는 서버 어디에도 없다. 설정의 `gym.name`·`address`·`lat`·`lng`·`radiusM`도 없다. 예전에 저장한 설정 문서에 그 키가 있어도 읽을 때 무시하고 다음 저장 때 빠진다. 예전 앱이 보내도 받기만 하고 저장하지 않는다. `POST /v1/health/gym/check-in`은 `verifiedBy: "photo"`만 받고 `"geofence"`는 400 `INVALID`. 예전 `geofence` 기록은 그대로 읽히고 방문으로 센다(하루 카드 `gymVerifiedBy`에 그대로 나옴). 그래서 DB의 CHECK 제약은 바꾸지 않았다. 다시 필요하면 핏두잉 저장소의 장소 검색을 옮기고 `Gym` 구조체와 검사를 되살린다 - `health/settings.go`, `health/handler.go`의 `checkIn`.
- **헬스장 위치로 자동 출석 (사용자 요청, 2026-11-08 다시 켬)** - 카카오 장소 검색 없이, 헬스장에 있을 때 "지금 위치를 헬스장으로"를 눌러 폰 GPS 좌표를 저장한다. Play 서비스 지오펜스(DWELL 5분, 반경 100·150·250m)가 알려 주면 그날 출석을 `geofence`로 보낸다. 좌표는 폰에만 둔다(`health/GymGeofence.kt`). 앱이 꺼져 있어도 알려면 위치 "항상 허용"(ACCESS_BACKGROUND_LOCATION)이 필요한데, Play가 위치 권한 선언(시연 영상)과 검토 없이는 비공개·내부 테스트 업로드도 받지 않았다(2026-11-08 CD 실패). 그래서 같은 날 위치 권한 선언(에뮬레이터 시연 영상)을 내고 다시 켰다(`BuildConfig.GYM_AUTO=true`). 켤 때는 늘 앱 안 안내 창("앱이 닫혀 있거나 사용 중이 아닐 때에도 위치 정보를 수집") → 동의 → "항상 허용" 순서이고, 위치를 저장해도 자동 출석은 꺼진 채로 둔다. "위치로 출석" 버튼(반경 + 위치 오차 최대 100m 안이면 `geofence`)은 자동 출석과 상관없이 늘 있다. 디버그 빌드는 머무는 시간이 30초(시연·확인용), 출시 빌드는 5분. 선언이 거절되면 매니페스트의 권한을 빼고 `GYM_AUTO`를 false로.
- (같은 날 바뀜: 아래 「바코드는 식약처 → Open Food Facts」) **바코드 조회는 키 둘 다 있을 때만, 없으면 503** - 식품안전나라 C005와 공공데이터포털 영양성분DB를 이어 부른다(핏두잉 그대로). 두 API는 키가 요청 주소에 들어가서, 연결 실패 에러에서 주소를 지워 로그에 키가 남지 않게 했다. 식품안전나라 주소가 `http://`인 것도 핏두잉 그대로다(https 지원 여부를 확인하지 못했다) - `health/foods/barcode.go`의 `defaultFoodSafety`.
- **바코드는 식약처 → Open Food Facts, 앱은 Google 코드 스캐너 (2026-11-08)** - 식약처 키 두 개는 회원가입·활용신청이 필요해 아직 없다. 키 없이 되는 Open Food Facts를 예비로 붙였다(보내는 것은 바코드 번호뿐, 신라면 같은 한국 제품도 있음). 식약처 키를 넣으면 그쪽을 먼저 보고, 제품만 알고 영양성분이 없으면 Open Food Facts 값으로 채운다 - `foods/openfoodfacts.go`. 앱은 `play-services-code-scanner`(카메라 권한 없이 Play 서비스 화면)로 EAN·UPC만 찍는다. 이 SDK는 진단 정보를 Google에 보낸다(개인정보처리방침·데이터 보안에 적음).

## 건강 앱 화면 (핏두잉 이식, 2026-11-08)

- **하단바는 홈·거래·건강·투자·설정, 확인 칸은 뺌** - 6칸은 좁다. 확인할 거래가 있으면 거래 칸에 점이 뜨고, 거래 화면 위 띠·홈 카드·위젯에서 확인 화면으로 간다. 되돌리려면 `MainActivity.kt`의 `ModoBottomBar` 목록과 `Screen.topLevel`.
- **건강 화면은 서버가 만든 값(오늘·주간·하루·달력)을 그대로 그림** - 핏두잉 앱은 폰 DB에서 계산했지만, 여기서는 서버가 진짜 값이라 계산을 한 곳에 둔다. 그래서 네트워크가 없으면 건강 화면이 뜨지 않는다. 끼니 일정·먹은 양·알림 일정 계산만 핏두잉 규칙 그대로 `core/HealthPlan.kt`에 옮겼다(테스트 8개).
- (같은 날 바뀜: 아래 「끼니 사진은 VM 디스크에도」) **끼니 사진은 폰에만, 서버에는 분석할 때만 보냄** - 긴 변 1024px·JPEG 85로 줄여 보낸다. 폰 `filesDir/meal_photos/`에 끼니별로 두고 보관 기간(계속·30·90·180일)이 지나면 지운다 - `health/MealPhotos.kt`.
- **끼니 사진은 VM 디스크에도 (사용자 결정, 2026-11-08)** - 기록을 저장하면 앱이 그 끼니 사진을 `PUT /v1/health/meals/photos`로 올리고(바꾸기), 서버는 `MEAL_PHOTO_DIR`(VM은 `/var/lib/myhome/meal_photos`)에 파일을, `meal_photos`(00010)에 위치만 둔다. 폰 사진은 캐시라 보관 기간이 지나 지워지면 기록 화면에서 다시 받는다. 끼니를 지우면 사진도 지운다. 올리기 실패는 기억했다가 위젯 갱신 주기에 다시 올린다. 백업은 `myhome-backup`이 버킷 `photos/`에 한 번씩 올린다.
- **사진은 시스템 카메라 앱 + 앨범 선택** - CameraX를 더하지 않으려고. 핏두잉은 잠금 우회를 막으려 앱 카메라만 썼는데, 잠금이 없으므로 앨범도 연다. 바코드는 Google 코드 스캐너로 찍는다.
- (2026-11-08 위치 기능 제거로 없어짐, 「건강」의 「헬스장 위치 기능 제거」) **헬스 도착은 버튼을 누를 때 위치를 한 번 읽어 거리만 판단** - 좌표는 서버에 보내지 않고 "위치로 확인"만 보낸다. 거리 ≤ 반경 + 위치 정확도(최대 100m)면 도착. 사진 인증 사진은 보내지도 남기지도 않는다. 지오펜스(백그라운드 위치)는 쓰지 않는다 - `HealthGymScreen.kt`.
- **알림은 AlarmManager에 다음 것 하나만 걸고, 울리면 그다음을 검** - 이미 찍은 끼니를 빼야 해서 매일 반복 알림을 쓰지 않는다(핏두잉과 같은 이유). (2026-11-08 바뀜: 아래 「건강 신뢰성 고침」의 정확한 알람) 정확한 알람 권한 없이 `setAndAllowWhileIdle`이라 절전 중엔 몇 분 늦을 수 있다. 찍은 끼니는 폰 사본(최근 2주)으로 판단해 알림 수신기가 네트워크를 쓰지 않는다. 재부팅·시각 변경 때 다시 건다 - `health/Reminders.kt`.
- **설정 저장은 이 화면이 다루는 키만 보냄** - 받은 설정을 통째로 돌려보내면 `targetHistory`도 같이 가서 목표가 바뀐 날이 기록되지 않는다(지난날이 새 목표로 판정됨).
- **영양 목표는 성별·키·몸무게·나이·활동 4단계로 추천받거나 직접 입력** - 핏두잉의 활동 상세 입력과 비율 직접 설정 화면은 옮기지 않았다. 추천 방식은 감량·유지면 balanced, 린매스업·벌크업이면 training. 1,200kcal 미만은 앱과 서버 둘 다 막는다.

## 로그인: Cloudflare Access (사용자 결정, 2026-11-08)

- **카카오 로그인을 완전히 없애고 웹은 Cloudflare Access로** - 운영은 OCI VM 한 대를 Cloudflare Tunnel 뒤에 둔다(들어오는 포트 없음, `myhome.kodeploy.com`). 로그인 화면·OAuth 콜백·카카오 키를 서버가 들고 있을 필요가 없어진다. 허용 계정은 카카오 회원번호 대신 구글 이메일(`ALLOWED_EMAILS`, 대소문자 무시) - 설정은 `docs/ACCESS_SETUP.md`. 되돌리려면 git 기록에서 `auth/kakao.go`와 마이그레이션 00007 이전 스키마를 본다.
- **서버도 Access JWT를 직접 검증(안전망)** - Access 애플리케이션이 빠지거나 경로가 잘못돼 요청이 그대로 들어와도 웹 화면을 열지 않게. 모든 웹 화면 요청에 `Cf-Access-Jwt-Assertion`(없으면 `CF_Authorization` 쿠키)이 있어야 하고 RS256 서명, `aud`(문자열·배열 둘 다), `iss = https://<팀>`, `exp`·`nbf`(1분 오차), 허용 이메일을 본다. 키는 `https://<팀>/cdn-cgi/access/certs`에서 받아 캐시하고, 모르는 `kid`면 다시 받되 1분에 한 번(실패 뒤에는 5초), 10초 타임아웃. 표준 라이브러리만 써서 새 의존성이 없다 - `auth/access.go`. 간격은 `AccessVerifier.MinRefetch`.
- **JWT가 없거나 틀리면 403 안내 화면, 키를 못 받으면 503** - 사용자 잘못과 서버 문제를 나눠 보이게. htmx 요청은 401 + `HX-Redirect: /`(Access 로그인으로 다시). 사유는 서버 로그 `Access JWT 거부`에만 남기고 토큰은 남기지 않는다 - `auth/session.go`의 `accessUser`.
- **서버 세션은 CSRF용으로 남기고 JWT 이메일에 묶음** - htmx·폼의 CSRF 토큰을 그대로 쓰려고. 첫 요청에 세션을 만들고, 세션 이메일이 JWT 이메일과 다르면 옛 세션을 지우고 새로 만든다. 웹 로그인 유지 시간은 Access 세션 시간이 정한다(서버 세션 `SESSION_TTL`은 CSRF용) - `sessions.email`.
- **세션 쿠키로 `/v1` API를 부르는 것은 그대로 허용(Access JWT 안 봄)** - 세션은 JWT를 확인한 뒤에만 생기고, `ALLOWED_EMAILS`에서 빠진 이메일의 세션은 무시한다. 웹 화면 안의 JSON 호출이 Access Bypass 경로(`/v1/*`)를 타서 JWT가 없을 수 있기 때문이다 - `Authenticator.session`.
- **운영에서 Access 설정이 빠지면 `serve`가 시작하지 않음** - `APP_ENV`가 `development`가 아니면 `CF_ACCESS_TEAM_DOMAIN`·`CF_ACCESS_AUD`·`ALLOWED_EMAILS`가 다 있어야 한다. 검사는 `serve`에만 있다: `myhome invest`(trader.env)·`migrate`·`token`은 웹 설정 없이 돈다 - `config.ValidateServe`. 혹시 검사를 건너뛰어도 웹 화면은 503으로 닫힌다.
- **개발용 로그인은 `APP_ENV=development` + `DEV_LOGIN_EMAIL`(허용 목록 안) + Access 미설정일 때만** - 로컬에서 Cloudflare 없이 웹을 보려고. 운영 설정에 `DEV_LOGIN_EMAIL`을 넣으면 시작을 거부한다. `/login`은 운영에서 "/"로 가는 안내만 한다.
- **로그아웃은 서버 세션을 지우고 `/cdn-cgi/access/logout`으로** - Access 로그인 상태(Access 쿠키)까지 끊으려고. Access가 없으면 `/login`.
- **앱 API(`/v1/*`)는 Access Bypass, 기기 토큰은 만료 없이 해제만** - 앱이 Access 로그인 화면을 다룰 수 없다. 서비스 토큰은 지금 쓰지 않는다.
- **앱 첫 연결은 1회용 코드 + PKCE(S256)** - 앱이 브라우저로 `GET /app/link?challenge=&state=`를 열고, Access 로그인 뒤 서버가 `myhome://auth?code=&state=`로 앱을 부르면, 앱이 `POST /v1/auth/exchange {code, verifier, device_name}`로 기기 토큰을 받는다. URL에는 기기 토큰을 절대 싣지 않고, 코드를 가로챈 다른 앱은 verifier가 없어 못 쓴다. 돌아갈 주소는 고정(`auth.AppRedirectURL`)이고 요청에서 받지 않는다. 화면은 meta refresh와 "앱으로 돌아가기" 버튼 둘 다(인라인 스크립트 없음) - `auth/applink.go`, `web/templates/applink.html`.
- **코드는 5분, 해시만 저장, 먼저 소모하고 나서 verifier 확인** - 한 번 틀린 코드도 다시 못 쓰게(맞혀 보기 방지). 없음·만료·사용됨·verifier 불일치는 같은 401 메시지. 형식이 틀린 요청(400)은 코드를 소모하지 않는다. 만료 코드는 발급·교환 때 지운다. 교환은 전역 IP 제한기만 거친다(코드가 256비트 난수라 맞혀 볼 수 없다) - `app_link_codes`, `LinkCodeTTL`.
- **남은 위험: 폰의 악성 앱이 직접 연결을 시작하는 경우** - `myhome://`를 가로채는 앱이 자기 challenge로 `/app/link`를 열면, 브라우저가 이미 Access에 로그인돼 있을 때 확인 없이 코드가 그 앱으로 갈 수 있다(PKCE는 남이 시작한 흐름의 코드를 가로채는 것만 막는다). 사용자 1명·개인 폰이라 받아들였다. 막으려면 연결 화면에서 바로 넘기지 말고 "이 폰을 연결" 버튼(POST + CSRF)을 누르게 하거나, 사용자 정의 스킴 대신 App Links(https 검증 도메인)로 바꾼다.
- **`state`는 서버가 저장하지 않고 그대로 돌려줌** - 앱이 자기가 보낸 값인지 확인하는 용도라서. base64url 16~64자만 받는다.
- **기기 이름은 앞뒤 공백을 떼고 60자로 자름, 비면 `android-YYYYMMDD`(KST)** - 예전에는 60자를 넘으면 기본 이름으로 바꿨다.
- **마이그레이션 00007: 세션은 지우고 이메일로, 기기는 `email` 열 추가, 예전 `kakao_id`는 남김** - 세션은 로그인 상태일 뿐이라 지웠다(웹을 다시 열면 새로 생긴다). 기기는 데이터 삭제 금지 규칙에 따라 `kakao_id`를 기록으로 두고 예전 토큰도 그대로 쓴다. 관리 화면 기기 목록은 앱 연결·카카오(예전)·직접 발급으로 나눠 보이고, `/v1/devices`의 `via_kakao`는 `via`(`app_link`|`kakao`|`cli`)와 `email`로 바뀌었다. down은 `email`을 지우고 세션을 `kakao_id`로 되돌린다.
- **예전 카카오 환경변수는 읽지 않고 이름만 경고** - 남아 있으면 `serve`가 시작할 때 "더 이상 쓰지 않는 환경변수"로 이름만 로그에 남긴다(값은 남기지 않는다) - `config.Obsolete`.
- **정적 파일·`/healthz`·`/login`·`/auth/logout`은 서버 JWT 검사 밖** - 정적 파일은 공개 자산이고, 헬스체크는 VM 안에서 부르며, 나머지는 안내·세션 정리만 한다. 바깥에서는 Access가 막는다.
- **테스트는 가짜 Access(`auth/accesstest`)** - 테스트 안에서 RSA 키를 만들어 JWKS를 httptest 서버로 내주고 JWT를 서명한다. 실제 Cloudflare Access·JWKS로는 확인하지 못했다.
- **Access 로그인 방법은 이메일 코드(One-time PIN)만 (사용자 결정, 2026-11-08 실제 설정)** - 구글 OAuth 클라이언트를 만들지 않으려고. 서버는 로그인 방법을 보지 않고 Access JWT의 이메일만 허용 목록과 맞추므로, 나중에 구글을 더해도 코드는 그대로다. 세션 730h. 우회 앱은 `/v1`과 `/healthz` - `ACCESS_SETUP.md`.

## AI 분류 추천 (사용자 결정, 2026-11-08)

- **서버가 Gemini를 부르고, 보내는 것은 가맹점 이름과 분류 이름뿐** - 끼니 사진 분석과 같은 키·모델(`GEMINI_API_KEY`, `GEMINI_MODEL`)을 쓴다. 요청은 고정 문구 + 보관하지 않은 지출 분류 이름 + 가맹점 목록(JSON의 `id`·`name`)이 전부다. 금액·시각·알림 원문·카드·계좌·잔액은 보내지 않는다(테스트가 요청 본문 글을 통째로 맞춰 보고, 금액·이름 조각이 없는지 본다). 답은 `{"results":[{id, category, confidence}]}`이고 `category`는 응답 스키마 enum(분류 이름 + "모름")으로 막는다. 바꾸려면 `ai/merchant.go`의 `merchantPrompt`·`MerchantPromptText`.
- **(2026-11-10에 바뀜: 확신 0.8 이상은 바로 넣는다 - 아래 「AI 자동 분류」)** **순서: 사용자 규칙 → 규칙이 없으면 AI가 추천만 → 확인 필요에서 한 번 눌러 확정 → 학습 규칙. 자동 확정은 없음** - 틀린 추천이 예산에 바로 빠지지 않게. 확정은 기존 경로 그대로다: 앱은 `PATCH /v1/transactions/{id}` `{"category_id": 추천 분류, "remember_rule": true}`, 웹은 "AI 추천" 버튼. 그래서 같은 가맹점의 다른 확인 필요 건도 기존 학습처럼 함께 분류되고(`also_updated`), 다음 거래부터는 AI가 아니라 학습 규칙이 분류한다. 새 경로는 만들지 않았다.
- **대상은 규칙 없음(`no_rule`) + 나간 지출 + 가맹점 이름 있음만** - 결제대행사 이름뿐인 건(`payment_gateway`: KG이니시스, 네이버페이 등), 송금(`transfer`), 입금, 종류 모름, 취소 미매칭, 이름 없음은 이름만으로 무엇을 샀는지 알 수 없어서 AI에 보내지 않고 그대로 확인 필요로 둔다. 쿠팡은 seed 규칙(쇼핑)이 먼저 잡는다. "카카오페이_가게"처럼 결제대행사 이름이 섞인 건은 파이프라인 분류와 같이 떼어낸 이름("가게")을 보낸다. 조건은 `suggest.Eligible`과 `queries/suggestions.sql`의 `ListSuggestionMerchants` 두 곳에 있으니 같이 고친다.
- **가맹점마다 한 번만 묻고 답을 저장(`merchant_suggestions`, 마이그레이션 00008)** - 키는 결제대행사 이름을 뗀 뒤 `classify.Normalize`(공백·대소문자·법인 표기 무시)한 이름이다. 같은 가맹점 거래가 여러 건이어도 한 번, 다음 거래가 와도 다시 묻지 않는다. "모름"도 추천 없음(`category_id` NULL, 확신 0)으로 저장해서 다시 묻지 않는다. 다시 묻게 하려면 그 행을 지운다(`DELETE FROM merchant_suggestions WHERE merchant_key = '…'`, 전부는 `TRUNCATE merchant_suggestions`). 지점명이 다르면("…강남점", "…역삼점") 다른 가맹점이다. down은 표를 지운다(답만 사라지고 거래·규칙은 그대로).
- **확신 0.6 미만은 저장하되 보여주지 않음** - 틀린 추천을 한 번 누르면 잘못된 학습 규칙이 생기므로 애매한 것은 숨기고 직접 고르게 한다. 값은 `suggest.MinConfidence`. 저장된 확신 값을 보여줄 때마다 다시 비교하므로 바꾸면 이미 저장된 답에도 바로 반영된다.
- **추천한 분류를 보관하면 추천을 숨기고, 다음 점검에서 다시 물음** - 보관한 분류는 고를 수 없으니 그 답은 쓸모가 없다. 다시 물을 때는 보관하지 않은 분류만 보내서 같은 답이 되풀이되지 않는다. 분류 행을 지우면(웹·API는 지우지 않고 보관만 한다) 답도 함께 지워진다(`ON DELETE CASCADE`). 분류 이름을 바꾸면 분류 번호로 저장했으므로 새 이름으로 보인다. 새 분류를 만들어도 이미 저장된 답은 그대로다(새 분류로 다시 받으려면 행을 지운다).
- **작업은 `myhome serve` 안에서 따로 돌고, 수집은 기다리지 않음** - 위젯 때문에 수집은 동기 처리 그대로다. `no_rule` 확인 필요 거래가 커밋되면 막지 않는 신호(`Processor.OnNoRule` → `Worker.Notify`)로 깨우고, 신호를 놓쳐도 1분마다 본다. 시작할 때도 한 번 본다. 한 번에 20개씩 묻고, 한 번 점검에 호출은 10번까지(남은 것은 다음 점검). 호출 하나는 45초 제한(Gemini 클라이언트는 한 번 25초, 5xx·시간 초과면 1초 뒤 한 번 더 - 두 번째는 45초에서 잘릴 수 있다). 실패·패닉은 기록하고 1분부터 두 배씩, 최대 30분 기다렸다가 다시 한다. 응답 모양이 틀리면 저장하지 않고 다시 묻지만, 답에서 빠진 가맹점·목록 밖 분류 이름·0~1 밖 확신은 그 가맹점만 "모름"으로 저장한다(이상한 답 때문에 같은 가맹점을 거듭 묻지 않게). serve가 끝나면 작업도 끝나고, DB 연결을 닫기 전에 끝나기를 기다린다. 값은 `suggest/worker.go`의 `Default*`, 띄우는 곳은 `cmd/myhome/main.go`의 `startSuggestWorker`.
- **로그에는 건수만** - info 로그는 호출·물어본·추천·낮은 확신·모름·남은 수만 남긴다. 가맹점 이름과 키는 남기지 않고, 응답 해석 실패 에러에도 응답 내용을 넣지 않는다. API 키는 요청 헤더로만 간다.
- **`AI_CLASSIFY`(기본 `on`)로 끄고, `AI_PROVIDER=mock`이면 낱말 표로 고정 추천** - `off`면 묻지도 않고 API·웹에 추천도 안 보인다(저장된 답은 남는다). 키가 없는 로컬·테스트(mock)에서도 화면을 보려고, mock은 작은 낱말 표(분식·식당·국밥·커피·카페·마트·주유·서점 등)로 늘 같은 답을 준다. "상회"는 일부러 확신 0.4라 숨겨지는 것을 볼 수 있다. 실제 판단이 아니다. 표는 `ai/merchant.go`의 `mockMerchantWords`. `on`/`off` 말고 `true`/`false`, `1`/`0`도 받고, 그 밖의 값이면 서버가 시작하지 않는다.
- **거래 JSON에 `suggestion` 추가** - `{"category_id": 정수, "category": 분류 이름, "confidence": 0~1}` 또는 `null`. `GET /v1/review`, `GET /v1/transactions`, `GET /v1/summary`의 `recent`에서 채운다. `PATCH`·`POST` 응답의 거래는 늘 `null`이다(방금 고친 거래라서). 대상이 아닌 거래(확정·제외·다른 사유)는 늘 `null`. 추천을 읽다 실패하면 목록은 그대로 주고 `null`로 둔다(덧붙이는 정보라서) - `api/transactions.go`의 `AttachSuggestions`.
- **웹 확인 필요 화면: "AI 추천: 분류" 주 버튼 하나** - 누르면 그 분류로 정하고 "이 가맹점은 항상"을 켠다(체크 상자와 상관없이). 같은 폼이라 메모·정산 분류도 같이 저장된다. 수동 분류 버튼과 체크 상자는 그대로다. 버튼은 기존 무채색 강조색(`.btn`, 빨강·파랑 없음)이고 스크립트를 더하지 않았다(CSP 그대로) - `templates/review.html`, `web/pages.go`의 `classifyReview`(`suggest_category_id`).
- **"모름"이라는 이름의 분류는 AI에 보내지 않음** - AI의 "모름" 답과 구분할 수 없어서. 그런 분류를 만들 일은 거의 없다.
- **확인 화면 메모 칸에서 Enter로 저장되지 않게 함** - 폼의 첫 제출 버튼(기본 버튼)이 AI 추천 버튼이라, 메모를 쓰다 Enter를 누르면 추천 분류로 확정되고 규칙까지 배웠다(그전에도 첫 분류 버튼으로 저장됐다). 폼 맨 앞에 꺼진 숨은 제출 버튼을 두어 Enter 제출이 일어나지 않게 했다 - `templates/review.html`.

## 건강 신뢰성 고침 (리뷰 반영, 2026-11-08)

- **"기타"는 그날 비어 있는 다음 번호(7→8→9), 기록이 있는 칸에 새로 저장하면 "이미 기록이 있어요. 바꿀까요?"** - 늘 7을 쓰고 서버가 (날짜, 번호)로 덮어써서 두 번째 간식이 첫 간식과 사진을 조용히 지웠다. 그날 쓴 번호는 서버 하루 기록 + 못 보낸 기록(서버에 못 닿으면 폰 사본)으로 본다. 셋 다 차면 기타 1을 고르고 묻는다. 기록을 고치는 중이면 묻지 않는다 - `HealthPlan.extraChipIndex`·`needsOverwriteConfirm`.
- **지오펜스 출석은 WorkManager로 보냄(날짜마다 하나, 연결되면, 30초부터 늘려 가며 다시)** - 지하 헬스장처럼 신호가 없으면 그날 출석을 잃었다. 도착 시각(`checkInAt`)을 같이 보내서 늦게 보내져도 도착 시각으로 남는다. 서버가 거절(4xx)하면 그만두고, 토큰 문제는 계속 다시 한다. "위치로 출석" 버튼도 연결이 안 되면 같은 작업으로 넘긴다 - `GymCheckInWorker`.
- **지오펜스는 30분 주기에서도 다시 걸고, 못 건 이유를 헬스 화면에 보임** - 위치를 끄면 시스템이 지오펜스를 지우는데 앱 시작·화면·재부팅 때만 다시 걸어서, 화면은 "켜져 있어요"인데 실제로는 꺼져 있었다. `addGeofences` 실패와 지오펜스 오류 이벤트(위치 꺼짐)를 남기고 "켜져 있어요" 대신 이유를 보인다 - `GymGeofence.registrationError`, `WidgetRefresher.refresh`.
- **건강 알림은 "알람 및 리마인더"를 허용하면 `setExactAndAllowWhileIdle`** - `setAndAllowWhileIdle`은 절전 중 1시간 가까이 늦을 수 있어 "마감 30분 전"이 마감 뒤에 왔다. `SCHEDULE_EXACT_ALARM`을 선언하고 건강 설정 화면에 설명과 허용 버튼(시스템 설정)을 둔다. `USE_EXACT_ALARM`은 Play가 알람·캘린더 앱에만 허용해서 쓰지 않는다. 허용이 바뀌면 다시 건다 - `Reminders.canExact`.
- **늦게 울린 알림은 뜻이 없어졌으면 띄우지 않음** - 마감이 지난 "30분 전", 도착 시각이 지난 "헬스 갈 시간", 1시간 넘게 늦은 것은 뺀다 - `HealthPlan.dueReminders`.
- **헬스를 인증했거나 휴식권을 쓴 날은 헬스 알림을 울리지 않음** - 폰 사본(`HealthLocal.gymDone`, 최근 2주)을 인증·휴식권·자동 출석 성공 때와 건강 화면을 받을 때 맞춘다(알림 수신기는 네트워크를 쓰지 않는다).
- **헬스 화면 이번 주 ✓는 주가 걸친 달을 다 읽음** - 달 기록 API가 달 단위라 이번 주 중 지난달 날은 ✓가 안 나왔다. 주간 API에는 헬스가 없어서 달 기록을 한두 번 부른다 - `HealthPlan.weekMonths`.
- **건강 첫 화면의 헬스 칸은 휴식권을 쓴 날 "쉬는 날"로 끝난 것으로 보임** - 예전에는 방문(`gym`)만 봐서 쉬는 날에도 도착 시각을 계속 보였다.
- **서버에 못 보낸 끼니 기록은 폰에 남겼다가 WorkManager로 보냄(MealOutbox)** - 연결·서버 문제(네트워크, 5xx, 429)면 기록을 SharedPreferences의 JSON 목록에 남기고, 사진은 바로 끼니 사진으로 옮겨 "올릴 것"으로 표시한다(임시 사진은 하루 뒤 지워져서). 기록을 보낸 뒤 사진을 올린다. 같은 끼니는 마지막 것만 남기고, 보내는 사이 다시 저장한 기록은 지우지 않는다. 서버가 거절한 기록은 다시 보내지 않고 "보내지 못함"으로 남긴다(다시 저장하면 바뀐다). 건강 첫 화면 칸은 "보내는 중". Room 표 대신 JSON인 것은 몇 건뿐이고 Room 버전·마이그레이션을 늘리지 않으려고 - `health/MealOutbox.kt`.
- **보내지 못한 끼니와 올릴 사진은 보관 기간이 지나도 지우지 않음** - `MealPhotos.purge`가 두 목록을 건너뛴다.
- **끼니 사진은 올리기 전에 "올릴 것"으로 표시하고, 폰에 사진이 없으면 올리지 않고 표시만 지움** - 올린 뒤에 표시해서 올리는 중에 앱이 죽으면 잊었다. 또 보관 기간이 지나 폰 사진이 지워진 뒤 다시 올리면 0장이 가서 서버 사진이 지워졌다. 서버가 404(끼니가 지워짐)면 그만둔다.
- **찍는 중인 사진 경로와 건강 화면 내비게이션은 rememberSaveable** - 카메라가 열린 사이 Activity가 다시 만들어지면 찍은 사진을 잃었다. 촬영 화면은 `key(날짜, 끼니, 고치는 기록)` 아래에 있어서 `HealthNav`도 저장해야 같은 자리로 돌아온다 - `HealthNav.Saver`.
- **사진 한 장 2MB(전체 7MB), 분석·사진 보관은 종류마다 한 번에 하나, `GOMEMLIMIT=150MiB`** - 16MB 분석 요청 하나가 base64·JSON으로 5번쯤 복사돼 `MemoryMax=200M`에 닿을 수 있었다. 앱은 0.3MB쯤 보낸다. multipart는 `ParseMultipartForm` 대신 파트마다 한 번만 읽고, 차례는 본문을 읽기 전에 기다린다. 분석 하나는 55초 안(서버 WriteTimeout 60초) - `health.MaxPhotoBytes`, `AnalyzeBudget`.
- **Gemini는 5xx·시간 초과·연결 실패면 1초 뒤 한 번 더(한 번 25초), 4xx는 바로 실패** - 402(선불 크레딧 없음, 2026-11 운영에서 보임)는 503 `AI_NO_CREDIT` "AI 크레딧이 없어요 - 직접 입력해 주세요"로 따로 알리고 앱은 다시 알아보기 버튼을 숨긴다. 그 밖의 실패에는 촬영 화면에 "다시 알아보기"가 생긴다 - `ai.StatusError`, `ai.NoCredit`.
- **끼니 사진 바꾸기는 끼니 행을 잠근 트랜잭션 안에서, 끼니가 없으면 404 `MEAL_NOT_FOUND`** - 지우기와 올리기가 겹치면 주인 없는 파일·행이 남았다. `SELECT ... FOR UPDATE`로 지우기·다른 올리기와 차례를 맞추고, 파일도 잠금 안에서 쓴다. 잠금 안에서는 그 끼니 파일을 쓰는 곳이 하나라 남은 `.partial`(쓰다 죽은 파일)을 지운다. 마이그레이션은 필요 없다 - `LockMealLog`.
- **바코드 스캐너 모듈은 촬영 화면을 열 때 미리 받음, 바코드 영양값은 기준 양을 보임** - 처음 찍을 때 모듈을 받느라 실패했다(`ModuleInstallClient`). Open Food Facts는 100g(ml)당 값이 많아서 기록 확인 화면에 "영양성분 기준: …"을 보이고(100g 기준이면 경고색) 먹은 양으로 고치게 한다.

## 운영 VM과 Play 배포 (사용자 결정, 2026-11-08)

- **운영은 OCI E2.1.Micro VM 한 대(kodeploy 아님)** - 예약 공인 IP `140.245.75.93`(토스 허용 IP), 사설 `10.0.3.238`. 인바운드는 managed에서 오는 SSH뿐이고 웹은 Cloudflare 터널(`myhome`)로만 나간다. Terraform은 kodeploy 저장소 `terraform/oci-infra/home.tf` - `deploy/home/setup-vm.sh`(스왑·PostgreSQL 16·cloudflared·DB 역할), `deploy/home/deploy.sh`(빌드→마이그레이션→권한→재시작).
- **비밀값은 저장소 밖** - 로컬 `/root/myhome-secrets/`(web.env, trader.env)에서 ssh 표준입력으로만 VM에 보낸다. DB 비밀번호는 VM에서 만들고 밖으로 내보내지 않는다.
- **매매 타이머는 설치만 하고 끈다** - 켜는 건 사람이 정한다(TQ에서 넘어오는 날, `import-tq` 뒤). 이식 점검에서 trade에 거래일 확인이 없다는 빈틈이 나왔다. 실패 알림은 앱 알림으로 채웠다(「자동매매」의 매매 실패 항목).
- **Play는 예전 핏두잉 앱 자리(`com.fitdoing.app`)를 쓴다** - 패키지 이름은 바꿀 수 없어서 속 이름만 fitdoing이고, 스토어 이름·아이콘·화면은 MyHome. 서명은 핏두잉 업로드 키, versionCode는 2025-01-01부터 흐른 분(핏두잉 번호보다 항상 큼) - `android/app/build.gradle.kts`, `.github/workflows/android-play.yml`.
- **targetSdk 36** - Play가 36 미만을 받지 않는다. AGP 8.7.3은 35까지만 시험돼서 `android.suppressUnsupportedCompileSdk=36`을 뒀다. 앱은 이미 `enableEdgeToEdge()`라 화면 동작은 그대로다.
- **개인정보처리방침은 Notion 공개 페이지** - 핏두잉 방침이 이미 공개된 Notion 사이트 아래 하위 페이지로 만들어 따로 호스팅을 두지 않았다(`https://midi-broccoli-56c.notion.site/412502846708815483d9f80155f93791`). 스토어 등록정보와 앱 설정 「정보」에 같은 주소를 둔다(`SettingsScreen.kt`의 `PRIVACY_POLICY_URL`). 처리하는 정보나 외부 서비스가 바뀌면 이 페이지와 Play 데이터 보안을 같이 고친다.
- **아이콘은 무채색 바탕의 흰 집 하나** - 원본 `docs/store/icon.svg`, 스토어용 PNG도 같은 폴더. 앱 색 기준(무채색)을 따른다.

## 가계부 신뢰성 수정 (리뷰 반영, 2026-11-08)

- **결제가 아닌 알림은 무시(`raw_events.ignore_reason`, 마이그레이션 00011)** - 광고·혜택(적립해 드려요, "결제하면", 쿠폰·혜택·이벤트·%할인·받기, "(광고)"), 증권(N주를 …원에 구매·판매, 시세 %), 안내("취소는 …", 예정, "수 있어요")는 거래를 만들지 않고 사유(ad/securities/notice/custom)만 남긴다. 해석 실패로 세지 않는다. 낱말로 거르는 것은 "승인·일시불·결제했어요" 같은 돈이 움직였다는 문구가 없을 때만 한다(실제 결제 보존). 규칙은 `parser/ignore.go`, 더할 문구는 `PARSER_IGNORE_PATTERNS`("문구1;문구2", 들어 있으면 무조건 무시). 고친 뒤 `reprocess`로 다시 처리한다.
- **같은 앱이 같은 알림을 10분 안에 다시 올리면 중복** - 패키지·제목·본문·big_text가 같으면 앞 거래에 합친다(제외, 사유 duplicate). 예외 규칙보다 먼저 봐서 이체·취소도 두 번 세지 않는다. 대가: 본문에 시각이 없는 앱에서 10분 안에 같은 곳에서 같은 금액을 두 번 결제하면 한 건이 중복으로 빠진다(웹에서 되살린다). 시간은 `pipeline/processor.go`의 `repostWindow`.
- **앱 대기열 키는 `postTime` 그대로(`Notification.when`으로 바꾸지 않음)** - `when`이 앱마다 안정적인지 실기기로 확인하지 못했고, 같은 `when`으로 다른 결제를 올리는 앱이면 앱에서 조용히 사라져 서버에도 남지 않는다. 재게시는 서버가 위 규칙으로 거른다.
- **다른 앱이 24시간 안에 알린 같은 금액의 취소는 같은 환불** - KB 취소 뒤 토스 취소가 10분 늦게 와도 두 번째는 중복으로 합치고 다른 결제에서 또 빼지 않는다. 가맹점은 비슷하거나, 한쪽이 비었거나, 결제대행사 이름뿐이면 같다고 본다. 같은 앱의 취소끼리는 합치지 않는다(따로 환불한 두 건일 수 있어서). 시간은 `cancelDupWindow`.
- **중복 합치기는 맞는 종류끼리만** - 승인·충전끼리, 송금·출금끼리, 취소끼리, 입금끼리. 종류를 모르는 건은 같은 방향이면 전처럼 합친다 - `dedupeKinds`.
- **요약에 `failed_events`(최근 7일 해석 실패 + 받은 지 15분 지나도 처리 안 된 원문)** - 앱 홈("읽지 못한 알림 N건")과 위젯("실패 N")에 작게 보인다. 무시한 알림은 세지 않는다. 지난 실패가 영영 남지 않게 7일로 잘랐다 - `api/summary.go`의 `attentionLookback`·`staleUnprocessed`.
- **원화 금액 없는 외화 승인·취소는 금액 0 확인 필요(`foreign_currency`)** - 금액을 지어내지 않고 메모에 "외화 USD 12.34"를 남긴다. 금액은 웹에서 원화로 고친다. 금액이 0이라 중복·취소 상계는 하지 않는다. 표기는 통화 코드(USD 등), $·€·£·¥, 달러·유로·엔·위안·파운드 - `parser/common.go`의 `foreignRe`.
- **미처리 원문은 10분마다 다시 처리** - 수집 때 처리에 실패해도 201을 주므로(앱이 알림을 지우게) 서버를 다시 띄울 때까지 남던 것을 `serve` 안의 주기 작업이 처리한다. 한 건이 실패해도 나머지는 계속하고, serve가 끝나면 같이 끝난다 - `Processor.RunPending`.
- **새 기간에 예산이 없으면 가장 최근 예산을 자동 복사** - 1일에 예산이 비어 위젯이 '예산 없음'이 되고 예산 알림이 멈췄다. 지금 기간을 볼 때(요약·예산 조회·웹 예산 화면) 비어 있으면 한 번 복사한다. 하나라도 있으면 건드리지 않고, 지난·다음 기간에는 만들지 않는다. 지난달이 비었으면 그 전 예산을 쓴다 - `api.EnsureBudgets`.
- **전송 대기열: 같은 항목이 5번 이상 일시 오류면 남겨 두고 뒤부터 보냄** - 한 전송에서 연달아 3번 실패하면 서버 문제로 보고 멈춘다(요청 폭주 방지). 건너뛴 항목은 지우거나 거부로 바꾸지 않고 다음 전송 때 다시 보낸다 - `core/UploadQueuePolicy`.
- **수집기가 연결되면 떠 있는 알림을 다시 훑음** - 연결이 끊긴 동안 온 알림을 놓치지 않게 `onListenerConnected`에서 `getActiveNotifications`를 대기열에 넣는다(이미 보낸 것은 서버가 같은 해시로 거른다). 끊기면 `requestRebind`를 부르고, 상태 화면에 수집기 연결 여부와 마지막 수집 시각을 보여준다. 이미 지워진 알림은 되살릴 수 없다. 실기기 확인 전.
- **`/healthz`는 DB까지 확인(2초 한도, 실패 503)** - 응답은 `{"status":"ok"}` 또는 `{"status":"db_unavailable"}`뿐이고 오류 내용은 쓰지 않는다. 밖의 heartbeat와 deploy.sh도 DB 장애를 보게 된다.
- **"충전소"는 충전 알림이 아님** - 주유·충전소 승인을 충전으로 보던 것을 고쳤다(가맹점 이름도 깨지지 않게).

## 자동매매 신뢰성 보강 (2026-11-08)

- **결과를 모르는 주문은 멈춘다** - 주문은 보내기 전에 결과 모름(`unknown`: clientOrderId·유형·수량·단가·시각)으로 기록하고, 타임아웃·연결 끊김·5xx·orderId 없는 응답이면 다음 유형으로 내려가지 않고 실패(긴급)로 끝낸다. 다음 실행이 증권사 주문 목록(`FindOrders`)에서 찾아 있으면 이어받고, 확실히 없을 때만 같은 유형부터 다시 낸다. 조회 실패·후보 여럿이면 계속 멈춘다. 명시적 거절(4xx + 오류 코드, 408·409 제외)만 같은 실행에서 승격한다 - `run/trade.go`의 `submit`·`resolveUnknown`, `toss.IsRejected`.
- **clientOrderId는 run이 만든다** (`tq-<뉴욕 주문일>-<종목>-<방향>-<수량>-<유형>`, TQ와 같은 모양) - 기록한 키, 보낸 키, 찾는 키가 같아야 해서다. 예전에는 어댑터가 KST 날짜로 만들어 다음 실행의 키와 달랐다("다음 실행은 같은 키로 다시 낸다"는 주석이 틀렸다). 바꾸려면 `run.clientOrderID` 한 곳.
- **주문 목록에 clientOrderId가 실려 오는지는 미확인** - 키가 맞는 행이 없으면 같은 종목·방향인데 기록에 없는 주문이 딱 하나일 때 그것으로 이어받는다(이 계좌는 이 전략만 쓴다). 토스 스펙에서 확인되면 `pickUnknown`을 키 일치로만 좁힌다.
- **적용월이 이번 달이 아니면 trade는 실패** - "이미 완료"보다 먼저 본다. 판정이 빠진 달이 지난달 체결 표시에 가려져 0으로 끝나지 않게.
- **판정 타이머에 장 시작 전 만회(26~31일·1~3일 11:37 UTC) 추가** - 평일 월말(2026-08-31·11-30 월요일)은 1일 저녁 만회가 1일 장 마감 뒤라 1일 종가가 먼저 들어와 룩어헤드로 막혔다. 월말 당일 아침의 같은 실행은 "장 마감 전"으로 0 종료한다(실패 아님). 판정 규칙(`JudgmentDay`)은 그대로다.
- (2026-11-09 1~7일로 줄임, 아래 「매매 타이머 1~7일」) **매매 타이머 1~15일** (TQ는 1~8일) - 적용월 안의 늦은 매수는 허용이고(§12.1.3), 8일에 낸 주문이 확인되지 않아 다음 달 trade를 막던 자리를 없앤다. 휴장일은 trade가 건너뛴다.
- **매도 체결을 확인한 실행에서 바로 매수** - 매도대금이 주문가능금액에 99% 이상 보일 때만 낸다. 안 보이면 다음 실행에 맡긴다(매도대금이 현금으로 한 달 놀지 않게). 체결된 매도 뒤에만 산다는 규칙은 그대로다.
- **`myhome invest orders [--clear-stale]`** - 지난 적용월 주문 기록을 사람이 보고 치우는 명령. 증권사에 살아 있을 수 있는 주문이 있거나(조회 실패 포함), 장부에 없는 체결이 있는데 증권사 보유와 장부가 다르면 지우지 않는다. 이번 적용월 기록은 지우지 않는다. 우회 옵션은 없다.
- **사이징은 현재가(야후 일봉 마지막 값) 기준** - 토스 클라이언트에는 문서로 확인한 시세 조회가 없어서 야후를 쓴다. 이 값은 마스터에 쓰지 않는다(§10.0). 받지 못하거나 종가와 30% 넘게 다르면 마스터 종가로 돌아간다.
- **자금 부족 매수는 한 번만 줄여 다시** - 안전폭을 한 번 더 얹은 단가로 증권사 매수가능 수량을 다시 묻고(직전 수량보다 적게) 같은 유형으로 1회 더 낸다. 그래도 안 되면 예전처럼 멈춘다.
- **refresh 재시도 대기 중에는 상태 잠금을 놓는다** - 최대 90분을 쥐고 기다리면 겹친 judge·주간 갱신이 10분 뒤 75로 끝나 알림이 갔다. 다시 잡지 못하면 쓰지 않고 75 - `store.Held.Pause`.
- **`GET /healthz/invest`** - 로그인 없는 자동매매 신선도. position이 없으면 200, 마지막 성공 status가 8일보다 오래됐거나 마지막 judge·trade가 실패했으면 503. 상태 낱말만 싣는다(금액 없음). `/healthz`는 그대로 두고, 하트비트(`heartbeat.gs`)가 둘 다 부른다. Access 우회는 `ACCESS_SETUP.md`.
- **앱은 알림을 못 띄우면 읽음 처리하지 않는다** - 권한·앱 알림·채널 중 하나라도 꺼져 있으면 `last_id`를 넘기지 않는다. 켜면 서버가 주는 7일·20건이 그때 온다.
- **타이머는 `enable-invest.sh`로 켠다** - `Persistent=true`라 예전 실행 기록(`stamp-myhome-invest-*`)이 있으면 켜는 순간 놓친 일정을 돌린다. 기록을 지우고 켠 뒤 status를 한 번 돌린다. 끄기는 `disable-invest.sh`(돌고 있는 작업은 끊지 않는다).
- **남은 것 (메모만, 코드 안 고침)** - refresh는 장 마감 뒤인데 야후가 그날 SPX·NDX를 아직 안 줬으면 "새 거래일 없음"으로 0 종료한다. 그러면 그날 저녁 judge가 1로 실패하고 다음 날 장 시작 전 만회가 판정한다. 그날 저녁에 끝내려면 refresh가 이때 75를 내게 바꾼다.
- **남은 것 (메모만, 코드 안 고침)** - status(deadman)는 적용월이 이번 달인지 보지 않고 35일 규칙에 기댄다. 판정 누락은 trade가 먼저 실패로 알리고, 매매 창 밖이면 최대 일주일 뒤 status가 잡는다.

- **매매 타이머 1~7일 (사용자 결정, 2026-11-09)** - 적용월 첫 거래일에 내고, 전환 달(매도 체결 → 매수 체결)도 거래일 2~3일이면 끝난다. 7일까지 못 끝냈으면 장애라 사람이 본다(실패는 앱 알림). 15일까지 늘렸던 이유(마지막 날 낸 주문을 확인할 실행이 없어 다음 달을 막음)는 `orders --clear-stale`로 사람이 치운다. 그래서 전환할 수 있는 날은 8~24일이다(판정 25~31일·1일, 매매 1~7일을 피함). 다시 늘리려면 `myhome-invest-trade.timer`의 `OnCalendar` 한 줄.
- **11/2 토스 앱 수동 매수를 11월분으로 기록 (사용자 결정, 2026-11-09)** - 전환 때 토스 55주 vs TQ 장부 43주. 토스 주문 조회로 11/2 TQQQ 12주 $63.46을 찾았고, 적용월 신호(TQQQ)와 같은 종목·같은 달이라 `adjust`(앱 밖 매수)가 아니라 `execute`(11월 체결, 7일 지연)로 적었다. 원화는 토스 API가 주지 않아(`totalPurchaseAmount.krw`가 0) 사용자가 앱에서 본 원금 5,567,291원에서 기존 장부 합계를 뺀 1,017,316원, 환율은 그 금액 ÷ 체결액(수수료 포함)으로 맞췄다. 남은 주문가능 $286.01은 `--apply-cash`로 장부 현금에 넣어 12월 매수에 같이 쓴다(원금에는 안 더한다). 바꾸려면 `amend`로 단가·환율만 고칠 수 있다.
- **체결 환율은 결제 뒤 토스 거래내역으로 자동 확정 (사용자 요청, 2026-11-10)** - 통합증거금 환율은 주문·체결 조회에 없고 결제가 끝난 `GET /api/v1/transactions`의 `exchangeRate`에만 있다(미국 주식 T+1). 그래서 trade는 시장 환율로 추정해 적고, 결제 뒤 trade(이미 완료된 날)·refresh가 결제 내역의 평균 단가·적용 환율(정산금액 가중)로 `amend`와 같은 길을 밟아 다시 매긴다(수량은 그대로). 자동으로는 trade가 남긴 달만 고치고, 사람이 `execute`로 적은 달은 `amend --auto`로 부를 때만 고친다(사람이 적은 원금이 정답일 수 있어서). 결제 내역 수량이 그 달 체결과 다르면(앱 밖 매매 섞임) 고치지 않고 알린다. 전환 달은 매도도 같은 환율 하나로 다시 매긴다(amend 모형 그대로) — 매도 환율을 따로 쓰려면 `Account.SellAll` 호출에 매도 쪽 환율을 넘기면 된다. 계약은 `broker.Settlements`(선택 계약)라 다른 증권사는 구현 안 해도 된다 — `run/fxconfirm.go`, `toss/settle.go`.
- **평가용 환율(USDKRW_MKT)은 토스 매매기준율 (사용자 요청, 2026-11-10)** - 그날 뉴욕 16:00 시각의 `GET /api/v1/exchange-rate` `midRate`. 토스가 못 준 날만 야후 `KRW=X`(예전 방식). 2026-11-02 기준 야후 일봉 1,325.5 vs 토스 1,344.4로 야후가 1% 넘게 달랐다. 이미 있는 값은 바꾸지 않는다. 판정은 환율을 안 써서 출처가 섞여도 판정·주문은 그대로다 — `run/refresh.go`의 `brokerFX`.
- **main에 push하면 자동 배포 (사용자 결정, 2026-11-10 "매번 배포해")** - 서버는 테스트 통과 뒤 VM으로, 앱은 Play 비공개 테스트(alpha)로. VM은 들어오는 포트가 없어서 러너가 터널 SSH로 들어가고, 키는 `restrict,command="sudo /opt/myhome/ci-deploy.sh"`로 묶었다(셸·포워딩 없음, sudo도 그 스크립트 하나). 끌어오는 방식(VM이 GitHub를 주기적으로 확인)은 비공개 저장소 읽기 토큰을 VM에 둬야 하고 작은 VM에서 빌드할 수도 없어서 안 했다. 🔴 이 키는 배포 권한 = 마이그레이션을 root로 돌리는 권한이라 main push 권한과 같은 무게다. 토스 키 같은 비밀값은 GitHub에 올리지 않는다(VM에 이미 있는 것을 쓴다). 키를 VM에 까는 것(`setup-ci-deploy.sh`)과 GitHub Secret 등록은 사람이 한다. 끄려면 `HOME_KNOWN_HOSTS` 변수를 지우면 배포 단계가 건너뛰어진다 - `.github/workflows/server.yml`, `deploy/home/{install,ci-deploy,setup-ci-deploy}.sh`.
- **백테스트 결과 표를 다시 넣음 (사용자 요청, 2026-11-10 "그게 있어야 장기투자 가능")** - TQ 뷰어의 「백테스트 결과」(★ SPX185+vol30을 SPX185 단독·175/200·NDX200→QQQ·TQQQ·QLD·QQQ·SPX 보유와 5~10년 적립으로 비교, 최악·하위10%·하위30%·중앙·상위·손실%·MDD)를 이식할 때 상태 화면만 옮기고 빠뜨렸다. 원본 xlsx(수수료 0.10% 시트)를 `internal/invest/backtest/backtest.json`으로 옮겨 내장했다 — 다시 계산하지 않는 고정 표다(앱 안 백테스트·튜닝 금지 규칙 그대로). TQ와 같이 하위 화면에 두고, 최악부터 보여 주고, 「읽기 전에」(§13 문장, 숫자는 표에서 뽑음)를 같은 화면에 둔다. 기본 표본은 닷컴 제외(TQ 기본값)이고, 맨 위 요약은 공식 기준인 7년·전체 표본에서 ★를 TQQQ·QLD·QQQ 보유와 나란히 둔다 — 붙잡는 논리가 "TQQQ·QLD를 그냥 보유하는 것보다 안전"이라서. 웹 `/invest/backtest`, 앱 투자 → 전략 → 백테스트 결과(`GET /v1/invest/backtest`). 원본이 바뀌면 xlsx에서 JSON을 다시 뽑고 `backtest_test.go`의 고정값을 고친다.

## 앱 화면 단순화 (2026-11-09)

- **가계부·설정 화면은 카드 대신 가로로 꽉 찬 줄과 1dp 선** - "박스 안 박스", "글자 크기에 맞춘 상자"가 헷갈린다는 의견. 노션·토스처럼 줄로만 나누고, 주인공 버튼은 화면당 하나(가로로 꽉 찬 56dp)만 둔다. 공통 부품은 `ui/Common.kt`(FlatRow, PrimaryButton, TextTabs, LineField 등). 시안은 Design 아티팩트 "MyHome 단순화 시안". 건강 화면은 기존 카드 디자인이 괜찮다는 의견이라 카드를 유지한다.
- **홈은 처음 설정 순서부터** - 로그인 → 알림 읽기 허용(돌아오면 배터리 절전 예외를 이어서 묻는다) → 카드·은행 앱 고르기 → 예산(선택). 세 단계를 마쳐야 "쓸 수 있는 돈" 홈이 나온다. 전송·토큰·수집기 같은 말은 화면에서 뺐고, 서버 주소·토큰 직접 입력은 설정 > 문제가 생겼을 때 > 고급으로 옮겼다.
- **알림 읽을 앱은 폰에 깔린 앱에서 고른다** - 예전에는 알림이 한 번 와야 목록에 떠서 "0개"로 막혔다. 런처 앱 목록(매니페스트 `<queries>` LAUNCHER, 새 권한 없음)을 보여주고, 이름에 카드·페이·뱅크 같은 말이 든 앱을 위에 둔다. 패키지명은 여전히 코드에 박지 않는다(순서만 바꾼다). 바꾸려면 `AppsScreen.kt`의 `PAY_WORDS`.
- **확인 필요 화면을 거래 화면의 "분류 고를 것" 탭으로 합침** - 위젯의 확인 필요 항목은 그 탭을 열고 분류 고르기 판을 바로 띄운다. 거래 줄을 누르면 어떤 거래든 분류를 바꾸거나 가계부에서 뺄 수 있다.
- **앱에서 분류·예산·예산 시작일을 고친다** - 웹 관리에만 있던 것(`POST/PATCH /v1/categories`, `PUT /v1/budgets`, `PUT /v1/settings/budget_start_day`)을 설정 > 분류와 예산에서 부른다. 특수 분류(수입·정산·내 계좌 이체·투자·저축)는 계산 규칙에 묶여 있어 앱에서 보여주지 않는다. 현금 입력(`POST /v1/transactions`)은 거래 화면 아래 줄에서.
- **건강: 탄단지 위주, 끼니마다 박스, 먹은 시각** - 오늘 합계는 탄수화물·단백질·지방을 크게, 칼로리는 그 아래 한 줄. 식단 표 대신 먹은 끼니마다 박스(사진·음식·먹은 시각·탄단지), 아직 안 먹은 끼니는 점선 박스로 목표량을 보인다. 위 끼니 칸에서 헬스를 빼서 아래 한 줄로, 맨 위 날짜 제목은 뺐다. 떠 있던 촬영 버튼은 내용을 가려서 아래에 붙인 버튼으로 바꿨다.
- **먹은 시각은 `loggedAt`으로 보낸다** - 서버가 이미 받는 필드라 서버는 그대로다. 기록 화면의 "먹은 시각"은 새 기록이면 지금, 고칠 때는 저장된 시각이 기본이다(예전에는 고치면 시각이 저장 시각으로 바뀌었다). 끼니 날이 전날인데 새벽(05시 전) 시각이면 다음 날 새벽으로 본다(`HealthPlan.eatenAt`).

## 분류 폴더와 설명 (사용자 결정, 2026-11-09)

- **분류를 폴더처럼 3단계까지 묶는다** - 예: 생활비 › 식비 › 배달. `categories.parent_id`(00012). 깊이·순환·종류 검사는 서버 `internal/cattree`(`CheckPlace`)와 앱 `CategoryTree`가 같은 규칙으로 한다. 폴더가 다르면 같은 이름을 쓸 수 있다(이름 유일 조건을 폴더 단위로 바꿨다). 특수 분류(수입·정산·내 계좌 이체·투자·저축)는 늘 맨 위이고 폴더로도 못 쓴다. 단계 수를 바꾸려면 `cattree.MaxDepth`와 `CategoryTree.MAX_DEPTH`.
- **결제는 어느 단계에든 넣고, 폴더 값은 아래를 더한다** - 쓴 돈·정산은 아래를 모두 더한다. 예산은 폴더에 직접 정하면 그 값이 폴더 전체 한도이고(아래 예산은 그 안의 세부 한도), 안 정하면 아래 예산의 합이다. 총예산은 맨 위 분류만 더해 같은 돈을 두 번 세지 않는다. 계산은 `api.rollUp` 한 곳, 요약 응답은 폴더 다음에 그 안 순서로 `parent_id`·`depth`·`path`·`own_budget`을 준다. 위젯과 앱 홈은 맨 위 분류만 보이고 폴더를 누르면 펼친다. 예산 알림은 직접 예산을 정한 분류만 보낸다(폴더 합계까지 알리면 같은 초과를 두 번 알린다).
- **지우기는 숨기기다** - 지난 결제가 분류를 가리키고 있어서 행을 지우지 않는다. 폴더를 지우면 안의 분류도 숨기고, 되살리면 안과 위 폴더도 같이 돌아온다. 숨긴 분류로 가는 가게 규칙은 건너뛰어서, 그 가게 결제는 다른 규칙이 없으면 "분류 고를 것"(AI 추천 대상)으로 간다. 예전에는 숨겨도 규칙대로 계속 그 분류로 들어갔다.
- **분류마다 설명(메모)을 적고 AI가 참고한다** - `categories.memo`(300자). AI 분류 추천 프롬프트에 분류를 "- 생활비 › 식비: 설명" 한 줄씩 싣고, 설명을 먼저 따르라고 한다. 보내는 것은 가게 이름과 분류 이름·설명뿐이다(금액·시각·원문은 여전히 안 보낸다). 분류 목록(경로·설명)의 지문을 추천과 함께 저장해서(`merchant_suggestions.catalog`), 폴더·이름·설명이 바뀌면 같은 가게를 한 번 다시 묻는다. 배포 직후에는 저장된 추천이 모두 한 번씩 다시 물어진다.
- **알림 읽을 앱 화면에 앱마다 알림 확인을 둔다** - 고른 앱마다 최근 알림이 왔는지 보여주고, 안 왔으면 켜는 곳을 안내하며 누르면 폰 설정의 그 앱 알림 화면을 연다. 앱 안 메뉴 이름은 업데이트마다 바뀌어서 "앱 안 설정의 알림"까지만 적는다. 쿠팡에서 국민카드로 결제하면 승인 알림은 KB Pay에서 온다는 안내를 넣었다(실제 폰에서 결제 알림이 0건이던 일 때문).

## 집중 타이머 (사용자 요청, 2026-11-09)

- **열품타처럼 과목 타이머 + 공부 장소에 머문 시간** - 앱 하단바 '집중' 칸(투자 자리). 화면은 가운데 날짜 · 오늘 순공 큰 시간 · 타이머/통계/장소 탭 · 과목 줄(색 동그라미 재생 버튼, 이름, 오늘 시간, ⋮). 과목을 누르면 시작, 다시 누르면 멈추고, 다른 과목을 누르면 넘어간다(한 번에 하나). 열품타의 하단바(홈·할 일·달력·그룹)는 만들지 않고 우리 하단바를 쓴다 - `ui/FocusScreen.kt`, `focus/FocusTimer.kt`.
- **투자는 하단바에서 빼서 홈 오른쪽 위 '투자' 버튼으로** - 자동이라 볼 일이 적다. 6칸은 너무 좁다(「하단바와 투자 칸」). 처음엔 홈 맨 아래 줄이었는데 찾기 어려워 위로 옮겼다(사용자 의견). 거래 칸 안에는 넣지 않는다(가계부와 자동매매를 섞지 않는 결정). 매매 실패 알림을 누르면 전처럼 투자 화면으로 간다(뒤로 가면 홈).
- **하루는 05:00 KST에 바뀐다** - 새벽까지 공부해도 전날 순공. 05:00을 넘는 기록은 두 날로 나눠 센다. 앱 `core/Focus.kt`의 `FocusDay`와 서버 `focus.DayStartHour`가 같은 값이어야 한다.
- **폰이 원본, 서버는 보관** - 신호가 없어도 재고 멈춰야 해서 Room(DB 2판, 자동 마이그레이션)에 먼저 남기고 `FocusSync`가 `/v1/focus`에 올린다(앱이 만든 UUID로 PUT, 같은 것을 여러 번 보내도 한 줄, 서버에 더 새 것이 있으면 서버 것). 과목 → 기록 → 머묾 순서. 서버가 400이면 그 줄은 넘어가고(폰에는 남음), 404·405(서버를 아직 안 올림)·연결·로그인 문제는 나중에 다시. 새로 깐 앱은 한 번 `/v1/focus/all`을 받아 폰에 없는 것만 채운다.
- **도는 타이머는 설정에 시작 시각만** - 앱이 꺼지거나 재부팅돼도 시작 시각부터 지금까지로 센다. 도는 동안 시계가 가는 알림(무음 채널 '집중 타이머', 멈추기 버튼). 1초 미만은 버리고, 깜빡 켜 둔 것은 24시간 직전까지만 남긴다(서버 한도).
- **과목은 지우지 않고 숨긴다** - 지난 기록이 가리켜서. 기록은 통계 탭에서 지울 수 있다(서버에도 지움 표시).
- **머문 시간은 지오펜스 들어옴~나감** - 장소에서 "지금 위치를 공부 장소로"(최대 10곳, 반경 100·150·250m, 기본 150). 좌표는 폰 설정에만, 서버에는 장소 이름과 시각만. 3분 미만은 버리고, 5분 안에 같은 곳에 다시 들어오면 이어 붙이고, 16시간 넘으면 나감을 놓친 것으로 보고 버린다(`StayRules`). 두 장소가 겹치는 시간은 한 번만 센다. 집중률 = 순공 ÷ 머문 시간(반올림, 장소 밖에서 잰 시간 때문에 100%를 넘을 수 있다).
- **지오펜스 묶음은 헬스장과 따로** - `removeGeofences(PendingIntent)`가 그 PendingIntent로 건 것을 다 지워서, 같이 쓰면 한쪽을 끌 때 다른 쪽도 꺼진다. 권한('항상 허용')과 위치 잡기는 `GymGeofence`의 것을 쓰고, 켤 때 앱 안 안내 → 동의 → '항상 허용' 순서는 헬스장과 같다. 앱 시작·재부팅·30분 주기에 다시 건다.
- **Play 위치 권한 선언은 헬스장 자동 출석으로 냈다** - 백그라운드 위치를 새 목적(공부 장소 머문 시간)에도 쓰므로 Play Console의 위치 권한 선언 설명(과 시연 영상)에 이 기능을 더해야 한다. 안 하면 다음 업로드가 검토에서 걸릴 수 있다.
- **위젯은 하나에 두 페이지(가계부 · 집중), 위의 탭으로 바꾼다 (사용자 요청)** - 홈 화면 위젯은 옆으로 미는 손짓을 받지 못한다(Glance·RemoteViews에 손짓 처리가 없고, 넘기는 묶음 뷰는 세로로 넘기거나 저절로 넘어간다). 그래서 탭을 누르면 바뀌고 위젯마다 기억한다(Glance 상태 `page`). 집중 페이지는 오늘 순공과 과목 줄(작게 0줄·중간 3줄·크게 6줄, 도는 과목이 밖에 있으면 마지막 줄에)이고, 동그라미를 누르면 앱을 열지 않고 시작·멈춘다. 도는 동안의 초는 시스템 Chronometer가 그려서 앱을 깨우지 않는다(1시간 전에는 "12:34", 넘으면 "1:12:34"로 보인다). 앱·알림·위젯 어디서 바꿔도 위젯을 다시 그린다 - `widget/SummaryWidget.kt`. 따로 떼려면 `FocusPage`를 새 GlanceAppWidget으로 옮긴다.
- **웹 화면은 아직 없다** - 서버에 `/v1/focus/days`(날마다 순공·머문 시간·과목별)가 있어 나중에 웹 통계를 붙일 수 있다.


## 꾹 눌러 복사 · 누르면 동작 · 편의 기능 (사용자 요청, 2026-11-10)

- **앱 전체를 SelectionContainer 하나로 감싼다** - 가게 이름·금액·메모·투자 숫자를 꾹 눌러 골라 복사한다. 화면마다 감싸지 않고 `AppRoot`의 화면 자리 한 곳에 둔다(새 화면도 저절로 된다). 하단바·머리는 밖이라 고르지 않고, 버튼·탭·메뉴 글자는 `DisableSelection`으로 끈다.
- **화면 안의 누르는 칸은 `Modifier.tap`(clickable 대신)** - 글자 위를 꾹 누르면 clickable이어도 골라지기만 한다. 그런데 손가락이 글자를 살짝 벗어나 줄의 빈 곳을 꾹 눌렀다 떼면 clickable은 누르기로 쳐서, 가게 이름을 복사하려다 분류 판이 열린다(Robolectric 테스트로 재현). `tap`은 길게 누르기를 받아서 버려서 그때도 열리지 않고 글자 고르기·복사는 그대로 된다 - `ui/Common.kt`. 하단바·머리는 고르기 밖이라 clickable 그대로. 대화상자 버튼·메뉴는 material3 그대로(꾹 눌렀다 떼면 눌림).
- **🔴 대화상자·메뉴·아래 판은 `ModoAlertDialog`·`ModoDropdownMenu`·`ModoBottomSheet`로** - 따로 뜨는 창이라 바깥 SelectionContainer에 글자가 붙으면 꾹 누를 때 "layouts are not part of the same hierarchy"로 죽는다(Compose 1.7.6 `NodeCoordinator.findCommonAncestor`. material3 AlertDialog를 그대로 둔 Robolectric 테스트에서 이 오류로 죽는 것을 확인). 대화상자·판은 창 안에 자기 SelectionContainer를 두고 메뉴는 고르기를 끈다 - `ui/Selection.kt`. material3 것을 바로 쓰지 않는다. Compose가 창을 넘는 좌표를 지원하면 걷어 낸다.
- **웬만한 줄은 누르면 간다** - 홈: 큰 숫자 → 이번 달 거래, 분류 → 그 분류(폴더면 안 분류까지) 거래만, 최근 결제 → 그 거래의 분류 판, 읽지 못한 알림 → 웹 관리 '읽지 못한 알림' 탭, "새로 받음" → 새로 받기. 투자: 보유·이번 달·원금 대조·환율 → 평가, 누적 원금 타일 → 원금 내역으로 내림. 백테스트: 머리 칸 → 줄 세우기(1등부터 → 꼴찌부터 → 원래대로), 줄 → ★와 칸마다 견주기. 건강: 요일·오늘 합계·주간 그래프 막대 → 그날 기록, 기록 화면 달 이름 → 오늘, 헬스 → 헬스. 집중: 큰 시간 → 통계(다시 누르면 타이머), 통계의 과목 → 그 과목 타이머 켜고 끄기, 기록·장소 줄 → ⋮ 메뉴. 문제 해결: 정상인 줄도 그 설정으로, 마지막 읽은 알림 → 보낸 기록. 갈 곳이 없는 숫자(일정 줄 등)는 누르는 척하지 않는다.
- **거래 찾기는 전체 기간, 서버 `q`로** - 돋보기를 누르면 가게 이름·메모로 찾는다(서버가 이미 `q`·`from`·`to`를 받는다, 최근 200건). '분류 고를 것' 탭은 폰에서 거른다. 거르거나 찾으면 맨 위에 건수·쓴 돈·들어온 돈. 달 이름을 누르면 이번 달로.
- **거래 판에서 메모·이름·금액 고치기, 같은 가게 거래 보기** - 서버 `PATCH /v1/transactions/{id}`가 원래 받던 `memo`·`merchant`·`amount`를 앱에서도 보낸다(바꾼 것만, null은 빼서). 외화 결제 금액을 웹까지 가지 않고 고친다. 고친 거래는 `manually_edited`라 재처리에서 그대로다.
- **하단바에서 지금 칸을 한 번 더 누르면 처음 상태로** - 맨 위로 + 새로 받기(화면을 `key(reselect)`로 새로 그린다). 거래 칸을 하단바로 들어가면 분류 거르기를 푼다.
- **아래로 당겨 새로 받기를 투자·평가·건강에도** - 홈·거래에만 있었다.
- **앱 화면 테스트는 Robolectric + Compose UI 테스트** - 폰 없이 JVM에서 누르기·꾹 누르기를 돌린다(`app/src/test/.../SelectionTest.kt`). 대화상자 창을 그리려면 `@GraphicsMode(NATIVE)`. CI(Play 업로드)는 지금처럼 `:core:test`만 돌린다(Robolectric은 처음에 안드로이드 이미지를 내려받아 느리다).

## 처음 설정(온보딩) (사용자 요청, 2026-11-10)

- **한 쪽에 한 가지씩, 순서대로** - 시작 → 로그인 → 결제 알림 읽기(알림 읽기·카드·은행 앱·절전 예외·예산) → 앱 알림·알람 및 리마인더 → 건강 목표 → 끼니·헬스·취침 시간 → 끝. 쪽마다 지금 할 것 하나만 큰 버튼이고, "나중에"로 넘길 수 있다. 끝 쪽은 안 한 것을 "아직"으로 보여 주고 누르면 그 쪽으로 돌아간다 - `ui/Onboarding.kt`.
- **처음 켤 때 한 번, 이미 한 쪽은 건너뛴다** - `AppSettings.onboarded`가 false면 앱이 처음 설정부터 열린다(끝내거나 "건너뛰기"를 누르면 true). 이 기능 전부터 쓰던 폰도 업데이트 뒤 한 번 뜨지만, 로그인·결제 알림처럼 이미 한 것은 빼고 남은 것(정시 알람, 건강 목표 등)만 보여 준다. 설정 > "처음 설정 다시 보기"는 모든 쪽을 보여 준다. 홈의 가계부 처음 설정(번호 순서)은 그대로 둔다(처음 설정을 건너뛰어도 가계부는 거기서 한다).
- **건강 목표를 "정했다"는 성별·나이·활동이 있는 것** - 서버 기본값(`health.Defaults`)은 키 175·몸무게 70·목표가 채워져 있고 이 셋만 비어 있어서다. 그래서 처음 설정은 서버 기본 키·몸무게를 칸에 채우지 않는다(내 값이 아니다). 목표는 건강 설정과 같은 추천(`/v1/health/targets/recommend`)으로 계산해 보여 주고 "이 목표로 저장"에서만 저장한다. `targetHistory`는 보내지 않는다(목표가 바뀐 날을 서버가 남기게).
- **끼니·헬스·취침 쪽은 `notifications`를 보내지 않는다** - 건강 설정에서 알림을 꺼 둔 사람이 처음 설정을 다시 봐도 켜지지 않게.
- **알림 권한은 앱 알림 → 알람 및 리마인더 순서** - Android 13+ 알림은 한 번 묻고, 거절하면 버튼이 "설정에서 알림 켜기"(앱 알림 설정 화면)로 바뀐다. Android 14부터 새로 깐 앱은 정시 알람이 꺼져 있어서, 안 켜면 끼니 마감 30분 전 알림이 마감 뒤에 올 수 있다고 그 자리에서 말한다. 허용하고 돌아오면 다음 알림을 제시각 알람으로 다시 건다.
- **위치는 처음 설정에서 묻지 않는다** - 헬스장 자동 출석·공부 장소 머문 시간은 백그라운드 위치라 Play 정책상 그 기능을 켤 때 그 화면에서 설명하고 묻는다. 끝 쪽에 "헬스·집중 화면에서 따로 켜요"라고만 적는다.
- **테스트** - 어떤 쪽을 보여 주는지(`onboardPages`), 앞뒤·끼워 넣기·건강 쪽 빼기(`OnboardNav`), 그리고 처음부터 끝까지 눌러 가는 Robolectric 테스트(가짜 `OnboardHost`로 서버·시스템 화면 대신): 건강 목표 저장 내용(성별·나이·목표·kcal, `targetHistory` 없음), 끼니 3개·바꾼 마감·헬스 주 횟수, `notifications` 없음 - `app/src/test/.../OnboardingTest.kt`.

## 실제 알림으로 파서 보정 1차 (사용자 질문 "가계부에 안 적힌 건", 2026-11-10)

- **원인** - 11/09에 들어온 원문 5건이 전부 "금액을 찾지 못함"이었다. 삼성페이는 금액을 제목에 `₩12,345 결제 완료` 꼴로 쓰고(본문은 가게 이름만) 파서는 `12,345원` 꼴만 찾았다. 쿠팡 2건은 결제가 아니라 "배송중" 안내였다. KB Pay·토스 원문은 한 건도 오지 않았다(폰에서 수집 대상이 아니거나 그 앱 알림이 꺼져 있다).
- **`₩`·`￦`·`KRW` 앞붙임 금액도 원화로 읽는다** - `parser/common.go amountRe`. `₩30,000원`처럼 둘 다 붙어도 한 번만 센다. 잔액 표시(`잔액 ₩…`)는 거래 금액에서 빼고, 익명화도 가린다(`anonymize.balanceRe`).
- **배송·배달 안내는 무시(notice)** - `배송중/시작/완료/출발`, `배달 중/완료`, `송장 번호`. 결제 문구(`결제 완료` 등)가 같이 있으면 무시하지 않는다(soft 규칙).
- **패키지 매핑은 실제로 본 것만 설정에 넣었다** - `PARSER_PACKAGES="samsungpay=com.samsung.android.spay;coupang=com.coupang.mobile"`(VM 비밀값 web.env, 따옴표: 셸로 읽을 때 `;`가 끊지 않게). 코드에는 여전히 패키지명을 박지 않는다. 삼성페이·쿠팡은 범용 파서를 쓰고 출처 이름만 "삼성페이"·"쿠팡"이 된다. 국민카드를 삼성페이로 쓰면 KB Pay 알림도 오는데, 중복 합치기는 패키지가 다르고 3분 안 같은 금액이면 하나로 합친다.
- 테스트는 합성 픽스처(실제 가게 이름 아님) 6건과 익명화(잔액 ₩) 1건. 고친 뒤 VM에서 11/09~11/10 원문을 `reprocess --apply`로 다시 처리한다.

## AI 자동 분류 (사용자 요청 "ai가 자동분류하면 알아서 넣어줘야", 2026-11-10)

- **확신 0.8 이상이면 확인 필요에 두지 않고 그 분류로 바로 확정** - `suggest.AutoConfidence`. 0.6~0.8은 지금처럼 확인 필요에 "AI 추천"만 붙고, 0.6 미만·모름은 그냥 확인 필요. serve의 추천 작업(`Worker.Auto=true`)이 묻고 난 뒤, 그리고 전에 받아 둔 답이 있는 가맹점의 새 거래도 같은 점검에서 넣는다(AI를 다시 부르지 않음). 거래가 들어오면 작업을 바로 깨우므로 보통 몇 초 안에 들어간다.
- **학습 규칙은 만들지 않는다** - 같은 가맹점은 저장된 AI 답으로 다시 넣는다. 틀렸으면 거래를 눌러 분류를 고치고 "이 가맹점은 항상"을 켜면 그때 학습 규칙이 생겨 AI 답보다 먼저 쓰인다. `manually_edited`도 켜지 않는다(재처리하면 다시 AI 답으로 넣는다).
- **그 사이에 사용자가 분류한 거래는 건드리지 않는다** - `FileWithAICategory`가 `needs_review`·`no_rule`일 때만 바꾼다.
- **"AI가 넣음" 표시는 따로 없다** - 화면 글자를 줄이는 중이라서. 필요하면 `transactions`에 열을 더하고 거래 줄에 작은 표시를 단다.
- 끄려면 `AI_CLASSIFY=off`(추천도 같이 꺼짐) 또는 `cmd/myhome/main.go`에서 `Auto: false`.

## 앱 글자 줄이기 · 거래 목록 · 폴더 (사용자 요청, 2026-11-10)

- **긴 안내 문단은 "?"로** - 화면에는 한 줄짜리만 두고, 자세한 설명은 제목 옆 작은 "?"(`HelpDot`)를 누르면 대화상자로 뜬다. `FlatRow`·`SwitchRow`·`SectionLabel`·`SectionCard`에 `help`를, 화면 위 안내 한 줄은 `HintLine`. "?"를 눌러도 줄 자체는 눌리지 않는다(테스트). 남긴 긴 글: 확인 대화상자, Play 정책상 화면에 그대로 보여야 하는 위치 사용 안내, 폰 설정 메뉴 경로, 백테스트 표의 읽는 법 한 줄(🔴 TQ 규칙: 읽는 법은 표와 같은 화면에).
- **칩은 한 줄, 넘치면 글자를 줄여 맞춘다** - "린매스업"이 좁은 칸에서 두 줄로 깨졌다(`FitText`, 0.9배씩 최소 0.6배). 평소 활동은 "거의 없음·가벼움·많음·아주 많음" 네 칸 한 줄로 줄이고 뜻은 "?"로(`ACTIVITY_HELP`).
- **입력칸 단위는 라벨이 아니라 칸 안 오른쪽에** - "몸무게(kg)"가 세 칸 줄에서 두 줄로 깨졌다. `LineField(suffix=)`, 건강 설정 `Field(unit=)`. 라벨은 한 줄(넘치면 …).
- **거래 목록: 날짜("오늘"·"어제")와 그날 나간 돈은 크게, 결제 금액도 굵게, 그 밑에 "그 뒤 이번 달 남은 돈"을 작게** - 남은 돈은 서버 요약과 같은 셈(예산 − 확정 지출 + 정산)을 기간 거래에 시간순으로 누적한다(`core/RunningBudget`). 확인 필요·뺀 거래는 셈에 안 들어가서 값이 없다. 이번 기간 목록을 다 받았을 때만 보인다(지난달·500건 넘게 잘린 목록은 셈이 안 맞아서 숨김). 넘으면 "○원 넘음"(빨강).
- **폴더 관리는 설정 > "분류·폴더·예산"** - 전에는 "분류와 예산"이라 폴더가 어디 있는지 몰랐다. 화면에 "폴더 만들기" 줄을 따로 두고, 홈 "분류별 남은 돈"의 링크도 "분류·예산"으로.
- **백테스트 화면이 열자마자 죽던 것 고침** - 읽는 법 문장의 "손실%는"을 `String.format`에 넣어 `UnknownFormatConversionException`이 났다(3e534e7부터). `%%`로 고치고, 합성 표로 화면을 그려 보는 테스트를 더했다.

## 무음 촬영 (사용자 요청 "사진찍을 때 무음으로", 2026-11-10)

- **기본 카메라 앱 대신 앱 안 카메라, 셔터는 미리보기 한 장 뜨기** - 한국 폰의 기본 카메라 앱은 셔터 소리를 끌 수 없다. CameraX 미리보기(`PreviewView`, TextureView)를 화면 가득 띄우고 셔터를 누르면 `PreviewView.bitmap`을 JPEG 92로 저장한다. 카메라에 촬영 요청(ImageCapture)을 보내지 않으니 어느 폰에서도 소리가 나지 않는다. 해상도는 미리보기 크기(4:3, 보통 긴 변 1440~1920px)인데 서버로는 어차피 긴 변 1024px로 줄여 보낸다. 끼니 사진과 헬스 사진 인증 둘 다 - `ui/SilentCamera.kt` `rememberSilentCamera`.
- **카메라 권한(CAMERA)을 새로 묻는다** - 처음 찍을 때 한 번. 거절하면 예전처럼 기본 카메라 앱으로 찍고 "소리가 날 수 있어요"라고 알린다. 카메라 없는 기기도 설치되게 `uses-feature required=false`.
- 새 의존성: `androidx.camera:camera-camera2/lifecycle/view` 1.4.1(앱 안 카메라 미리보기).
- 되돌리려면 `rememberSilentCamera` 대신 `ActivityResultContracts.TakePicture()`(git 기록의 HealthCaptureScreen·HealthGymScreen).

## 건강 첫 화면 글자 빼기 (사용자 요청, 2026-11-10)

- **날짜 줄 밑 1끼·2끼·3끼 칸을 뺐다** - 아래 끼니 박스(먹은 끼니·예정·놓침)와 아래 "사진 찍어서 ○끼 기록" 버튼이 같은 일을 해서 겹쳤다. 되살리려면 git 기록의 `HealthScreen.MealTiles`.
- **영양 합계 카드의 "오늘 합계"·"○끼 기록 · ○일째" 글자를 뺐다** - 숫자(탄단지·칼로리와 목표)만 남는다. 기록 화면의 "이날 합계"는 그대로.
- **"이만큼 먹으면 좋아요" 같은 안내 문장을 뺐다** - 예정 끼니 박스는 "○끼 예정 · ○시까지"만, 놓친 끼니는 "○시까지"만. 주간 그래프의 "○요일은 목표보다 ○g 많았어요" 문장도 빼고 "달성 ○/○일"로 줄였다.

## 홈·거래·건강·촬영 화면 다듬기와 화면 찍기 (사용자 시안, 2026-11-10)

- **홈** - 위 "11월 ⌄"(누르면 지난 12개 기간 중 골라 그 기간 요약을 본다. 위젯·알림은 늘 이번 기간)와 "투자 ↗". 카드 하나에 "이번 달 쓸 수 있는 돈" 큰 숫자, 막대, 예산 | 사용. "분류별 남은 돈"·"최근 거래"는 굵은 머리 + "전체 보기 ›". 지난 기간을 볼 때는 할 일·최근 거래를 숨긴다(이번 기간 것이라서). "○분 전 받음" 줄은 뺐다(받지 못했을 때만 한 줄). 서버는 원래 `GET /v1/summary?month=`를 받는다.
- **거래** - 제목 줄에는 찾기만, 그 아래 "‹ 11월 ›"와 "총 ○원", 탭 이름 "전체 · 미분류 · 수입", 날짜 묶음 사이에 선, "+ 현금 지출 기록"은 목록 끝 가운데. 시안은 날짜 머리가 작았지만 앞서 받은 요청("오늘·어제는 큰 글씨, 결제 밑에 남은 돈")을 따라 날짜는 크게, 결제 밑 남은 돈도 그대로 둔다.
- **건강: 찍은 순서대로 쌓인다** - 사진을 찍으면 마감 시각과 상관없이 비어 있는 앞 끼니 번호(1끼·2끼…)로, 정한 끼니를 다 찍으면 기타 번호로 들어간다(`HealthPlan.nextMealIndex`). 다음 끼니 하나만 점선 박스로, 안에는 오늘 남은 탄·단·지·kcal만(`HealthPlan.remaining`, 넘으면 "넘음"). "놓침"·끼니마다 예정 박스는 없앴다. 끼니 마감 알림은 그대로다.
- **끼니 촬영 화면은 열자마자 카메라** - "‹ 2끼 기록 ⌄"(끼니 바꾸기는 여기), 미리보기(네 귀퉁이 꺾쇠, 손전등), 앨범 · 셔터 · "0 / 3", "최대 3장 · 영양성분표도 가능", 아래 바코드 스캔 | 직접 입력. 한 장 이상 찍으면 "음식 알아보기". 무음 촬영(미리보기 한 장 뜨기)은 화면 안 카메라(`SilentCameraView`)로, 헬스 사진 인증은 전처럼 전체 화면 카메라.
- **위젯** - 작은 위젯은 남은 돈을 "193만원"처럼 줄여 한 줄로(전에는 두 줄로 깨져 잘렸다), "확인 N"은 작은 위젯에서 뺐다. 중간 위젯 집중 페이지는 과목 2개까지(셋째 줄이 반쯤 잘렸다).
- **설정 줄 설명 빼기** - 예산 알림·알림 지우기는 "?"로, 나머지 설명 줄은 지웠다.
- **화면 찍기(`ShotsTest`·`WidgetShotsTest`)** - 앱 전체(MainActivity)를 JVM(Robolectric)에서 띄워 하단바·줄을 눌러 가며 화면마다 PNG를 남긴다. 위젯은 GlanceRemoteViews로 크기 셋 × 가계부·집중, 밝게·어둡게. 평소 테스트에서는 건너뛰고 환경 변수가 있을 때만 돈다: 로컬 서버(합성 데이터) + `SHOTS_OUT`·`SHOTS_SERVER`·`SHOTS_TOKEN`. 그래서 `MyHomeApp`을 open으로, 배경 일(주기 작업·지오펜스 등)을 `startBackground()`로 나눴다(테스트 앱은 이것만 바꾼다). 위젯 내용은 `SummaryWidgetContent`로 나눴다. 대화상자·아래 판(다른 창)과 카메라 미리보기는 안 찍힌다. 새 테스트 의존성: `androidx.work:work-testing`(JVM에서 WorkManager 켜기).

## 몸 기록과 AI 분류 공통 메모 (사용자 요청, 2026-11-10)

- **몸무게는 서버에 날짜마다 하나** - `body_weights(day, weight_kg)`(00015). `GET/PUT/DELETE /v1/health/body`. 20~400kg, 소수 한 자리. 건강 설정의 몸무게(목표 계산용)는 따로 두고 바꾸지 않는다.
- **저울을 찍으면 AI가 화면 숫자를 읽는다** - `POST /v1/health/body/scan`(사진 한 장, 끼니 사진과 같은 크기 제한·차례). Gemini에 "체중계 화면 숫자를 kg로"만 묻고(`ai/scale.go`), 저장은 하지 않는다. 앱이 읽은 값을 보여 주고 확인을 받은 뒤 PUT. 못 읽으면 직접 입력 칸을 연다. 저울 사진은 서버·폰 어디에도 남기지 않는다. mock은 늘 72.4kg.
- **몸 사진은 폰에만** - `filesDir/body_photos/{날짜}_{시각}.jpg`(긴 변 1024px). 서버로 보내지 않고, 앱 백업도 꺼져 있다(allowBackup=false). 폰을 바꾸면 옮겨지지 않는다 - 서버 보관이 필요해지면 끼니 사진처럼 올리는 경로를 더한다. 찍기는 무음 카메라(뒤 카메라).
- **화면** - 건강 첫 화면에 "몸무게" 카드(최근 값·지난번과 차이) → "몸 기록": 큰 숫자, 꺾은선(최근 90번), 저울 찍기 · 직접 입력 · 몸 사진, 기록 목록(누르면 고치기·지우기), 몸 사진 모음(누르면 크게·지우기).
- **AI 분류 공통 메모** - 분류마다 다는 설명과 따로, AI가 모든 가게에 먼저 적용하는 메모 하나(설정 `ai_classify_memo`, 1000자). 프롬프트에 "공통 메모:"로 분류 목록 앞에 한 번 싣는다. 메모를 바꾸면 분류 목록 지문이 바뀌어 같은 가게를 한 번 다시 묻는다(메모가 없으면 예전 지문 그대로라 배포해도 다시 묻지 않는다). 앱 분류·폴더·예산 맨 위 "AI 분류 메모". 웹에는 아직 칸이 없다(`PUT /v1/settings/ai_classify_memo`로는 된다).
