# MyHome 가계부

개인용(사용자 1명) 가계부. 폰이 결제 알림을 수집해서 서버로 보내면, 서버가 해석·분류하고 분류별 월 예산에서 차감한다. 안드로이드 홈 위젯과 PC 웹에서 확인한다.

> 작업 지침은 [`CLAUDE.md`](CLAUDE.md), 진행 상황은 [`docs/PROGRESS.md`](docs/PROGRESS.md), 결정 기록은 [`docs/DECISIONS.md`](docs/DECISIONS.md).

## 구성

```
/server   Go 서버 (net/http, pgx, sqlc, goose)
/android  Android 앱 (Kotlin, Compose, Glance, WorkManager, Room)
/docs     문서
```

## 로컬 실행

필요한 것: Docker, Go 1.24+

```bash
cp .env.example .env        # 값 채우기 (.env는 커밋 금지)
make up                     # postgres + server (docker compose)
curl localhost:8080/healthz # {"status":"ok"}
```

Docker 없이 서버만 띄우기:

```bash
make db        # postgres만 docker로
make run       # go run ./cmd/myhome serve
```

## 자주 쓰는 명령

| 명령 | 설명 |
|---|---|
| `make test` | 서버 테스트 (`TEST_DATABASE_URL`의 DB 사용) |
| `make migrate` | DB 마이그레이션 적용 |
| `make docker-build` | 서버 이미지 빌드 |
