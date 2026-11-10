package com.yuntyu.myhome.ui

import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.text.selection.SelectionContainer
import androidx.compose.runtime.mutableStateOf
import androidx.compose.ui.Modifier
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.SemanticsProperties
import androidx.compose.ui.test.SemanticsMatcher
import androidx.compose.ui.test.SemanticsNodeInteraction
import androidx.compose.ui.test.hasSetTextAction
import androidx.compose.ui.test.hasText
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.compose.ui.test.performScrollTo
import androidx.compose.ui.test.performTextInput
import com.yuntyu.myhome.core.api.GymPlan
import com.yuntyu.myhome.core.api.HealthSettings
import com.yuntyu.myhome.core.api.MealSlot
import com.yuntyu.myhome.core.api.Recommendation
import com.yuntyu.myhome.core.api.SleepPlan
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.jsonArray
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode

/** 처음 설정(온보딩): 어떤 쪽을 보여 주는지와, 쪽마다 눌러서 끝까지 가는지 (합성 값) */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [27], application = android.app.Application::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
class OnboardingTest {
    @get:Rule val rule = createComposeRule()

    private val nothing = OnboardFacts(hasToken = false, listenerOn = false, appsChosen = false, notifyOk = false, exactOk = false, healthPersonal = null)
    private val everything = OnboardFacts(hasToken = true, listenerOn = true, appsChosen = true, notifyOk = true, exactOk = true, healthPersonal = true)

    @Test fun freshInstallSeesEveryPage() {
        assertEquals(OnboardPage.entries.toList(), onboardPages(nothing, all = false))
    }

    @Test fun setUpPhoneSkipsWhatIsDone() {
        assertEquals(listOf(OnboardPage.WELCOME, OnboardPage.DONE), onboardPages(everything, all = false))
        // 가계부는 다 했고 정시 알람·건강만 남은 폰(이 기능이 나오기 전부터 쓰던 폰)
        val old = everything.copy(exactOk = false, healthPersonal = false)
        assertEquals(
            listOf(OnboardPage.WELCOME, OnboardPage.NOTIFY, OnboardPage.BODY, OnboardPage.ROUTINE, OnboardPage.DONE),
            onboardPages(old, all = false),
        )
        // 설정에서 다시 보면 다
        assertEquals(OnboardPage.entries.toList(), onboardPages(everything, all = true))
    }

    @Test fun navMovesAndJumpsInOrder() {
        val nav = OnboardNav().apply { pages = listOf(OnboardPage.WELCOME, OnboardPage.BODY, OnboardPage.DONE) }
        assertFalse(nav.prev())
        nav.next(); assertEquals(OnboardPage.BODY, nav.current)
        nav.next(); assertEquals(OnboardPage.DONE, nav.current)
        nav.next(); assertEquals(OnboardPage.DONE, nav.current)
        // 끝에서 안 한 것을 누르면 순서대로 끼워 넣고 그 쪽으로
        nav.jump(OnboardPage.LOGIN)
        assertEquals(listOf(OnboardPage.WELCOME, OnboardPage.LOGIN, OnboardPage.BODY, OnboardPage.DONE), nav.pages)
        nav.next(); assertEquals(OnboardPage.BODY, nav.current)
        // 건강 쪽에 있을 때는 빼지 않는다, 지나간 뒤에는 뺀다
        nav.dropHealth(); assertTrue(OnboardPage.BODY in nav.pages)
        nav.next(); nav.dropHealth(); assertFalse(OnboardPage.BODY in nav.pages)
    }

    private class FakeHost : OnboardHost {
        val calls = mutableListOf<String>()
        val saved = mutableListOf<JsonObject>()
        var finished = 0
        override fun login() { calls += "login" }
        override fun allowListener() { calls += "listener" }
        override fun chooseApps() { calls += "apps" }
        override fun allowBattery() { calls += "battery" }
        override fun budgets() { calls += "budgets" }
        override fun allowNotifications() { calls += "notify" }
        override fun allowExact() { calls += "exact" }
        override fun retryHealth() { calls += "retry" }
        override fun pickTime(initial: String, onPick: (String) -> Unit) { calls += "time:$initial"; onPick("09:30") }
        override suspend fun recommend(goal: String, weightKg: Double, heightCm: Int, age: Int, sex: String, activity: String): Result<Recommendation> {
            calls += "recommend:$goal/$weightKg/$heightCm/$age/$sex/$activity"
            return Result.success(Recommendation(kcal = 2100, carbs = 250, protein = 130, fat = 60, maintenanceKcal = 2400.0))
        }
        override suspend fun saveHealth(patch: JsonObject): String? { saved += patch; return null }
        override fun finish() { finished++ }
    }

    // 서버 기본값 같은 합성 설정(성별·나이·활동을 안 정함)
    private val serverDefaults = HealthSettings(
        goal = "keep", heightCm = 175, weightKg = 70.0,
        meals = listOf(MealSlot(1, "08:00"), MealSlot(2, "14:00"), MealSlot(3, "17:00"), MealSlot(4, "19:00")),
        gym = GymPlan(enabled = true, days = listOf(0, 1, 2, 3, 4), weeklyTarget = 5, arriveBy = "19:00"),
        sleep = SleepPlan(enabled = true),
    )

    // 아래 큰 버튼과 위 줄은 스크롤 밖이라 스크롤은 되는 것만
    private fun click(text: String, substring: Boolean = false): SemanticsNodeInteraction {
        val node = rule.onNodeWithText(text, substring = substring)
        runCatching { node.performScrollTo() }
        return node.performClick().also { rule.waitForIdle() }
    }

