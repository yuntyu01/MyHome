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
    compileSdk = 36

    defaultConfig {
        // Play에서는 예전 핏두잉 앱 자리를 그대로 쓴다(패키지 이름은 바꿀 수 없다). docs/DECISIONS.md 「Play 배포」
        applicationId = "com.fitdoing.app"
        minSdk = 26
        targetSdk = 36
        // 2025-01-01부터 흐른 분. 핏두잉이 올린 번호보다 항상 크고, CI가 몇 번 돌든 늘어난다.
        versionCode = buildSetting("android.versioncode").toIntOrNull()
            ?: ((System.currentTimeMillis() / 1000 - 1_735_689_600) / 60).toInt()
        versionName = buildSetting("android.versionname", "0.2.0")

        // 기본 서버 주소(선택). 앱 설정 화면에서 바꿀 수 있다.
        buildConfigField("String", "DEFAULT_SERVER_URL", "\"${buildSetting("myhome.serverUrl")}\"")
    }

    // 업로드 키(핏두잉 것과 같은 키). 값은 local.properties 또는 환경변수(KEYSTORE_FILE·KEYSTORE_PASSWORD·KEY_ALIAS·KEY_PASSWORD)로만 받는다.
    val keystoreFile = buildSetting("keystore.file")
    signingConfigs {
        if (keystoreFile.isNotEmpty()) {
            create("upload") {
                storeFile = file(keystoreFile)
                storePassword = buildSetting("keystore.password")
                keyAlias = buildSetting("key.alias")
                keyPassword = buildSetting("key.password")
            }
        }
    }

    buildTypes {
        release {
            if (keystoreFile.isNotEmpty()) signingConfig = signingConfigs.getByName("upload")
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
    implementation(libs.androidx.compose.material.icons.core)
    implementation(libs.androidx.lifecycle.runtime.compose)
    debugImplementation(libs.androidx.compose.ui.tooling)

    implementation(libs.androidx.room.runtime)
    implementation(libs.androidx.room.ktx)
    ksp(libs.androidx.room.compiler)
    implementation(libs.androidx.work.runtime.ktx)
    implementation(libs.androidx.glance.appwidget)
    implementation(libs.androidx.glance.material3)
    implementation(libs.okhttp)
    implementation(libs.play.services.code.scanner)
    implementation(libs.play.services.location)
    implementation(libs.kotlinx.serialization.json)
    implementation(libs.kotlinx.coroutines.android)

    testImplementation(libs.junit)
}
