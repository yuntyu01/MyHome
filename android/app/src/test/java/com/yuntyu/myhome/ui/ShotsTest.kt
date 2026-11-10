package com.yuntyu.myhome.ui

import android.Manifest
import android.content.Context
import android.graphics.Bitmap
import android.graphics.Canvas
import android.os.PowerManager
import androidx.compose.ui.test.SemanticsNodeInteraction
import androidx.compose.ui.test.hasClickAction
import androidx.compose.ui.test.hasContentDescription
import androidx.compose.ui.test.hasText
import androidx.compose.ui.test.junit4.createEmptyComposeRule
import androidx.compose.ui.test.performClick
import androidx.compose.ui.test.performTouchInput
import androidx.test.core.app.ActivityScenario
import androidx.work.testing.WorkManagerTestInitHelper
import com.yuntyu.myhome.MyHomeApp
import com.yuntyu.myhome.security.TokenStore
import org.junit.Assume.assumeTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.Shadows.shadowOf
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode
import java.io.File

/**
 * 앱 화면 전부 찍기(사용자 요청 "앱 화면 전부 찍어줘", 2026-11-10). 평소 테스트에서는 건너뛴다.
 * 로컬 서버(합성 데이터)를 띄우고 이렇게 돌린다:
 *   SHOTS_OUT=/어디/shots SHOTS_SERVER=http://127.0.0.1:18080 SHOTS_TOKEN=<기기 토큰> ./gradlew :app:testDebugUnitTest --tests '*ShotsTest*'
 * 앱 전체(MainActivity)를 JVM에서 띄우고 하단바·줄을 눌러 가며 화면마다 PNG를 남긴다. 대화상자·아래 판(다른 창)은 안 찍힌다.
 * 카메라는 JVM에서 켜지지 않아 촬영 화면의 미리보기 칸은 검게 나온다.
 */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34], application = ShotsTest.ShotApp::class, qualifiers = "w411dp-h891dp-night-xxhdpi")
@GraphicsMode(GraphicsMode.Mode.NATIVE)
class ShotsTest {
    /** 배경 일(주기 작업·지오펜스 등)을 끄고, 서버 주소·토큰·처음 설정 완료를 넣어 둔 앱 */
    class ShotApp : MyHomeApp() {
        override fun onCreate() {
            val server = System.getenv("SHOTS_SERVER")
            val token = System.getenv("SHOTS_TOKEN")
            if (server != null && token != null) {
                // TokenStore가 읽기 전에 "토큰 있음"으로 표시(값은 아래에서 바로 넣는다: JVM에는 Android Keystore가 없다)
                getSharedPreferences("secure_token", Context.MODE_PRIVATE).edit().putString("token_ct", "x").putString("token_iv", "x").commit()
                getSharedPreferences("settings", Context.MODE_PRIVATE).edit()
                    .putString("server_url", server).putBoolean("onboarded", true).putBoolean("notif_asked", true)
                    .putStringSet("selected_packages", setOf("test.kb", "test.toss")).commit()
            }
            super.onCreate()
            if (token != null) TokenStore::class.java.getDeclaredField("cached").apply { isAccessible = true }.set(tokenStore, token)
        }

        override fun startBackground() {
            WorkManagerTestInitHelper.initializeTestWorkManager(this)
            runCatching { com.yuntyu.myhome.focus.FocusTimer.init(this) }
        }
    }

    @get:Rule val rule = createEmptyComposeRule()
    private lateinit var scenario: ActivityScenario<MainActivity>
    private val out by lazy { File(System.getenv("SHOTS_OUT")!!).apply { mkdirs() } }
    private val missing = mutableListOf<String>()

    private fun settle(rounds: Int = 8) {
        repeat(rounds) { Thread.sleep(200); rule.waitForIdle() }
    }

