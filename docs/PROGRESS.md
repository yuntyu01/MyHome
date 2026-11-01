# 진행 상황

> 새 세션에서 이어갈 때: 이 파일과 `git log`부터 읽는다.

## 현재 Phase

Phase 1 완료 → Phase 2 진행 예정

## 완료된 것

### Phase 0: 저장소 뼈대
- 모노레포 디렉터리, `.gitignore`, `.env.example`, `README.md`
- `docker-compose.yml` (postgres 16 + server), 서버 멀티스테이지 `Dockerfile`
- `Makefile` (up/down/db/run/migrate/test/build/docker-build)
- 빈 서버 `GET /healthz`
- 검증: `docker compose up` → postgres healthy, `curl /healthz` → 200 (실제 실행 확인)

### Phase 1: 서버 알림 수신 API
- goose 마이그레이션 `devices`, `raw_events` (서버 시작 시 자동 적용, `myhome migrate`)
- sqlc 생성 코드 `internal/dbq`
- `POST /v1/events`: Bearer 기기 토큰 검증, 원문 저장, `content_hash` 멱등 처리
- CLI `myhome token issue|list|revoke` (`CLI_TOKEN_ISSUE_ENABLED`로 발급 끄기)
- 본문 크기 제한, 기기별·IP별 rate limit, 구조화(JSON) 로그(원문 미기록), 패닉 복구
- 멀티스테이지 Dockerfile (scratch, 약 5MB), `docs/DEPLOY.md`
- 검증: `go test ./...` 통과(정상 저장·중복·인증 실패·검증·폐기 토큰), Docker 이미지 빌드 성공,
  compose에서 실제 토큰 발급 → 201 → 재전송 200(duplicate) → 무토큰 401 확인

## 남은 것

- Phase 2: Android 수집기
- Phase 3: 해석·분류·예산 코어
- Phase 4: 카카오 로그인
- Phase 5: PC 웹
- Phase 6: 위젯·앱 화면
- Phase 7: 마무리

## 막힌 것

- 없음

## 작업 환경 메모 (이 클라우드 세션 기준)

- Docker Hub가 429(요청 제한)를 반환해서 `mirror.gcr.io/library/*` 이미지를 받아 표준 이름으로 태그해서 썼다. 사용자 환경에서는 영향 없음.
- Docker 빌드 시 이 환경에서는 `--network=host --build-arg HTTPS_PROXY=... --secret id=ca_bundle,...`가 필요했다(프록시 때문). 일반 환경에서는 `make docker-build`로 충분.
- Android SDK 다운로드 주소(dl.google.com)가 막혀 있다. Android 빌드 검증은 Phase 2에서 다시 판단.
