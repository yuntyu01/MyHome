# 진행 상황

> 새 세션에서 이어갈 때: 이 파일과 `git log`부터 읽는다.

## 현재 Phase

Phase 0 완료 → Phase 1 진행 예정

## 완료된 것

### Phase 0: 저장소 뼈대
- 모노레포 디렉터리, `.gitignore`, `.env.example`, `README.md`
- `docker-compose.yml` (postgres 16 + server), 서버 멀티스테이지 `Dockerfile`
- `Makefile` (up/down/db/run/migrate/test/build/docker-build)
- 빈 서버 `GET /healthz`
- 검증: `docker compose up` → postgres healthy, `curl /healthz` → 200 (실제 실행 확인)

## 남은 것

- Phase 1: 알림 수신 API
- Phase 2: Android 수집기
- Phase 3: 해석·분류·예산 코어
- Phase 4: 카카오 로그인
- Phase 5: PC 웹
- Phase 6: 위젯·앱 화면
- Phase 7: 마무리

## 막힌 것

- (Phase 0) `make migrate`는 Phase 1에서 `migrate` 서브커맨드가 생긴 뒤에 동작한다.

## 작업 환경 메모 (이 클라우드 세션 기준)

- Docker Hub가 429(요청 제한)를 반환해서 `mirror.gcr.io/library/*` 이미지를 받아 표준 이름으로 태그해서 썼다. 사용자 환경에서는 영향 없음.
- Android SDK 다운로드 주소(dl.google.com)가 막혀 있다. Android 빌드 검증은 Phase 2에서 다시 판단.
