package com.yuntyu.myhome.ui

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import com.yuntyu.myhome.MyHomeApp
import com.yuntyu.myhome.core.api.ApiResult
import com.yuntyu.myhome.core.api.Backtest
import com.yuntyu.myhome.core.api.BacktestColumn
import com.yuntyu.myhome.core.api.BacktestRow
import com.yuntyu.myhome.core.api.describe
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext

/*
 * 투자 → 백테스트 결과 (TQ 뷰어 이식, 웹 /invest/backtest와 같은 표).
 * 고정된 과거 표다. 힘든 구간에서 "예상 범위 안이다"를 확인하는 근거라 장기 보유의 바탕이 된다.
 * 🔴 TQ 규칙: 하위 화면에 둔다 · 최악·하위10%를 앞에 둔다 · 읽는 법을 표와 같은 화면에 둔다.
 * 기본 표본은 닷컴 제외(TQ와 같음)지만, 맨 위 요약은 공식 기준인 전체 표본이다.
 */

private val KEY_COLS = listOf("worst", "median", "loss_pct", "mdd_worst_pct")

@Composable
fun InvestBacktestScreen(modifier: Modifier) {
    var data by remember { mutableStateOf<Backtest?>(null) }
    var error by remember { mutableStateOf<String?>(null) }
    LaunchedEffect(Unit) {
        when (val r = withContext(Dispatchers.IO) { MyHomeApp.instance.api.investBacktest() }) {
            is ApiResult.Ok -> { data = r.value; error = null }
            else -> error = r.describe()
        }
    }
    var all by rememberSaveable { mutableStateOf(false) }
    var horizon by rememberSaveable { mutableIntStateOf(-1) }
    val m = LocalModo.current
    Column(
        modifier.fillMaxSize().verticalScroll(rememberScrollState()).padding(16.dp),
        verticalArrangement = Arrangement.spacedBy(14.dp),
    ) {
        val b = data
        if (b == null) {
            Text(error?.let { "불러오지 못했어요: $it" } ?: "불러오는 중…", color = m.muted)
            return@Column
        }
        Text(
            "S&P500 185일 이동평균 · 나스닥100 21일 변동성 30% · 월 1회 판정\n${b.period} · 단위 억원",
            style = MaterialTheme.typography.bodySmall,
            color = m.muted,
            modifier = Modifier.padding(horizontal = 6.dp),
        )

        // 붙잡는 논리: "QQQ보다 안전"이 아니라 "TQQQ·QLD를 그냥 보유하는 것보다 안전"
        val keyCols = KEY_COLS.mapNotNull { k -> b.columns.firstOrNull { it.key == k } }
        SectionCard("TQQQ·QLD를 그냥 들고 있는 것과 비교") {
            val first = b.key.firstOrNull()
            Text(
                "7년 적립 · 전체 표본(닷컴 포함)" + (first?.let { " · 원금 %.2f억 · 표본 %d개".format(it.principal, it.windows) } ?: ""),
                style = MaterialTheme.typography.bodySmall,
                color = m.muted,
            )
            BacktestTable(b.key, keyCols, nameWidth = 116.dp, cellWidth = 64.dp)
            Text(
                "이 전략의 근거는 수익이 아니라 하방이다. 최악·손실%·MDD를 먼저 보라.",
                style = MaterialTheme.typography.bodySmall,
                color = m.muted,
            )
        }

        SegTabs(listOf("닷컴 제외", "전체 표본"), if (all) 1 else 0) { all = it == 1 }
        Text(
            if (all) "지금 보는 것은 전체 표본이다 — 닷컴 붕괴를 포함한다. 공식 수치가 이 기준이다."
            else "지금 보는 것은 닷컴 버블을 뺀 표본이다 — 이 전략이 겪은 최악의 구간이 빠져 있다.",
            Modifier.fillMaxWidth().clip(RoundedCornerShape(16.dp)).background(m.pill).padding(horizontal = 18.dp, vertical = 12.dp),
            style = MaterialTheme.typography.bodySmall,
            color = if (all) MaterialTheme.colorScheme.onSurface else m.over,
        )

        val labels = b.horizons.map { it.label }
        val hi = if (horizon in labels.indices) horizon else labels.indexOf("7년").coerceAtLeast(0)
        Row(Modifier.fillMaxWidth().horizontalScroll(rememberScrollState()), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            labels.forEachIndexed { i, l ->
                val on = i == hi
                Text(
                    l,
                    Modifier.clip(RoundedCornerShape(999.dp))
                        .background(if (on) MaterialTheme.colorScheme.onBackground else m.pill)
                        .clickable(role = Role.Tab) { horizon = i }
                        .padding(horizontal = 14.dp, vertical = 7.dp),
                    color = if (on) MaterialTheme.colorScheme.background else m.muted,
                    fontWeight = FontWeight.SemiBold,
                    fontSize = 14.sp,
                )
            }
        }
        b.horizons.getOrNull(hi)?.let { h ->
            val rows = if (all) h.all else h.exDotcom
            SectionCard("${h.label} 적립") {
                rows.firstOrNull()?.let {
                    Text(
                        "원금 %.2f억 · 표본 %d개 윈도우 · %s".format(it.principal, it.windows, if (all) "전체 표본" else "닷컴 제외"),
                        style = MaterialTheme.typography.bodySmall,
                        color = m.muted,
                    )
                }
                BacktestTable(rows, b.columns, nameWidth = 128.dp, cellWidth = 66.dp)
                Text("옆으로 밀면 나머지 칸이 보여요", style = MaterialTheme.typography.bodySmall, color = m.faint)
            }
        }

        SectionCard("읽기 전에") {
            for (n in b.notes) {
                Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    Text("·", color = m.muted)
                    Text(n, style = MaterialTheme.typography.bodyMedium)
                }
            }
        }
        Text(
            "★가 이 전략, 굵은 칸이 항목별 1등이에요. 손실%는 낮을수록, MDD는 0에 가까울수록 1등이에요. 다시 계산하지 않는 고정 표예요(TQ 백테스트 v2, 수수료 %.2f%%).".format(b.feeRate * 100),
            style = MaterialTheme.typography.bodySmall,
            color = m.muted,
            modifier = Modifier.padding(horizontal = 6.dp),
        )
    }
}