    private fun shot(name: String) {
        settle()
        scenario.onActivity { a ->
            val v = a.window.decorView
            val bmp = Bitmap.createBitmap(v.width, v.height, Bitmap.Config.ARGB_8888)
            v.draw(Canvas(bmp))
            File(out, "$name.png").outputStream().use { bmp.compress(Bitmap.CompressFormat.PNG, 100, it) }
        }
    }

    private fun find(label: String, substring: Boolean): SemanticsNodeInteraction? {
        for (m in listOf(hasClickAction() and hasText(label, substring = substring), hasClickAction() and hasContentDescription(label, substring = substring))) {
            val nodes = rule.onAllNodes(m, useUnmergedTree = false).fetchSemanticsNodes()
            if (nodes.isNotEmpty()) return rule.onAllNodes(m)[nodes.lastIndex]
        }
        return null
    }

    /** 글자(또는 설명)가 맞는 누를 수 있는 칸을 누른다. 판마다 이름이 달라서 여러 이름을 받는다. 없으면 적어 두고 넘어간다 */
    private fun tap(vararg labels: String, substring: Boolean = false): Boolean {
        for (l in labels) {
            val n = find(l, substring) ?: continue
            n.performClick()
            settle()
            return true
        }
        missing += labels.joinToString("/")
        return false
    }

    /** 꾹 눌러 rows 줄만큼 끄는 중을 찍는다. 떼지 않고 취소해서 실제로는 옮기지 않는다 */
    private fun dragShot(label: String, rows: Float, name: String) {
        val n = find(label, false) ?: run { missing += "끌기:$label"; return }
        val h = n.fetchSemanticsNode().boundsInRoot.height
        n.performTouchInput { down(center) }
        rule.mainClock.advanceTimeBy(1_000)
        n.performTouchInput { repeat(10) { moveBy(androidx.compose.ui.geometry.Offset(0f, rows * h / 10)) } }
        shot(name)
        n.performTouchInput { cancel() }
        settle()
    }

    private fun back() {
        scenario.onActivity { it.onBackPressedDispatcher.onBackPressed() }
        settle()
    }

    @Test fun everyScreen() {
        runScreens()
    }

    private fun runScreens() {
        assumeTrue("SHOTS_SERVER·SHOTS_TOKEN·SHOTS_OUT가 있을 때만", System.getenv("SHOTS_OUT") != null && System.getenv("SHOTS_SERVER") != null)
        val app = androidx.test.core.app.ApplicationProvider.getApplicationContext<MyHomeApp>()
        shadowOf(app).grantPermissions(Manifest.permission.POST_NOTIFICATIONS, Manifest.permission.CAMERA)
        shadowOf(app.getSystemService(PowerManager::class.java)).setIgnoringBatteryOptimizations(app.packageName, true)
        android.provider.Settings.Secure.putString(app.contentResolver, "enabled_notification_listeners", "${app.packageName}/com.yuntyu.myhome.capture.CaptureService")
        seedFocus(app)
        seedBodyPhotos(app)
        scenario = ActivityScenario.launch(MainActivity::class.java)
        settle(15)

        shot("01-홈")
        // 분류 박스 펼치기(안 분류 + 최근 결제 3개). 한 달 예산 창은 따로 뜨는 창이라 여기서는 안 찍힌다(CategoryMoveTest가 본다)
        if (tap("생활")) { shot("01b-홈-분류-펼침"); tap("생활") }
        tap("거래"); shot("02-거래")
        tap("미분류", "분류 고를 것", substring = true); shot("03-거래-미분류")
        tap("수입", "들어온 돈"); shot("04-거래-수입")

        tap("건강"); shot("05-건강")
        if (tap("예정", substring = true) || tap("사진 찍어서", substring = true)) { shot("06-끼니-촬영"); back() }
        if (tap("지난 기록")) { shot("07-건강-지난기록"); back() }
        if (tap("건강 설정")) { shot("08-건강-설정"); back() }
        if (tap("헬스")) { shot("09-헬스"); back() }
        if (tap("몸무게")) { shot("09b-몸-기록"); back() }

        tap("집중"); shot("10-집중")

        tap("설정"); shot("11-설정")
        if (tap("분류·폴더·예산", "분류와 예산")) {
            shot("12-분류·예산")
            dragShot("쇼핑", 1f, "12b-분류-끌기-폴더-안으로")
            dragShot("쇼핑", 2.6f, "12c-분류-끌기-자리")
            back()
        }
        if (tap("알림 읽을 앱")) { shot("13-알림-읽을-앱"); back() }
        if (tap("문제가 생겼을 때")) {
            shot("14-문제-해결")
            if (tap("보낸 기록")) { shot("15-보낸-기록"); back() }
            if (tap("삼성 폰 절전 설정 안내")) { shot("16-절전-안내"); back() }
            back()
        }
        if (tap("처음 설정 다시 보기")) { shot("17-처음-설정"); back() }

        tap("홈")
        if (tap("투자 열기", "투자")) {
            shot("18-투자")
            if (tap("백테스트 결과", substring = true)) { shot("19-백테스트"); back() }
        }
        File(out, "missing.txt").writeText(missing.joinToString("\n"))
        scenario.close()
    }

