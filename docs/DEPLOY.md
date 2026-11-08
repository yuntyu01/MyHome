# 배포 (kodeploy)

> KoDeploy 문서와 소스를 직접 읽지 못하고 검색 요약으로만 확인했다. **확인 필요**로 표시한 항목은 대시보드에서 직접 봐야 한다.
>
> 확인한 동작: 저장소에 Dockerfile이 있으면 그것을 쓰고 없으면 nixpacks로 생성한다. 빌드는 rootless BuildKit, 클러스터는 OCI ARM, 주소는 `{앱이름}.kodeploy.com`이다.

## 1. 준비물

- PostgreSQL 16 이상. 이 서버는 **MySQL로는 돌아가지 않는다.**
  - KoDeploy의 DB 토글: 저장소 README에는 PostgreSQL도 있지만 서비스 소개에는 MySQL만 있다. **확인 필요.**
  - 토글이 넣어 주는 환경변수 이름도 **확인 필요.** `DATABASE_URL`이 아니면 직접 만들어 넣는다.
  - 토글이 없으면 외부 관리형 PostgreSQL을 쓴다.
- HTTPS 도메인 (앱이 Bearer 토큰을 보내므로 **HTTP 평문 배포 금지**)
- Cloudflare Access(이메일 코드 로그인). 웹 화면은 Access 뒤에 두고 `/v1/*`만 통과시킨다. 설정은 [`ACCESS_SETUP.md`](ACCESS_SETUP.md). 운영 설정에서 Access 값이 빠지면 서버가 시작하지 않는다.

## 2. 빌드 설정

| 항목 | 값 |
|---|---|
| 저장소 | 이 저장소. 루트의 `Dockerfile`을 쓴다 |
| 빌드 컨텍스트 | 저장소 루트 (`.dockerignore`가 `server/`만 남긴다) |
| 앱 포트 | `8080` (다른 포트만 허용되면 `HTTP_ADDR=:<포트>`) |
| 헬스체크 | `GET /healthz` → 200 (DB까지 확인, 2초 안에 안 닿으면 503) |
| 실행 명령 | 기본값(`/myhome serve`) |

`server/Dockerfile`은 로컬 compose용이다. 배포에는 쓰지 않는다.

이미지는 `scratch` 기반이라 셸이 없고 `/tmp`도 없다. 관리 명령은 둘 중 하나로 실행한다.

- **PC에서 실행** (DB에 외부에서 접속할 수 있을 때): `cd server && DATABASE_URL=... go run ./cmd/myhome <명령>`
- **컨테이너 안에서 실행**: `/myhome <명령>`. KoDeploy 터미널이 셸을 요구하면 동작하지 않는다(**확인 필요**). 그때는 루트 `Dockerfile`의 `FROM scratch`를 `FROM alpine:3`으로 바꾼다.

## 3. 환경변수

필수:

