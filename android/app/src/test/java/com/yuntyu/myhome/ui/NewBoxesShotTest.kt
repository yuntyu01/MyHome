package com.yuntyu.myhome.ui

import android.graphics.Bitmap
import android.graphics.Canvas
import androidx.activity.ComponentActivity
import androidx.compose.foundation.layout.Column
import androidx.compose.material3.LocalContentColor
import androidx.compose.material3.MaterialTheme
import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.ui.Modifier
import androidx.compose.ui.test.junit4.createAndroidComposeRule
import com.yuntyu.myhome.core.NoticePolicy
import com.yuntyu.myhome.core.api.BalanceBrief
import com.yuntyu.myhome.core.api.BalanceGap
import com.yuntyu.myhome.core.api.PeriodInfo
import com.yuntyu.myhome.core.api.RawEventDto
import com.yuntyu.myhome.core.api.Summary
import com.yuntyu.myhome.core.api.Totals
import com.yuntyu.myhome.data.SummaryRepository
import org.junit.Assume.assumeTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode
import java.io.File

/**
 * 2026-11-15에 더한 칸 찍기(합성 데이터): 홈 계좌 잔액·빈 돈, 빈 돈 줄, 알림 지우기 묶음. 평소 테스트에서는 건너뛴다.
 *   SHOTS_OUT=/어디/shots ./gradlew :app:testDebugUnitTest --tests '*NewBoxesShotTest*'
 */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [27], application = android.app.Application::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
class NewBoxesShotTest {
    @get:Rule val rule = createAndroidComposeRule<ComponentActivity>()

    private fun render(content: @Composable () -> Unit) {
        rule.setContent {
            MyHomeTheme {
                ModoBackground {
                    CompositionLocalProvider(LocalContentColor provides MaterialTheme.colorScheme.onBackground) { content() }
                }
            }
        }
        rule.waitForIdle()
    }

    private fun save(name: String) {
        val out = File(System.getenv("SHOTS_OUT")!!).apply { mkdirs() }
        val v = rule.activity.window.decorView
        val full = Bitmap.createBitmap(v.width, v.height, Bitmap.Config.ARGB_8888)
        v.draw(Canvas(full))
        File(out, "$name.png").outputStream().use { full.compress(Bitmap.CompressFormat.PNG, 100, it) }
    }

    @Test @Config(qualifiers = "w360dp-h1400dp-night-xxhdpi")
    fun homeBalance() {
        assumeTrue(System.getenv("SHOTS_OUT") != null)
        val s = Summary(
            period = PeriodInfo("2026-11", "2026-11-01", "2026-12-01"),
            totals = Totals(budget = 1_000_000, spent = 120_000, remaining = 880_000),
            balances = listOf(BalanceBrief("toss", "토스뱅크", 1_015_000, "2026-11-15T12:03:00+09:00", 2)),
        )
        render { InUseHome(Modifier, SummaryRepository.State(s, 0, null), emptyList(), onNavigate = {}, onTransactions = {}, onCategory = {}, onOpenTx = {}, serverUrl = null) }
        save("new-home-balance")
    }

    @Test @Config(qualifiers = "w360dp-h1000dp-night-xxhdpi")
    fun gapsAndModes() {
        assumeTrue(System.getenv("SHOTS_OUT") != null)
        val apps = listOf(AppEntry("a.kb", "KB Pay", null), AppEntry("a.toss", "토스", null), AppEntry("a.cp", "쿠팡", null), AppEntry("a.kp", "카카오페이", null))
        val modes = mapOf("a.cp" to NoticePolicy.Mode.ALL, "a.kp" to NoticePolicy.Mode.NONE)
        render {
            Column {
                SectionLabel("토스뱅크")
                FlatRow("1,015,000원", sub = "11/15 12:03 알림 기준", bold = true, chevron = false)
                GapRow(BalanceGap(4, "2026-11-15T12:01:00+09:00", "2026-11-15T12:02:00+09:00", 1_070_000, 1_000_000, "다른상점", -50_000), false, {}, {})
                GapRow(BalanceGap(6, "2026-11-15T12:02:00+09:00", "2026-11-15T12:03:00+09:00", 1_000_000, 1_015_000, "김철수", 5_000), false, {}, {})
                UnreadRow(RawEventDto(3, "test.toss", "토스", "결제 내역을 확인해 주세요", "", "2026-11-15T03:03:00Z", "no amount"), "토스", false, {}, {})
                SectionLabel("결제 알림 지우기")
                ModeGroups(apps, { modes[it] ?: NoticePolicy.Mode.KNOWN }, ModeDrag()) { _, _ -> }
            }
        }
        save("new-gaps-modes")
    }
}
