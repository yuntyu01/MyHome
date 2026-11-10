package com.yuntyu.myhome.ui

import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.runtime.Composable
import androidx.compose.runtime.key
import androidx.compose.runtime.remember
import androidx.compose.ui.Modifier
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.compose.ui.test.performTouchInput
import androidx.compose.ui.unit.dp
import com.yuntyu.myhome.core.CategoryTree
import com.yuntyu.myhome.core.DropPlace
import com.yuntyu.myhome.core.api.Category
import com.yuntyu.myhome.core.api.CategoryBudget
import com.yuntyu.myhome.core.api.PeriodInfo
import com.yuntyu.myhome.core.api.Summary
import com.yuntyu.myhome.core.api.Totals
import com.yuntyu.myhome.data.SummaryRepository
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode

/** 분류 꾹 눌러 끌기(분류·폴더·예산 화면)와 홈 폴더 격자 (합성 분류) */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [27], application = android.app.Application::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
class CategoryMoveTest {
    @get:Rule val rule = createComposeRule()

    // 생활비(1) › [식비(2) › 배달(3), 교통(4)], 고정비(5), 쇼핑(6). 줄 높이는 FlatRow 최소 56dp
    private val cats = listOf(
        Category(1, "생활비", "expense", sort = 10),
        Category(2, "식비", "expense", sort = 10, parentId = 1),
        Category(3, "배달", "expense", sort = 10, parentId = 2),
        Category(4, "교통", "expense", sort = 20, parentId = 1),
        Category(5, "고정비", "expense", sort = 20),
        Category(6, "쇼핑", "expense", sort = 30),
    )
    private val taps = mutableListOf<Long>()
    private val drops = mutableListOf<Pair<Long, DropPlace>>()
    private var rowPx = 0f

    @Composable
    private fun Harness() {
        val tree = remember { CategoryTree(cats) }
        val scroll = rememberScrollState()
        val d = rememberCategoryDragger(scroll)
        rowPx = with(LocalDensity.current) { 56.dp.toPx() }
        Column(Modifier.fillMaxSize().dragViewport(d).verticalScroll(scroll).dropMarks(d, d.target(tree), Color.Red)) {
            for ((c, depth) in tree.preorder()) key(c.id) {
                DragRow(d, tree, c, onTap = { taps += c.id }, onDrop = { id, p -> drops += id to p }) {
                    FlatRow(c.name, leading = indentLead(depth))
                }
            }
        }
    }

    /** name 줄 가운데를 꾹 누르고 dy(줄 높이 배수)만큼 끌어서 놓는다 */
    private fun longDrag(name: String, rows: Float) {
        rule.onNodeWithText(name).performTouchInput { down(center) }
        rule.mainClock.advanceTimeBy(1_000)
        rule.onNodeWithText(name).performTouchInput {
            repeat(8) { moveBy(Offset(0f, rows * rowPx / 8)) }
            up()
        }
        rule.waitForIdle()
    }

    @Test fun tapOpensLongPressDragMoves() {
        rule.setContent { MyHomeTheme { Harness() } }
        rule.onNodeWithText("교통").performClick()
        rule.waitForIdle()
        assertEquals(listOf(4L), taps)

        // 쇼핑을 한 줄 위(고정비 가운데)에 놓으면 고정비 안으로
        longDrag("쇼핑", -1f)
        assertEquals(6L to DropPlace(5), drops.last())
        // 쇼핑을 교통 위쪽(2.3줄 위)에 놓으면 생활비 안, 교통 앞
        longDrag("쇼핑", -2.3f)
        assertEquals(6L to DropPlace(1, before = 4), drops.last())
        // 생활비 폴더를 맨 아래로 끌면 안까지 통째로 맨 위 단계 맨 끝
        longDrag("생활비", 6f)
        assertEquals(1L to DropPlace(null), drops.last())
        assertEquals("꾹 눌러 끌면 누르기는 안 된다", listOf(4L), taps)
    }

    @Test fun longPressWithoutMovingDoesNothing() {
        rule.setContent { MyHomeTheme { Harness() } }
        rule.onNodeWithText("고정비").performTouchInput { down(center) }
        rule.mainClock.advanceTimeBy(1_000)
        rule.onNodeWithText("고정비").performTouchInput { up() }
        rule.waitForIdle()
        assertTrue("taps=$taps drops=$drops", taps.isEmpty() && drops.isEmpty())
    }

    // 홈: 처음 보이는 폴더는 펼쳐서 안을 두 칸으로, "생활 거래 모두 보기", 접어 둔 분류는 "분류 N개 더 보기"
    @Test fun homeFolderShowsKidsInGridAndCollapses() {
        fun cb(id: Long, name: String, parent: Long? = null, budget: Long, spent: Long) =
            CategoryBudget(id, name, parentId = parent, depth = if (parent == null) 0 else 1, budget = budget, spent = spent, remaining = budget - spent, ratio = if (budget > 0) spent.toDouble() / budget else 0.0)
        val s = Summary(
            period = PeriodInfo("2026-11", "2026-11-01", "2026-12-01"),
            totals = Totals(budget = 2_100_000, spent = 11_900, remaining = 2_088_100),
            categories = listOf(
                cb(1, "생활", budget = 600_000, spent = 11_900),
                cb(2, "식비", 1, budget = 300_000, spent = 3_600),
                cb(3, "카페·간식", 1, budget = 50_000, spent = 4_800),
                cb(4, "교통", 1, budget = 50_000, spent = 0),
                cb(5, "구독", 1, budget = 15_000, spent = 0),
                cb(6, "쇼핑", budget = 100_000, spent = 0),
            ),
        )
        var opened = -1L
        rule.setContent {
            MyHomeTheme {
                InUseHome(
                    Modifier, SummaryRepository.State(s, 0, null), emptyList(),
                    onNavigate = {}, onTransactions = {}, onCategory = { opened = it }, onOpenTx = {}, serverUrl = null,
                )
            }
        }
        rule.onNodeWithText("588,100원").assertExists()
        rule.onNodeWithText("296,400원").assertExists() // 식비 칸
        rule.onNodeWithText("분류 1개 더 보기").assertExists()
        rule.onNodeWithText("예산 변경").assertExists()
        rule.onNodeWithText("카페·간식").performClick()
        rule.waitForIdle()
        assertEquals(3L, opened)
        rule.onNodeWithText("생활").performClick() // 접기
        rule.waitForIdle()
        rule.onNodeWithText("296,400원").assertDoesNotExist()
        rule.onNodeWithText("분류 1개 더 보기").performClick()
        rule.waitForIdle()
        rule.onNodeWithText("쇼핑").assertExists()
        rule.onNodeWithText("접기").assertExists()
    }
}
