package com.yuntyu.myhome.ui

import android.graphics.Bitmap
import android.graphics.Canvas
import androidx.activity.ComponentActivity
import androidx.compose.foundation.layout.Column
import androidx.compose.material3.LocalContentColor
import androidx.compose.material3.MaterialTheme
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.ui.test.junit4.createAndroidComposeRule
import androidx.compose.ui.test.onNodeWithContentDescription
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import com.yuntyu.myhome.core.CategoryTree
import com.yuntyu.myhome.core.api.Category
import com.yuntyu.myhome.core.api.TransactionDto
import org.junit.Assert.assertEquals
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode
import java.io.File

/** 거래 밑 분류 동그라미(2026-11-16): 지금 분류에 체크, 다른 걸 누르면 그 분류로, 더보기는 판. 합성 값. SHOTS_OUT이 있으면 찍는다 */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [27], application = android.app.Application::class, qualifiers = "w360dp-h640dp-xxhdpi")
@GraphicsMode(GraphicsMode.Mode.NATIVE)
class QuickPickTest {
    @get:Rule val rule = createAndroidComposeRule<ComponentActivity>()

    private val tree = CategoryTree(listOf(
        Category(5, "생활", "expense", sort = 50),
        Category(1, "식비", "expense", sort = 10, parentId = 5),
        Category(2, "카페·간식", "expense", sort = 20, parentId = 5),
        Category(4, "쇼핑", "expense", sort = 40, parentId = 5),
        Category(11, "기타", "expense", sort = 110),
    ))
    private val tx = TransactionDto(id = 7, occurredAt = "2026-11-16T12:00:00+09:00", amount = 3_300, flow = "out", merchant = "합성편의점",
        direction = "expense", categoryId = 1, category = "식비", status = "confirmed")

    @Test fun showsCurrentAndPicksAnother() {
        var picked: Long? = null
        var more = 0
        rule.setContent {
            MyHomeTheme {
                ModoBackground {
                    CompositionLocalProvider(LocalContentColor provides MaterialTheme.colorScheme.onBackground) {
                        Column {
                            TxRow(tx, withDay = false) {}
                            QuickCategoryPick(tx, tree, onPick = { picked = it.id }, onMore = { more++ })
                        }
                    }
                }
            }
        }
        rule.waitForIdle()
        System.getenv("SHOTS_OUT")?.let { dir ->
            val v = rule.activity.window.decorView
            val bmp = Bitmap.createBitmap(v.width, v.height, Bitmap.Config.ARGB_8888)
            v.draw(Canvas(bmp))
            File(dir).mkdirs()
            File(dir, "거래-분류-동그라미.png").outputStream().use { bmp.compress(Bitmap.CompressFormat.PNG, 100, it) }
        }
        rule.onNodeWithContentDescription("선택됨").assertExists() // 지금 분류(식비)에 체크
        rule.onNodeWithText("카페·간식").performClick()
        assertEquals(2L, picked)
        rule.onNodeWithText("더보기").performClick()
        assertEquals(1, more)
    }
}
