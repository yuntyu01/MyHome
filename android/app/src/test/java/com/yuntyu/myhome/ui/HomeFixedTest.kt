package com.yuntyu.myhome.ui

import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.test.assertIsSelected
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.compose.ui.test.performScrollTo
import com.yuntyu.myhome.core.api.CategoryBudget
import com.yuntyu.myhome.core.api.FixedItem
import com.yuntyu.myhome.core.api.PeriodInfo
import com.yuntyu.myhome.core.api.Summary
import com.yuntyu.myhome.core.api.Totals
import com.yuntyu.myhome.data.SummaryRepository
import org.junit.Assert.assertEquals
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode

/** 홈 고정비 박스(접기·펴기, 항목 고치기·추가)와 예산 시작일 고르기 (합성 값) */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [27], application = android.app.Application::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
class HomeFixedTest {
    @get:Rule val rule = createComposeRule()

    private val s = Summary(
        period = PeriodInfo("2026-11", "2026-11-05", "2026-12-04"),
        totals = Totals(budget = 1_000_000, spent = 100_000, remaining = 550_000, fixed = 350_000),
        categories = listOf(CategoryBudget(id = 1, name = "생활", budget = 600_000, spent = 100_000, remaining = 500_000, ratio = 0.17)),
    )
    private val items = listOf(FixedItem(1, "주식 적립", 300_000), FixedItem(2, "월세 이체", 50_000))

    @Test fun foldsAndEdits() {
        var open by mutableStateOf(true)
        val saved = mutableListOf<Triple<Long?, String, Long>>()
        rule.setContent {
            MyHomeTheme {
                InUseHome(
                    Modifier, SummaryRepository.State(s, 0, null), emptyList(),
                    onNavigate = {}, onTransactions = {}, onCategory = {}, onOpenTx = {}, serverUrl = null,
                    fixed = items, fixedOpen = open, onFixedOpen = { open = it },
                    saveFixed = { cur, name, amount -> saved += Triple(cur?.id, name, amount); null },
                )
            }
        }
        // 펼친 상태: 머리에 합계, 항목 줄과 추가 줄
        rule.onNodeWithText("-350,000원").performScrollTo().assertExists()
        rule.onNodeWithText("주식 적립").assertExists()
        rule.onNodeWithText("고정비 추가").assertExists()

        // 머리를 누르면 접혀서 합계만
        rule.onNodeWithText("고정비").performClick()
        rule.waitForIdle()
        assertEquals(false, open)
        rule.onNodeWithText("주식 적립").assertDoesNotExist()
        rule.onNodeWithText("-350,000원").assertExists()

        // 다시 펴서 항목을 누르면 고치기 창, 저장하면 그 항목으로
        rule.onNodeWithText("고정비").performClick()
        rule.waitForIdle()
        rule.onNodeWithText("월세 이체").performScrollTo().performClick()
        rule.waitForIdle()
        rule.onNodeWithText("저장").performClick()
        rule.waitForIdle()
        assertEquals(listOf(Triple<Long?, String, Long>(2L, "월세 이체", 50_000)), saved)
    }

    @Test fun startDayPicksFromGrid() {
        var saved: Int? = null
        rule.setContent { MyHomeTheme { StartDayDialog(1, onDismiss = {}) { saved = it } } }
        rule.onNodeWithText("5").performClick()
        rule.waitForIdle()
        rule.onNodeWithText("5").assertIsSelected()
        rule.onNodeWithText("매달 5일로").performClick()
        assertEquals(5, saved)
    }
}
