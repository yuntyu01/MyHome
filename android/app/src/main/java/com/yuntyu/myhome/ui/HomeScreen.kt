package com.yuntyu.myhome.ui

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Button
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import com.yuntyu.myhome.MyHomeApp
import com.yuntyu.myhome.core.Money
import com.yuntyu.myhome.core.WidgetModel
import com.yuntyu.myhome.widget.WidgetRefresher
import kotlinx.coroutines.launch

/** 앱 홈: 이번 달 요약, 확인 필요, 분류별 남은 돈, 최근 거래. */
@Composable
fun HomeScreen(modifier: Modifier, resumeTick: Int, onNavigate: (Screen) -> Unit) {
    val ctx = LocalContext.current
    val app = MyHomeApp.instance
    val state by app.summaryRepo.state.collectAsState()
    val hasToken by app.tokenStore.hasToken.collectAsState()
    val scope = rememberCoroutineScope()
    // 화면에 들어올 때마다 새로 받고 위젯도 맞춘다
    LaunchedEffect(resumeTick, hasToken) { if (hasToken) WidgetRefresher.refresh(ctx) }

    Column(
        modifier.fillMaxSize().verticalScroll(rememberScrollState()).padding(16.dp),
        verticalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        if (!hasToken || app.settings.serverUrl == null) {
            SectionCard("시작하기") {
                Text("서버 주소를 저장하고 카카오로 로그인(또는 토큰 입력)하세요.")
                Button(onClick = { onNavigate(Screen.SETTINGS) }) { Text("설정 열기") }
                OutlinedButton(onClick = { onNavigate(Screen.STATUS) }) { Text("수집 상태·권한") }
            }
        }
        val s = state.summary
        if (s != null) {
            SectionCard("${s.period.label} 남은 돈") {
                Text(
                    Money.won(s.totals.remaining),
                    style = MaterialTheme.typography.headlineMedium,
                    fontWeight = FontWeight.Bold,
                    color = if (s.totals.remaining < 0) MaterialTheme.colorScheme.error else MaterialTheme.colorScheme.onSurface,
                )
                Text(
                    "예산 ${Money.grouped(s.totals.budget)} · 지출 ${Money.grouped(s.totals.spent)}" +
                        (if (s.totals.settled > 0) " · 정산 +${Money.grouped(s.totals.settled)}" else ""),
                    style = MaterialTheme.typography.bodySmall,
                )
                Text("수입 ${Money.won(s.totals.income)} · 투자·저축 ${Money.won(s.totals.investment)}", style = MaterialTheme.typography.bodySmall)
            }
            if (s.needsReview > 0) {
                Button(onClick = { onNavigate(Screen.REVIEW) }, modifier = Modifier.fillMaxWidth()) {
                    Text("확인 필요 ${s.needsReview}건 분류하기")
                }
            }
            SectionCard("분류별 남은 돈") {
                val cats = s.categories.filter { it.budget > 0 || it.spent != 0L }
                if (cats.isEmpty()) Text("웹에서 예산을 설정하면 여기에 보여요.", style = MaterialTheme.typography.bodySmall)
                for (c in cats) {
                    Column {
                        Row(Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
                            Text(c.name, Modifier.weight(1f))
                            Text(
                                Money.won(c.remaining),
                                fontWeight = FontWeight.SemiBold,
                                color = budgetColor(if (c.budget > 0) c.ratio else 0.0),
                            )
                        }
                        if (c.budget > 0) {
                            LinearProgressIndicator(
                                progress = { c.ratio.toFloat().coerceIn(0f, 1f) },
                                modifier = Modifier.fillMaxWidth().padding(top = 4.dp),
                                color = budgetColor(c.ratio),
                            )
                        }
                        Spacer(Modifier.height(6.dp))
                    }
                }
            }
            SectionCard("최근 거래") {
                for (t in s.recent) {
                    Row(Modifier.fillMaxWidth()) {
                        Column(Modifier.weight(1f)) {
                            Text(t.merchant.ifBlank { "(이름 없음)" }, maxLines = 1)
                            Text(
                                WidgetModel.shortTime(t.occurredAt) + " · " + t.category.ifBlank { "확인 필요" },
                                style = MaterialTheme.typography.bodySmall,
                                color = MaterialTheme.colorScheme.onSurfaceVariant,
                            )
                        }
                        Text(
                            (if (t.flow == "in") "+" else "") + Money.won(t.amount),
                            color = if (t.flow == "in") MaterialTheme.colorScheme.primary else MaterialTheme.colorScheme.onSurface,
                        )
                    }
                    Spacer(Modifier.height(6.dp))
                }
                OutlinedButton(onClick = { onNavigate(Screen.TRANSACTIONS) }) { Text("이번 달 거래 전체") }
            }
            Text(
                "갱신: " + formatTime(state.updatedAt, "MM/dd HH:mm") + (state.error?.let { " · 최근 갱신 실패: $it" } ?: ""),
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
        } else if (hasToken) {
            Text(state.error ?: "불러오는 중…")
        }
        Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            OutlinedButton(onClick = { scope.launch { WidgetRefresher.refresh(ctx) } }) { Text("새로고침") }
            OutlinedButton(onClick = { onNavigate(Screen.STATUS) }) { Text("수집 상태") }
            OutlinedButton(onClick = { onNavigate(Screen.SETTINGS) }) { Text("설정") }
        }
    }
}
