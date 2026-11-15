package com.yuntyu.myhome.ui

import android.widget.Toast
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
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
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.unit.dp
import com.yuntyu.myhome.MyHomeApp
import com.yuntyu.myhome.core.Money
import com.yuntyu.myhome.core.api.ApiResult
import com.yuntyu.myhome.core.api.BalanceAccount
import com.yuntyu.myhome.core.api.BalanceGap
import com.yuntyu.myhome.widget.WidgetRefresher
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import java.time.OffsetDateTime

/**
 * 계좌 잔액과 빈 돈(사용자 요청, 2026-11-15 "토스뱅크 잔액 기준, 빈 값은 놓친 것이니 채워 가야").
 * 알림에 찍힌 잔액을 이어 봐서 안 맞는 곳을 보여 주고, 채우기(확인 필요 거래로 넣음)나 무시를 고른다.
 */
@Composable
fun BalanceScreen(modifier: Modifier) {
    val app = MyHomeApp.instance
    val ctx = LocalContext.current
    val scope = rememberCoroutineScope()
    val m = LocalModo.current
    var accounts by remember { mutableStateOf<List<BalanceAccount>?>(null) }
    var error by remember { mutableStateOf<String?>(null) }
    var tick by remember { mutableIntStateOf(0) }
    var busy by remember { mutableStateOf(false) }

    LaunchedEffect(tick) {
        when (val r = withContext(Dispatchers.IO) { app.api.balances() }) {
            is ApiResult.Ok -> { accounts = r.value.accounts; error = null }
            else -> error = r.plain()
        }
    }

    fun act(g: BalanceGap, fill: Boolean) {
        busy = true
        scope.launch {
            val r = withContext(Dispatchers.IO) { if (fill) app.api.fillBalanceGap(g.toId) else app.api.ignoreBalanceGap(g.toId) }
            busy = false
            if (r is ApiResult.Ok) {
                if (fill) Toast.makeText(ctx, "확인 필요에 넣었어요", Toast.LENGTH_SHORT).show()
                WidgetRefresher.refresh(ctx)
            } else {
                Toast.makeText(ctx, r.plain(), Toast.LENGTH_LONG).show()
            }
            tick++
        }
    }

    LazyColumn(modifier.fillMaxSize()) {
        val list = accounts
        when {
            error != null && list == null -> item { EmptyNote("불러오지 못했어요", error) }
            list == null -> Unit
            list.isEmpty() -> item { EmptyNote("잔액이 찍힌 알림이 아직 없어요") }
        }
        for (a in list.orEmpty()) {
            item(key = "acct-${a.source}") {
                SectionLabel(
                    a.label,
                    Modifier.padding(top = 12.dp),
                    help = "알림에 찍힌 잔액을 이어 봐서, 앞 잔액에 이번 거래를 더하거나 뺀 값이 이번 잔액과 다르면 그 사이에 가계부가 놓친 돈이 있는 거예요. " +
                        "채우면 확인 필요에 넣고 분류는 거기서 골라요. 이미 적힌 거래인데 잔액만 안 찍혔던 거면 무시하세요.",
                )
                FlatRow(
                    Money.won(a.balance),
                    sub = "${shortAt(a.at)} 알림 기준",
                    bold = true,
                    chevron = false,
                )
                if (a.gaps.isEmpty()) FlatRow("빈 돈 없음", chevron = false, subColor = m.muted)
            }
            items(a.gaps, key = { "gap-${it.toId}" }) { g ->
                GapRow(g, busy, onFill = { act(g, true) }, onIgnore = { act(g, false) })
                Hairline(Modifier.padding(start = Gutter))
            }
        }
    }
}

@Composable
internal fun GapRow(g: BalanceGap, busy: Boolean, onFill: () -> Unit, onIgnore: () -> Unit) {
    val m = LocalModo.current
    Column(Modifier.fillMaxWidth().padding(start = Gutter, end = Gutter - 8.dp, top = 12.dp, bottom = 4.dp)) {
        Text(
            (if (g.amount < 0) "나간 돈 " else "들어온 돈 ") + Money.won(kotlin.math.abs(g.amount)),
            style = MaterialTheme.typography.titleMedium,
            color = if (g.amount < 0) m.over else m.income,
        )
        Text(
            "${shortAt(g.fromAt)} ${Money.won(g.fromBalance)} → ${shortAt(g.toAt)} ${Money.won(g.toBalance)}" +
                (g.toMerchant.takeIf { it.isNotBlank() }?.let { " ($it 앞)" } ?: ""),
            style = MaterialTheme.typography.bodySmall,
            color = m.muted,
        )
        Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.End, verticalAlignment = Alignment.CenterVertically) {
            TextButton(onClick = onIgnore, enabled = !busy) { Text("무시", color = m.muted) }
            TextButton(onClick = onFill, enabled = !busy) { Text("채우기") }
        }
    }
}

/** "2026-11-15T12:03:00+09:00" → "11/15 12:03" */
private fun shortAt(iso: String): String =
    runCatching { OffsetDateTime.parse(iso).atZoneSameInstant(java.time.ZoneId.of("Asia/Seoul")).let { "%d/%d %02d:%02d".format(it.monthValue, it.dayOfMonth, it.hour, it.minute) } }
        .getOrDefault(iso)
