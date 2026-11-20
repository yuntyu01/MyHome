package com.yuntyu.myhome.ui

import androidx.compose.animation.AnimatedVisibility
import androidx.compose.animation.core.LinearEasing
import androidx.compose.animation.core.animateFloat
import androidx.compose.animation.core.infiniteRepeatable
import androidx.compose.animation.core.rememberInfiniteTransition
import androidx.compose.animation.core.tween
import androidx.compose.foundation.Canvas
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.pulltorefresh.PullToRefreshBox
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
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.geometry.Size
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.Path
import androidx.compose.ui.graphics.StrokeCap
import androidx.compose.ui.graphics.StrokeJoin
import androidx.compose.ui.graphics.drawscope.Stroke
import androidx.compose.ui.layout.onGloballyPositioned
import androidx.compose.ui.layout.positionInParent
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.selected
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import com.yuntyu.myhome.MyHomeApp
import com.yuntyu.myhome.core.Money
import com.yuntyu.myhome.core.api.ApiResult
import com.yuntyu.myhome.core.api.InvestDaily
import com.yuntyu.myhome.core.api.InvestDailyRow
import com.yuntyu.myhome.core.api.InvestFlow
import com.yuntyu.myhome.core.api.InvestRow
import com.yuntyu.myhome.core.api.InvestStatus
import com.yuntyu.myhome.core.api.InvestValue
import com.yuntyu.myhome.core.api.describe
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext

/*
 * 투자 탭: 자동매매(서버 myhome invest) 상태를 읽기만 한다. 주문·수정 버튼은 두지 않는다(주문은 서버의 예약 작업만 낸다).
 * 맨 위: 지금 종목·증권 총액·주식에 넣은 돈(사용자 요청, 2026-11-20 — TQ는 첫 화면에 시장 숫자를 두지 않았다).
 * 그 아래: 이번 달 자동 매수 단계를 GitHub Actions처럼 체크한다. 단계를 누르면 근거(SPX·185일선·변동성,
 * 입금 시각, 환전 환율·기준율 차이, 주문·체결)가 펼쳐진다. 세후 평가는 한 번 더 들어간 평가 화면에 있다.
 * 아래로 당기면 새로 받는다.
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
                SummaryHero(s, onClick = toValue)
                s.daily?.takeIf { it.has }?.let { DailySignal(it) }
                s.flow?.takeIf { it.steps.isNotEmpty() }?.let { FlowCard(it) }
                s.daily?.let { DailyTable(it) }
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
                Note("날짜는 뉴욕 기준, 실행 시각은 한국 시간")
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
                // 토스가 마지막으로 알려 준 주문가능 현금(매매 작업 시각 기준). 장부와 다르면 장부가 놓친 돈이다
                if (v.brokerCashUsd.isNotBlank()) {
                    Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                        Tile("토스 주문가능", v.brokerCashUsd, "${v.brokerCashAt} 기준", Modifier.weight(1f))
                        Tile(
                            "장부와 차이",
                            v.cashGapUsd.ifBlank { "없음" },
                            if (v.cashGapUsd.isBlank()) null else "배당·입금 등",
                            Modifier.weight(1f),
                            if (v.cashGapUsd.isBlank()) null else m.warn,
                        )
                    }
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
                Note("날짜는 뉴욕 기준 · 원 단위 반올림")
            }
        }
    }
    }
}

/** 맨 위 요약: 종목 알약 + 수량, 증권 총액(큰 숫자), 주식·현금 나눔, 넣은 돈·손익 두 칸. 누르면 평가 화면 */
@Composable
private fun SummaryHero(s: InvestStatus, onClick: () -> Unit) {
    val m = LocalModo.current
    val sum = s.summary
    val symbol = sum?.symbol?.ifBlank { null } ?: s.symbol.ifBlank { "—" }
    val shares = sum?.shares ?: s.shares
    Column(
        Modifier.fillMaxWidth().clip(RoundedCornerShape(16.dp)).tap(onClickLabel = "평가 보기", onClick = onClick)
            .padding(horizontal = 6.dp, vertical = 4.dp),
        verticalArrangement = Arrangement.spacedBy(2.dp),
    ) {
        Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            Text(
                symbol,
                Modifier.clip(RoundedCornerShape(50)).background(m.pill).padding(horizontal = 10.dp, vertical = 3.dp),
                fontWeight = FontWeight.Bold,
                fontSize = 14.sp,
            )
            Text(if (shares > 0) "${shares}주 · ${symbolRole(symbol)}" else "보유 없음", color = m.muted, fontSize = 14.sp)
        }
        Text("증권 총액", Modifier.padding(top = 8.dp), style = MaterialTheme.typography.titleSmall, color = m.muted)
        Text(
            if (sum?.canValue == true) Money.won(sum.totalKrw) else "—",
            style = MaterialTheme.typography.headlineLarge.copy(fontSize = 36.sp),
            color = MaterialTheme.colorScheme.onBackground,
        )
        if (sum?.canValue == true) {
            Text("주식 ${Money.won(sum.stockKrw)} · 현금 ${Money.won(sum.cashKrw)}", style = MaterialTheme.typography.bodySmall, color = m.muted)
        }
        if (s.problems > 0) {
            Text("확인할 문제 ${s.problems}건", style = MaterialTheme.typography.bodySmall, color = m.over)
        }
    }
    if (sum != null) {
        Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            Tile("주식에 넣은 돈", Money.won(sum.investedKrw), "누적 원금", Modifier.weight(1f), onClick = onClick)
            Tile(
                "손익 (세전)",
                if (sum.canValue) (if (sum.gainKrw > 0) "+" else "") + Money.won(sum.gainKrw) else "—",
                sum.gainPct.ifBlank { null },
                Modifier.weight(1f),
                if (sum.canValue && sum.gainKrw < 0) m.over else null,
                onClick = onClick,
            )
        }
        if (sum.basis.isNotBlank()) Note(sum.basis)
    }
}

