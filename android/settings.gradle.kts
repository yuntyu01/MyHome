pluginManagement {
    repositories {
        google()
        mavenCentral()
        gradlePluginPortal()
    }
}

dependencyResolutionManagement {
    repositoriesMode.set(RepositoriesMode.FAIL_ON_PROJECT_REPOS)
    repositories {
        google()
        mavenCentral()
    }
}

rootProject.name = "MyHome"

// core: Android에 의존하지 않는 순수 Kotlin 로직(API 클라이언트, 모델, 포맷).
// Android SDK 없이도 `gradle -p core test`로 단위 테스트를 돌릴 수 있게 별도 빌드로 둔다.
includeBuild("core")
include(":app")
