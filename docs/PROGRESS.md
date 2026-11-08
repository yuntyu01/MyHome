# 진행 상황

> 새 세션에서 이어갈 때: 이 파일과 `git log`부터 읽는다.

## 현재 Phase

**Phase 7 완료 — 전체 완료** (이후 작업은 아래 "사용자 할 일"과 "막힌 것" 참고)

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

### Phase 2: Android v0 수집기
- `android/core` (순수 Kotlin): API 클라이언트(OkHttp), 수집 규칙, 중복 키(서버 content_hash와 동일), 서버 주소 검사, 금액 포맷
- `android/app`: `CaptureService`(NotificationListenerService) → Room 대기열 → `UploadWorker`(WorkManager, 재시도·재부팅 유지) → 성공 시 `cancelNotification`
- 화면: 홈(상태), 권한 안내(삼성 안내 포함), 최근 알림·수집 대상 선택(실제 패키지명), 설정(서버 URL, 토큰 직접 입력, 자동 삭제), 전송 로그(최근 50건)
- 토큰 Keystore 암호화 저장, 앱 백업 제외, 테스트 알림 기능
- `docs/ANDROID_SETUP.md`
- 검증: `gradle -p core test` 16개 통과(MockWebServer로 Bearer·JSON·재시도 판단 확인, Kotlin↔Go 해시 일치 확인).
  `./gradlew assembleDebug`는 당시 환경에서 실행 못 함(아래 "앱 첫 빌드"에서 확인).

### Phase 3: 서버 해석·분류·예산 코어
- 마이그레이션: `categories`(기본 분류 seed), `transactions`, `budgets`, `merchant_rules`(seed 약 100개), `settings`
- 파서: 소스별 플러그인(KB·토스 잠정, 범용), 패키지 매핑 `PARSER_PACKAGES`, 합성 픽스처 20건
- 파이프라인: 파싱 → 예외(카드대금·내 계좌·머니 충전) → 중복 합치기 → 취소 상계 → 규칙 분류 → 확인 필요. 수신 즉시 동기 처리
- 사용자 수정: 분류 변경 + "항상 이 분류" 학습(같은 가맹점 확인 필요 건 일괄 분류), 메모, 제외, 정산 분류 지정, 현금 수동 입력
- 명령: `reprocess`(수동 수정 보존, 연결 거래 포함), `process-pending`, `export-raw`(익명화)
- API: `/v1/summary`, `/v1/transactions`(GET/POST/PATCH), `/v1/review`, `/v1/categories`, `/v1/budgets`(+지난달 복사), `/v1/rules`, `/v1/settings`, `/v1/raw-events?failed=true`, `/v1/analytics`
- 남은 돈: `CategorySummary` 쿼리 하나(예산 − 확정 지출(취소 차감) + 정산)
- `docs/PARSERS.md`
- 검증: `go test ./...` 전부 통과. 파이프라인 통합 테스트 21개(중복 양방향, 같은 앱 비중복, 전액·부분·미매칭 취소,
  카드대금, 내 계좌→투자, 머니 충전 vs 티머니, 친구 송금, 해석 실패, 금액만, 테스트 알림, 멱등, 학습, 정산 되돌림,
  현금 입력, 재처리 수동 보존, 재처리 연결 일관성), API E2E 5개

### Phase 4: 카카오 로그인
- 서버: 카카오 API 인터페이스(`auth.Kakao`) + 실제 구현(`KakaoHTTP`), 웹 OAuth 로그인(state 검증), `ALLOWED_KAKAO_IDS` 허용 목록,
  DB 세션 + HttpOnly/Secure/SameSite=Lax 쿠키, CSRF 검사, 로그아웃, 개발용 로그인(development 전용)
- 앱 로그인 API `POST /v1/auth/kakao/device`(회원번호·앱 ID 검증 후 기기 토큰 발급), 기기 목록·폐기 API
- 사용자 API는 기기 토큰 또는 웹 세션으로 접근
- Android: 카카오 SDK 로그인 → 서버 검증 → Keystore 저장, 네이티브 앱 키는 빌드 설정 주입, 토큰 직접 입력 유지
- `docs/KAKAO_SETUP.md`
- 검증: 허용 계정 로그인(세션 쿠키 속성, 세션 API, CSRF 거부/허용, 로그아웃), 미허용 계정 거부, state 위조 거부,
  앱 기기 토큰 발급 → 수집 성공 → 폐기 → 401, 다른 앱 토큰·미허용·잘못된 토큰 거부, KakaoHTTP 요청 형식. core 테스트 18개 통과

