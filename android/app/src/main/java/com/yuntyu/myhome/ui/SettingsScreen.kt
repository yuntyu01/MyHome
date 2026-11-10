package com.yuntyu.myhome.ui

import android.widget.Toast
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Switch
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.remember
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.LocalUriHandler
import androidx.compose.ui.unit.dp
import com.yuntyu.myhome.BuildConfig
import com.yuntyu.myhome.MyHomeApp
import com.yuntyu.myhome.auth.AppLink
import com.yuntyu.myhome.capture.CaptureService
import com.yuntyu.myhome.core.Money

/**
 * 설정: 가계부 · 알림 · 건강 · 계정을 줄로 나눈다. 서버 주소, 기기 연결, 보낸 기록 같은 어려운 항목은
 * 맨 아래 "문제가 생겼을 때"(문제 해결 화면) 안에 둔다.
 */
@Composable
fun SettingsScreen(modifier: Modifier, resumeTick: Int, onNavigate: (Screen) -> Unit = {}) {
    val ctx = LocalContext.current
    val app = MyHomeApp.instance
    val m = LocalModo.current
    val settings by app.settings.state.collectAsState()
    val hasToken by app.tokenStore.hasToken.collectAsState()
    val summary by app.summaryRepo.state.collectAsState()
    val linkMessage by AppLink.message.collectAsState()
    val listenerOn = remember(resumeTick) { PermissionStatus.listenerEnabled(ctx) }
    val batteryOk = remember(resumeTick) { PermissionStatus.batteryUnrestricted(ctx) }
    val connected = remember(resumeTick) { CaptureService.current != null }
    val authProblem = remember(resumeTick, hasToken) { app.settings.authProblem }
    val appNames = remember(settings.selectedPackages) {
        settings.selectedPackages.map { pkg ->
            runCatching { ctx.packageManager.getApplicationLabel(ctx.packageManager.getApplicationInfo(pkg, 0)).toString() }.getOrDefault(pkg)
        }.sorted()
    }

    // 결제 알림 읽기 상태를 쉬운 말 하나로
    val (readText, readOk) = when {
        !listenerOn -> "꺼져 있어요" to false
        !connected -> "멈춰 있어요" to false
        !hasToken || authProblem != null -> "로그인이 필요해요" to false
        settings.selectedPackages.isEmpty() -> "읽을 앱을 골라 주세요" to false
        !batteryOk -> "절전 때문에 놓칠 수 있어요" to false
        else -> "잘 되고 있어요" to true
    }

    Column(modifier.fillMaxSize().verticalScroll(rememberScrollState()).padding(bottom = 24.dp)) {
        SectionLabel("가계부", Modifier.padding(top = 0.dp))
        FlatRow(
            "결제 알림 읽기",
            value = readText,
            valueColor = if (readOk) m.muted else m.over,
            leading = null,
            trailing = null,
            onClick = { onNavigate(Screen.STATUS) },
        )
        FlatRow(
            "알림 읽을 앱",
            value = when (appNames.size) {
                0 -> "아직 안 골랐어요"
                1 -> appNames.first()
                else -> "${appNames.first()} 외 ${appNames.size - 1}개"
            },
            onClick = { onNavigate(Screen.APPS) },
        )
        val budget = summary.summary?.totals?.budget ?: 0L
        FlatRow("분류·폴더·예산", value = if (budget > 0) "한 달 ${Money.won(budget)}" else "예산 없음", onClick = { onNavigate(Screen.BUDGETS) })

        SectionLabel("알림")
        SwitchRow("예산 80% 넘으면 알려 주기", settings.budgetAlerts, sub = "넘었을 때도 알려요. 소리 없이 와요") { app.settings.setBudgetAlerts(it) }
        SwitchRow("적은 뒤 원래 결제 알림 지우기", settings.autoDelete, sub = "가계부에 적으면 카드·은행 앱 알림을 치워요") { app.settings.setAutoDelete(it) }

        SectionLabel("건강")
        FlatRow("끼니 시간과 목표", sub = "끼니 수·마감, 영양 목표, 알림", onClick = { onNavigate(Screen.HEALTH_SETTINGS) })
        FlatRow("헬스장 가면 자동 기록", sub = "헬스장 위치와 출석", onClick = { onNavigate(Screen.HEALTH_GYM) })

        SectionLabel("계정")
        FlatRow(
            if (hasToken && authProblem == null) "로그인돼 있어요" else "로그인하기",
            sub = linkMessage ?: if (hasToken && authProblem == null) "이 폰으로 연결돼 있어요" else "브라우저에서 이메일로 받은 코드를 넣으면 끝나요",
            value = if (hasToken && authProblem == null) "다시 연결" else null,
            onClick = {
                if (!AppLink.start(ctx)) {
                    Toast.makeText(ctx, "서버 주소가 없어요. 문제 해결에서 넣어 주세요", Toast.LENGTH_LONG).show()
                    onNavigate(Screen.STATUS)
                }
            },
        )

        FlatRow("처음 설정 다시 보기", sub = "로그인·결제 알림·앱 알림·건강 목표를 순서대로", onClick = { onNavigate(Screen.ONBOARDING) })

        Hairline(Modifier.padding(top = 16.dp))
        FlatRow("문제가 생겼을 때", sub = "결제가 안 적히면 여기서 하나씩 확인해요", titleColor = m.muted, onClick = { onNavigate(Screen.STATUS) })
        // Play 정책: 스토어와 앱 안에 개인정보처리방침 링크를 둔다
        val uri = LocalUriHandler.current
        FlatRow("개인정보처리방침", value = "버전 ${BuildConfig.VERSION_NAME}", titleColor = m.muted, onClick = { uri.openUri(PRIVACY_POLICY_URL) })
    }
}

/** Play 스토어 등록정보와 같은 주소(Notion 공개 페이지). docs/DECISIONS.md 「Play 배포」 */
const val PRIVACY_POLICY_URL = "https://midi-broccoli-56c.notion.site/412502846708815483d9f80155f93791"

/** 카드 안 켜기·끄기 한 줄(건강 화면들이 쓴다) */
@Composable
fun ToggleRow(label: String, checked: Boolean, onChange: (Boolean) -> Unit) {
    Row(Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
        Text(label, Modifier.weight(1f))
        Switch(checked = checked, onCheckedChange = onChange)
    }
}
