package com.yuntyu.myhome.ui

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Button
import androidx.compose.material3.FilledTonalButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.TextButton
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import com.yuntyu.myhome.MyHomeApp
import com.yuntyu.myhome.core.Money
import com.yuntyu.myhome.core.WidgetModel
import com.yuntyu.myhome.core.api.CategoryBudget
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
                Text("설정에서 '브라우저에서 연결하기'를 눌러 이메일 코드로 한 번 로그인하세요.")
                Button(onClick = { onNavigate(Screen.SETTINGS) }) { Text("설정 열기") }
                FilledTonalButton(onClick = { onNavigate(Screen.STATUS) }) { Text("수집 상태·권한") }
            }
        }
        val s = state.summary
        if (s != null) {
            val m = LocalModo.current
            // 큰 숫자: 이번 달 남은 돈
            Column(Modifier.padding(horizontal = 6.dp, vertical = 4.dp)) {
                Text("${WidgetModel.monthName(s.period.label)} 남은 돈", style = MaterialTheme.typography.titleSmall, color = m.muted)
                Text(
                    Money.won(s.totals.remaining),
                    style = MaterialTheme.typography.headlineLarge.copy(fontSize = 36.sp),
                    color = if (s.totals.remaining < 0) m.over else MaterialTheme.colorScheme.onBackground,
                )
                Text(
                    "예산 ${Money.grouped(s.totals.budget)} · 지출 ${Money.grouped(s.totals.spent)}" +
                        (if (s.totals.settled > 0) " · 정산 +${Money.grouped(s.totals.settled)}" else ""),
                    style = MaterialTheme.typography.bodySmall,
                    color = m.muted,
                )
                if (s.totals.budget > 0) {
                    val r = (s.totals.spent - s.totals.settled).toDouble() / s.totals.budget
                    ModoBar(r, budgetColor(r), Modifier.padding(top = 12.dp))
                }
            }
            if (s.needsReview > 0) {
                Row(
                    Modifier.fillMaxWidth().clip(RoundedCornerShape(16.dp)).background(m.pill)
                        .clickable { onNavigate(Screen.REVIEW) }.padding(horizontal = 18.dp, vertical = 14.dp),
                    verticalAlignment = Alignment.CenterVertically,
                ) {
                    Text("확인이 필요한 거래 ${s.needsReview}건", Modifier.weight(1f), fontWeight = FontWeight.SemiBold)
                    Text("›", color = m.muted)
                }
            }
            if (s.failedEvents > 0) {
                // 서버가 읽지 못한 알림: 거래로 안 잡혔을 수 있다. 원문은 웹에서만 본다.
                Row(
                    Modifier.fillMaxWidth().clip(RoundedCornerShape(16.dp)).background(m.pill)
                        .padding(horizontal = 18.dp, vertical = 14.dp),
                    verticalAlignment = Alignment.CenterVertically,
                ) {
                    Text("읽지 못한 알림 ${s.failedEvents}건", Modifier.weight(1f), fontWeight = FontWeight.SemiBold, color = m.warn)
                    Text("웹 관리에서 확인", style = MaterialTheme.typography.bodySmall, color = m.muted)
                }
            }
            Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                StatTile("지출", tileAmount(s.totals.spent), MaterialTheme.colorScheme.onSurface, Modifier.weight(1f))
                StatTile("수입", tileAmount(s.totals.income), m.income, Modifier.weight(1f))
                StatTile("투자·저축", tileAmount(s.totals.investment), MaterialTheme.colorScheme.onSurface, Modifier.weight(1f))
            }
            Text("분류별 남은 돈", style = MaterialTheme.typography.labelLarge, color = m.muted, modifier = Modifier.padding(start = 6.dp, top = 8.dp))
            val cats = WidgetModel.homeCategories(s)
            if (cats.top.isEmpty() && cats.rest.isEmpty()) Text("웹에서 예산을 설정하면 여기에 보여요.", style = MaterialTheme.typography.bodySmall, color = m.muted, modifier = Modifier.padding(horizontal = 6.dp))
            for (c in cats.top) BudgetRow(c)
            if (cats.rest.isNotEmpty()) {
                // 펼친 분류가 없으면(기간 시작 직후) 처음부터 펼쳐 둔다
                var showRest by rememberSaveable { mutableStateOf(false) }
                val open = showRest || cats.top.isEmpty()
                Row(
                    Modifier.fillMaxWidth().clip(RoundedCornerShape(16.dp)).background(m.card)
                        .clickable { showRest = !open }.padding(horizontal = 18.dp, vertical = 14.dp),
                    verticalAlignment = Alignment.CenterVertically,
                ) {
                    Text("나머지 분류 ${cats.rest.size}개", Modifier.weight(1f), color = m.muted)
                    Text(if (open) "⌄" else "›", color = m.muted)
                }
                if (open) for (c in cats.rest) BudgetRow(c)
            }
            SectionCard("최근 거래") {
                for (t in s.recent.take(5)) {
                    Row(Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
                        Column(Modifier.weight(1f)) {
                            Text(t.merchant.ifBlank { "(이름 없음)" }, maxLines = 1)
                            Text(
                                WidgetModel.shortTime(t.occurredAt) + " · " + t.category.ifBlank { "확인 필요" },
                                style = MaterialTheme.typography.bodySmall,
                                color = if (t.category.isBlank()) m.warn else m.muted,
                            )
                        }
                        Text(
                            (if (t.flow == "in") "+" else "") + Money.won(t.amount),
                            fontWeight = FontWeight.SemiBold,
                            color = if (t.flow == "in") m.income else MaterialTheme.colorScheme.onSurface,
                        )
                    }
                }
                TextButton(onClick = { onNavigate(Screen.TRANSACTIONS) }) { Text("이번 달 거래 전체") }
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
            FilledTonalButton(onClick = { scope.launch { WidgetRefresher.refresh(ctx) } }) { Text("새로고침") }
            FilledTonalButton(onClick = { onNavigate(Screen.STATUS) }) { Text("수집 상태") }
        }
    }
}