### Phase 5: PC 웹
- `internal/web`: html/template + htmx + Chart.js(내장), 로그인 화면
- 대시보드(분류별 남은 돈 막대: 80% 이상 경고색, 초과 빨강 / 총 지출·수입), 거래 목록(검색·필터·분류 수정·메모·제외, 행 단위 htmx 갱신),
  확인 필요 큐(한 건씩, "이 가맹점은 항상"), 예산(기간별·분류별, 지난달 복사), 분석(분류 도넛, 일별 추이, 전월 대비, 월간 요약),
  관리(현금 입력, 분류, 규칙, 설정, 해석 실패 원문, 기기 폐기)
- 다크 모드, 모바일 레이아웃, CSP 등 보안 헤더
- 검증: 웹 핸들러 테스트 6개(전 화면 렌더링·CSP, 확인 필요 분류·학습, 행 갱신·제외, 예산 저장·복사, 관리 폼, CSRF 거부) 통과.
  Playwright로 데스크톱·모바일 × 라이트·다크 24장 캡처 확인: 콘솔 오류 없음, 가로 넘침 없음, htmx 분류·행 갱신·테마 전환 동작

### Phase 6: Android 위젯과 앱 화면
- core: 요약·거래·분류 모델, API(summary, review, transactions, categories, PATCH), 위젯 크기별 레이아웃·분류 선택·상태색 로직
- Glance 위젯(작게·중간·크게), 캐시 기반 렌더링, 전송 성공 직후 즉시 갱신 + 30분 주기 갱신, 탭 시 앱/확인 필요 항목으로 이동
- 앱 화면: 홈(이번 달 요약·분류별 남은 돈·최근 거래), 확인 필요 처리(분류 대화상자·학습), 이번 달 거래, 수집 상태
- 검증: core 테스트 24개 통과(서버 요약 JSON 해석, 상위 분류, 상태색, 최근 결제 필터, 크기별 레이아웃, 시각 포맷).
  `assembleDebug`는 당시 Phase 2와 같은 이유로 실행 못 함

### Phase 7: 마무리
- 예산 알림: 80% 도달·초과, 무음 채널, 기간·분류·단계별 1회, 설정에서 끄기 (판단 로직은 core에서 테스트)
- 월간 리포트(`/report`, `GET /v1/reports/monthly`): 지난달 요약, 예산 초과 분류, 분류별 증감, 많이 쓴 가맹점
- 대사 도우미(`/reconcile`, `GET /v1/reconcile`): 출처별 확정 지출, 제외 내역, 최근 6개 기간 출처별 표
- 백업: `scripts/backup.sh`(pg_dump custom), `scripts/restore.sh`, `docs/BACKUP.md`, `make backup`
- README 최종 정리
- 검증: 서버 `go test ./...` 전부 통과(하위 테스트 포함 83개), core 테스트 29개 통과, Docker 이미지 빌드 후 compose로 기존 DB에 마이그레이션 3개 적용·healthz/login/static 확인,
  백업 → 새 DB 복구 → 건수 일치 확인, Playwright로 리포트·대사 화면 확인

### 최종 검토
- 빌드할 수 없는 Android 앱 코드를 별도 검토(Glance API 시그니처 대조 등). 컴파일 실패 확정 항목 없음.
  런타임 문제 4건(위젯 갱신 지연, 큰 위젯 행 잘림, 평문 HTTP 차단, 앱 이름 조회 불가)과 작은 문제들을 고침(`DECISIONS.md` "최종 검토")

