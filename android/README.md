# Android 앱

- `app/`: Android 앱 (Compose UI, NotificationListenerService, Room 대기열, WorkManager 전송, Glance 위젯)
- `core/`: Android에 의존하지 않는 순수 Kotlin 로직 (API 클라이언트, 모델, 금액 포맷, 수집 규칙). 포함 빌드(`includeBuild`)라서 SDK 없이도 테스트할 수 있다.

```bash
./gradlew assembleDebug    # APK 빌드 (Android SDK 필요)
./gradlew -p core test     # core 단위 테스트
```

설치·권한·확인 절차는 [`docs/ANDROID_SETUP.md`](../docs/ANDROID_SETUP.md).
