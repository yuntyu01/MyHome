package com.yuntyu.myhome.ui

import android.widget.Toast
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Button
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.FilledTonalButton
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.remember
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.unit.dp
import com.yuntyu.myhome.MyHomeApp
import com.yuntyu.myhome.notify.Notifications
import com.yuntyu.myhome.work.WorkScheduler

@Composable
fun StatusScreen(modifier: Modifier, resumeTick: Int, onNavigate: (Screen) -> Unit) {
    val ctx = LocalContext.current
    val app = MyHomeApp.instance
    val settings by app.settings.state.collectAsState()
    val hasToken by app.tokenStore.hasToken.collectAsState()
    val pending by remember { app.db.pendingEvents().pendingCount() }.collectAsState(initial = 0)
    val rejected by remember { app.db.pendingEvents().rejectedCount() }.collectAsState(initial = 0)
    val listenerOn = remember(resumeTick) { PermissionStatus.listenerEnabled(ctx) }
    val batteryOk = remember(resumeTick) { PermissionStatus.batteryUnrestricted(ctx) }
    val authProblem = remember(resumeTick, pending) { app.settings.authProblem }

    Column(
        modifier.fillMaxSize().verticalScroll(rememberScrollState()).padding(16.dp),
        verticalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        SectionCard("수집 상태") {
            StatusRow("알림 접근 권한", listenerOn, if (listenerOn) null else "권한 안내에서 허용해 주세요")
            StatusRow("배터리 최적화 예외", batteryOk, if (batteryOk) null else "백그라운드에서 수집이 멈출 수 있어요")
            StatusRow("서버 주소", settings.serverUrl != null, settings.serverUrl ?: "설정에서 입력")
            StatusRow("기기 토큰", hasToken && authProblem == null, authProblem ?: if (hasToken) null else "설정에서 로그인하거나 토큰 입력")
            StatusRow("수집 대상 앱", settings.selectedPackages.isNotEmpty(), "${settings.selectedPackages.size}개 선택됨")
            StatusRow("전송 대기", pending == 0, "대기 ${pending}건" + if (rejected > 0) " · 거부 ${rejected}건" else "")
        }

        Button(onClick = { onNavigate(Screen.PERMISSIONS) }, modifier = Modifier.padding(top = 4.dp)) { Text("권한 안내") }
        FilledTonalButton(onClick = { onNavigate(Screen.APPS) }) { Text("최근 알림 · 수집 대상 고르기") }
        FilledTonalButton(onClick = { onNavigate(Screen.SETTINGS) }) { Text("설정") }
        FilledTonalButton(onClick = { onNavigate(Screen.LOGS) }) { Text("전송 로그") }
        FilledTonalButton(onClick = {
            WorkScheduler.flushNow(ctx)
            Toast.makeText(ctx, "전송을 시작했어요", Toast.LENGTH_SHORT).show()
        }) { Text("지금 전송") }
        FilledTonalButton(onClick = {
            val ok = Notifications.postTest(ctx)
            Toast.makeText(ctx, if (ok) "테스트 알림을 띄웠어요. 잠시 뒤 사라지면 성공" else "알림 권한이 필요해요", Toast.LENGTH_LONG).show()
        }) { Text("테스트 알림 보내기") }
        Text(
            "테스트 알림은 서버에 원문으로 저장되지만 거래로 기록되지 않아요.",
            style = MaterialTheme.typography.bodySmall,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
        )
    }
}