| 이름 | 설명 |
|---|---|
| `DATABASE_URL` | 예: `postgres://user:pass@host:5432/myhome?sslmode=require` |
| `TRUST_PROXY_HEADERS` | KoDeploy에서는 `true`. 프록시 뒤라서 끄면 모든 요청이 같은 IP로 잡혀 IP별 제한에 함께 걸린다 |
| `CF_ACCESS_TEAM_DOMAIN` | Zero Trust 팀 도메인 `<팀>.cloudflareaccess.com` (https:// 있어도 됨) |
| `CF_ACCESS_AUD` | 웹 Access 애플리케이션의 AUD 태그 |
| `ALLOWED_EMAILS` | 허용할 이메일(쉼표 구분, 대소문자 무시). Access 정책과 같은 값 |

선택 (기본값):

| 이름 | 기본값 | 설명 |
|---|---|---|
| `HTTP_ADDR` | `:8080` | 서버 주소 |
| `LOG_LEVEL` | `info` | `debug`/`info`/`warn`/`error` |
| `MIGRATE_ON_START` | `true` | 서버 시작 시 마이그레이션 자동 적용 |
| `MAX_BODY_BYTES` | `65536` | 요청 본문 최대 크기 |
| `RATE_LIMIT_RPS` | `5` | 기기별 초당 요청 수 (IP별은 4배) |
| `RATE_LIMIT_BURST` | `30` | 순간 허용량 (IP별은 2배) |
| `CLI_TOKEN_ISSUE_ENABLED` | `true` | `false`면 CLI 비상 토큰 발급 금지 |
| `PARSER_PACKAGES` | (빈 값) | 패키지명 → 파서 매핑. `docs/PARSERS.md` |
| `PARSER_IGNORE_PATTERNS` | (빈 값) | 결제 알림이 아니라서 무시할 문구를 더한다("문구1;문구2", 정규식 아님). 내장 광고·증권 규칙 외 |
| `APP_ENV` | `production` | `development`면 Access 없이 시작할 수 있고 개발용 로그인 허용 |
| `COOKIE_SECURE` | `true` | 세션 쿠키 Secure 속성. https 운영에서는 반드시 true |
| `SESSION_TTL` | `720h` | 서버 세션(CSRF용) 유지 기간. 웹 로그인 유지는 Access 세션 시간이 정한다 |
| `DEV_LOGIN_EMAIL` | | 로컬 개발용 로그인(`ALLOWED_EMAILS`에 있어야 함). `APP_ENV=development`에서만 허용(운영에서 넣으면 서버가 시작 거부) |

예전 카카오 변수(`KAKAO_REST_API_KEY`, `KAKAO_CLIENT_SECRET`, `KAKAO_REDIRECT_URI`, `KAKAO_APP_ID`, `KAKAO_AUTH_BASE`, `KAKAO_API_BASE`, `ALLOWED_KAKAO_IDS`, `DEV_LOGIN_KAKAO_ID`)는 읽지 않는다. 남아 있으면 서버가 시작할 때 이름만 경고로 남긴다. 지워도 된다.

건강(`/v1/health`, 핏두잉 이식). 키가 없으면 그 기능만 꺼지고 나머지는 그대로 돈다:

| 이름 | 기본값 | 설명 |
|---|---|---|
| `AI_PROVIDER` | `GEMINI_API_KEY`가 있으면 `gemini`, 없으면 `mock` | 끼니 사진 분석과 가계부 AI 분류 추천. `mock`은 키 없이 고정 결과를 준다(로컬·테스트용, 분류 추천은 작은 낱말 표). `gemini`인데 키가 없으면 서버가 시작하지 않는다 |
| `GEMINI_API_KEY` | | Google AI Studio의 Gemini API 키(사진 분석·분류 추천 같은 키). 요청 헤더로만 보내고 로그에 남기지 않는다 |
| `GEMINI_MODEL` | `gemini-3.5-flash-lite` | 사진 분석·분류 추천 모델(핏두잉 기본값). 사진 분석은 응답 헤더 `X-AI-Model`에 나온다 |
| `AI_CLASSIFY` | `on` | 가계부 AI 분류 추천(`on`/`off`). 규칙이 없는 지출의 가맹점 이름만 위 제공자로 묻고, 확인 필요 화면에 추천을 보여준다. `off`면 묻지도 보여주지도 않는다. `DECISIONS.md` 「AI 분류 추천」 |
| `MEAL_PHOTO_DIR` | | 끼니 사진을 저장할 폴더. 비우면 사진은 분석만 하고 저장하지 않는다(올리기는 503). VM은 `/var/lib/myhome/meal_photos`(`myhome.service`의 `StateDirectory`) |
| `FOODSAFETY_API_KEY` | | 식품안전나라 Open API 키(C005 바코드제품정보). 아래 키와 **둘 다** 있어야 바코드 조회가 켜진다(없으면 503) |
| `DATA_GO_KR_API_KEY` | | 공공데이터포털 키(식품의약품안전처 식품영양성분DB) |

- 헬스장 위치 기능(장소 검색, 위치 도착 인증)은 없앴다(2026-11-08). 헬스 인증은 사진과 휴식권만 쓴다.
- 끼니 사진 분석(`POST /v1/health/meals/analyze`)과 사진 보관(`PUT /v1/health/meals/photos`)만 본문을 7MB(사진 3장 × 2MB + 여유)까지 받는다. `MAX_BODY_BYTES`는 그 밖의 요청에만 적용된다. 분석 사진은 메모리에서 분석하고 버린다(저장하지 않음). 분석과 사진 보관은 종류마다 한 번에 하나씩 하고, VM 유닛은 `GOMEMLIMIT=150MiB`(`MemoryMax=200M`)다.
- 역할을 나눈 DB(`deploy/home/postgres-roles.sql`)를 쓰면 마이그레이션 뒤에 그 파일을 다시 돌려야 새 테이블(건강, 앱 연결 코드 `app_link_codes`, AI 분류 추천 `merchant_suggestions`)에 웹 역할 권한이 붙는다.

전체 목록과 예시는 `.env.example`에 있다.

## 4. 첫 배포 절차

1. DB를 만들고 `DATABASE_URL`을 넣는다.
2. 배포한다. `MIGRATE_ON_START=true`면 테이블이 자동으로 생긴다.
   - 수동으로 하려면: `/myhome migrate up`, 상태 확인은 `/myhome migrate status`
3. `https://<도메인>/healthz`가 `{"status":"ok"}`인지 확인한다.
4. 앱 연결: 앱 설정 → 서버 주소 저장 → "브라우저에서 연결하기" → 이메일 코드 로그인 → 앱으로 자동 복귀(`ACCESS_SETUP.md`). 비상시에만 CLI로 기기 토큰을 발급한다:
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
/myhome export-raw --from 2026-11-01 --to 2026-11-14 > raw.jsonl   # 익명화 내보내기
```

컨테이너 안에는 `/tmp`가 없어서 `--out /tmp/...`는 실패한다. `--out` 없이 표준출력으로 받는다.

## 6. 토큰 관리

```
/myhome token list            # 기기 목록
/myhome token revoke --id 3   # 분실한 폰의 토큰 폐기
```

## 7. 백업

`scripts/backup.sh`로 매일 백업한다. 자세한 건 [`BACKUP.md`](BACKUP.md).

## 8. 로컬에서 이미지 빌드

```bash
# 배포와 같은 이미지 (ARM 클러스터 기준, 에뮬레이션 불필요)
docker buildx build --platform linux/arm64 -t myhome-server:deploy .

# 로컬 compose와 같은 이미지 (server/Dockerfile)
make docker-build
# TLS 재암호화 프록시 뒤라면:
docker build --secret id=ca_bundle,src=/path/to/ca.crt -t myhome-server:local server
```

## 9. 자동매매 타이머 켜기·끄기 (운영 VM)

`deploy/home/deploy.sh`는 매매 타이머를 설치만 하고 켜지 않는다. 켜고 끄는 것은 사람이 VM에서 한다(deploy.sh가 두 스크립트를 `/opt/myhome/`에 넣는다).

```bash
sudo /opt/myhome/enable-invest.sh    # 켜기: 지난 실행 기록 정리 → 타이머 켜기 → status 한 번 → 다음 일정 출력
sudo /opt/myhome/disable-invest.sh   # 끄기: 타이머만 끈다. 돌고 있는 작업은 끊지 않는다
```

- 켜기 전에 `import-tq`로 TQ 상태를 옮기고 TQ 쪽 실행을 멈춘다(토스 토큰이 1개라 동시 실행 금지).
- `systemctl enable --now`를 바로 쓰지 않는다. 타이머가 `Persistent=true`라 예전에 켰던 기록(`/var/lib/systemd/timers/stamp-myhome-invest-*`)이 있으면 켜는 순간 놓친 일정(예: trade)을 돌린다. `enable-invest.sh`는 그 기록을 지우고 켠다.
- 마지막에 출력되는 `systemctl list-timers`에서 다음 실행 시각을 확인한다.

| 타이머 | 일정 (UTC) | 하는 일 |
|---|---|---|
| refresh | 금 22:43 | 주간 가격 갱신 |
| judge | 25~31일·1일 22:37, 26~31일·1~3일 11:37 | 월말 판정. 11:37은 장 시작 전 만회(평일 월말 판정이 빠졌을 때) |
| trade | 1~15일 15:17 | 주문·체결 확인 한 단계 |
| status | 월 02:23 | deadman. 성공하면 `INVEST_HEARTBEAT_URL`로 신호 |

지난달 주문 기록이 남아 trade가 "진행 중인 주문 기록이 YYYY-MM 것이다"로 멈추면(VM에서 `myhome-trader`로):

```bash
myhome invest orders                 # 남은 기록 보기 (읽기 전용)
myhome invest orders --clear-stale   # 지난 적용월 기록 지우기
```

`--clear-stale`은 증권사에 묻고, 살아 있을 수 있는 주문이 하나라도 있거나 장부에 반영되지 않은 체결이 있으면(증권사 보유와 장부가 다르면) 지우지 않는다. 이번 적용월 기록은 지우지 않는다.

밖에서 보는 자동매매 신선도는 `GET /healthz/invest`다(로그인 없음, 금액 없음). 매매 전이면 200, 마지막 성공 status가 8일보다 오래됐거나 마지막 judge·trade가 실패했으면 503. `deploy/home/heartbeat.gs`가 `/healthz`와 함께 30분마다 부른다. Access 우회는 `ACCESS_SETUP.md`.