    companion object {
        /** 몸 사진: 합성 그림 석 장(폰에만 두는 사진이라 앱 폴더에 바로 넣는다) */
        fun seedBodyPhotos(app: MyHomeApp) {
            val dir = File(app.filesDir, "body_photos").apply { mkdirs() }
            val today = java.time.LocalDate.now()
            listOf(0L to 0xFF8E9AAF.toInt(), 7L to 0xFFB8A1C9.toInt(), 14L to 0xFF9FC2B4.toInt()).forEach { (ago, color) ->
                val bmp = Bitmap.createBitmap(300, 400, Bitmap.Config.ARGB_8888)
                val c = Canvas(bmp)
                c.drawColor(color)
                val p = android.graphics.Paint().apply { this.color = 0x55FFFFFF; isAntiAlias = true }
                c.drawCircle(150f, 110f, 55f, p); c.drawRoundRect(85f, 175f, 215f, 380f, 50f, 50f, p)
                File(dir, "${today.minusDays(ago)}_${1_000 + ago}.jpg").outputStream().use { bmp.compress(Bitmap.CompressFormat.JPEG, 85, it) }
            }
        }

        /** 집중: 합성 과목 셋과 오늘 기록(앱 DB에 바로 넣는다) */
        fun seedFocus(app: MyHomeApp) = kotlinx.coroutines.runBlocking {
            val dao = app.db.focus()
            val now = System.currentTimeMillis()
            val day = com.yuntyu.myhome.core.FocusDay.startOf(com.yuntyu.myhome.core.FocusDay.dayOf(now))
            val subs = listOf("알고리즘" to "#7C83FD", "영어" to "#F4A261", "운영체제" to "#2A9D8F")
            subs.forEachIndexed { i, (n, c) -> dao.upsertSubject(com.yuntyu.myhome.data.FocusSubject("s$i", n, c, i * 10, updatedAt = now, synced = true)) }
            fun at(h: Int, m: Int) = day + ((h - com.yuntyu.myhome.core.FocusDay.DAY_START_HOUR) * 60 + m) * 60_000L // 하루는 새벽 5시에 시작
            listOf(Triple("s0", at(9, 0), at(10, 40)), Triple("s1", at(11, 0), at(11, 45)), Triple("s0", at(13, 30), at(14, 20)), Triple("s2", at(15, 0), at(15, 50)))
                .filter { it.third < now }
                .forEachIndexed { i, (sid, a, b) -> dao.upsertSession(com.yuntyu.myhome.data.FocusSession("x$i", sid, a, b, updatedAt = now, synced = true)) }
        }
    }
}