### 디자인 개편 (modo 스타일, 사용자 요청)
- 웹: modo 테마 토큰(색·그라데이션·모서리), Pretendard, 큰 제목 + 글자 탭, 휴대폰 폭 알약 하단바와 "더보기" 화면, 거래 목록을 카드형으로, 스위치·알약 버튼
- 앱: 고정 modo 색 체계(다크·라이트), Pretendard, 그라데이션 바탕, 떠 있는 알약 하단바, 카드형 목록, 위젯 색
- 검증: 서버 테스트 전부 통과, Playwright로 휴대폰·PC × 다크·라이트 확인(콘솔 오류·가로 넘침 없음, Pretendard 적용 확인). 앱 빌드는 당시 미검증

### 앱 첫 빌드 (2026-11-02)
- 로컬 WSL에서 `:app:assembleDebug` 성공(코드 수정 없음). 서버 테스트(DB 포함)와 core 테스트도 통과
- 빌드가 만드는 Room 스키마 `android/app/schemas/`를 저장소에 추가
- 2026-11-02 에뮬레이터(Android 16)에서 처음 실행: 앱 화면과 위젯이 합성 데이터로 그려지는 것 확인. 실제 폰, 알림 수집, 카카오 로그인, 릴리스 빌드는 미검증

### 화면 단순화와 버그 수정 (2026-11-02, 사용자 요청)
- 거래 행 접기, 홈 분류 정렬·접기, 관리 탭 분리, 쉬운 말, 휴대폰 하단바 글자(웹·앱), 자세한 내용과 되돌리는 법은 `DECISIONS.md`
- 버그: CU·KT·SKT 지점명 자동 분류, 범용 파서 가맹점 조각, 첫 `make up` 실패, 리포트 여백, 관리 화면 잘림(설정·기기 섹션이 안 보였음)
- 앱 첫 실행에서 찾아 고침: 다크 모드에서 글자가 검게 나와 안 보임, 위젯 기본 크기에서 최근 결제 줄 잘림
- 검증: 서버 테스트 통과, core 테스트 32개 통과, Playwright로 저장·펼치기 흐름 21개 통과(콘솔 오류 없음), 첫 실행은 DB 초기화를 늦춰 옛 설정 3/3 실패 → 새 설정 0/3 실패 확인
- 2026-11-06 에뮬레이터에서 새 색(무채색·라벤더·살구)을 앱 홈과 위젯, 다크·라이트로 확인. 테스트 알림으로 최근 알림 목록 → 앱 선택 → 서버 저장·해석 → 알림 삭제까지 확인, 선택 전 알림은 보내지 않는 것도 확인
- 그때 찾아 고침: 최근 알림 목록이 안드로이드의 묶음 요약 알림까지 세서 건수가 늘고 마지막 제목이 빈칸이 됨, 묶을 때 다시 보내진 알림을 두 번 셈. core 테스트 33개 통과

### 하단바와 투자 칸 (2026-11-08, 사용자 요청)
- 앱 하단바 홈·거래·확인·투자·설정, 휴대폰 웹 하단바 홈·거래·확인·투자·더보기(분석은 더보기로)
- 앱 투자 칸과 평가 화면: 서버 `GET /v1/invest`, `/v1/invest/value`(읽기 전용)
- 검증: 서버 테스트(DB 포함, 자동매매 포함) 통과, 새 테스트 2개(앱 투자 조회·하단바 5칸), core 테스트 35개 통과, APK 빌드. 데모 DB에 합성 투자 상태를 넣고 기기 토큰으로 조회 확인. 에뮬레이터 화면 확인은 못 함(에뮬레이터 부팅이 끝나지 않음)

