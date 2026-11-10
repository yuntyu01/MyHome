package com.yuntyu.myhome.ui

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.pulltorefresh.PullToRefreshBox
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.layout.onGloballyPositioned
import androidx.compose.ui.layout.positionInParent
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import com.yuntyu.myhome.MyHomeApp
import com.yuntyu.myhome.core.Money
import com.yuntyu.myhome.core.api.ApiResult
import com.yuntyu.myhome.core.api.InvestRow
import com.yuntyu.myhome.core.api.InvestStatus
import com.yuntyu.myhome.core.api.InvestValue
import com.yuntyu.myhome.core.api.describe
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext

/*
 * 투자 탭: 자동매매(서버 myhome invest) 상태를 읽기만 한다. 웹 /invest와 같은 값이다.
 * 가계부와 섞지 않으려고 탭을 따로 뒀다. 주문·수정 버튼은 두지 않는다(주문은 서버의 예약 작업만 낸다).
 * 평가액·가격 같은 시장 숫자는 첫 화면에 두지 않고 한 번 더 들어간 평가 화면에만 둔다(TQ 규칙, 웹과 같음).
 * 아래로 당기면 새로 받는다. 보유·이번 달·원금 대조·환율 줄은 누르면 평가 화면으로 간다.
 */

/** 누르면 평가 화면으로 가는 줄(원금·환율·매수 숫자가 거기 있다) */
private val VALUE_ROWS = setOf("원금 대조", "환율")

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun InvestScreen(modifier: Modifier, resumeTick: Int, onNavigate: (Screen) -> Unit) {
    var data by remember { mutableStateOf<InvestStatus?>(null) }
    var error by remember { mutableStateOf<String?>(null) }
    var refreshing by remember { mutableStateOf(false) }
    val scope = rememberCoroutineScope()
    suspend fun load() {
        when (val r = withContext(Dispatchers.IO) { MyHomeApp.instance.api.invest() }) {
            is ApiResult.Ok -> { data = r.value; error = null }
            else -> error = r.describe()
        }
    }
    LaunchedEffect(resumeTick) { load() }
    val m = LocalModo.current
    val toValue = { onNavigate(Screen.INVEST_VALUE) }
    PullToRefreshBox(
        isRefreshing = refreshing,
        onRefresh = { scope.launch { refreshing = true; load(); refreshing = false } },
        modifier = modifier.fillMaxSize(),
    ) {
    Column(
        Modifier.fillMaxSize().verticalScroll(rememberScrollState()).padding(16.dp),
        verticalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        val s = data
        when {
            s == null -> Text(error?.let { "불러오지 못했어요: $it" } ?: "불러오는 중…", color = m.muted)
            !s.migrated -> NotMigrated()
            else -> {
                if (error != null) Text("새로 불러오지 못했어요: $error", style = MaterialTheme.typography.bodySmall, color = m.over)
                if (s.priceStale) StaleBanner(s.lastPriceLabel)
                Hero(
                    label = "보유",
                    value = if (s.shares > 0) "${s.symbol} ${s.shares}주" else "보유 없음",
                    sub = if (s.problems > 0) "확인할 문제 ${s.problems}건" else "문제 없음 · 눌러서 평가 보기",
                    subColor = if (s.problems > 0) m.over else m.muted,
                    onClick = toValue,
                )
                s.month?.let { RowGroup("이번 달", listOf(it)) { toValue() } }
                RowGroup("일정", s.schedule.orEmpty())
                RowGroup("점검", s.checks.orEmpty()) { r -> if (r.title in VALUE_ROWS) toValue() }
                SectionCard("평가") {
                    Row(
                        Modifier.fillMaxWidth().tap(onClickLabel = "평가 보기") { onNavigate(Screen.INVEST_VALUE) },
                        verticalAlignment = Alignment.CenterVertically,
                    ) {
                        Column(Modifier.weight(1f)) {
                            Text("평가 보기")
                            Text("세후 평가액 · 원금 · 원금 내역", style = MaterialTheme.typography.bodySmall, color = m.muted)
                        }
                        Text("›", color = m.muted)
                    }
                }
                SectionCard("전략") {
                    Row(
                        Modifier.fillMaxWidth().tap(onClickLabel = "백테스트 결과 보기") { onNavigate(Screen.INVEST_BACKTEST) },
                        verticalAlignment = Alignment.CenterVertically,
                    ) {
                        Column(Modifier.weight(1f)) {
                            Text("백테스트 결과")
                            Text("숫자가 어땠는가 · TQQQ·QLD 보유와 비교", style = MaterialTheme.typography.bodySmall, color = m.muted)
                        }
                        Text("›", color = m.muted)
                    }
                }
                Note("읽기만 하는 화면이에요. 날짜는 뉴욕 기준이고, 실행 시각은 한국 시간이에요. 아래로 당기면 새로 받아요.")
            }
        }
    }
    }
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun InvestValueScreen(modifier: Modifier) {
    var data by remember { mutableStateOf<InvestValue?>(null) }
    var error by remember { mutableStateOf<String?>(null) }
    var refreshing by remember { mutableStateOf(false) }
    val scope = rememberCoroutineScope()
    suspend fun load() {
        when (val r = withContext(Dispatchers.IO) { MyHomeApp.instance.api.investValue() }) {
            is ApiResult.Ok -> { data = r.value; error = null }
            else -> error = r.describe()
        }
    }
    LaunchedEffect(Unit) { load() }
    val m = LocalModo.current
    val scroll = rememberScrollState()
    // 원금 내역 카드의 위치(타일을 누르면 거기로 내린다)
    var ledgerY by remember { mutableIntStateOf(0) }
    PullToRefreshBox(
        isRefreshing = refreshing,
        onRefresh = { scope.launch { refreshing = true; load(); refreshing = false } },
        modifier = modifier.fillMaxSize(),
    ) {
    Column(
        Modifier.fillMaxSize().verticalScroll(scroll).padding(16.dp),
        verticalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        val v = data
        when {
            v == null -> Text(error?.let { "불러오지 못했어요: $it" } ?: "불러오는 중…", color = m.muted)
            !v.migrated -> NotMigrated()
            else -> {
                if (v.priceStale) StaleBanner(v.lastPriceLabel)
                Hero(
                    label = (if (v.shares > 0) "${v.symbol} ${v.shares}주" else "보유 없음") + " · 세후 평가액",
                    value = if (v.canValue) Money.won(v.netKrw) else "—",
                    sub = if (v.canValue) v.basis else v.why,
                    subColor = if (v.canValue) m.muted else m.over,
                    valueColor = if (v.canValue && v.netKrw < 0) m.over else null,
                )
                Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    Tile("누적 원금", Money.won(v.contributedKrw), "눌러서 내역 보기", Modifier.weight(1f), onClick = { scope.launch { scroll.animateScrollTo(ledgerY) } })
                    Tile(
                        "세후 손익",
                        if (v.canValue) (if (v.gainKrw > 0) "+" else "") + Money.won(v.gainKrw) else "—",
                        v.gainPct.ifBlank { null }.takeIf { v.canValue },
                        Modifier.weight(1f),
                        if (v.canValue && v.gainKrw < 0) m.over else null,
                    )
                }
                Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    Tile("미납 세금", Money.won(v.taxKrw), "평가액에서 뺐어요", Modifier.weight(1f))
                    Tile("현금", v.cashUsd, "다음 달로 이월", Modifier.weight(1f))
                }
                SectionCard("원금 내역", Modifier.onGloballyPositioned { ledgerY = it.positionInParent().y.toInt() }) {
                    val ledger = v.ledger.orEmpty()
                    if (ledger.isEmpty()) Text("원금 내역이 없어요.", color = m.muted)
                    for (l in ledger) {
                        Row(Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
                            Column(Modifier.weight(1f)) {
                                Text("${l.on} · ${l.source}", maxLines = 1)
                                Text(
                                    listOf("${l.ym} 적용", l.shares, l.note).filter { it.isNotBlank() }.joinToString(" · "),
                                    style = MaterialTheme.typography.bodySmall,
                                    color = m.muted,
                                )
                            }
                            Text(Money.won(l.amount), fontWeight = FontWeight.SemiBold)
                        }
                    }
                    Text(
                        "합계 ${ledger.size}건 · ${Money.won(v.ledgerSum)} · " +
                            if (v.ledgerBad) "누적 원금과 ${Money.grouped(v.ledgerGap)}원 어긋남" else "누적 원금과 일치",
                        style = MaterialTheme.typography.bodySmall,
                        color = if (v.ledgerBad) m.over else m.muted,
                    )
                }
                Note("날짜는 뉴욕 기준이고, 금액은 원 단위로 반올림해서 보여줘요.")
            }
        }
    }
    }
}

