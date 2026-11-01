import org.jetbrains.kotlin.gradle.dsl.JvmTarget
import java.util.Properties

plugins {
    alias(libs.plugins.android.application)
    alias(libs.plugins.kotlin.android)
    alias(libs.plugins.kotlin.compose)
    alias(libs.plugins.kotlin.serialization)
    alias(libs.plugins.ksp)
}

// 비밀이 아닌 빌드 설정도 저장소에 박지 않고 local.properties(커밋 안 함) 또는 환경변수로 받는다.
val localProps = Properties().apply {
    val f = rootProject.file("local.properties")
    if (f.exists()) f.inputStream().use { load(it) }
}
fun buildSetting(key: String, default: String = ""): String =
    (localProps.getProperty(key) ?: System.getenv(key.uppercase().replace('.', '_')) ?: default)

android {
    namespace = "com.yuntyu.myhome"
    compileSdk = 35

    defaultConfig {
        applicationId = "com.yuntyu.myhome"
        minSdk = 26
        targetSdk = 35
        versionCode = 1
        versionName = "0.1.0"

        // 기본 서버 주소(선택). 앱 설정 화면에서 바꿀 수 있다.
        buildConfigField("String", "DEFAULT_SERVER_URL", "\"${buildSetting("myhome.serverUrl")}\"")

        // 카카오 네이티브 앱 키: local.properties의 kakao.nativeAppKey 또는 환경변수 KAKAO_NATIVEAPPKEY.
        // 비어 있으면 카카오 로그인 버튼이 비활성화되고 "토큰 직접 입력"만 쓸 수 있다.
        val kakaoKey = buildSetting("kakao.nativeAppKey")
        buildConfigField("String", "KAKAO_NATIVE_APP_KEY", "\"$kakaoKey\"")
        manifestPlaceholders["kakaoScheme"] = if (kakaoKey.isBlank()) "kakaodisabled" else "kakao$kakaoKey"
    }

    buildTypes {
        release {
            isMinifyEnabled = false
            proguardFiles(getDefaultProguardFile("proguard-android-optimize.txt"), "proguard-rules.pro")
        }
    }

    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }

    buildFeatures {
        compose = true
        buildConfig = true
    }
}

kotlin {
    compilerOptions {
        jvmTarget.set(JvmTarget.JVM_17)
    }
}

ksp {
    arg("room.schemaLocation", "$projectDir/schemas")
}

dependencies {
    implementation("com.yuntyu.myhome:core")

    implementation(libs.androidx.core.ktx)
    implementation(libs.androidx.activity.compose)
    implementation(libs.androidx.lifecycle.runtime.ktx)
    implementation(platform(libs.androidx.compose.bom))
    implementation(libs.androidx.compose.ui)
    implementation(libs.androidx.compose.ui.tooling.preview)
    implementation(libs.androidx.compose.material3)
    debugImplementation(libs.androidx.compose.ui.tooling)

    implementation(libs.androidx.room.runtime)
    implementation(libs.androidx.room.ktx)
    ksp(libs.androidx.room.compiler)
    implementation(libs.androidx.work.runtime.ktx)
    implementation(libs.okhttp)
    implementation(libs.kotlinx.serialization.json)
    implementation(libs.kotlinx.coroutines.android)
    implementation(libs.kakao.user)

    testImplementation(libs.junit)
}
