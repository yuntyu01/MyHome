package com.yuntyu.myhome.ui

import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.text.selection.SelectionContainer
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.ui.geometry.Rect
import androidx.compose.ui.platform.ClipboardManager
import androidx.compose.ui.platform.LocalClipboardManager
import androidx.compose.ui.platform.LocalTextToolbar
import androidx.compose.ui.platform.TextToolbar
import androidx.compose.ui.platform.TextToolbarStatus
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.longClick
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.compose.ui.test.percentOffset
import androidx.compose.ui.test.performTouchInput
import androidx.compose.ui.text.AnnotatedString
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode

/**
 * 꾹 눌러 글자 고르기(앱 전체 SelectionContainer)가 누르기·대화상자와 부딪히지 않는지 (합성 글자).
 * - Application은 기본 것(MyHomeApp은 DB·작업 예약을 켜서 화면 조각 테스트에는 필요 없다)
 * - API 27: 28부터 고르기 중에 뜨는 돋보기 창이 Robolectric에서 그려지지 않아 죽는다(앱과 상관없음)
 * - NATIVE 그래픽: 대화상자·메뉴 창을 실제로 그린다
 */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [27], application = android.app.Application::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
class SelectionTest {
    @get:Rule val rule = createComposeRule()

    /** 고르면 뜨는 복사 메뉴를 받아 두는 가짜 */
    private class FakeToolbar : TextToolbar {
        var copy: (() -> Unit)? = null
        var shown = 0
        override fun showMenu(rect: Rect, onCopyRequested: (() -> Unit)?, onPasteRequested: (() -> Unit)?, onCutRequested: (() -> Unit)?, onSelectAllRequested: (() -> Unit)?) {
            shown++
            copy = onCopyRequested
        }
        override fun hide() {}
        override val status: TextToolbarStatus get() = if (copy != null) TextToolbarStatus.Shown else TextToolbarStatus.Hidden
    }

    private class FakeClipboard : ClipboardManager {
        var copied: AnnotatedString? = null
        override fun setText(annotatedString: AnnotatedString) { copied = annotatedString }
        override fun getText(): AnnotatedString? = copied
    }

    private val toolbar = FakeToolbar()
    private val clipboard = FakeClipboard()

    /** 앱(AppRoot)처럼 화면을 SelectionContainer 하나로 감싼다 */
    private fun screen(content: @Composable () -> Unit) = rule.setContent {
        MyHomeTheme {
            CompositionLocalProvider(LocalTextToolbar provides toolbar, LocalClipboardManager provides clipboard) {
                SelectionContainer { Column { content() } }
            }
        }
    }

    @Test fun tapsStillReachRows() {
        var clicked = 0
        screen { FlatRow("합성 가게", value = "1,000원", onClick = { clicked++ }) }
        rule.onNodeWithText("합성 가게").performClick()
        rule.onNodeWithText("1,000원").performClick()
        assertEquals(2, clicked)
    }

    // 가게 이름을 꾹 누르면 골라져서 복사되고, 손을 떼도 그 줄이 열리지 않는다
    @Test fun longPressOnRowTextCopiesInsteadOfOpening() {
        var clicked = 0
        screen { FlatRow("합성가게", sub = "식비 · 12:30", onClick = { clicked++ }) }
        // 줄이 글자를 합쳐 한 노드라서, 글자 자체를 누르려면 합치기 전 트리에서 찾는다
        rule.onNodeWithText("합성가게", useUnmergedTree = true).performTouchInput { longClick() }
        rule.waitForIdle()
        assertEquals("꾹 눌렀다 떼면 누르기가 아니다", 0, clicked)
        assertTrue("고르면 복사 메뉴가 떠야 한다", toolbar.shown > 0)
        rule.runOnIdle { toolbar.copy?.invoke() }
        assertTrue("복사한 글자: ${clipboard.copied}", clipboard.copied?.text.orEmpty().contains("합성가게"))
        // 그다음 그냥 누르면 열린다
        rule.onNodeWithText("식비 · 12:30", useUnmergedTree = true).performClick()
        assertEquals(1, clicked)
    }

    // 글자 옆 빈 곳(꺾쇠 쪽)을 꾹 눌렀다 떼도 열리지 않는다(Modifier.tap). clickable이면 이때 열린다
    @Test fun longPressBesideTheTextDoesNotOpenTheRow() {
        var clicked = 0
        screen { FlatRow("합성가게", onClick = { clicked++ }) }
        rule.onNodeWithText("합성가게").performTouchInput { longClick(percentOffset(0.97f, 0.5f)) }
        rule.waitForIdle()
        assertEquals(0, clicked)
        rule.onNodeWithText("합성가게").performClick()
        assertEquals(1, clicked)
    }

    // 🔴 대화상자는 다른 창이라 바깥 SelectionContainer에 붙으면 꾹 누를 때 죽는다(Selection.kt). Modo* 는 안 죽고 골라져야 한다
    @Test fun longPressInsideADialogSelectsWithoutCrashing() {
        var confirmed = 0
        screen {
            Text("바깥 글자")
            ModoAlertDialog(
                onDismissRequest = {},
                confirmButton = { TextButton(onClick = { confirmed++ }) { Text("합성 확인") } },
                title = { Text("합성 제목") },
                text = { Text("합성 본문 글자") },
            )
        }
        rule.onNodeWithText("합성 본문 글자").performTouchInput { longClick() }
        rule.waitForIdle()
        rule.onNodeWithText("합성 제목").performTouchInput { longClick() }
        rule.waitForIdle()
        // 대화상자 버튼은 material3 그대로라 꾹 눌렀다 떼면 누른 것과 같다. 죽지 않고 눌리기만 하면 된다
        rule.onNodeWithText("합성 확인").performTouchInput { longClick() }
        rule.waitForIdle()
        val afterLong = confirmed
        rule.onNodeWithText("합성 확인").performClick()
        assertEquals(afterLong + 1, confirmed)
    }

    // 아래 판도 창이 따로라 같은 이유로 ModoBottomSheet
    @Test fun longPressInsideASheetSelectsWithoutCrashing() {
        var picked = 0
        screen {
            Text("바깥 글자")
            ModoBottomSheet(onDismissRequest = {}) {
                Text("합성 판 제목")
                FlatRow("합성 분류", chevron = false, onClick = { picked++ })
            }
        }
        rule.onNodeWithText("합성 판 제목").performTouchInput { longClick() }
        rule.onNodeWithText("합성 분류").performTouchInput { longClick() }
        rule.waitForIdle()
        assertEquals(0, picked)
        rule.onNodeWithText("합성 분류").performClick()
        assertEquals(1, picked)
    }

    @Test fun longPressInsideAMenuDoesNotCrash() {
        var picked = 0
        screen {
            Text("바깥 글자")
            ModoDropdownMenu(expanded = true, onDismissRequest = {}) {
                androidx.compose.material3.DropdownMenuItem(text = { Text("합성 메뉴") }, onClick = { picked++ })
            }
        }
        // 메뉴는 고르기를 꺼서, 꾹 눌렀다 떼면 그냥 누른 것과 같다(material3 메뉴 그대로). 죽지 않고 눌리기만 하면 된다
        rule.onNodeWithText("합성 메뉴").performTouchInput { longClick() }
        rule.waitForIdle()
        val afterLong = picked
        rule.onNodeWithText("합성 메뉴").performClick()
        assertEquals(afterLong + 1, picked)
    }
}
