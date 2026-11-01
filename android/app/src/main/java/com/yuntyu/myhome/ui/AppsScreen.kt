package com.yuntyu.myhome.ui

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material3.Checkbox
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.remember
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp
import com.yuntyu.myhome.MyHomeApp

/**
 * 최근 알림을 보낸 앱 목록. 실제 패키지명을 보여주고 체크한 앱만 수집한다.
 * 패키지명은 코드에 박지 않는다(앱 업데이트·지역에 따라 다를 수 있어서).
 */
@Composable
fun AppsScreen(modifier: Modifier) {
    val app = MyHomeApp.instance
    val apps by remember { app.db.recentApps().all() }.collectAsState(initial = emptyList())
    val settings by app.settings.state.collectAsState()
    // 선택했지만 아직 알림이 안 온 앱도 보이게 합친다.
    val knownPkgs = apps.map { it.packageName }.toSet()
    val selectedOnly = settings.selectedPackages.filter { it !in knownPkgs }

    LazyColumn(modifier.fillMaxSize()) {
        item {
            Text(
                "결제 알림이 오는 앱(KB Pay, 토스, 카카오페이, 쿠팡 등)을 체크하세요. 목록이 비어 있으면 알림 접근 권한을 켠 뒤 해당 앱에서 알림이 한 번 와야 보여요.",
                modifier = Modifier.padding(16.dp),
                style = MaterialTheme.typography.bodyMedium,
            )
        }
        items(selectedOnly) { pkg ->
            AppRow(label = pkg, pkg = pkg, sub = "선택됨 · 아직 받은 알림 없음", checked = true) {
                app.settings.setPackageSelected(pkg, it)
            }
        }
        items(apps, key = { it.packageName }) { a ->
            AppRow(
                label = a.appLabel,
                pkg = a.packageName,
                sub = "${formatTime(a.lastSeenAt)} · ${a.count}건 · ${a.lastTitle}",
                checked = a.packageName in settings.selectedPackages,
            ) { app.settings.setPackageSelected(a.packageName, it) }
        }
    }
}

@Composable
private fun AppRow(label: String, pkg: String, sub: String, checked: Boolean, onChange: (Boolean) -> Unit) {
    Row(
        Modifier.fillMaxWidth().clickable { onChange(!checked) }.padding(horizontal = 8.dp, vertical = 6.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Checkbox(checked = checked, onCheckedChange = onChange)
        Column(Modifier.weight(1f)) {
            Text(label, style = MaterialTheme.typography.bodyLarge)
            Text(pkg, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.primary)
            Text(sub, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant, maxLines = 1)
        }
    }
    HorizontalDivider()
}