/** 좁은 칸 금액: 100만 원 미만은 그대로, 그 이상은 "320만"처럼 줄인다(웹과 같은 규칙). */
private fun tileAmount(n: Long): String = if (kotlin.math.abs(n) < 1_000_000) Money.won(n) else Money.compact(n)

/** 작은 숫자 칸(지출·수입·투자). */
@Composable
private fun StatTile(label: String, value: String, color: Color, modifier: Modifier = Modifier) {
    val m = LocalModo.current
    Column(modifier.clip(RoundedCornerShape(16.dp)).background(m.card).padding(horizontal = 14.dp, vertical = 12.dp)) {
        Text(label, style = MaterialTheme.typography.bodySmall, color = m.muted)
        Text(value, style = MaterialTheme.typography.titleMedium, color = color, maxLines = 1)
    }
}

/** 분류 한 줄: 이름, 남은 돈, 막대, 지출·예산. */
@Composable
private fun BudgetRow(c: CategoryBudget) {
    val m = LocalModo.current
    Column(Modifier.fillMaxWidth().clip(RoundedCornerShape(16.dp)).background(m.card).padding(horizontal = 18.dp, vertical = 14.dp)) {
        Row(Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
            Text(c.name, Modifier.weight(1f))
            Text(Money.won(c.remaining), fontWeight = FontWeight.Bold, color = if (c.remaining < 0) m.over else MaterialTheme.colorScheme.onSurface)
        }
        if (c.budget > 0) ModoBar(c.ratio, budgetColor(c.ratio), Modifier.padding(top = 10.dp))
        Text(
            "지출 ${Money.grouped(c.spent)}" + (if (c.budget > 0) " · 예산 ${Money.grouped(c.budget)}" else " · 예산 없음"),
            style = MaterialTheme.typography.bodySmall,
            color = m.muted,
            modifier = Modifier.padding(top = 4.dp),
        )
    }
}