private fun symbolRole(symbol: String): String = when (symbol) {
    "TQQQ" -> "나스닥100 3배"
    "QQQM" -> "나스닥100"
    else -> ""
}.ifBlank { "보유" }

/**
 * 이번 달 자동 매수 단계(GitHub Actions처럼). 머리 줄에 전체 상태, 단계마다 상태 동그라미·이름·시각.
 * 줄을 누르면 근거가 펼쳐진다. 문제가 있는 단계는 처음부터 펼쳐 둔다. 박스 안 박스는 만들지 않고 1dp 선으로 나눈다.
 */
@Composable
private fun FlowCard(flow: InvestFlow) {
    val m = LocalModo.current
    var open by remember(flow.ym) { mutableStateOf(flow.steps.filter { it.state == "failed" }.map { it.key }.toSet()) }
    Column(Modifier.fillMaxWidth().clip(RoundedCornerShape(16.dp)).background(m.card)) {
        Row(
            Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 14.dp),
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(10.dp),
        ) {
            StepIcon(flow.state, 22.dp)
            Text(flow.title, Modifier.weight(1f), fontWeight = FontWeight.SemiBold, fontSize = 16.sp)
            Text(flow.value, color = stateColor(flow.state), fontSize = 14.sp, fontWeight = FontWeight.SemiBold)
        }
        for (step in flow.steps) {
            Hairline()
            val expanded = step.key in open
            Row(
                Modifier.fillMaxWidth()
                    .tap(onClickLabel = if (expanded) "접기" else "자세히") { open = if (expanded) open - step.key else open + step.key }
                    .padding(horizontal = 16.dp, vertical = 12.dp),
                verticalAlignment = Alignment.CenterVertically,
                horizontalArrangement = Arrangement.spacedBy(10.dp),
            ) {
                StepIcon(step.state, 18.dp)
                Text(
                    step.title, Modifier.weight(1f),
                    color = if (step.state == "waiting" || step.state == "skipped") m.muted else MaterialTheme.colorScheme.onSurface,
                )
                if (step.value.isNotBlank()) Text(step.value, color = if (step.state == "failed") m.over else m.muted, fontSize = 13.sp)
                // ⌃와 ⌄는 글자 폭이 달라 칸을 고정한다(안 그러면 누를 때 시각이 옆으로 움직인다)
                Text(if (expanded) "⌃" else "⌄", Modifier.width(14.dp), color = m.faint, fontSize = 13.sp, textAlign = TextAlign.Center)
            }
            AnimatedVisibility(expanded) {
                Column(
                    Modifier.fillMaxWidth().padding(start = 44.dp, end = 16.dp, bottom = 12.dp),
                    verticalArrangement = Arrangement.spacedBy(4.dp),
                ) {
                    if (step.lines.isEmpty()) Text("아직 기록이 없어요", style = MaterialTheme.typography.bodySmall, color = m.faint)
                    for (line in step.lines) {
                        Text(line, style = MaterialTheme.typography.bodySmall, color = m.muted)
                    }
                }
            }
        }
    }
}

