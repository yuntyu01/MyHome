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
- 기기 설치와 실제 동작, 릴리스 빌드는 미검증

## 남은 것

- 없음(Phase 0~7 완료). 실사용 후 파서 보정은 `docs/PARSERS.md` 절차로.

## 사용자 할 일

1. kodeploy 배포와 환경변수 설정 (`docs/DEPLOY.md`)
2. 카카오 디벨로퍼스 설정 (`docs/KAKAO_SETUP.md`) → `ALLOWED_KAKAO_IDS`에 본인 회원번호
3. APK 설치, 권한 허용 (`docs/ANDROID_SETUP.md`) - 기기에서 문제가 나면 메시지를 그대로 전달
4. 앱 "최근 알림"에서 실제 패키지명 확인 → 수집 대상 체크, 서버 `PARSER_PACKAGES` 설정
5. 원본 앱(KB Pay, 토스 등) 알림 채널을 무음으로
6. 1~2주 실사용 후 `myhome export-raw`로 익명화 원문을 뽑아 파서 보정 요청 (`docs/PARSERS.md`)

## 막힌 것

- 없음

## 작업 환경 메모 (이 클라우드 세션 기준)

- Docker Hub가 429(요청 제한)를 반환해서 `mirror.gcr.io/library/*` 이미지를 받아 표준 이름으로 태그해서 썼다. 사용자 환경에서는 영향 없음.
- Docker 빌드 시 이 환경에서는 `--network=host --build-arg HTTPS_PROXY=... --secret id=ca_bundle,...`가 필요했다(프록시 때문). 일반 환경에서는 `make docker-build`로 충분.
- Android SDK 다운로드 주소(dl.google.com)가 막혀 있다. Android 빌드 검증은 Phase 2에서 다시 판단.