@Composable
private fun NotMigrated() {
    SectionCard("투자") {
        Text("아직 옮기지 않았어요.")
        Text(
            "서버에서 myhome invest import-tq로 TQ 상태를 옮기면 여기에 보여요.",
            style = MaterialTheme.typography.bodySmall,
            color = LocalModo.current.muted,
        )
    }
}

@Composable
private fun StaleBanner(lastLabel: String) {
    val m = LocalModo.current
    Text(
        "가격 갱신이 멈췄어요 (마지막 ${lastLabel.ifBlank { "기록 없음" }})",
        Modifier.fillMaxWidth().clip(RoundedCornerShape(16.dp)).background(m.pill).padding(horizontal = 18.dp, vertical = 14.dp),
        color = m.over,
        fontWeight = FontWeight.SemiBold,
    )
}

/** 큰 숫자 한 덩어리(홈의 "남은 돈"과 같은 모양). onClick이 있으면 눌러서 간다 */
@Composable
private fun Hero(label: String, value: String, sub: String, subColor: Color, valueColor: Color? = null, onClick: (() -> Unit)? = null) {
    val m = LocalModo.current
    Column(
        Modifier.fillMaxWidth().clip(RoundedCornerShape(16.dp))
            .then(if (onClick != null) Modifier.tap(onClick = onClick) else Modifier)
            .padding(horizontal = 6.dp, vertical = 4.dp),
    ) {
        Text(label, style = MaterialTheme.typography.titleSmall, color = m.muted)
        Text(
            value,
            style = MaterialTheme.typography.headlineLarge.copy(fontSize = 36.sp),
            color = valueColor ?: MaterialTheme.colorScheme.onBackground,
        )
        if (sub.isNotBlank()) Text(sub, style = MaterialTheme.typography.bodySmall, color = subColor)
    }
}