/**
 * 오늘 신호(사용자 요청, 2026-11-20): 마지막 거래일 종가로 보면 TQQQ인지 QQQM인지와 근거. 판정은 달 마지막 거래일
 * 하나라 참고일 뿐이고, 이번 달 종목과 다르면 마지막 줄(이대로면 다음 달에 바뀜)만 라벤더로 짚는다.
 */
@Composable
private fun DailySignal(d: InvestDaily) {
    val m = LocalModo.current
    SectionCard("오늘 신호") {
        Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(10.dp)) {
            Text(
                d.signal,
                Modifier.clip(RoundedCornerShape(50)).background(m.pill).padding(horizontal = 10.dp, vertical = 3.dp),
                fontWeight = FontWeight.Bold,
                fontSize = 14.sp,
            )
            Text("${d.title} · ${symbolRole(d.signal)}", color = m.muted, fontSize = 14.sp)
        }
        Column(verticalArrangement = Arrangement.spacedBy(4.dp)) {
            d.lines.forEachIndexed { i, line ->
                Text(line, style = MaterialTheme.typography.bodySmall, color = if (i == d.lines.lastIndex && !d.same) m.warn else m.muted)
            }
        }
    }
}

/** 처음에 보이는 매일 기록 줄 수. 나머지는 "더 보기" */
private const val DAILY_SHOWN = 7

/**
 * 매일 기록 표: 날짜 · 신호 · 원금 · 금액 · 수익률(최근 날부터). 줄을 누르면 그날 S&P500·변동성과 손익.
 * 밀린 날(갱신이 한꺼번에 받은 날)은 계좌를 몰라 돈 칸이 "—"다. 박스 안 박스 없이 1dp 선으로 나눈다.
 */
@Composable
private fun DailyTable(d: InvestDaily) {
    val m = LocalModo.current
    var all by remember { mutableStateOf(false) }
    var open by remember { mutableStateOf(setOf<String>()) }
    Column(Modifier.fillMaxWidth()) {
        Text("매일 기록", Modifier.padding(start = 6.dp, bottom = 8.dp), style = MaterialTheme.typography.labelLarge, color = m.muted)
        Column(Modifier.fillMaxWidth().clip(RoundedCornerShape(16.dp)).background(m.card)) {
            if (d.rows.isEmpty()) {
                Text(
                    "아직 기록이 없어요. 거래일마다 미국 장이 끝난 뒤(한국 아침 7시 40분쯤) 남겨요.",
                    Modifier.padding(horizontal = 18.dp, vertical = 16.dp),
                    color = m.muted,
                )
                return@Column
            }
            DailyCells("날짜", "신호", "원금", "금액", "수익률", header = true)
            val rows = if (all) d.rows else d.rows.take(DAILY_SHOWN)
            for (r in rows) {
                Hairline()
                val expanded = r.day in open
                DailyCells(
                    r.day, r.signal,
                    if (r.hasMoney) Money.grouped(r.investedKrw) else "—",
                    if (r.hasMoney) Money.grouped(r.totalKrw) else "—",
                    if (r.hasMoney) r.gainPct.ifBlank { "—" } else "—",
                    gainColor = if (r.hasMoney && r.gainKrw < 0) m.over else null,
                    modifier = Modifier.tap(onClickLabel = if (expanded) "접기" else "자세히") { open = if (expanded) open - r.day else open + r.day },
                )
                AnimatedVisibility(expanded) { DailyDetail(r) }
            }
            if (d.rows.size > DAILY_SHOWN) {
                Hairline()
                Text(
                    if (all) "접기" else "더 보기 (${d.rows.size}일)",
                    Modifier.fillMaxWidth().tap(role = Role.Button) { all = !all }.padding(vertical = 12.dp),
                    color = m.muted,
                    fontSize = 13.sp,
                    textAlign = TextAlign.Center,
                )
            }
        }
        Note("세전 · 금액은 주식 + 현금, 수익률은 아직 안 바꾼 원화를 빼고 계산")
    }
}

