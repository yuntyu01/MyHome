package com.yuntyu.myhome.ui

import android.graphics.Bitmap
import android.graphics.Canvas
import android.view.View
import android.widget.FrameLayout
import androidx.glance.appwidget.ExperimentalGlanceRemoteViewsApi
import androidx.glance.appwidget.GlanceRemoteViews
import com.yuntyu.myhome.MyHomeApp
import com.yuntyu.myhome.core.FocusDay
import com.yuntyu.myhome.data.FocusSession
import com.yuntyu.myhome.data.FocusSubject
import androidx.compose.ui.unit.DpSize
import androidx.compose.ui.unit.dp
import com.yuntyu.myhome.core.api.CategoryBudget
import com.yuntyu.myhome.core.HealthWidgetData
import com.yuntyu.myhome.core.WidgetBody
import com.yuntyu.myhome.core.WidgetMeal
import com.yuntyu.myhome.core.api.HealthToday
import com.yuntyu.myhome.core.api.Nutrition
import com.yuntyu.myhome.core.api.PeriodInfo
import com.yuntyu.myhome.core.api.Pick
import com.yuntyu.myhome.core.api.PickChoice
import com.yuntyu.myhome.core.api.Summary
import com.yuntyu.myhome.core.api.Totals
import com.yuntyu.myhome.widget.SummaryWidget
import com.yuntyu.myhome.widget.SummaryWidgetContent
import kotlinx.coroutines.runBlocking
import org.junit.Assume.assumeTrue
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode
import java.io.File
import java.time.LocalDate

/** 위젯 찍기(합성 요약, 서버 없이). SHOTS_OUT이 있을 때만 돈다. 평소·쓴 돈·건강·집중(작게·중간·크게), 고르기(중간·크게, 누르기 전·누른 뒤) × 밝게·어둡게 */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34], application = ShotsTest.ShotApp::class, qualifiers = "w411dp-h891dp-xxhdpi")
@GraphicsMode(GraphicsMode.Mode.NATIVE)
class WidgetPickShotTest {
    @Test fun light() = shots("밝게")

    @Config(qualifiers = "w411dp-h891dp-night-xxhdpi")
    @Test fun dark() = shots("어둡게")

    private val summary = Summary(
        period = PeriodInfo("2026-11", "2026-11-05", "2026-12-04"),
        totals = Totals(budget = 600000, spent = 214300, remaining = 385700),
        categories = listOf(
            CategoryBudget(id = 1, name = "식비", budget = 300000, spent = 182000, remaining = 118000, ratio = 0.61),
            CategoryBudget(id = 2, name = "카페·간식", budget = 60000, spent = 32300, remaining = 27700, ratio = 0.54),
            CategoryBudget(id = 4, name = "쇼핑", budget = 100000, spent = 0, remaining = 100000, ratio = 0.0),
        ),
        pick = Pick(
            id = 9, merchant = "GS25 합성점", amount = 3300, occurredAt = "2026-11-17T08:42:00+09:00", reason = "ambiguous", left = 3,
            choices = listOf(PickChoice(1, "식비"), PickChoice(2, "카페·간식"), PickChoice(5, "생활"), PickChoice(4, "쇼핑"), PickChoice(3, "교통"), PickChoice(7, "구독")),
        ),
    )

