package com.yuntyu.myhome.ui

import android.graphics.Bitmap
import android.graphics.Canvas
import androidx.activity.ComponentActivity
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.runtime.key
import androidx.compose.runtime.remember
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.test.junit4.createAndroidComposeRule
import com.yuntyu.myhome.core.CategoryTree
import com.yuntyu.myhome.core.api.Category
import org.junit.Assume.assumeTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode
import java.io.File

/** 분류 줄 오른쪽 손잡이(≡) 모양. SHOTS_OUT이 있을 때만 찍는다(합성 분류) */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [27], application = android.app.Application::class, qualifiers = "w360dp-h640dp-xxhdpi")
@GraphicsMode(GraphicsMode.Mode.NATIVE)
class HandleShotTest {
    @get:Rule val rule = createAndroidComposeRule<ComponentActivity>()

    @Test fun shot() {
        val dir = System.getenv("SHOTS_OUT")
        assumeTrue(dir != null)
        val tree = CategoryTree(listOf(
            Category(1, "생활", "expense", sort = 10),
            Category(2, "식비", "expense", sort = 10, parentId = 1),
            Category(3, "카페·간식", "expense", sort = 20, parentId = 1),
            Category(5, "고정비", "expense", sort = 20),
        ))
        rule.setContent {
            MyHomeTheme {
                ModoBackground {
                    val scroll = rememberScrollState()
                    val d = rememberCategoryDragger(scroll)
                    Column(Modifier.fillMaxSize().dragViewport(d).verticalScroll(scroll).dropMarks(d, null, Color.Red)) {
                        for ((c, depth) in remember { tree.preorder() }) key(c.id) {
                            DragRow(d, tree, c, onTap = {}, onDrop = { _, _ -> }) { handle ->
                                FlatRow(c.name, value = "100,000원", bold = tree.children(c.id).isNotEmpty(), leading = indentLead(depth), trailing = handle)
                            }
                        }
                    }
                }
            }
        }
        rule.waitForIdle()
        val v = rule.activity.window.decorView
        val bmp = Bitmap.createBitmap(v.width, v.height, Bitmap.Config.ARGB_8888)
        v.draw(Canvas(bmp))
        File(dir!!).mkdirs()
        File(dir, "분류-손잡이.png").outputStream().use { bmp.compress(Bitmap.CompressFormat.PNG, 100, it) }
    }
}