/** 서버가 만든 문구 줄 묶음. bad인 값만 경고색(살구). onClick이 그 줄에서 갈 곳이 있으면 꺾쇠를 단다 */
@Composable
private fun RowGroup(title: String, rows: List<InvestRow>, onClick: ((InvestRow) -> Unit)? = null) {
    if (rows.isEmpty()) return
    val m = LocalModo.current
    SectionCard(title) {
        for (r in rows) {
            val go = onClick != null && (title == "이번 달" || r.title in VALUE_ROWS)
            Row(
                Modifier.fillMaxWidth().then(if (go) Modifier.tap { onClick?.invoke(r) } else Modifier),
                verticalAlignment = Alignment.CenterVertically,
                horizontalArrangement = Arrangement.spacedBy(12.dp),
            ) {
                Column(Modifier.weight(1f)) {
                    Text(r.title)
                    if (r.meta.isNotBlank()) Text(r.meta, style = MaterialTheme.typography.bodySmall, color = m.muted)
                }
                Text(
                    r.value,
                    fontWeight = FontWeight.SemiBold,
                    color = if (r.bad) m.over else MaterialTheme.colorScheme.onSurface,
                    textAlign = TextAlign.End,
                )
                if (go) Text("›", color = m.muted)
            }
        }
    }
}

@Composable
private fun Tile(label: String, value: String, sub: String?, modifier: Modifier = Modifier, valueColor: Color? = null, onClick: (() -> Unit)? = null) {
    val m = LocalModo.current
    Column(
        modifier.clip(RoundedCornerShape(16.dp)).background(m.card)
            .then(if (onClick != null) Modifier.tap(onClick = onClick) else Modifier)
            .padding(horizontal = 14.dp, vertical = 12.dp),
    ) {
        Text(label, style = MaterialTheme.typography.bodySmall, color = m.muted)
        Text(value, style = MaterialTheme.typography.titleMedium, color = valueColor ?: MaterialTheme.colorScheme.onSurface, maxLines = 1)
        if (sub != null) Text(sub, style = MaterialTheme.typography.bodySmall, color = m.muted, maxLines = 1)
    }
}

@Composable
private fun Note(text: String) {
    Text(
        text,
        style = MaterialTheme.typography.bodySmall,
        color = LocalModo.current.muted,
        modifier = Modifier.padding(horizontal = 6.dp),
    )
}