### 건강 서버 이식 (2026-11-08, 핏두잉 서버에서)
- 완료: 핏두잉 서버의 개인용 부분을 `/v1/health` 아래로 옮김 - 오늘·달력·하루 카드·주간 그래프, 끼니 저장·조회·삭제(음식별 먹은 비율, 나눠 먹음, 기타 7~9), 사진 분석(Gemini·mock, 1~3장), 헬스 인증(위치·사진), 주간 휴식권, 장소 검색(카카오), 바코드 조회, 설정·영양 목표 추천·날짜별 목표. 마이그레이션 `00006_health.sql`, 패키지 `internal/ai`·`internal/health`(+`places`·`foods`). 뺀 것과 바뀐 점은 `DECISIONS.md` 「건강」
- 검증: `gofmt`·`go vet` 통과, DB 포함 전체 테스트 통과. 새 테스트는 단위(ai 8·health 10·foods 4·places 2·config 1)와 서버 14개(기기 토큰): 인증 필요, 웹 세션+CSRF, 설정 왕복·부분 저장·목표 이력, 끼니 덮어쓰기·삭제·검증, 오늘·달력·하루·주간, 기타 끼니, 휴식권 주간 초기화·중복·이미 인증한 날, mock 사진 분석(여러 장·너무 큰 사진·16MB 초과), 추천, 장소·바코드(가짜 서버). 사진 경로의 본문 제한을 일부러 빼면 테스트가 실패하는 것 확인. 마이그레이션 up → down → up 확인
- 미검증: 실제 Gemini 키로 사진 분석, 실제 식품안전나라·공공데이터포털 호출(키 없음), 실제 폰 앱에서 호출(앱 화면은 따로 작업 중). 카카오 장소 검색은 2026-11-08에 뺐다

### 건강 앱 화면 (2026-11-08, 핏두잉 앱을 참고해 Kotlin으로)
- 완료: 하단바 건강 칸(홈·거래·건강·투자·설정, 확인은 거래 칸 점·띠로). 오늘(요일 줄, 끼니 칸, 오늘 합계, 식단 표, 주간 그래프, 촬영 버튼), 끼니 기록(사진 1~3장·앨범 → 분석 → 음식 고치기·먹은 양·나눠 먹음 → 저장, 고치기·지우기), 기록 달력과 하루 카드, 헬스(위치 도착 확인·사진 인증·휴식권), 건강 설정(끼니·헬스·헬스장 검색·취침·영양 목표 추천·알림·사진 보관), 건강 알림(끼니 마감 30분 전·마감, 헬스 1시간 전, 취침)
- 검증: core 테스트 43개 통과(끼니 일정·먹은 양·알림 일정 8개 새로), APK 빌드. 데모 서버(mock AI)에 앱과 같은 요청으로 설정 저장·추천·사진 분석·끼니 저장·오늘·주간·하루·달력·헬스 인증·휴식권 409·삭제 확인
- 미검증: 에뮬레이터·실제 폰에서 화면, 카메라·위치 권한 흐름, 알림이 제때 울리는지, 실제 Gemini 분석

### 로그인: Cloudflare Access + 구글, 헬스장 위치·실패 메일 제거 (2026-11-08, 사용자 결정)
- 완료(서버): 카카오 로그인을 없애고 웹은 Cloudflare Access(구글) 뒤로. 서버도 모든 웹 요청의 Access JWT를 직접 검증(표준 라이브러리, JWKS 캐시·모르는 kid 재수신 1분 제한), 세션은 CSRF용으로 JWT 이메일에 묶음, 운영에서 Access 설정이 없으면 `serve` 시작 거부, 개발용 로그인은 `DEV_LOGIN_EMAIL`. 앱 첫 연결은 `GET /app/link`(Access 뒤) → `myhome://auth?code=&state=` → `POST /v1/auth/exchange`(1회용 코드 5분 + PKCE S256). 마이그레이션 `00007_access_login.sql`(세션 이메일, `devices.email`, `app_link_codes`). `docs/KAKAO_SETUP.md` → `docs/ACCESS_SETUP.md`. 결정은 `DECISIONS.md` 「로그인: Cloudflare Access + 구글」
- 완료(서버): 헬스장 위치 기능 제거 - 장소 검색 API·`internal/health/places`·`KAKAO_REST_API_KEY`, 설정의 헬스장 위치 필드, 위치 도착 인증(`geofence`는 400, 예전 기록은 그대로 읽힘). 실패 메일 제거에 맞춰 문서 정리(코드는 메인 세션)
- 검증: `gofmt`·`go vet` 통과, DB 포함 전체 테스트 통과. 새 테스트: 검증기 단위(정상·aud 문자열/배열·대소문자·허용 오차, 거부 17가지를 사유까지, 모르는 kid 재수신과 1분 제한, 키 서버 실패 503·5초 재시도), 서버(JWT로 화면·세션 재사용·CSRF, JWT 없는 폼 403, Access 쿠키, 거부 7가지 × 세션 유무, htmx 401, 카카오 경로 404, 계정이 바뀐 세션, 키 서버 다운 503, 운영 Access 없음 503, 개발용 로그인, 로그아웃, 앱 연결 화면·잘못된 값 9가지·교환 성공/재사용/만료/불일치 후 소모/형식 오류 6가지/기기 이름), 설정(시작 거부·팀 도메인·이메일·개발용 로그인·예전 변수). 검사 8곳을 일부러 망가뜨려 테스트가 잡는지 확인. 마이그레이션 up → down → up(예전 카카오 기기 행 보존) 확인
- 미검증: 실제 Cloudflare Access·JWKS·Tunnel(작업 환경에서 접속 불가, 가짜 키 서버로만 확인), Access 경로 Bypass(`/v1`)가 하위 경로를 덮는지, cloudflared의 `X-Forwarded-For`, 실제 폰에서 브라우저 → `myhome://auth` 복귀

