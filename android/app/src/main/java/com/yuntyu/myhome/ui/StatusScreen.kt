package com.yuntyu.myhome.ui

import android.Manifest
import android.os.Build
import android.widget.Toast
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import com.yuntyu.myhome.MyHomeApp
import com.yuntyu.myhome.auth.AppLink
import com.yuntyu.myhome.capture.CaptureService
import com.yuntyu.myhome.core.ServerUrl
import com.yuntyu.myhome.notify.Notifications
import com.yuntyu.myhome.work.WorkScheduler
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext

/**
 * 문제 해결: 결제가 가계부에 안 적힐 때 위에서부터 하나씩 확인한다. 줄마다 상태 점(정상 회색·문제 살구색)과 쉬운 말,
 * 누르면 고치는 곳으로 간다. 서버 주소·토큰 직접 입력 같은 비상용은 맨 아래 "고급"에 둔다.
 */
@Composable
fun StatusScreen(modifier: Modifier, resumeTick: Int, onNavigate: (Screen) -> Unit) {
    val ctx = LocalContext.current
    val app = MyHomeApp.instance
    val scope = rememberCoroutineScope()
    val m = LocalModo.current
    val settings by app.settings.state.collectAsState()
    val hasToken by app.tokenStore.hasToken.collectAsState()
    val pending by remember { app.db.pendingEvents().pendingCount() }.collectAsState(initial = 0)
    val rejected by remember { app.db.pendingEvents().rejectedCount() }.collectAsState(initial = 0)
    var localTick by remember { mutableIntStateOf(0) }
    val listenerOn = remember(resumeTick, localTick) { PermissionStatus.listenerEnabled(ctx) }
    val batteryOk = remember(resumeTick, localTick) { PermissionStatus.batteryUnrestricted(ctx) }
    val notifOk = remember(resumeTick, localTick) { PermissionStatus.canPostNotifications(ctx) }
    val authProblem = remember(resumeTick, pending, hasToken) { app.settings.authProblem }
    // 권한만 보고는 알림 읽는 기능이 실제로 붙어 있는지 모른다. 연결 여부와 마지막으로 읽은 시각을 따로 보여준다.
    val connected = remember(resumeTick) { CaptureService.current != null }
    val lastCaptured = remember(resumeTick, pending) { app.settings.lastCapturedAt }
    LaunchedEffect(resumeTick, listenerOn) { if (listenerOn) CaptureService.ensureBound(ctx) }
    val notifLauncher = rememberLauncherForActivityResult(ActivityResultContracts.RequestPermission()) { localTick++ }
    val login = { if (!AppLink.start(ctx)) Toast.makeText(ctx, "아래 고급에서 서버 주소를 먼저 넣어 주세요", Toast.LENGTH_LONG).show() }

    @Composable
    fun Check(title: String, ok: Boolean, sub: String, action: (() -> Unit)? = null) {
        FlatRow(title, sub = sub, subColor = if (ok) m.muted else m.over, leading = { Dot(if (ok) m.faint else m.over) }, onClick = if (ok) null else action)
    }

    Column(modifier.fillMaxSize().verticalScroll(rememberScrollState()).padding(bottom = 32.dp)) {
        Text(
            "결제가 가계부에 안 적힐 때 위에서부터 확인해 주세요. 살구색 줄을 누르면 고치는 곳으로 가요.",
            Modifier.padding(horizontal = Gutter, vertical = 8.dp), fontSize = 15.sp, color = m.muted, lineHeight = 22.sp,
        )
        SectionLabel("확인할 것")
        Check("알림 읽기 권한", listenerOn, if (listenerOn) "켜져 있어요" else "꺼져 있어요. 눌러서 MyHome을 켜 주세요") { PermissionStatus.openListenerSettings(ctx) }
        if (listenerOn) {
            val since = app.settings.listenerConnectedAt
            Check(
                "알림 읽는 기능",
                connected,
                if (connected) (if (since > 0) "${formatTime(since)}부터 작동 중이에요" else "작동 중이에요")
                else "멈춰 있어요. 다시 연결을 요청했어요. 계속 멈춰 있으면 눌러서 권한을 껐다 켜 주세요",
            ) { PermissionStatus.openListenerSettings(ctx) }
        }
        Check("마지막으로 읽은 결제 알림", lastCaptured > 0, if (lastCaptured > 0) formatTime(lastCaptured, "M월 d일 HH:mm") else "아직 없어요. 결제하면 여기에 시각이 나와요")
        Check("배터리 절전 예외", batteryOk, if (batteryOk) "되어 있어요" else "안 되어 있어요. 절전 때문에 알림을 놓칠 수 있어요. 눌러서 허용") { PermissionStatus.requestBatteryExemption(ctx) }
        Check(
            "로그인",
            hasToken && authProblem == null,
            when {
                !hasToken -> "아직 안 했어요. 눌러서 로그인"
                authProblem != null -> "로그인이 풀렸어요. 눌러서 다시 로그인"
                else -> "연결돼 있어요"
            },
            login,
        )
        Check("알림 읽을 앱", settings.selectedPackages.isNotEmpty(), if (settings.selectedPackages.isEmpty()) "아직 안 골랐어요. 눌러서 고르기" else "${settings.selectedPackages.size}개 골랐어요") {
            onNavigate(Screen.APPS)
        }
        Check(
            "아직 못 보낸 알림",
            pending == 0,
            if (pending == 0) "없어요" else "${pending}건 있어요. 인터넷이 되면 자동으로 보내요. 눌러서 지금 보내기",
        ) {
            WorkScheduler.flushNow(ctx)
            Toast.makeText(ctx, "지금 보내요", Toast.LENGTH_SHORT).show()
        }
        if (rejected > 0) {
            Check("서버가 받지 않은 알림", false, "${rejected}건. 지우지 않고 보관해요. 서버를 고친 뒤 눌러서 다시 보내 주세요") {
                scope.launch {
                    val n = withContext(Dispatchers.IO) { app.db.pendingEvents().requeueRejected() }
                    WorkScheduler.flushNow(ctx)
                    Toast.makeText(ctx, "${n}건을 다시 보내요", Toast.LENGTH_SHORT).show()
                }
            }
        }
        if (Build.VERSION.SDK_INT >= 33) {
            Check("앱 알림 표시", notifOk, if (notifOk) "켜져 있어요" else "꺼져 있어요. 예산 알림과 테스트 알림이 안 보여요. 눌러서 켜기") {
                notifLauncher.launch(Manifest.permission.POST_NOTIFICATIONS)
            }
        }

        SectionLabel("직접 해 보기")
        FlatRow("테스트 알림 보내기", sub = "잠깐 뒤 알림이 사라지면 잘 되는 거예요. 가계부에는 안 적혀요", onClick = {
            val ok = Notifications.postTest(ctx)
            Toast.makeText(ctx, if (ok) "테스트 알림을 띄웠어요" else "앱 알림 표시를 먼저 켜 주세요", Toast.LENGTH_LONG).show()
        })
        FlatRow("보낸 기록", sub = "최근 50건, 보냈는지 못 보냈는지", onClick = { onNavigate(Screen.LOGS) })
        FlatRow("삼성 폰 절전 설정 안내", sub = "앱이 잠들지 않게 하는 메뉴 위치", onClick = { onNavigate(Screen.PERMISSIONS) })

        AdvancedSection()
    }
}

