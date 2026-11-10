package com.yuntyu.myhome.ui

import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.text.selection.SelectionContainer
import androidx.compose.ui.Modifier
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onAllNodesWithContentDescription
import androidx.compose.ui.test.onFirst
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.compose.ui.unit.dp
import com.yuntyu.myhome.core.api.Backtest
import com.yuntyu.myhome.core.api.MyHomeJson
import com.yuntyu.myhome.core.api.TransactionDto
import androidx.compose.material3.Button
import androidx.compose.material3.Text
import androidx.compose.ui.test.onNodeWithContentDescription
import org.junit.Assert.assertTrue
import org.robolectric.Shadows.shadowOf
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode

/** 글자 줄이기(2026-11-10): "?" 설명, 한 줄 칩, 결제 밑 남은 돈, 백테스트 화면이 죽지 않는지 (합성 값) */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [27], application = android.app.Application::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
class SimpleScreensTest {
    @get:Rule val rule = createComposeRule()

    private fun screen(content: @androidx.compose.runtime.Composable () -> Unit) =
        rule.setContent { MyHomeTheme { SelectionContainer { Column { content() } } } }

    // 줄에는 짧은 글만, 자세한 설명은 "?"를 눌러야 보인다. "?"를 눌러도 줄은 눌리지 않는다
    @Test fun helpDotShowsTheLongTextOnlyWhenTapped() {
        var rowClicks = 0
        screen { FlatRow("합성 설정", help = "합성 긴 설명 문장", onClick = { rowClicks++ }) }
        rule.onNodeWithText("합성 긴 설명 문장").assertDoesNotExist()
        rule.onAllNodesWithContentDescription("설명").onFirst().performClick()
        rule.waitForIdle()
        rule.onNodeWithText("합성 긴 설명 문장").assertExists()
        assertTrue("? 를 눌렀는데 줄이 눌림: $rowClicks", rowClicks == 0)
    }

    // 좁은 칸 넷에 "린매스업"이 두 줄로 깨지지 않는다(한 줄 칩 높이가 같다)
    @Test fun goalChipsStayOnOneLine() {
        screen {
            Row(Modifier.width(300.dp)) {
                for ((_, l) in GOALS) Chip(l, false, Modifier.weight(1f)) {}
            }
        }
        rule.waitForIdle()
        val short = rule.onNodeWithText("감량").fetchSemanticsNode().size.height
        val long = rule.onNodeWithText("린매스업").fetchSemanticsNode().size.height
        assertTrue("린매스업 높이 $long > 감량 $short", long <= short)
    }

    // 나간 돈은 크게, 그 뒤 남은 돈은 밑에 작게. 넘으면 "넘음"
    @Test fun paymentShowsWhatIsLeftUnderTheAmount() {
        val t = TransactionDto(id = 1, occurredAt = "2026-11-10T12:00:00+09:00", amount = 5_000, flow = "out", merchant = "합성카페", direction = "expense", category = "카페·간식", status = "confirmed")
        screen {
            TxRow(t, withDay = false, remaining = 85_000)
            TxRow(t.copy(id = 2, merchant = "합성식당"), withDay = false, remaining = -3_000)
        }
        rule.onNodeWithText("85,000원 남음").assertExists()
        rule.onNodeWithText("3,000원 넘음").assertExists()
    }

    // 🔴 "손실%는"을 String.format에 넣으면 UnknownFormatConversionException으로 화면이 죽었다(3e534e7). 표를 넣고 그려 본다
    @Test fun backtestScreenRendersWithData() {
        val b = MyHomeJson.decodeFromString(Backtest.serializer(), BACKTEST)
        screen { BacktestContent(Modifier, b, null) }
        rule.waitForIdle()
        rule.onNodeWithText("TQQQ 보유", substring = true).assertExists()
        rule.onNodeWithText("읽기 전에").assertExists()
    }

    // 무음 카메라: 권한이 있으면 앱 안 카메라 화면이 열리고(기본 카메라 앱 아님), 닫으면 사진 없이 닫힌다.
    // 실제 카메라는 JVM에서 못 켜서 찍기는 폰에서 확인한다
    @Test fun silentCameraOpensInAppAndCloses() {
        val app = androidx.test.core.app.ApplicationProvider.getApplicationContext<android.app.Application>()
        shadowOf(app).grantPermissions(android.Manifest.permission.CAMERA)
        var shots = 0
        screen {
            val shoot = rememberSilentCamera { shots++ }
            Button(onClick = shoot) { Text("합성 찍기") }
        }
        rule.onNodeWithText("합성 찍기").performClick()
        rule.waitForIdle()
        rule.onNodeWithContentDescription("닫기").assertExists()
        rule.onNodeWithContentDescription("닫기").performClick()
        rule.waitForIdle()
        rule.onNodeWithContentDescription("닫기").assertDoesNotExist()
        assertTrue(shots == 0)
    }

    private companion object {
        // 서버 /v1/invest/backtest 모양(합성 값)
        val BACKTEST = """
            {"period":"1998-08 ~ 2026-07 · 336개월","fee_rate":0.001,"unit":"억원",
             "columns":[{"key":"worst","label":"최악","percent":false,"higher_is_better":true},
                        {"key":"median","label":"중앙","percent":false,"higher_is_better":true},
                        {"key":"loss_pct","label":"손실%","percent":true,"higher_is_better":false},
                        {"key":"mdd_worst_pct","label":"MDD최악","percent":true,"higher_is_better":true}],
             "key":[{"name":"★ 합성 전략","star":true,"principal":1.26,"windows":100,"worst":0.66,"median":2.1,"loss_pct":11.5,"mdd_worst_pct":-59.5,"tops":["loss_pct"]},
                    {"name":"TQQQ 보유","principal":1.26,"windows":100,"worst":0.22,"median":3.0,"loss_pct":21.4,"mdd_worst_pct":-97.9,"tops":["median"]}],
             "horizons":[{"label":"7년",
                          "ex_dotcom":[{"name":"★ 합성 전략","star":true,"principal":1.26,"windows":90,"worst":0.7,"median":2.2,"loss_pct":10.0,"mdd_worst_pct":-50.0,"tops":[]}],
                          "all":[{"name":"★ 합성 전략","star":true,"principal":1.26,"windows":100,"worst":0.66,"median":2.1,"loss_pct":11.5,"mdd_worst_pct":-59.5,"tops":[]}]}],
             "notes":["최악·하위10%를 먼저 보라."]}
        """.trimIndent()
    }

    // 금액 칸: 치는 값은 숫자만, 보이는 것은 "500,000"(쉼표), 커서는 쉼표를 건너 맞는다
    @Test fun moneyFieldShowsCommas() {
        val t = ThousandsTransformation.filter(androidx.compose.ui.text.AnnotatedString("1234567"))
        assertTrue(t.text.text == "1,234,567")
        assertTrue(t.offsetMapping.originalToTransformed(1) == 2 && t.offsetMapping.originalToTransformed(7) == 9)
        assertTrue(t.offsetMapping.transformedToOriginal(2) == 1 && t.offsetMapping.transformedToOriginal(9) == 7)
        assertTrue(ThousandsTransformation.filter(androidx.compose.ui.text.AnnotatedString("500000")).text.text == "500,000")
    }
}