    // 아래 큰 버튼(할 일 줄과 글자가 같을 수 있어서 역할로 고른다)
    private fun press(text: String) {
        rule.onNode(hasText(text) and SemanticsMatcher.expectValue(SemanticsProperties.Role, Role.Button)).performClick()
        rule.waitForIdle()
    }

    private fun type(label: String, value: String) {
        rule.onNode(hasSetTextAction() and hasText(label)).performScrollTo().performTextInput(value)
    }

    @Test fun walksThroughEveryPageToTheEnd() {
        val host = FakeHost()
        val nav = OnboardNav()
        val status = mutableStateOf(OnboardStatus(exactNeeded = true))
        rule.setContent {
            MyHomeTheme { SelectionContainer { Column { OnboardingContent(Modifier, nav, status.value, host) } } }
        }
        rule.waitForIdle()
        assertEquals(OnboardPage.entries.toList(), nav.pages)

        press("시작하기")
        // 로그인: 버튼은 로그인, 돌아와서 토큰이 생기면 "다음"
        press("이메일로 로그인하기")
        assertEquals(listOf("login"), host.calls)
        status.value = status.value.copy(hasToken = true, health = serverDefaults)
        press("다음")

        // 결제 알림: 지금 할 것 하나만 큰 버튼, 나중에로 넘길 수 있다
        assertEquals(OnboardPage.MONEY, nav.current)
        press("알림 읽기 허용하기")
        status.value = status.value.copy(listenerOn = true)
        press("카드·은행 앱 고르기")
        assertEquals(listOf("login", "listener", "apps"), host.calls)
        click("나중에")

        // 알림: 앱 알림 → 정시 알람 순서
        assertEquals(OnboardPage.NOTIFY, nav.current)
        press("알림 허용하기")
        status.value = status.value.copy(notifyOk = true)
        press("알람 및 리마인더 허용하기")
        status.value = status.value.copy(exactOk = true)
        press("다음")

        // 건강 목표: 서버 기본 키·몸무게는 비워 두고, 다 넣어야 계산된다
        assertEquals(OnboardPage.BODY, nav.current)
        click("감량")
        click("남성")
        type("키", "180")
        type("몸무게", "75.5")
        type("나이", "29")
        click("가벼움")
        press("목표 계산하기")
        assertTrue(host.calls.last(), host.calls.last() == "recommend:lose/75.5/180/29/male/low")
        rule.onNodeWithText("하루 2,100 kcal").assertExists()
        press("이 목표로 저장")
        val body = host.saved.single()
        assertEquals("male", body["sex"]!!.jsonPrimitive.content)
        assertEquals("29", body["age"]!!.jsonPrimitive.content)
        assertEquals("lose", body["goal"]!!.jsonPrimitive.content)
        assertEquals("2100", body["targets"]!!.jsonObject["kcal"]!!.jsonPrimitive.content)
        // 목표가 바뀐 날을 서버가 남기도록 이력은 보내지 않는다
        assertFalse("targetHistory" in body)
        // 실제 앱은 저장한 설정을 다시 받는다(성별·나이·활동이 생겨 건강 목표를 정한 것이 된다)
        status.value = status.value.copy(health = serverDefaults.copy(sex = "male", age = 29, activity = "low"))
        rule.waitForIdle()
        // 끼니 쪽에 있는 동안에는 건강 쪽을 목록에서 빼지 않는다
        assertTrue(OnboardPage.ROUTINE in nav.pages)

        // 끼니·헬스·취침: 3끼로 줄이고 1끼 마감을 바꾼 뒤 저장
        assertEquals(OnboardPage.ROUTINE, nav.current)
        click("3끼")
        click("1끼 마감")
        press("저장하고 다음")
        val routine = host.saved[1]
        val meals = routine["meals"]!!.jsonArray
        assertEquals(3, meals.size)
        assertEquals("09:30", meals[0].jsonObject["deadline"]!!.jsonPrimitive.content)
        assertEquals("5", routine["gym"]!!.jsonObject["weeklyTarget"]!!.jsonPrimitive.content)
        // 알림 켜기·끄기는 건강 설정에서 정한 값을 그대로 둔다
        assertFalse("notifications" in routine)

        // 끝: 앱 고르기를 건너뛰어서 결제 알림 읽기가 남았다고 보여 주고, 누르면 그 쪽으로 돌아간다
        assertEquals(OnboardPage.DONE, nav.current)
        rule.onNodeWithText("거의 다 됐어요").assertExists()
        click("결제 알림 읽기")
        assertEquals(OnboardPage.MONEY, nav.current)
        status.value = status.value.copy(appsChosen = 2, batteryOk = true)
        press("다음")
        // 이미 지나온 쪽은 넘긴다
        while (nav.current != OnboardPage.DONE) if (nav.current == OnboardPage.BODY) click("나중에") else press(if (nav.current == OnboardPage.ROUTINE) "저장하고 다음" else "다음")
        rule.onNodeWithText("준비됐어요").assertExists()
        press("MyHome 시작하기")
        assertEquals(1, host.finished)
    }

    @Test fun welcomeCanBeSkippedAndHealthWaitsForLogin() {
        val host = FakeHost()
        val nav = OnboardNav()
        rule.setContent {
            MyHomeTheme { SelectionContainer { Column { OnboardingContent(Modifier, nav, OnboardStatus(), host) } } }
        }
        rule.waitForIdle()
        nav.current = OnboardPage.BODY
        rule.waitForIdle()
        // 로그인 전에는 건강 쪽이 로그인으로 보낸다
        press("로그인하러 가기")
        assertEquals(OnboardPage.LOGIN, nav.current)
        nav.current = OnboardPage.WELCOME
        rule.waitForIdle()
        click("건너뛰기")
        assertEquals(1, host.finished)
    }
}