### AI 분류 추천 (2026-11-08, 사용자 결정)
- 완료(서버·웹): 규칙이 없어서 확인 필요로 온 지출(`no_rule`, 나간 돈, 가맹점 이름 있음)에 서버가 Gemini로 분류를 추천한다. 가맹점 이름과 지출 분류 이름만 보내고, 가맹점마다 한 번만 물어 `merchant_suggestions`(마이그레이션 00008)에 저장한다. `myhome serve` 안의 작업(`internal/suggest`)이 수집 직후 신호와 1분 주기로 20개씩 묻고, 실패·패닉은 기다렸다가(1분부터 두 배, 최대 30분) 다시 한다. 확신 0.6 미만·"모름"·보관한 분류는 보여주지 않는다(보관한 분류는 다시 묻는다). 거래 JSON에 `suggestion`(`/v1/review`, `/v1/transactions`, `/v1/summary`의 `recent`), 웹 확인 필요 화면에 "AI 추천: 분류" 버튼(누르면 그 분류 + "이 가맹점은 항상"). 확정은 기존 PATCH `remember_rule` 경로 그대로. `AI_CLASSIFY=off`로 끄고, `AI_PROVIDER=mock`이면 낱말 표로 고정 추천. 결정은 `DECISIONS.md` 「AI 분류 추천」
- 검증: `gofmt`·`go vet` 통과, DB 포함 전체 테스트 통과. 새 테스트 16개: ai 3(요청 글이 고정 문구 + 분류 + 가맹점 이름뿐, enum, 이상한 답은 그 가맹점만 모름, 응답 모양 오류는 실패이고 에러에 이름 없음, mock 낱말 표), config 1(`AI_CLASSIFY`), suggest 8(20개씩 묶기·가맹점마다 한 번·저장된 가맹점 다시 안 묻기·호출 상한, `no_rule`·지출·이름 있음만 보내고 결제대행사·송금·입금·확정·이름 없음·취소·들어온 돈은 안 보냄, 가짜 Gemini 서버로 요청 본문 전체 확인(금액·시각·카드 문구 없음), 낮은 확신 숨김·모름 저장, 보관한 분류 숨김·다시 묻기·이름 바뀐 분류, 패닉·실패 뒤 다시 해서 저장·종료, 수집 신호로 깨우기, 마이그레이션 down/up), 서버 4(API JSON 모양·null·PATCH로 확정하면 학습 규칙과 같은 가맹점 함께 분류, 꺼짐이면 null, 웹 버튼 한 번에 분류·학습·메모, 낮은 확신은 버튼 없음, 꺼짐이면 버튼 없음). 검사 10곳을 일부러 망가뜨려 테스트가 다 잡는 것 확인. 실제 `myhome serve`(mock, 격리 스키마)에서 수집 직후 추천이 붙고, 로그에 건수만 남고(가맹점 이름 0건), SIGTERM에 정상 종료하는 것 확인. Playwright로 확인 필요 화면을 휴대폰·PC × 라이트·다크로 보고 버튼 눌러 다음 건으로 넘어가는 것 확인(콘솔 오류·가로 넘침 없음)
- 미검증: 실제 Gemini로 분류(작업 환경에 키가 없어 가짜 서버로만 확인. 운영 키로 처음 돌 때 응답 모양·enum 지원·추천 품질 확인 필요), 앱 화면(앱은 메인 세션이 `suggestion`을 읽어 붙인다)

