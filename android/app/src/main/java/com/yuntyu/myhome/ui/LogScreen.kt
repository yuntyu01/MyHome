package com.yuntyu.myhome.ui

import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp
import com.yuntyu.myhome.MyHomeApp

/** 최근 50건 전송 로그. 본문은 저장하지 않고 패키지·결과만 보여준다. */
@Composable
fun LogScreen(modifier: Modifier) {
    val logs by MyHomeApp.instance.db.sendLog().recent().collectAsState(initial = emptyList())
    LazyColumn(modifier.fillMaxSize()) {
        if (logs.isEmpty()) item { Text("아직 전송 기록이 없어요.", Modifier.padding(16.dp)) }
        items(logs, key = { it.id }) { l ->
            Column(Modifier.padding(horizontal = 16.dp, vertical = 8.dp)) {
                Text(
                    (if (l.success) "✅ 성공" else "❌ 실패") + " · " + formatTime(l.at, "MM/dd HH:mm:ss"),
                    style = MaterialTheme.typography.bodyMedium,
                    color = if (l.success) MaterialTheme.colorScheme.primary else MaterialTheme.colorScheme.error,
                )
                Text(l.packageName, style = MaterialTheme.typography.bodySmall)
                Text(l.message, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
            }
            HorizontalDivider()
        }
    }
}
