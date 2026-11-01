package com.yuntyu.myhome.ui

import android.Manifest
import android.os.Build
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
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
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.unit.dp

@Composable
fun PermissionScreen(modifier: Modifier, resumeTick: Int) {
    val ctx = LocalContext.current
    var localTick by remember { mutableIntStateOf(0) }
    val listenerOn = remember(resumeTick, localTick) { PermissionStatus.listenerEnabled(ctx) }
    val batteryOk = remember(resumeTick, localTick) { PermissionStatus.batteryUnrestricted(ctx) }
    val notifOk = remember(resumeTick, localTick) { PermissionStatus.canPostNotifications(ctx) }
    val notifLauncher = rememberLauncherForActivityResult(ActivityResultContracts.RequestPermission()) { localTick++ }

    Column(
        modifier.fillMaxSize().verticalScroll(rememberScrollState()).padding(16.dp),
        verticalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        SectionCard("1. 알림 접근 권한 (필수)") {
            StatusRow("알림 접근", listenerOn)
            Text("결제 앱 알림을 읽으려면 필요해요. 목록에서 'MyHome 결제 알림 수집'을 켜 주세요.")
            Button(onClick = { PermissionStatus.openListenerSettings(ctx) }) { Text("알림 접근 설정 열기") }
            Text(
                "Android 13 이상에서 '제한된 설정' 안내가 뜨면: 설정 > 애플리케이션 > MyHome > 오른쪽 위 ⋮ > '제한된 설정 허용' 후 다시 시도하세요.",
                style = MaterialTheme.typography.bodySmall,
            )
        }

        SectionCard("2. 배터리 최적화 예외 (강력 권장)") {
            StatusRow("배터리 최적화 제외", batteryOk)
            Text("절전 때문에 수집기나 전송이 멈추지 않게 해요.")
            Button(onClick = { PermissionStatus.requestBatteryExemption(ctx) }) { Text("배터리 최적화 예외 요청") }
        }

        SectionCard("삼성 갤럭시 추가 설정") {
            Text(
                """
                One UI 버전에 따라 메뉴 이름이 조금 다를 수 있어요.
                • 설정 > 애플리케이션 > MyHome > 배터리 > '제한 없음'
                • 설정 > 배터리 > 백그라운드 사용 한도 > '사용하지 않는 앱을 절전 상태로 전환' 끄기
                • 같은 화면의 '절전 예외 앱'에 MyHome 추가
                • 최근 앱 화면에서 MyHome 아이콘 길게 눌러 '앱 잠금(이 앱 잠그기)'
                """.trimIndent(),
                style = MaterialTheme.typography.bodyMedium,
            )
            FilledTonalButton(onClick = { PermissionStatus.openAppDetails(ctx) }) { Text("앱 정보 열기") }
        }

        SectionCard("3. 앱 알림 (선택)") {
            StatusRow("알림 표시", notifOk)
            Text("테스트 알림과 예산 알림(기본 무음)에 써요.")
            if (!notifOk && Build.VERSION.SDK_INT >= 33) {
                Button(onClick = { notifLauncher.launch(Manifest.permission.POST_NOTIFICATIONS) }) { Text("알림 권한 요청") }
            }
        }

        SectionCard("원본 앱 알림 무음") {
            Text(
                "전송에 성공하면 이 앱이 원본 알림을 지워요. 소리·진동이 거슬리면 KB Pay, 토스 등 원본 앱의 알림 채널을 '무음'으로 바꾸세요. " +
                    "알림을 '끄면' 수집도 안 되니 꼭 '무음'으로 설정하세요.",
            )
        }
    }
}
