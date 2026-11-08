package com.yuntyu.myhome.ui

import android.widget.Toast
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Button
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.FilledTonalButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Switch
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.LocalUriHandler
import androidx.compose.ui.text.input.PasswordVisualTransformation
import androidx.compose.ui.unit.dp
import com.yuntyu.myhome.MyHomeApp
import com.yuntyu.myhome.auth.AppLink
import com.yuntyu.myhome.core.ServerUrl
import com.yuntyu.myhome.work.WorkScheduler
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext

@Composable
fun SettingsScreen(modifier: Modifier, onNavigate: (Screen) -> Unit = {}) {
    val ctx = LocalContext.current
    val app = MyHomeApp.instance
    val settings by app.settings.state.collectAsState()
    val hasToken by app.tokenStore.hasToken.collectAsState()
    val scope = rememberCoroutineScope()
    var url by remember { mutableStateOf(settings.serverUrl.orEmpty()) }
    var tokenInput by remember { mutableStateOf("") }

    Column(
        modifier.fillMaxSize().verticalScroll(rememberScrollState()).padding(16.dp),
        verticalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        SectionCard("앱") {
            ItemRow("건강 설정", sub = "끼니·헬스·취침·영양 목표·알림") { onNavigate(Screen.HEALTH_SETTINGS) }
            ItemRow("수집 상태", sub = "알림 접근 권한, 수집할 앱, 전송 로그") { onNavigate(Screen.STATUS) }
        }
        SectionCard("서버") {
            OutlinedTextField(
                value = url,
                onValueChange = { url = it },
                label = { Text("서버 주소 (https://…)") },
                singleLine = true,
                modifier = Modifier.fillMaxWidth(),
            )
            Button(onClick = {
                val normalized = ServerUrl.normalize(url)
                if (normalized == null) {
                    Toast.makeText(ctx, "https 주소를 입력하세요", Toast.LENGTH_SHORT).show()
                } else {
                    url = normalized
                    app.settings.setServerUrl(normalized)
                    WorkScheduler.flushNow(ctx)
                    Toast.makeText(ctx, "저장했어요", Toast.LENGTH_SHORT).show()
                }
            }) { Text("저장") }
        }

        ConnectSection()

        SectionCard("기기 토큰 직접 입력 (비상용)") {
            Text(
                if (hasToken) "토큰이 저장돼 있어요(Keystore 암호화)." else "저장된 토큰이 없어요.",
                style = MaterialTheme.typography.bodyMedium,
            )
            OutlinedTextField(
                value = tokenInput,
                onValueChange = { tokenInput = it.trim() },
                label = { Text("mhd_로 시작하는 토큰") },
                singleLine = true,
                visualTransformation = PasswordVisualTransformation(),
                modifier = Modifier.fillMaxWidth(),
            )
            Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                Button(enabled = tokenInput.isNotBlank(), onClick = {
                    scope.launch {
                        withContext(Dispatchers.IO) { app.tokenStore.save(tokenInput) }
                        app.settings.authProblem = null
                        tokenInput = ""
                        WorkScheduler.flushNow(ctx)
                        Toast.makeText(ctx, "토큰을 저장했어요", Toast.LENGTH_SHORT).show()
                    }
                }) { Text("토큰 저장") }
                FilledTonalButton(enabled = hasToken, onClick = { app.tokenStore.clear() }) { Text("토큰 삭제") }
            }
        }

        SectionCard("동작") {
            ToggleRow("전송 성공 시 원본 알림 자동 삭제", settings.autoDelete) { app.settings.setAutoDelete(it) }
            ToggleRow("예산 알림 (80% 도달·초과, 무음)", settings.budgetAlerts) { app.settings.setBudgetAlerts(it) }
        }

        SectionCard("거부된 알림") {
            Text("서버가 형식 오류로 거부한 원문은 지우지 않고 보관해요. 서버를 고친 뒤 다시 보낼 수 있어요.", style = MaterialTheme.typography.bodySmall)
            FilledTonalButton(onClick = {
                scope.launch {
                    val n = withContext(Dispatchers.IO) { app.db.pendingEvents().requeueRejected() }
                    WorkScheduler.flushNow(ctx)
                    Toast.makeText(ctx, "${n}건을 다시 보내요", Toast.LENGTH_SHORT).show()
                }
            }) { Text("거부된 알림 다시 보내기") }
        }

        // Play 정책: 스토어와 앱 안에 개인정보처리방침 링크를 둔다
        SectionCard("정보") {
            val uri = LocalUriHandler.current
            FilledTonalButton(onClick = { uri.openUri(PRIVACY_POLICY_URL) }) { Text("개인정보처리방침") }
        }
    }
}

/** Play 스토어 등록정보와 같은 주소(Notion 공개 페이지). docs/DECISIONS.md 「Play 배포」 */
const val PRIVACY_POLICY_URL = "https://midi-broccoli-56c.notion.site/412502846708815483d9f80155f93791"

@Composable
fun ToggleRow(label: String, checked: Boolean, onChange: (Boolean) -> Unit) {
    Row(Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
        Text(label, Modifier.weight(1f))
        Switch(checked = checked, onCheckedChange = onChange)
    }
}

/**
 * 앱 연결: 브라우저에서 Cloudflare Access(이메일 코드)로 로그인하면 앱으로 돌아와 이 폰 전용 토큰을 받는다.
 * 돌아오는 주소에는 1회용 코드만 실리고, 토큰은 앱이 직접 서버에서 받는다(AppLink).
 */
@Composable
fun ConnectSection() {
    val ctx = LocalContext.current
    val message by AppLink.message.collectAsState()
    val hasToken by MyHomeApp.instance.tokenStore.hasToken.collectAsState()
    SectionCard("서버 연결") {
        Text(
            "브라우저에서 이메일로 받은 코드로 로그인하면 앱으로 돌아와 이 폰 전용 토큰을 받아요. 토큰은 Android Keystore로 암호화해서 저장하고, 서버에서 폐기하기 전까지 계속 써요.",
            style = MaterialTheme.typography.bodySmall,
        )
        Button(onClick = {
            if (!AppLink.start(ctx)) Toast.makeText(ctx, "서버 주소를 먼저 저장해 주세요", Toast.LENGTH_SHORT).show()
        }) { Text(if (hasToken) "다시 연결하기" else "브라우저에서 연결하기") }
        message?.let { Text(it, style = MaterialTheme.typography.bodyMedium) }
    }
}
