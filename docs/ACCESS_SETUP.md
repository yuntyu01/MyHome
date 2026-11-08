# 로그인 설정 (Cloudflare Tunnel + Access, 이메일 코드)

> 작업 환경에서 Cloudflare 대시보드와 실제 Access에 접속해 확인하지 못했다. 메뉴 이름과 위치는 바뀔 수 있다. **미확인** 표시는 대시보드에서 직접 확인할 것.

## 구조

- 운영: OCI VM 한 대, 들어오는 포트 없이 Cloudflare Tunnel로만 받는다. 주소는 `myhome.kodeploy.com`.
- **웹 화면**(`/`, `/transactions`, `/app/link` 등): Cloudflare Access가 로그인(이메일로 받은 6자리 코드)으로 막는다. 서버도 모든 웹 요청의 Access JWT(`Cf-Access-Jwt-Assertion` 헤더, 없으면 `CF_Authorization` 쿠키)를 직접 검증한다. Access 설정이 잘못돼 요청이 그대로 들어와도 서버가 403으로 막는다.
- **앱 API**(`/v1/*`): Access를 거치지 않는다(Bypass). 기기 토큰(Bearer)으로 인증한다. 기기 토큰은 만료되지 않고, 웹 관리 → 기기에서 해제한다.
- 앱 첫 연결은 브라우저에서 Access 로그인 → 서버가 1회용 코드만 담아 `myhome://auth`로 앱을 부름 → 앱이 코드와 PKCE verifier로 기기 토큰을 받는다(`POST /v1/auth/exchange`). URL에는 기기 토큰이 실리지 않는다.

## 1. Tunnel

1. Zero Trust → Networks → Tunnels → 터널 만들기(cloudflared). 안내하는 설치 명령을 VM에서 실행한다.
2. Public hostname: `myhome.kodeploy.com` → 서비스 `http://127.0.0.1:8080`.
3. 서버는 `HTTP_ADDR=127.0.0.1:8080`으로 VM 안에서만 열고, `TRUST_PROXY_HEADERS=true`로 둔다(IP별 요청 제한이 실제 접속 IP를 보게). cloudflared가 `X-Forwarded-For`를 그대로 넘기는지는 **미확인**.

## 2. 로그인 방법: 이메일 코드(One-time PIN)

- 2026-11-08 실제 설정은 **이메일 코드만** 쓴다(Zero Trust → Settings → Authentication → Login methods의 One-time PIN). 구글 OAuth 클라이언트를 만들 필요가 없다.
- 구글 로그인으로 바꾸려면 Login methods에 Google을 추가하고 안내대로 Google Cloud Console에서 OAuth 클라이언트를 만든다(리디렉션 URI는 화면 안내 값). 서버 코드는 그대로다(허용 목록이 이메일 기준).

## 3. Access 애플리케이션 두 개

**A. 웹(허용)**

- Access → Applications → 추가 → Self-hosted
- 도메인 `myhome.kodeploy.com`, 경로 비움(전체)
- 로그인 방법: One-time PIN(이메일 코드)
- 정책: Action **Allow**, Include → Emails → 본인 이메일 하나
- 세션 시간: 730h(2026-11-08 설정값)

**B. 앱 API(통과)**

- Self-hosted를 하나 더: 도메인 `myhome.kodeploy.com`, 경로 `v1`
- 정책: Action **Bypass**, Include → Everyone
- 2026-11-08 실제 설정: 앱 이름 "myhome 앱 API (우회)", `/v1`과 `/healthz`를 우회. 실측으로 `/v1/summary`는 서버가 401(기기 토큰 검사), `/v1health`처럼 `/v1/`로 시작하지 않는 경로는 Access 로그인으로 넘어갔다
- 경로가 더 구체적인 애플리케이션이 우선해서 `/v1/*`만 Access를 건너뛴다(**미확인**: `v1` 경로가 하위 경로 전체를 덮는지, `v1/*`로 써야 하는지).

`/healthz`는 A에 막힌다. VM 안의 헬스체크(`myhome healthcheck`)는 상관없다. 밖에서 감시하려면 `/healthz`에도 B 같은 Bypass를 더한다.

## 4. 팀 도메인과 AUD 태그

- **팀 도메인**: Zero Trust → Settings → Custom domains(또는 General)의 Team domain, `<팀>.cloudflareaccess.com` → 서버 `CF_ACCESS_TEAM_DOMAIN` (https:// 있어도 됨)
- **AUD 태그**: Access → Applications → **A(웹)** → Configure → Overview의 Application Audience (AUD) Tag → 서버 `CF_ACCESS_AUD`. B(Bypass)의 AUD가 아니다.
- 서버는 서명 키를 `https://<팀>.cloudflareaccess.com/cdn-cgi/access/certs`에서 받아 캐시한다. VM에서 그 주소로 나가는 연결이 되어야 한다.

## 5. 서버 환경변수

| 이름 | 예 | 설명 |
|---|---|---|
| `CF_ACCESS_TEAM_DOMAIN` | `myteam.cloudflareaccess.com` | 팀 도메인 |
| `CF_ACCESS_AUD` | `4714c1…` | 웹 애플리케이션(A)의 AUD 태그 |
| `ALLOWED_EMAILS` | `me@gmail.com` | 허용할 이메일(쉼표 구분, 대소문자 무시). Access 정책과 같은 값 |
| `APP_ENV` | `production` | `development`가 아니면 위 세 값이 다 있어야 서버가 시작한다 |
| `COOKIE_SECURE` | `true` | 서버 세션 쿠키(CSRF용) |
| `DEV_LOGIN_EMAIL` | | 로컬 개발 전용. `APP_ENV=development`이고 Access 값이 없을 때만 `/auth/dev-login`이 열린다 |

## 6. 확인

1. 브라우저로 `https://myhome.kodeploy.com` → 이메일 코드 로그인 → 홈 화면
2. `curl -i https://myhome.kodeploy.com/` → Access 로그인 페이지로 리디렉션(서버까지 오지 않음)
3. `curl -i https://myhome.kodeploy.com/v1/summary` → 서버가 준 `401 {"error":"unauthorized"}`(Bypass가 동작함)
4. VM에서 `curl -i http://127.0.0.1:8080/` → 403 "Cloudflare Access 로그인이 필요해요"(서버 안전망)
5. 앱: 설정 → 서버 주소 저장 → "브라우저에서 연결하기" → 이메일 코드 로그인 → 앱으로 자동 복귀. 저절로 안 돌아오면 화면의 "앱으로 돌아가기"를 누른다. 웹 관리 → 기기에 "앱 연결"로 보인다.

## 7. 문제와 바꾸는 법

- 403이 계속 나면 서버 로그의 `Access JWT 거부` 사유를 본다: `aud 불일치` → `CF_ACCESS_AUD`, `iss 불일치` → `CF_ACCESS_TEAM_DOMAIN`, `허용되지 않은 이메일` → `ALLOWED_EMAILS`.
- 503 "잠시 후 다시 시도해 주세요"는 서명 키를 못 받은 것이다(VM의 바깥 연결 확인).
- 계정을 더할 때는 Access 정책(A)과 `ALLOWED_EMAILS`를 둘 다 바꾼다.
- 로그아웃은 서버 세션을 지우고 `/cdn-cgi/access/logout`으로 보내 Access 로그인도 끊는다.
- 앱 연결이 막히면 비상용으로 `myhome token issue --name <이름>` 토큰을 앱의 "토큰 직접 입력"에 넣는다(`CLI_TOKEN_ISSUE_ENABLED=true`일 때).