## 남은 것

- 없음(Phase 0~7 완료). 실사용 후 파서 보정은 `docs/PARSERS.md` 절차로.

## 사용자 할 일

1. kodeploy 배포와 환경변수 설정 (`docs/DEPLOY.md`)
2. Cloudflare Tunnel + Access 설정 (`docs/ACCESS_SETUP.md`) → 서버 `CF_ACCESS_TEAM_DOMAIN`·`CF_ACCESS_AUD`·`ALLOWED_EMAILS`, 앱에서 "브라우저에서 연결하기"
3. 실제 폰에 APK 설치, 권한 허용 (`docs/ANDROID_SETUP.md`) - 문제가 나면 메시지를 그대로 전달
4. 앱 "최근 알림"에서 실제 패키지명 확인 → 수집 대상 체크, 서버 `PARSER_PACKAGES` 설정
5. 원본 앱(KB Pay, 토스 등) 알림 채널을 무음으로
6. 1~2주 실사용 후 `myhome export-raw`로 익명화 원문을 뽑아 파서 보정 요청 (`docs/PARSERS.md`)

## 막힌 것

- 없음

## 작업 환경 메모 (이 클라우드 세션 기준)

- Docker Hub가 429(요청 제한)를 반환해서 `mirror.gcr.io/library/*` 이미지를 받아 표준 이름으로 태그해서 썼다. 사용자 환경에서는 영향 없음.
- Docker 빌드 시 이 환경에서는 `--network=host --build-arg HTTPS_PROXY=... --secret id=ca_bundle,...`가 필요했다(프록시 때문). 일반 환경에서는 `make docker-build`로 충분.
- Android SDK 다운로드 주소(dl.google.com)가 막혀 있다. Android 빌드 검증은 Phase 2에서 다시 판단.

## 자동매매 이식 (2026-11-06, 진행 중)

- 완료: `server/internal/invest` — 신호·거래일·계좌·preflight(도메인), 토스·야후 클라이언트, PostgreSQL 저장소(`00005_invest.sql`), 명령(refresh·judge·order·trade·execute·amend·adjust·tax·contribute·deviate·cancel·ledger·status·verify·why·reconcile·import-tq, `alert`는 2026-11-08에 뺌), systemd 유닛·DB 역할(`deploy/home/`)
- 검증
  - 판정 픽스처 336행, 계좌 산술(무작위 불변식 포함), 거래일 달력(2010~2026 실데이터 대조), 토스 28+5개, 야후, 매매 시나리오(DB) 통과
  - 매매 안전장치 4곳을 일부러 망가뜨려 테스트가 잡는지 확인(이중 주문, 매도대금 누락, dry-run 확정, 주문번호 0-패딩)
  - 실제 TQ 상태로 `import-tq` 후 status·order·trade --dry-run·judge --dry-run·verify·why·tax를 TQ(Python)와 나란히 돌려 출력 일치
  - 야후 실시세 대조(SPX·ETF 오차 0, NDX 1.4e-7)
- 미검증: 토스 실서버 호출(모의투자가 없어 첫 주문이 실전), VM 위 systemd 실행, 외부 하트비트 실제 수신(실패 메일은 2026-11-08에 뺐다)

### 운영 전환 전에 할 것

1. VM 만들기: kodeploy `terraform/oci-infra/home.tf` plan 확인 후 apply (예약 공인 IP 확인)
2. 토스 WTS → 설정 → Open API에 VM 공인 IP 등록
3. 주문 없는 읽기 전용 호출로 확인: `myhome invest reconcile` — 확인할 것: 매수가능 금액(USD)에 통합증거금 원화가 반영되는지, 평균 매입가 통화, 종료 주문 조회 날짜 기준, MOC 접수 시간
4. 전환 시점은 판정 창(25~31일)·실행 창(1~8일)을 피해 9~24일. 그 사이 TQ 실행을 멈추고(토큰이 1개뿐이라 동시 실행 금지) `import-tq`로 옮긴다
5. 그 달까지는 TQ(손으로 사고 fill.yml로 기록)를 그대로 쓴다
