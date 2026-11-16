package com.yuntyu.myhome.ui

import android.graphics.Bitmap
import android.graphics.Canvas
import androidx.activity.ComponentActivity
import androidx.compose.ui.Modifier
import androidx.compose.ui.test.junit4.createAndroidComposeRule
import androidx.compose.ui.test.onNodeWithText
import androidx.room.Room
import androidx.test.core.app.ApplicationProvider
import com.yuntyu.myhome.data.AppDatabase
import com.yuntyu.myhome.data.DismissedNotice
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.runBlocking
import org.junit.Assert.assertEquals
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode
import java.io.File

/** 지운 알림 기록(2026-11-16): 최근 30일·200건만 남기고 새것부터 보여 준다. 합성 알림 */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [27], application = android.app.Application::class, qualifiers = "w360dp-h640dp-xxhdpi")
@GraphicsMode(GraphicsMode.Mode.NATIVE)
class DismissedNoticeTest {
    @get:Rule val rule = createAndroidComposeRule<ComponentActivity>()

    private val day = 24L * 60 * 60 * 1000
    private val now = 1_763_290_000_000L // 2025-11-16 무렵(값만 쓴다)

    private fun n(i: Int, at: Long, title: String = "합성 $i") =
        DismissedNotice(packageName = "test.toss", appLabel = "합성앱", title = title, text = "합성 본문 $i", postedAt = at, dismissedAt = at)

    @Test fun keepsRecentOnly() = runBlocking {
        val db = Room.inMemoryDatabaseBuilder(ApplicationProvider.getApplicationContext(), AppDatabase::class.java).allowMainThreadQueries().build()
        val dao = db.dismissedNotices()
        dao.add(n(0, now - 31 * day)) // 30일 넘음: 다음 기록 때 지운다
        for (i in 1..DismissedNotice.KEEP + 5) dao.add(n(i, now - day + i))
        val left = dao.recent().first()
        assertEquals(DismissedNotice.KEEP, left.size)
        assertEquals("합성 ${DismissedNotice.KEEP + 5}", left.first().title) // 새것부터
        assertEquals(DismissedNotice.KEEP, dao.count().first())
        db.close()
    }

    @Test fun listShowsAppTitleText() {
        val list = listOf(
            n(2, now, "33,000원 결제").copy(id = 2, text = "토스뱅크 체크카드 | 합성헬스"),
            n(1, now - 60_000, "2,500원 결제").copy(id = 1, text = "합성커피"),
        )
        rule.setContent { MyHomeTheme { ModoBackground { DismissedList(Modifier, list) } } }
        rule.waitForIdle()
        System.getenv("SHOTS_OUT")?.let { dir ->
            val v = rule.activity.window.decorView
            val bmp = Bitmap.createBitmap(v.width, v.height, Bitmap.Config.ARGB_8888)
            v.draw(Canvas(bmp))
            File(dir).mkdirs()
            File(dir, "지운-알림.png").outputStream().use { bmp.compress(Bitmap.CompressFormat.PNG, 100, it) }
        }
        rule.onNodeWithText("33,000원 결제").assertExists()
        rule.onNodeWithText("토스뱅크 체크카드 | 합성헬스").assertExists()
    }
}