@Composable
private fun DailyCells(
    day: String, signal: String, invested: String, total: String, gain: String,
    header: Boolean = false, gainColor: Color? = null, modifier: Modifier = Modifier,
) {
    val m = LocalModo.current
    val size = if (header) 12.sp else 13.sp
    val color = if (header) m.muted else MaterialTheme.colorScheme.onSurface
    Row(
        modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = if (header) 10.dp else 12.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Text(day, Modifier.width(46.dp), fontSize = size, color = m.muted)
        Text(signal, Modifier.width(50.dp), fontSize = size, color = color, fontWeight = if (header) null else FontWeight.SemiBold)
        Text(invested, Modifier.weight(1f), fontSize = size, color = color, textAlign = TextAlign.End, maxLines = 1)
        Text(total, Modifier.weight(1f), fontSize = size, color = color, textAlign = TextAlign.End, maxLines = 1)
        Text(gain, Modifier.width(60.dp), fontSize = size, color = gainColor ?: color, textAlign = TextAlign.End, maxLines = 1)
    }
}

@Composable
private fun DailyDetail(r: InvestDailyRow) {
    val m = LocalModo.current
    Column(
        Modifier.fillMaxWidth().padding(start = 16.dp, end = 16.dp, bottom = 12.dp),
        verticalArrangement = Arrangement.spacedBy(4.dp),
    ) {
        Text(
            "S&P500 185일선보다 ${r.trend} ${if (r.c1) "✓" else "✗"} · 나스닥100 변동성 ${r.vol} ${if (r.c2) "✓" else "✗"}",
            style = MaterialTheme.typography.bodySmall,
            color = m.muted,
        )
        Text(
            if (r.hasMoney) "손익 ${if (r.gainKrw > 0) "+" else ""}${Money.won(r.gainKrw)}" else "이날은 계좌를 남기지 못했어요(밀려서 한꺼번에 받은 날)",
            style = MaterialTheme.typography.bodySmall,
            color = m.muted,
        )
    }
}

@Composable
private fun stateColor(state: String): Color {
    val m = LocalModo.current
    return when (state) {
        "done" -> m.ok
        "running" -> m.warn
        "failed" -> m.over
        else -> m.muted
    }
}