    /** 고를 결제가 없는 평소 위젯(작게·중간·크게): 폴더 안 분류와 최근 결제가 있는 합성 요약 */
    private val plain = Summary(
        period = PeriodInfo("2026-11", "2026-11-05", "2026-12-04"),
        totals = Totals(budget = 1_200_000, spent = 486_300, remaining = 713_700),
        categories = listOf(
            CategoryBudget(id = 5, name = "생활", budget = 700_000, spent = 301_400, remaining = 398_600, ratio = 0.43),
            CategoryBudget(id = 1, name = "식비", parentId = 5, depth = 1, budget = 400_000, spent = 248_000, remaining = 152_000, ratio = 0.62),
            CategoryBudget(id = 2, name = "카페·간식", parentId = 5, depth = 1, budget = 80_000, spent = 71_500, remaining = 8_500, ratio = 0.89),
            CategoryBudget(id = 3, name = "교통", parentId = 5, depth = 1, budget = 70_000, spent = 81_900, remaining = -11_900, ratio = 1.17),
            CategoryBudget(id = 4, name = "쇼핑", budget = 150_000, spent = 43_000, remaining = 107_000, ratio = 0.29),
            CategoryBudget(id = 6, name = "고정비", budget = 350_000, spent = 141_900, remaining = 208_100, ratio = 0.41),
        ),
        recent = listOf(
            com.yuntyu.myhome.core.api.TransactionDto(id = 21, occurredAt = "2026-11-16T19:56:00+09:00", amount = 2_500, flow = "out", merchant = "합성커피 역삼점", direction = "expense", categoryId = 2, category = "카페·간식", status = "confirmed"),
            com.yuntyu.myhome.core.api.TransactionDto(id = 20, occurredAt = "2026-11-16T19:49:00+09:00", amount = 141_900, flow = "out", merchant = "합성헬스 역삼점", direction = "expense", categoryId = 6, category = "고정비", status = "confirmed"),
            com.yuntyu.myhome.core.api.TransactionDto(id = 19, occurredAt = "2026-11-16T19:05:00+09:00", amount = 2_350, flow = "out", merchant = "지하철 2건", direction = "expense", categoryId = 3, category = "교통", status = "confirmed"),
            com.yuntyu.myhome.core.api.TransactionDto(id = 18, occurredAt = "2026-11-15T18:12:00+09:00", amount = 8_800, flow = "out", merchant = "합성도넛", direction = "expense", categoryId = 2, category = "카페·간식", status = "confirmed"),
        ),
    )

    /** 건강 칸 합성 값: 3끼 중 2끼, 칼로리·탄단지 먹은 양 / 목표, 끼니 표(합이 위 합계), 최근 몸무게 */
    private val health = HealthWidgetData(
        today = HealthToday(
            date = "2026-11-17", loggedCount = 2, mealCount = 3,
            total = Nutrition(kcal = 1_240, carbs = 152, protein = 88, fat = 31),
            targets = Nutrition(kcal = 2_100, carbs = 250, protein = 140, fat = 60),
        ),
        // 시안처럼 3끼 중 2끼(셋째는 "기록하기"), 두 끼 합이 위 합계
        meals = listOf(
            WidgetMeal(1, "08:10", Nutrition(480, 55, 28, 12)),
            WidgetMeal(2, "12:40", Nutrition(760, 97, 60, 19)),
        ),
        body = WidgetBody("2026-11-17", 72.4, -0.3),
    )
    private val today = LocalDate.of(2026, 11, 17)

    /** 집중 칸 합성 값: 과목 셋과 오늘 공부한 시간(오늘 시작 기준으로 둔다) */
    private val subjects = listOf(
        FocusSubject("s1", "수학", "#7B8FD1", 0, updatedAt = 0),
        FocusSubject("s2", "영어", "#D19A7B", 1, updatedAt = 0),
        FocusSubject("s3", "코딩", "#7BC4A0", 2, updatedAt = 0),
    )
    /** 수학을 25분 전에 시작해서 도는 중 */
    private fun runningMath() = com.yuntyu.myhome.focus.FocusTimer.Running("s1", System.currentTimeMillis() - 25 * 60_000L)

    private fun sessions(): List<FocusSession> {
        val start = FocusDay.startOf(FocusDay.dayOf(System.currentTimeMillis()))
        val h = 3_600_000L
        return listOf(
            FocusSession("a", "s1", start + h, start + h + 95 * 60_000L, updatedAt = 0),
            FocusSession("b", "s2", start + 3 * h, start + 3 * h + 40 * 60_000L, updatedAt = 0),
        )
    }

