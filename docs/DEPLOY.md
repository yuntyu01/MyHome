# 배포 (kodeploy)

> kodeploy의 화면 이름이나 설정 방법은 이 저장소에서 확인하지 못했다. 아래는 **일반적인 컨테이너 배포 기준**이다. kodeploy에서 같은 의미의 항목을 찾아서 넣으면 된다.

## 1. 준비물

- PostgreSQL 16 이상 (kodeploy가 제공하는 DB 또는 별도 관리형 DB)
- HTTPS 도메인 (앱이 Bearer 토큰을 보내므로 **HTTP 평문 배포 금지**)

## 2. 빌드 설정

| 항목 | 값 |
|---|---|
| 빌드 방식 | Dockerfile |
| 빌드 컨텍스트 | `server/` |
| Dockerfile 경로 | `server/Dockerfile` |
| 컨테이너 포트 | `8080` (`HTTP_ADDR`로 변경 가능) |
| 헬스체크 | `GET /healthz` → 200 |
| 실행 명령 | 기본값(`/myhome serve`) |

이미지는 `scratch` 기반이라 셸이 없다. 관리 명령은 `/myhome <명령>` 형식으로 실행한다.

## 3. 환경변수

필수:

| 이름 | 설명 |
|---|---|
| `DATABASE_URL` | 예: `postgres://user:pass@host:5432/myhome?sslmode=require` |

선택 (기본값):

| 이름 | 기본값 | 설명 |
|---|---|---|
| `HTTP_ADDR` | `:8080` | 서버 주소 |
| `LOG_LEVEL` | `info` | `debug`/`info`/`warn`/`error` |
| `MIGRATE_ON_START` | `true` | 서버 시작 시 마이그레이션 자동 적용 |
| `MAX_BODY_BYTES` | `65536` | 요청 본문 최대 크기 |
| `RATE_LIMIT_RPS` | `5` | 기기별 초당 요청 수 (IP별은 4배) |
| `RATE_LIMIT_BURST` | `30` | 순간 허용량 (IP별은 2배) |
| `TRUST_PROXY_HEADERS` | `false` | 리버스 프록시 뒤라면 `true` (X-Forwarded-For 사용) |
| `CLI_TOKEN_ISSUE_ENABLED` | `true` | `false`면 CLI 비상 토큰 발급 금지 |
| `PARSER_PACKAGES` | (빈 값) | 패키지명 → 파서 매핑. `docs/PARSERS.md` |
| `APP_ENV` | `production` | `development`면 개발용 로그인 허용 |
| `KAKAO_REST_API_KEY` | | 카카오 REST API 키 (`docs/KAKAO_SETUP.md`) |
| `KAKAO_CLIENT_SECRET` | | 카카오 Client Secret |
| `KAKAO_REDIRECT_URI` | | `https://<도메인>/auth/kakao/callback` |
| `KAKAO_APP_ID` | | 카카오 앱 ID(숫자). 앱 로그인 토큰 검증용 |
| `ALLOWED_KAKAO_IDS` | | 허용할 카카오 회원번호(쉼표 구분) |
| `COOKIE_SECURE` | `true` | 세션 쿠키 Secure 속성. https 운영에서는 반드시 true |
| `SESSION_TTL` | `720h` | 웹 세션 유지 기간 |
| `KAKAO_AUTH_BASE` | `https://kauth.kakao.com` | 카카오 인증 서버 주소 |
| `KAKAO_API_BASE` | `https://kapi.kakao.com` | 카카오 API 서버 주소 |
| `DEV_LOGIN_KAKAO_ID` | | 로컬 개발용 로그인. `APP_ENV=development`에서만 허용(운영에서 넣으면 서버가 시작 거부) |

전체 목록과 예시는 `.env.example`에 있다.

## 4. 첫 배포 절차

1. DB를 만들고 `DATABASE_URL`을 넣는다.
2. 배포한다. `MIGRATE_ON_START=true`면 테이블이 자동으로 생긴다.
   - 수동으로 하려면: `/myhome migrate up`, 상태 확인은 `/myhome migrate status`
3. `https://<도메인>/healthz`가 `{"status":"ok"}`인지 확인한다.
4. 기기 토큰 발급 (카카오 로그인을 붙이기 전이나 비상시):
   ```
   /myhome token issue --name galaxy
   ```
   출력된 토큰은 **한 번만** 보인다. 앱 설정의 "토큰 직접 입력"에 넣는다.
5. 수집 테스트:
   ```bash
   curl -X POST https://<도메인>/v1/events \
     -H "Authorization: Bearer <토큰>" -H "Content-Type: application/json" \
     -d '{"source_package":"test","title":"테스트","text":"1,000원","posted_at":1790000000000}'
   ```
   처음은 201, 같은 요청을 다시 보내면 200과 `"duplicate":true`.

## 5. 운영 명령

```
/myhome process-pending                                   # 처리 안 된 원문 처리(서버 시작 때도 자동)
/myhome reprocess --from 2026-11-01 --to 2026-11-30       # 재처리 계획
/myhome reprocess --from 2026-11-01 --to 2026-11-30 --apply
/myhome export-raw --from 2026-11-01 --to 2026-11-14 --out /tmp/raw.jsonl   # 익명화 내보내기
```

## 6. 토큰 관리

```
/myhome token list            # 기기 목록
/myhome token revoke --id 3   # 분실한 폰의 토큰 폐기
```

## 7. 백업

`scripts/backup.sh`로 매일 백업한다. 자세한 건 [`BACKUP.md`](BACKUP.md).

## 8. 로컬에서 이미지 빌드

```bash
make docker-build
# TLS 재암호화 프록시 뒤라면:
docker build --secret id=ca_bundle,src=/path/to/ca.crt -t myhome-server:local server
```
