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