    @OptIn(ExperimentalGlanceRemoteViewsApi::class)
    private fun shot(out: File, theme: String, name: String, size: DpSize, content: @androidx.compose.runtime.Composable () -> Unit) {
        val app = androidx.test.core.app.ApplicationProvider.getApplicationContext<MyHomeApp>()
        val rv = runBlocking { GlanceRemoteViews().compose(app, size) { content() } }.remoteViews
        val v: View = rv.apply(app, FrameLayout(app))
        val d = app.resources.displayMetrics.density
        val w = (size.width.value * d).toInt(); val h = (size.height.value * d).toInt()
        v.measure(View.MeasureSpec.makeMeasureSpec(w, View.MeasureSpec.EXACTLY), View.MeasureSpec.makeMeasureSpec(h, View.MeasureSpec.EXACTLY))
        v.layout(0, 0, w, h)
        val pad = (16 * d).toInt()
        val bmp = Bitmap.createBitmap(w + pad * 2, h + pad * 2, Bitmap.Config.ARGB_8888)
        val c = Canvas(bmp)
        c.drawColor(if (theme == "어둡게") 0xFF101012.toInt() else 0xFFD9DCE3.toInt())
        c.translate(pad.toFloat(), pad.toFloat())
        v.draw(c)
        File(out, "위젯-$name-$theme.png").outputStream().use { bmp.compress(Bitmap.CompressFormat.PNG, 100, it) }
    }

    /** 폰 홈 화면에 놓인 크기로 찍기(바탕 없이, 모서리는 합성할 때 둥글게): 가로 꽉(4칸)×2줄, ×3줄, 2칸×2줄 */
    @OptIn(ExperimentalGlanceRemoteViewsApi::class)
    private fun phoneShot(out: File, theme: String, name: String, size: DpSize, content: @androidx.compose.runtime.Composable () -> Unit) {
        val app = androidx.test.core.app.ApplicationProvider.getApplicationContext<MyHomeApp>()
        val rv = runBlocking { GlanceRemoteViews().compose(app, size) { content() } }.remoteViews
        val v: View = rv.apply(app, FrameLayout(app))
        val d = app.resources.displayMetrics.density
        val w = (size.width.value * d).toInt(); val h = (size.height.value * d).toInt()
        v.measure(View.MeasureSpec.makeMeasureSpec(w, View.MeasureSpec.EXACTLY), View.MeasureSpec.makeMeasureSpec(h, View.MeasureSpec.EXACTLY))
        v.layout(0, 0, w, h)
        val bmp = Bitmap.createBitmap(w, h, Bitmap.Config.ARGB_8888)
        v.draw(Canvas(bmp))
        File(out, "폰-$name-$theme.png").outputStream().use { bmp.compress(Bitmap.CompressFormat.PNG, 100, it) }
    }

    private fun phoneShots(theme: String) {
        val app = androidx.test.core.app.ApplicationProvider.getApplicationContext<MyHomeApp>()
        val out = File(System.getenv("SHOTS_OUT")!!, "phone").apply { mkdirs() }
        val wide2 = DpSize(380.dp, 186.dp)
        val wide3 = DpSize(380.dp, 290.dp)
        val square = DpSize(184.dp, 186.dp)
        phoneShot(out, theme, "가계부-4x2", wide2) { SummaryWidgetContent(app, SummaryWidget.PAGE_BUDGET, plain, null, emptyList(), null, null, today = today) }
        phoneShot(out, theme, "가계부-4x3", wide3) { SummaryWidgetContent(app, SummaryWidget.PAGE_BUDGET, plain, null, emptyList(), null, null, today = today) }
        phoneShot(out, theme, "쓴돈-4x2", wide2) { SummaryWidgetContent(app, SummaryWidget.PAGE_BUDGET, plain, null, emptyList(), null, null, head = SummaryWidget.HEAD_USED, today = today) }
        phoneShot(out, theme, "고르기-4x2", wide2) { SummaryWidgetContent(app, SummaryWidget.PAGE_BUDGET, summary, null, emptyList(), null, null, today = today) }
        phoneShot(out, theme, "고르기-4x3", wide3) { SummaryWidgetContent(app, SummaryWidget.PAGE_BUDGET, summary, null, emptyList(), null, 9L to 2L, today = today) }
        phoneShot(out, theme, "건강-4x2", wide2) { SummaryWidgetContent(app, SummaryWidget.PAGE_HEALTH, plain, null, emptyList(), null, null, health = health, today = today) }
        phoneShot(out, theme, "집중-4x2", wide2) { SummaryWidgetContent(app, SummaryWidget.PAGE_FOCUS, plain, subjects, sessions(), null, null, today = today) }
        // 시안처럼 4칸×3줄: 집중(수학 도는 중), 건강(끼니 표), 고르기
        phoneShot(out, theme, "집중-4x3", wide3) { SummaryWidgetContent(app, SummaryWidget.PAGE_FOCUS, plain, subjects, sessions(), runningMath(), null, today = today) }
        phoneShot(out, theme, "건강-4x3", wide3) { SummaryWidgetContent(app, SummaryWidget.PAGE_HEALTH, plain, null, emptyList(), null, null, health = health, today = today) }
        phoneShot(out, theme, "가계부-2x2", square) { SummaryWidgetContent(app, SummaryWidget.PAGE_BUDGET, plain, null, emptyList(), null, null, today = today) }
        phoneShot(out, theme, "건강-2x2", square) { SummaryWidgetContent(app, SummaryWidget.PAGE_HEALTH, plain, null, emptyList(), null, null, health = health, today = today) }
    }

