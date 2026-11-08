package com.yuntyu.myhome.ui

import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.remember
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp
import com.yuntyu.myhome.MyHomeApp

/** 최근 50건 보낸 기록. 본문은 저장하지 않고 패키지·결과만 보여준다. */
@Composable
fun LogScreen(modifier: Modifier) {
    val logs by remember { MyHomeApp.instance.db.sendLog().recent() }.collectAsState(initial = emptyList())
    LazyColumn(modifier.fillMaxSize()) {
        if (logs.isEmpty()) item { EmptyNote("아직 보낸 기록이 없어요", "결제 알림을 읽으면 여기에 보냈는지 남아요") }
        items(logs, key = { it.id }) { l ->
            Column(Modifier.padding(horizontal = Gutter, vertical = 10.dp)) {
                Text(
                    (if (l.success) "보냄" else "못 보냄") + " · " + formatTime(l.at, "MM/dd HH:mm:ss"),
                    style = MaterialTheme.typography.bodyMedium,
                    color = if (l.success) MaterialTheme.colorScheme.onBackground else MaterialTheme.colorScheme.error,
                )
                Text(l.packageName, style = MaterialTheme.typography.bodySmall)
                Text(l.message, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
            }
            Hairline(Modifier.padding(start = Gutter))
        }
    }
}
