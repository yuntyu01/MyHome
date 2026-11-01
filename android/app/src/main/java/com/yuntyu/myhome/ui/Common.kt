package com.yuntyu.myhome.ui

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ColumnScope
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.unit.dp
import java.text.SimpleDateFormat
import java.util.Date
import java.util.Locale
import java.util.TimeZone

/** modo 카드: 테두리·그림자 없이 바탕색만 다른 둥근 블록(16dp). 제목은 카드 위의 작은 회색 글자. */
@Composable
fun SectionCard(title: String, modifier: Modifier = Modifier, content: @Composable ColumnScope.() -> Unit) {
    val m = LocalModo.current
    Column(modifier.fillMaxWidth()) {
        Text(
            title,
            style = MaterialTheme.typography.labelLarge,
            color = m.muted,
            modifier = Modifier.padding(start = 6.dp, bottom = 8.dp),
        )
        Column(
            Modifier.fillMaxWidth().clip(RoundedCornerShape(16.dp)).background(m.card).padding(horizontal = 18.dp, vertical = 16.dp),
            verticalArrangement = Arrangement.spacedBy(10.dp),
            content = content,
        )
    }
}

/** 상태 한 줄: 왼쪽 점(정상: 강조색, 주의: 경고색) + 제목 + 설명. */
@Composable
fun StatusRow(label: String, ok: Boolean, detail: String? = null) {
    val m = LocalModo.current
    Row(verticalAlignment = Alignment.CenterVertically) {
        Box(Modifier.size(10.dp).clip(CircleShape).background(if (ok) m.accent else m.warn))
        Spacer(Modifier.width(12.dp))
        Column {
            Text(label, style = MaterialTheme.typography.bodyLarge)
            if (detail != null) Text(detail, style = MaterialTheme.typography.bodySmall, color = m.muted)
        }
    }
}

/** 얇은 둥근 진행 막대(modo 슬라이더 트랙처럼 5dp). */
@Composable
fun ModoBar(ratio: Double, color: Color, modifier: Modifier = Modifier) {
    val m = LocalModo.current
    Box(modifier.fillMaxWidth().height(5.dp).clip(RoundedCornerShape(3.dp)).background(m.raised)) {
        Box(
            Modifier.fillMaxWidth(ratio.toFloat().coerceIn(0f, 1f)).height(5.dp).clip(RoundedCornerShape(3.dp)).background(color),
        )
    }
}

private val KST: TimeZone = TimeZone.getTimeZone("Asia/Seoul")

fun formatTime(ms: Long, pattern: String = "MM/dd HH:mm"): String =
    SimpleDateFormat(pattern, Locale.KOREA).apply { timeZone = KST }.format(Date(ms))