/** 상태 동그라미: 끝남 = 초록 바탕 체크, 진행 중 = 라벤더 고리가 돈다, 문제 = 살구 바탕 ×, 아직 = 빈 고리, 해당 없음 = 빈 고리 + 가로줄 */
@Composable
private fun StepIcon(state: String, size: Dp) {
    val m = LocalModo.current
    val color = stateColor(state)
    val faint = m.faint
    val ink = m.bg
    val spin = if (state == "running") {
        val t = rememberInfiniteTransition(label = "step")
        t.animateFloat(0f, 360f, infiniteRepeatable(tween(1200, easing = LinearEasing)), label = "spin").value
    } else 0f
    Canvas(Modifier.size(size)) {
        val w = this.size.minDimension
        val stroke = w * 0.11f
        when (state) {
            "done" -> {
                drawCircle(color)
                val p = Path().apply {
                    moveTo(w * 0.28f, w * 0.52f); lineTo(w * 0.44f, w * 0.67f); lineTo(w * 0.73f, w * 0.36f)
                }
                drawPath(p, ink, style = Stroke(width = stroke, cap = StrokeCap.Round, join = StrokeJoin.Round))
            }
            "failed" -> {
                drawCircle(color)
                drawLine(ink, Offset(w * 0.34f, w * 0.34f), Offset(w * 0.66f, w * 0.66f), stroke, StrokeCap.Round)
                drawLine(ink, Offset(w * 0.66f, w * 0.34f), Offset(w * 0.34f, w * 0.66f), stroke, StrokeCap.Round)
            }
            "running" -> {
                val inset = stroke / 2
                drawCircle(color.copy(alpha = 0.25f), radius = w / 2 - inset, style = Stroke(stroke))
                drawArc(
                    color, startAngle = spin - 90f, sweepAngle = 100f, useCenter = false,
                    topLeft = Offset(inset, inset), size = Size(w - stroke, w - stroke), style = Stroke(stroke, cap = StrokeCap.Round),
                )
                drawCircle(color, radius = w * 0.16f)
            }
            "skipped" -> {
                drawCircle(faint, radius = w / 2 - stroke / 2, style = Stroke(stroke * 0.8f))
                drawLine(faint, Offset(w * 0.32f, w / 2), Offset(w * 0.68f, w / 2), stroke * 0.8f, StrokeCap.Round)
            }
            else -> drawCircle(faint, radius = w / 2 - stroke / 2, style = Stroke(stroke * 0.8f))
        }
    }
}

/** 입금일 고르기(1~7일 — 매매 창이 1~10일이라). 정하지 않으면 첫 거래일부터 산다. 설정 화면이 쓴다 */
@Composable
internal fun DepositDayDialog(current: Int, onDismiss: () -> Unit, onSave: (Int) -> Unit) {
    val m = LocalModo.current
    var day by remember { mutableIntStateOf(current.coerceIn(0, 20)) }
    ModoAlertDialog(
        onDismissRequest = onDismiss,
        title = { Text("투자 입금일") },
        text = {
            Column(verticalArrangement = Arrangement.spacedBy(4.dp)) {
                Text(
                    "월급이 토스증권에 들어오는 날이에요. 이 날 전에는 사지 않고, 적립금이 들어오면 그날 밤에 사요. 10일까지 안 들어오면 알려 드려요.",
                    fontSize = 14.sp, color = m.muted, modifier = Modifier.padding(bottom = 8.dp),
                )
                for (week in (1..7).chunked(7)) {
                    Row(Modifier.fillMaxWidth()) {
                        for (d in week) {
                            val on = d == day
                            Box(
                                Modifier.weight(1f).height(40.dp).padding(2.dp).clip(CircleShape)
                                    .background(if (on) MaterialTheme.colorScheme.onBackground else Color.Transparent)
                                    .tap(onClickLabel = "${d}일", role = Role.RadioButton) { day = d }
                                    .semantics { selected = on },
                                contentAlignment = Alignment.Center,
                            ) {
                                Text(
                                    "$d", fontSize = 15.sp,
                                    fontWeight = if (on) FontWeight.Bold else FontWeight.Normal,
                                    color = if (on) MaterialTheme.colorScheme.background else MaterialTheme.colorScheme.onBackground,
                                )
                            }
                        }
                        repeat(7 - week.size) { Spacer(Modifier.weight(1f)) }
                    }
                }
            }
        },
        confirmButton = { TextButton(onClick = { onSave(day) }, enabled = day > 0) { Text(if (day > 0) "매달 ${day}일로" else "날짜를 골라 주세요") } },
        dismissButton = {
            Row {
                if (current > 0) TextButton(onClick = { onSave(0) }) { Text("정하지 않기") }
                TextButton(onClick = onDismiss) { Text("닫기") }
            }
        },
    )
}

@Composable
private fun NotMigrated() {
    SectionCard("투자") {
        Text("자동매매 상태가 아직 없어요.")
        Text(
            "서버 DB에 자동매매 상태가 생기면 여기에 보여요.",
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
