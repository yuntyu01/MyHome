package com.yuntyu.myhome.ui

import androidx.activity.ComponentActivity
import androidx.compose.ui.Modifier
import androidx.compose.ui.test.assertIsFocused
import androidx.compose.ui.test.hasSetTextAction
import androidx.compose.ui.test.junit4.createAndroidComposeRule
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performTextInput
import com.yuntyu.myhome.MyHomeApp
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

/**
 * 알림 읽을 앱 검색: 글자를 치면 검색칸 위 칸들(지우기 묶음·고른 앱 확인)이 사라지는데, 그래도 검색칸이 포커스를 잃지 않는다.
 * 예전에는 한 글자("ㅂ")마다 키보드가 내려갔다(2026-11-15 사용자 제보).
 */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34], application = ShotsTest.ShotApp::class, qualifiers = "w411dp-h2000dp")
class AppsSearchTest {
    @get:Rule val rule = createAndroidComposeRule<ComponentActivity>()

    @Test fun searchKeepsFocusWhileTyping() {
        MyHomeApp.instance.settings.setPackageSelected("test.kb", true)
        rule.setContent { MyHomeTheme { AppsScreen(Modifier, onDone = {}) } }
        rule.waitUntil(15_000) { rule.onAllNodes(hasSetTextAction()).fetchSemanticsNodes().isNotEmpty() }
        rule.onNodeWithText("결제 알림 지우기").assertExists() // 고른 앱이 있으면 검색칸 위에 칸들이 있다
        val search = rule.onNode(hasSetTextAction())
        search.performTextInput("ㅂ")
        rule.waitForIdle()
        search.assertIsFocused()
        rule.onNodeWithText("결제 알림 지우기").assertDoesNotExist() // 위 칸이 사라져도
        search.performTextInput("ㅐ")
        rule.waitForIdle()
        search.assertIsFocused()
    }
}
