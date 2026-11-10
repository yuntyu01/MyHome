package com.yuntyu.myhome.ui

import android.graphics.Bitmap
import android.graphics.Canvas
import android.view.View
import android.widget.FrameLayout
import androidx.compose.ui.unit.DpSize
import androidx.glance.appwidget.ExperimentalGlanceRemoteViewsApi
import androidx.glance.appwidget.GlanceRemoteViews
import com.yuntyu.myhome.MyHomeApp
import com.yuntyu.myhome.core.api.ApiResult
import com.yuntyu.myhome.core.FocusDay
import com.yuntyu.myhome.widget.SummaryWidget
import com.yuntyu.myhome.widget.SummaryWidgetContent
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.runBlocking
import org.junit.Assume.assumeTrue
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode
import java.io.File

/** 위젯 찍기(사용자 요청 "위젯 사진도 다 찍어", 2026-11-10). ShotsTest와 같은 환경 변수로만 돈다. 크기 셋 × 가계부·집중, 밝게·어둡게 */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34], application = ShotsTest.ShotApp::class, qualifiers = "w411dp-h891dp-xxhdpi")
@GraphicsMode(GraphicsMode.Mode.NATIVE)
class WidgetShotsTest {
    @Test fun widgetsLight() = widgets("밝게")

    @Config(qualifiers = "w411dp-h891dp-night-xxhdpi")
    @Test fun widgetsDark() = widgets("어둡게")

    @OptIn(ExperimentalGlanceRemoteViewsApi::class)
    private fun widgets(theme: String) {
        assumeTrue(System.getenv("SHOTS_OUT") != null && System.getenv("SHOTS_SERVER") != null)
        val app = androidx.test.core.app.ApplicationProvider.getApplicationContext<MyHomeApp>()
        val out = File(System.getenv("SHOTS_OUT")!!, "widgets").apply { mkdirs() }
        ShotsTest.seedFocus(app)
        val summary = (app.api.summary(4) as? ApiResult.Ok)?.value
        val day = FocusDay.dayOf(System.currentTimeMillis())
        val (subjects, sessions) = runBlocking {
            app.db.focus().subjects().first() to app.db.focus().sessionsBetween(FocusDay.startOf(day), FocusDay.endOf(day)).first()
        }
        val sizes = listOf("작게" to SummaryWidget.SMALL, "중간" to SummaryWidget.MEDIUM, "크게" to SummaryWidget.LARGE)
        for ((sizeName, size) in sizes) for ((pageName, page) in listOf("가계부" to SummaryWidget.PAGE_BUDGET, "집중" to SummaryWidget.PAGE_FOCUS)) {
            val rv = runBlocking { GlanceRemoteViews().compose(app, size) { SummaryWidgetContent(app, page, summary, subjects, sessions, null) } }.remoteViews
            val v: View = rv.apply(app, FrameLayout(app))
            val d = app.resources.displayMetrics.density
            val w = (size.width.value * d).toInt(); val h = (size.height.value * d).toInt()
            v.measure(View.MeasureSpec.makeMeasureSpec(w, View.MeasureSpec.EXACTLY), View.MeasureSpec.makeMeasureSpec(h, View.MeasureSpec.EXACTLY))
            v.layout(0, 0, w, h)
            val pad = (16 * d).toInt()
            val bmp = Bitmap.createBitmap(w + pad * 2, h + pad * 2, Bitmap.Config.ARGB_8888)
            val c = Canvas(bmp)
            // 바탕화면 느낌의 회색 바탕 위에
            c.drawColor(if (theme == "어둡게") 0xFF101012.toInt() else 0xFFD9DCE3.toInt())
            c.translate(pad.toFloat(), pad.toFloat())
            v.draw(c)
            File(out, "위젯-$theme-$sizeName-$pageName.png").outputStream().use { bmp.compress(Bitmap.CompressFormat.PNG, 100, it) }
        }
    }
}