/** 이름 열은 고정, 숫자 칸은 옆으로 민다. ★ 행은 바탕, 항목별 1등은 굵게(색은 쓰지 않는다 — 무채색) */
@Composable
private fun BacktestTable(rows: List<BacktestRow>, cols: List<BacktestColumn>, nameWidth: Dp, cellWidth: Dp) {
    val m = LocalModo.current
    val rowH = 34.dp
    Row(Modifier.fillMaxWidth()) {
        Column(Modifier.width(nameWidth)) {
            HeaderCell("전략", nameWidth, TextAlign.Start)
            for (r in rows) {
                Box(
                    Modifier.width(nameWidth).height(rowH).background(if (r.star) m.raised else androidx.compose.ui.graphics.Color.Transparent)
                        .padding(start = 6.dp),
                    contentAlignment = Alignment.CenterStart,
                ) {
                    Text(r.shortName, fontSize = 13.sp, fontWeight = if (r.star) FontWeight.Bold else FontWeight.Normal, maxLines = 1)
                }
            }
        }
        Column(Modifier.horizontalScroll(rememberScrollState())) {
            Row { for (c in cols) HeaderCell(c.label, cellWidth, TextAlign.End) }
            for (r in rows) {
                Row(Modifier.height(rowH).background(if (r.star) m.raised else androidx.compose.ui.graphics.Color.Transparent)) {
                    for (c in cols) {
                        val top = c.key in r.tops
                        Box(Modifier.width(cellWidth).height(rowH).padding(end = 6.dp), contentAlignment = Alignment.CenterEnd) {
                            Text(
                                r.text(c.key),
                                fontSize = 13.sp,
                                fontWeight = if (top) FontWeight.ExtraBold else FontWeight.Normal,
                                color = if (top) MaterialTheme.colorScheme.onSurface else MaterialTheme.colorScheme.onSurface.copy(alpha = 0.78f),
                                maxLines = 1,
                            )
                        }
                    }
                }
            }
        }
    }
}

@Composable
private fun HeaderCell(text: String, width: Dp, align: TextAlign) {
    Text(
        text,
        Modifier.width(width).padding(horizontal = 6.dp, vertical = 6.dp),
        fontSize = 12.sp,
        color = LocalModo.current.muted,
        fontWeight = FontWeight.SemiBold,
        textAlign = align,
        maxLines = 1,
    )
}

/** 두세 칸짜리 고르기(알약 모양) */
@Composable
private fun SegTabs(items: List<String>, selected: Int, onSelect: (Int) -> Unit) {
    val m = LocalModo.current
    Row(Modifier.fillMaxWidth().clip(RoundedCornerShape(14.dp)).background(m.pill).padding(4.dp)) {
        items.forEachIndexed { i, label ->
            val on = i == selected
            Text(
                label,
                Modifier.weight(1f).clip(RoundedCornerShape(10.dp))
                    .background(if (on) m.card else androidx.compose.ui.graphics.Color.Transparent)
                    .clickable(role = Role.Tab) { onSelect(i) }
                    .padding(vertical = 9.dp),
                textAlign = TextAlign.Center,
                fontWeight = if (on) FontWeight.SemiBold else FontWeight.Normal,
                color = if (on) MaterialTheme.colorScheme.onSurface else m.muted,
            )
        }
    }
}
