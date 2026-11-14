package com.yuntyu.myhome.ui

import android.graphics.Bitmap
import android.graphics.Canvas
import androidx.activity.ComponentActivity
import androidx.compose.ui.Modifier
import androidx.compose.ui.test.junit4.createAndroidComposeRule
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import com.yuntyu.myhome.core.api.CategoryBudget
import com.yuntyu.myhome.core.api.PeriodInfo
import com.yuntyu.myhome.core.api.Summary
import com.yuntyu.myhome.core.api.Totals
import com.yuntyu.myhome.core.api.TransactionDto
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
 * 홈 "분류별 예산" 박스(트리) 찍기. 평소 테스트에서는 건너뛴다(합성 데이터).
 *   SHOTS_OUT=/어디/shots ./gradlew :app:testDebugUnitTest --tests '*HomeTreeShotTest*'
 * 폭 325dp·xxhdpi = 853px(사용자 시안 캡처와 같은 비율). 창을 길게 잡아 홈 전체를 그린 뒤 "분류별 예산"부터 잘라 저장한다.
 */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [27], application = android.app.Application::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
class HomeTreeShotTest {
    @get:Rule val rule = createAndroidComposeRule<ComponentActivity>()

    private fun cb(id: Long, name: String, parent: Long? = null, depth: Int = if (parent == null) 0 else 1, budget: Long, spent: Long) =
        CategoryBudget(id, name, parentId = parent, depth = depth, budget = budget, spent = spent, remaining = budget - spent, ratio = if (budget > 0) spent.toDouble() / budget else 0.0)

    private fun tx(id: Long, cat: Long, merchant: String, day: String, hhmm: String, amount: Long) =
        TransactionDto(id = id, occurredAt = "2026-11-${day}T$hhmm:00+09:00", amount = amount, flow = "out", merchant = merchant, direction = "expense", categoryId = cat, category = "합성", status = "confirmed")

    private fun summary(nested: Boolean) = Summary(
        period = PeriodInfo("2026-11", "2026-11-01", "2026-12-01"),
        totals = Totals(budget = 2_100_000, spent = 11_900, remaining = 2_088_100),
        categories = buildList {
            add(cb(1, "생활", budget = 600_000, spent = 11_900))
            add(cb(2, "식비", 1, budget = 300_000, spent = 3_600))
            add(cb(3, "카페·간식", 1, budget = 50_000, spent = 4_800))
            add(cb(4, "교통", 1, budget = 50_000, spent = 0))
            add(cb(5, "구독", 1, budget = 15_000, spent = 0))
            add(cb(6, "주식", budget = 1_500_000, spent = 0))
            if (nested) { // 식비 폴더 안에 분류 둘 → 한 단계 더 들여서
                add(cb(7, "배달", 2, depth = 2, budget = 100_000, spent = 0))
                add(cb(8, "편의점", 2, depth = 2, budget = 0, spent = 3_600))
            }
        },
        recent = listOf(
            tx(1, 3, "합성커피 역삼점", "10", "13:00", 1_800),
            tx(2, if (nested) 8 else 2, "합성편의점 역삼점", "10", "12:00", 3_600),
            tx(3, 3, "합성편의점 역삼점", "09", "18:00", 2_200),
        ),
    )

    private fun render(nested: Boolean) {
        rule.setContent {
            MyHomeTheme {
                // 앱(MainActivity)처럼 바탕과 글자색을 준다: Scaffold contentColor = onBackground
                ModoBackground {
                    androidx.compose.runtime.CompositionLocalProvider(
                        androidx.compose.material3.LocalContentColor provides androidx.compose.material3.MaterialTheme.colorScheme.onBackground,
                    ) {
                        InUseHome(
                            Modifier, SummaryRepository.State(summary(nested), 0, null), emptyList(),
                            onNavigate = {}, onTransactions = {}, onCategory = {}, onOpenTx = {}, serverUrl = null,
                        )
                    }
                }
            }
        }
        rule.onNodeWithText("생활").performClick()
        rule.waitForIdle()
    }

    /** "분류별 예산" 제목 조금 위부터 주식 박스 아래까지 잘라 PNG로 */
    private fun save(name: String) {
        val out = File(System.getenv("SHOTS_OUT")!!).apply { mkdirs() }
        val top = rule.onNodeWithText("분류별 예산").fetchSemanticsNode().boundsInWindow.top.toInt()
        val bottom = rule.onNodeWithText("주식").fetchSemanticsNode().boundsInWindow.bottom.toInt()
        // captureToImage는 Robolectric에서 다음 프레임을 기다리다 멈춘다 → 창을 직접 그린다(ShotsTest와 같은 방식)
        val v = rule.activity.window.decorView
        val full = Bitmap.createBitmap(v.width, v.height, Bitmap.Config.ARGB_8888)
        v.draw(Canvas(full))
        val y0 = (top - 90).coerceAtLeast(0)
        val y1 = (bottom + 150).coerceAtMost(full.height)
        val crop = Bitmap.createBitmap(full, 0, y0, full.width, y1 - y0)
        File(out, "$name.png").outputStream().use { crop.compress(Bitmap.CompressFormat.PNG, 100, it) }
    }

    @Test @Config(qualifiers = "w325dp-h2600dp-night-xxhdpi")
    fun darkLikeMockup() {
        assumeTrue(System.getenv("SHOTS_OUT") != null)
        render(nested = false)
        save("home-tree-dark")
    }

    @Test @Config(qualifiers = "w325dp-h2600dp-notnight-xxhdpi")
    fun lightLikeMockup() {
        assumeTrue(System.getenv("SHOTS_OUT") != null)
        render(nested = false)
        save("home-tree-light")
    }

    @Test @Config(qualifiers = "w325dp-h2600dp-night-xxhdpi")
    fun darkNestedFolderSelected() {
        assumeTrue(System.getenv("SHOTS_OUT") != null)
        render(nested = true)
        rule.onNodeWithText("편의점").performClick() // 고른 줄은 옅게 칠하고 최근 결제·"거래 모두 보기"가 그 분류로
        rule.waitForIdle()
        save("home-tree-dark-nested")
    }
}
