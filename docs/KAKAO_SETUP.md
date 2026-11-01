# 카카오 로그인 설정 (카카오 디벨로퍼스)

> 카카오 디벨로퍼스 화면의 메뉴 이름과 위치는 바뀔 수 있다. 이 문서는 작업 환경에서 실제 콘솔을 확인하지 못하고 쓴 것이라, 이름이 다르면 같은 의미의 항목을 찾으면 된다.

## 1. 앱 만들기

1. https://developers.kakao.com → 로그인 → **내 애플리케이션** → **애플리케이션 추가하기**
2. 앱 이름(예: MyHome 가계부) 입력 후 저장
3. **앱 키** 화면에서 두 값을 메모한다
   - **REST API 키** → 서버 `KAKAO_REST_API_KEY`
   - **네이티브 앱 키** → 안드로이드 `android/local.properties`의 `kakao.nativeAppKey`
4. 앱 ID(숫자)도 메모한다 → 서버 `KAKAO_APP_ID` (권장: 다른 앱에서 받은 카카오 토큰으로 기기 토큰을 받는 걸 막는다)

## 2. 카카오 로그인 켜기

1. **카카오 로그인** 메뉴 → **활성화 설정** ON
2. **Redirect URI** 등록: `https://<서버 도메인>/auth/kakao/callback`
   - 같은 값을 서버 `KAKAO_REDIRECT_URI`에 넣는다(한 글자라도 다르면 실패)
3. **동의항목**: 추가로 켤 항목 없음. 이 앱은 **회원번호**만 쓰고, 회원번호는 동의 없이 제공된다.

## 3. 보안: Client Secret (권장)

1. **보안** 메뉴 → **Client Secret 코드 생성**
2. 활성화 상태를 **사용함**으로
3. 코드를 서버 `KAKAO_CLIENT_SECRET`에 넣는다

## 4. 플랫폼 등록

### Web

- **플랫폼** → **Web 플랫폼 등록** → 사이트 도메인: `https://<서버 도메인>`

### Android

- **플랫폼** → **Android 플랫폼 등록**
  - 패키지명: `com.yuntyu.myhome`
  - **키 해시**: 앱에 서명하는 키마다 등록해야 한다(디버그 키, 릴리스 키 각각)

디버그 키 해시 구하기(macOS/Linux):

```bash
keytool -exportcert -alias androiddebugkey -keystore ~/.android/debug.keystore -storepass android -keypass android \
  | openssl sha1 -binary | openssl base64
```

릴리스 키:

```bash
keytool -exportcert -alias <별칭> -keystore <릴리스 키스토어 경로> | openssl sha1 -binary | openssl base64
```

> 키스토어 파일(`*.jks`, `*.keystore`)은 저장소에 커밋하지 않는다(`.gitignore`에 있음).

## 5. 서버 환경변수

```
KAKAO_REST_API_KEY=<REST API 키>
KAKAO_CLIENT_SECRET=<Client Secret>
KAKAO_REDIRECT_URI=https://<서버 도메인>/auth/kakao/callback
KAKAO_APP_ID=<앱 ID 숫자>
ALLOWED_KAKAO_IDS=<내 회원번호>
COOKIE_SECURE=true
```

### 내 회원번호 알아내기

처음에는 `ALLOWED_KAKAO_IDS`를 비워 둔 채 웹에서 **카카오로 로그인**을 누른다. "허용되지 않은 계정" 화면에 **회원번호**가 나온다(서버 로그에도 `허용되지 않은 카카오 회원번호`로 남는다). 그 숫자를 `ALLOWED_KAKAO_IDS`에 넣고 재배포한다. 여러 개는 쉼표로 구분한다.

## 6. 앱 빌드 설정

`android/local.properties` (커밋 안 됨):

```properties
kakao.nativeAppKey=<네이티브 앱 키>
```

빌드하면 카카오 로그인 화면용 스킴 `kakao<네이티브 앱 키>`가 자동으로 매니페스트에 들어간다.

## 7. 확인

1. 웹: `https://<도메인>/login` → 카카오로 로그인 → 대시보드가 보이면 성공
2. 앱: 설정 → 서버 주소 저장 → **카카오로 로그인** → "기기 토큰을 저장했어요" → 홈의 "기기 토큰" ✅
3. 웹 관리 → 기기 목록에 폰이 보인다. 분실 시 여기서 **폐기**하면 그 폰의 수집 요청은 401로 거부된다.

## 비상용

- 카카오 설정 전이나 장애 시: 서버에서 `/myhome token issue --name 이름` → 앱 설정의 **토큰 직접 입력**
- 카카오 로그인이 안정되면 `CLI_TOKEN_ISSUE_ENABLED=false`로 CLI 발급을 끌 수 있다(이미 발급한 토큰은 유지)

## 동작 방식 (참고)

- 웹: `/auth/kakao/login` → 카카오 인가 → `/auth/kakao/callback`(state 쿠키 검증) → 인가 코드를 토큰으로 교환 → `access_token_info`로 회원번호 확인 → 허용 목록 확인 → 세션 쿠키(HttpOnly, Secure, SameSite=Lax) 발급
- 앱: 카카오 SDK 로그인 → 카카오 액세스 토큰을 `POST /v1/auth/kakao/device`로 전송 → 서버가 `access_token_info`로 회원번호·앱 ID 확인 → 기기 토큰 발급(DB에는 해시만) → 앱은 Keystore로 암호화 저장
- 카카오 API 주소는 기본값(`https://kauth.kakao.com`, `https://kapi.kakao.com`)이며 `KAKAO_AUTH_BASE`, `KAKAO_API_BASE`로 바꿀 수 있다.
