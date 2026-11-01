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
- **http 서버 주소는 localhost·10.0.2.2·사설망(192.168.*, 10.*)만 허용** — 토큰 평문 전송 방지. 개발 편의는 유지 — `core/ServerUrl.kt`. 단, Android 9+는 기본적으로 평문 HTTP를 막으므로 개발 서버에 http로 붙으려면 network security config가 필요하다(현재 미포함, https 권장).
