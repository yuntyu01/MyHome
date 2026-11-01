# Android 앱 설치·확인 절차

## 0. 빌드

필요한 것: Android Studio (또는 Android SDK + JDK 17 이상)

```bash
cd android
./gradlew assembleDebug        # app/build/outputs/apk/debug/app-debug.apk
./gradlew -p core test         # 순수 Kotlin 로직 단위 테스트 (SDK 없이도 동작)
```

선택 설정은 `android/local.properties`에 넣는다(커밋 안 됨):

```properties
sdk.dir=/path/to/Android/sdk
# 앱 첫 실행 때 기본으로 들어갈 서버 주소 (설정 화면에서 바꿀 수 있음)
myhome.serverUrl=https://money.example.com
# Phase 4: 카카오 네이티브 앱 키 (docs/KAKAO_SETUP.md)
kakao.nativeAppKey=
```

> 라이브러리 버전은 `android/gradle/libs.versions.toml` 한 곳에 있다. 작업 환경에서 Google Maven을 쓸 수 없어서 앱 모듈은 실제로 빌드해 보지 못했다(`docs/PROGRESS.md` 참고). 첫 빌드에서 버전 문제가 나면 Android Studio의 업그레이드 도우미로 올리면 된다.

## 1. 설치

```bash
adb install -r app/build/outputs/apk/debug/app-debug.apk
```

또는 APK 파일을 폰으로 옮겨서 설치한다("출처를 알 수 없는 앱" 허용 필요).

## 2. 권한

앱 홈 → **권한 안내**

1. **알림 접근**: "알림 접근 설정 열기" → "MyHome 결제 알림 수집" 켜기
   - Android 13 이상에서 "제한된 설정" 안내가 뜨면: 설정 → 애플리케이션 → MyHome → 오른쪽 위 ⋮ → **제한된 설정 허용** → 다시 켜기
2. **배터리 최적화 예외**: "배터리 최적화 예외 요청" → 허용
3. **삼성 갤럭시 추가 설정** (One UI 버전에 따라 이름이 조금 다름)
   - 설정 → 애플리케이션 → MyHome → 배터리 → **제한 없음**
   - 설정 → 배터리 → 백그라운드 사용 한도 → "사용하지 않는 앱을 절전 상태로 전환" **끄기**, "절전 예외 앱"에 MyHome 추가
   - 최근 앱 화면에서 MyHome 아이콘 길게 눌러 **이 앱 잠그기**
4. **앱 알림**(Android 13+): 테스트 알림·예산 알림용

## 3. 서버 연결

앱 홈 → **설정**

1. 서버 주소 입력 → 저장 (https만 허용. 같은 Wi-Fi의 개발 PC는 `http://192.168.x.x:8080` 허용)
2. 토큰: Phase 4 이후에는 **카카오 로그인**. 그 전이나 비상시에는 서버에서 발급한 토큰을 "토큰 직접 입력"에 넣는다.
   ```
   /myhome token issue --name galaxy
   ```

## 4. 테스트 알림

앱 홈 → **테스트 알림 보내기**

- 앱이 `[MyHome 테스트]` 제목의 알림을 띄운다 → 수집기가 잡아서 대기열에 넣고 → 서버로 보낸다.
- **몇 초 안에 알림이 사라지면 성공**이다(자동 삭제가 켜져 있을 때).
- 홈의 "전송 대기"가 0건이고, **전송 로그**에 "✅ 성공 HTTP 201"이 보여야 한다.

## 5. 서버 확인

```sql
SELECT id, source_package, title, received_at FROM raw_events ORDER BY id DESC LIMIT 5;
```

테스트 알림 원문이 보이면 된다. 테스트 알림은 거래로 기록되지 않는다(Phase 3 이후).

## 6. 수집 대상 고르기

앱 홈 → **최근 알림 · 수집 대상 고르기**

- 알림 접근을 켠 뒤 알림이 온 앱들이 **실제 패키지명**과 함께 보인다.
- KB Pay, 토스, 카카오페이, 쿠팡 등 결제 알림이 오는 앱을 체크한다.
- 아직 목록에 없는 앱은 그 앱에서 알림이 한 번 와야 나타난다.

## 7. 실제 결제로 확인

1. 소액 결제를 한다.
2. 원본 앱 알림이 떴다가 **몇 초 뒤 사라지는지** 본다.
3. 서버 `raw_events`에 원문이 들어왔는지 확인한다.
4. 원본 앱의 알림 소리가 거슬리면 원본 앱 알림 채널을 **무음**으로 바꾼다(끄면 수집도 안 됨).

## 문제 해결

| 증상 | 확인할 것 |
|---|---|
| 알림이 안 사라짐 | 설정의 "자동 삭제", 전송 로그 실패 여부, 알림 접근 권한 |
| 전송 로그에 HTTP 401 | 토큰이 틀렸거나 폐기됨 → 다시 로그인하거나 토큰 재발급 |
| 한참 뒤에야 전송됨 | 배터리 최적화 예외, 삼성 절전 설정 |
| 재부팅 후 안 됨 | 알림 접근 권한이 꺼졌는지 확인. 대기열은 재부팅 후에도 유지된다 |
| "거부됨" 로그 | 서버가 형식 오류로 거부. 원문은 폰에 보관됨 → 서버 수정 후 설정의 "다시 보내기" |
