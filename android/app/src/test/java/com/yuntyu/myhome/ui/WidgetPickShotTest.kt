package com.yuntyu.myhome.ui

import android.graphics.Bitmap
import android.graphics.Canvas
import android.view.View
import android.widget.FrameLayout
import androidx.glance.appwidget.ExperimentalGlanceRemoteViewsApi
import androidx.glance.appwidget.GlanceRemoteViews
import com.yuntyu.myhome.MyHomeApp
import com.yuntyu.myhome.core.api.CategoryBudget
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

/** 위젯 고르기 칸 찍기(합성 요약, 서버 없이). SHOTS_OUT이 있을 때만 돈다. 중간·크게 × 밝게·어둡게, 누르기 전·누른 뒤 */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34], application = ShotsTest.ShotApp::class, qualifiers = "w411dp-h891dp-xxhdpi")
@GraphicsMode(GraphicsMode.Mode.NATIVE)
class WidgetPickShotTest {
    @Test fun light() = shots("밝게")

    @Config(qualifiers = "w411dp-h891dp-night-xxhdpi")
    @Test fun dark() = shots("어둡게")

    private val summary = Summary(
        period = PeriodInfo("2026-11", "2026-11-01", "2026-12-01"),
        totals = Totals(budget = 600000, spent = 214300, remaining = 385700),
        categories = listOf(
            CategoryBudget(id = 1, name = "식비", budget = 300000, spent = 182000, remaining = 118000, ratio = 0.61),
            CategoryBudget(id = 2, name = "카페·간식", budget = 60000, spent = 32300, remaining = 27700, ratio = 0.54),
            CategoryBudget(id = 4, name = "쇼핑", budget = 100000, spent = 0, remaining = 100000, ratio = 0.0),
        ),
        pick = Pick(
            id = 9, merchant = "GS25 합성점", amount = 3300, reason = "ambiguous", left = 3,
            choices = listOf(PickChoice(1, "식비"), PickChoice(2, "카페·간식"), PickChoice(5, "생활"), PickChoice(4, "쇼핑")),
        ),
    )

    /** 고를 결제가 없는 평소 위젯(작게·중간·크게): 폴더 안 분류와 최근 결제가 있는 합성 요약 */
    private val plain = Summary(
        period = PeriodInfo("2026-11", "2026-11-01", "2026-12-01"),
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

    @OptIn(ExperimentalGlanceRemoteViewsApi::class)
    private fun shots(theme: String) {
        assumeTrue(System.getenv("SHOTS_OUT") != null)
        val app = androidx.test.core.app.ApplicationProvider.getApplicationContext<MyHomeApp>()
        val out = File(System.getenv("SHOTS_OUT")!!, "widgets").apply { mkdirs() }
        for ((sizeName, size) in listOf("작게" to SummaryWidget.SMALL, "중간" to SummaryWidget.MEDIUM, "크게" to SummaryWidget.LARGE)) {
            val rv = runBlocking { GlanceRemoteViews().compose(app, size) { SummaryWidgetContent(app, SummaryWidget.PAGE_BUDGET, plain, null, emptyList(), null, null) } }.remoteViews
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
            File(out, "위젯-평소-$theme-$sizeName.png").outputStream().use { bmp.compress(Bitmap.CompressFormat.PNG, 100, it) }
        }
        for ((sizeName, size) in listOf("중간" to SummaryWidget.MEDIUM, "크게" to SummaryWidget.LARGE)) {
            for ((stateName, picking) in listOf("고르기" to null, "누름" to (9L to 2L))) {
                val rv = runBlocking { GlanceRemoteViews().compose(app, size) { SummaryWidgetContent(app, SummaryWidget.PAGE_BUDGET, summary, null, emptyList(), null, picking) } }.remoteViews
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
                File(out, "위젯-고르기-$theme-$sizeName-$stateName.png").outputStream().use { bmp.compress(Bitmap.CompressFormat.PNG, 100, it) }
            }
        }
    }
}
