package com.yuntyu.myhome.ui

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
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
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import com.yuntyu.myhome.MyHomeApp
import com.yuntyu.myhome.data.DismissedNotice

/** 폰에서 지운 결제 알림(사용자 요청, 2026-11-16). 지운 순간 떠 있던 앱·제목·내용, 새것부터. 이 폰에만 있다 */
@Composable
fun DismissedScreen(modifier: Modifier) {
    val list by remember { MyHomeApp.instance.db.dismissedNotices().recent() }.collectAsState(initial = null)
    DismissedList(modifier, list)
}

@Composable
internal fun DismissedList(modifier: Modifier, list: List<DismissedNotice>?) {
    val m = LocalModo.current
    LazyColumn(modifier.fillMaxSize()) {
        when {
            list == null -> item { EmptyNote("불러오는 중이에요") }
            list.isEmpty() -> item { EmptyNote("아직 지운 알림이 없어요", "결제 알림을 치우면 여기에 남아요") }
            else -> items(list, key = { it.id }) { n ->
                Column(Modifier.fillMaxWidth().padding(horizontal = Gutter, vertical = 12.dp), verticalArrangement = Arrangement.spacedBy(3.dp)) {
                    Text("${n.appLabel} · ${formatTime(n.postedAt)}", fontSize = 13.sp, color = m.muted)
                    if (n.title.isNotBlank()) Text(n.title, fontSize = 16.sp, fontWeight = FontWeight.SemiBold, color = MaterialTheme.colorScheme.onBackground)
                    if (n.text.isNotBlank()) Text(n.text, fontSize = 14.sp, lineHeight = 20.sp, color = MaterialTheme.colorScheme.onBackground)
                }
                Hairline(Modifier.padding(start = Gutter))
            }
        }
    }
}
