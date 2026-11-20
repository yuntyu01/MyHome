package com.yuntyu.myhome.ui

import android.graphics.Bitmap
import android.graphics.Canvas
import androidx.compose.ui.test.hasClickAction
import androidx.compose.ui.test.hasContentDescription
import androidx.compose.ui.test.hasText
import androidx.compose.ui.test.junit4.createEmptyComposeRule
import androidx.compose.ui.test.performClick
import androidx.compose.ui.test.performScrollTo
import androidx.test.core.app.ActivityScenario
import org.junit.Assume.assumeTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode
import java.io.File

/**
 * 투자 홈(요약 + 이번 달 매수 단계) 찍기(사용자 요청, 2026-11-20 "단계별로 하면 화면 사진도 보내줘"). 평소 테스트에서는 건너뛴다.
 * ShotsTest와 같은 앱 설정으로 로컬 서버(합성 데이터)에 붙는다:
 *   SHOTS_OUT=… SHOTS_SERVER=http://127.0.0.1:8081 SHOTS_TOKEN=<기기 토큰> ./gradlew :app:testDebugUnitTest --tests '*InvestFlowShotTest*'
 */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34], application = ShotsTest.ShotApp::class, qualifiers = "w411dp-h891dp-night-xxhdpi")
@GraphicsMode(GraphicsMode.Mode.NATIVE)
class InvestFlowShotTest {
    @get:Rule val rule = createEmptyComposeRule()
    private lateinit var scenario: ActivityScenario<MainActivity>
    private val out by lazy { File(System.getenv("SHOTS_OUT")!!).apply { mkdirs() } }

    private fun settle(rounds: Int = 8) = repeat(rounds) { Thread.sleep(200); rule.waitForIdle() }

    private fun shot(name: String) {
        settle()
        scenario.onActivity { a ->
            val v = a.window.decorView
            val bmp = Bitmap.createBitmap(v.width, v.height, Bitmap.Config.ARGB_8888)
            v.draw(Canvas(bmp))
            File(out, "$name.png").outputStream().use { bmp.compress(Bitmap.CompressFormat.PNG, 100, it) }
        }
    }

    private fun tap(label: String) {
        for (m in listOf(hasClickAction() and hasText(label), hasClickAction() and hasContentDescription(label))) {
            val nodes = rule.onAllNodes(m).fetchSemanticsNodes()
            if (nodes.isNotEmpty()) {
                // 화면 밖이면 눌리지 않는다(펼친 줄이 아래 칸을 밀어낸다). 먼저 화면 안으로 내린다
                val node = rule.onAllNodes(m)[nodes.lastIndex]
                runCatching { node.performScrollTo() }
                settle(2)
                node.performClick()
                settle()
                return
            }
        }
        error("누를 칸 없음: $label")
    }

    @Test fun investHome() {
        assumeTrue(System.getenv("SHOTS_OUT") != null && System.getenv("SHOTS_SERVER") != null)
        // 매매 실패 알림을 누른 것처럼 투자 화면으로 바로 연다
        val app = androidx.test.core.app.ApplicationProvider.getApplicationContext<android.content.Context>()
        scenario = ActivityScenario.launch(
            android.content.Intent(app, MainActivity::class.java).putExtra(MainActivity.EXTRA_SCREEN, "invest"),
        )
        settle(25)
        shot("invest-1-home")
        // 판정 검산과 환전을 펼쳐 근거를 본다
        tap("판정 검산")
        tap("돈 입금")
        shot("invest-2-judge-open")
        // 접고 환전·구매 성공을 펼친다(환전 시각·기준율 차이, 체결)
        tap("판정 검산")
        tap("돈 입금")
        tap("환전")
        tap("구매 성공")
        shot("invest-3-exchange-fill")
        // 매일 기록(사용자 요청, 2026-11-20): 표로 내려 마지막 날을 펼치고, 더 보기로 밀린 날까지
        tap("환전")
        tap("구매 성공")
        // 표 끝(더 보기)까지 내려야 줄이 화면 안에 있어 눌린다. 내린 뒤 한 번 더 기다려야 그림에 반영된다
        rule.onAllNodes(hasText("더 보기 (14일)"))[0].performScrollTo()
        settle()
        tap("11/19")
        shot("invest-4-daily")
        tap("더 보기 (14일)")
        rule.onAllNodes(hasText("세전 · 금액은 주식 + 현금, 수익률은 아직 안 바꾼 원화를 빼고 계산"))[0].performScrollTo()
        settle()
        shot("invest-5-daily-more")
        scenario.close()
    }
}