/** 고급(비상용): 서버 주소, 기기 토큰 직접 넣기. 평소에는 접혀 있다 */
@Composable
private fun AdvancedSection() {
    val ctx = LocalContext.current
    val app = MyHomeApp.instance
    val scope = rememberCoroutineScope()
    val m = LocalModo.current
    val settings by app.settings.state.collectAsState()
    val hasToken by app.tokenStore.hasToken.collectAsState()
    var open by rememberSaveable { mutableStateOf(false) }
    var url by remember { mutableStateOf(settings.serverUrl.orEmpty()) }
    var tokenInput by remember { mutableStateOf("") }

    Hairline(Modifier.padding(top = 16.dp))
    FlatRow(if (open) "고급 접기" else "고급", sub = if (open) null else "서버 주소, 기기 토큰 직접 넣기", titleColor = m.muted, chevron = false, onClick = { open = !open })
    if (!open) return
    Column(Modifier.padding(horizontal = Gutter), verticalArrangement = Arrangement.spacedBy(4.dp)) {
        LineField(url, { url = it }, "서버 주소 (https://…)")
    }
    FlatRow("서버 주소 저장", titleColor = m.muted, chevron = false, onClick = {
        val normalized = ServerUrl.normalize(url)
        if (normalized == null) {
            Toast.makeText(ctx, "https로 시작하는 주소를 넣어 주세요", Toast.LENGTH_SHORT).show()
        } else {
            url = normalized
            app.settings.setServerUrl(normalized)
            WorkScheduler.flushNow(ctx)
            Toast.makeText(ctx, "저장했어요", Toast.LENGTH_SHORT).show()
        }
    })
    Column(Modifier.padding(horizontal = Gutter, vertical = 8.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
        Text(if (hasToken) "기기 토큰이 저장돼 있어요(폰 안에서 암호화)." else "저장된 기기 토큰이 없어요.", fontSize = 14.sp, color = m.muted)
        LineField(tokenInput, { tokenInput = it.trim() }, "mhd_로 시작하는 토큰", secret = true)
    }
    FlatRow("토큰 저장", titleColor = if (tokenInput.isNotBlank()) androidx.compose.material3.MaterialTheme.colorScheme.onBackground else m.faint, chevron = false, onClick = {
        if (tokenInput.isBlank()) return@FlatRow
        scope.launch {
            withContext(Dispatchers.IO) { app.tokenStore.save(tokenInput) }
            app.settings.authProblem = null
            tokenInput = ""
            WorkScheduler.flushNow(ctx)
            Toast.makeText(ctx, "토큰을 저장했어요", Toast.LENGTH_SHORT).show()
        }
    })
    if (hasToken) FlatRow("이 폰의 토큰 지우기", sub = "지우면 다시 로그인해야 해요", titleColor = m.over, chevron = false, onClick = { app.tokenStore.clear() })
}