    private fun shots(theme: String) {
        assumeTrue(System.getenv("SHOTS_OUT") != null)
        // Robolectric은 위젯 목록(ListView 어댑터)을 그리지 못해서 같은 줄을 Column으로 그린다
        com.yuntyu.myhome.widget.widgetListsAsColumns = true
        val app = androidx.test.core.app.ApplicationProvider.getApplicationContext<MyHomeApp>()
        val out = File(System.getenv("SHOTS_OUT")!!, "widgets").apply { mkdirs() }
        phoneShots(theme)
        val sizes = listOf("작게" to SummaryWidget.SMALL, "중간" to SummaryWidget.MEDIUM, "크게" to SummaryWidget.LARGE)
        for ((sizeName, size) in sizes) {
            // 평소: 남은 날 · 하루 쓸 돈 / 남은 돈, 숫자를 누른 뒤: 쓴 돈 / 예산
            shot(out, theme, "평소-$sizeName", size) {
                SummaryWidgetContent(app, SummaryWidget.PAGE_BUDGET, plain, null, emptyList(), null, null, today = today)
            }
            shot(out, theme, "쓴돈-$sizeName", size) {
                SummaryWidgetContent(app, SummaryWidget.PAGE_BUDGET, plain, null, emptyList(), null, null, head = SummaryWidget.HEAD_USED, today = today)
            }
            shot(out, theme, "건강-$sizeName", size) {
                SummaryWidgetContent(app, SummaryWidget.PAGE_HEALTH, plain, null, emptyList(), null, null, health = health, today = today)
            }
            shot(out, theme, "집중-$sizeName", size) {
                SummaryWidgetContent(app, SummaryWidget.PAGE_FOCUS, plain, subjects, sessions(), null, null, today = today)
            }
            if (sizeName != "작게") shot(out, theme, "집중-도는중-$sizeName", size) {
                SummaryWidgetContent(app, SummaryWidget.PAGE_FOCUS, plain, subjects, sessions(), runningMath(), null, today = today)
            }
            // 건강 칸을 아직 안 받았을 때(앱 건강 칸을 한 번도 안 열었을 때)
            if (sizeName == "중간") shot(out, theme, "건강-처음-$sizeName", size) {
                SummaryWidgetContent(app, SummaryWidget.PAGE_HEALTH, plain, null, emptyList(), null, null, health = null, today = today)
            }
        }
        for ((sizeName, size) in sizes.drop(1)) {
            for ((stateName, picking) in listOf("고르기" to null, "누름" to (9L to 2L))) {
                shot(out, theme, "고르기-$sizeName-$stateName", size) {
                    SummaryWidgetContent(app, SummaryWidget.PAGE_BUDGET, summary, null, emptyList(), null, picking, today = today)
                }
            }
        }
    }
}
