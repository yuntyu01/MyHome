package com.yuntyu.myhome.ui

import android.content.Context
import android.widget.Toast
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.imePadding
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
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
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import com.yuntyu.myhome.MyHomeApp
import com.yuntyu.myhome.core.CategoryTree
import com.yuntyu.myhome.core.Money
import com.yuntyu.myhome.core.api.ApiResult
import com.yuntyu.myhome.core.api.Category
import com.yuntyu.myhome.core.api.ManualTransaction
import com.yuntyu.myhome.core.api.RawEventDto
import com.yuntyu.myhome.widget.WidgetRefresher
import com.yuntyu.myhome.work.WorkScheduler
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import java.time.OffsetDateTime
import java.time.ZoneId

/**
 * 읽지 못한 결제 알림을 앱 안에서 처리한다(사용자 요청, 2026-11-15 "웹으로 가지 말고 앱에서"). 예전에는 웹 관리 화면을 열었다.
 * 알림마다 원문을 보여 주고 적기(금액·분류 넣기, 시각은 알림 시각) 또는 결제 아님. 처리하면 목록에서 빠지고 폰의 원래 알림도 치운다.
 */
@Composable
fun UnreadScreen(modifier: Modifier) {
    val app = MyHomeApp.instance
    val ctx = LocalContext.current
    val scope = rememberCoroutineScope()
    val m = LocalModo.current
    var list by remember { mutableStateOf<List<RawEventDto>?>(null) }
    var cats by remember { mutableStateOf<List<Category>>(emptyList()) }
    var error by remember { mutableStateOf<String?>(null) }
    var tick by remember { mutableIntStateOf(0) }
    var busy by remember { mutableStateOf(false) }
    var recording by remember { mutableStateOf<RawEventDto?>(null) }

    LaunchedEffect(tick) {
        when (val r = withContext(Dispatchers.IO) { app.api.unreadEvents() }) {
            is ApiResult.Ok -> { list = r.value.rawEvents; error = null }
            else -> error = r.plain()
        }
    }
    LaunchedEffect(Unit) {
        (withContext(Dispatchers.IO) { app.api.categories() } as? ApiResult.Ok)?.let { cats = it.value.categories.filter { c -> !c.archived } }
    }

    // 처리 뒤: 목록 다시, 요약(홈 수) 다시, 폰에 남겨 둔 원래 알림 치우기
    fun done() {
        tick++
        scope.launch { WidgetRefresher.refresh(ctx) }
        WorkScheduler.checkNotices(ctx)
    }

    fun ignore(e: RawEventDto) {
        busy = true
        scope.launch {
            val r = withContext(Dispatchers.IO) { app.api.ignoreUnread(e.id) }
            busy = false
            if (r !is ApiResult.Ok) Toast.makeText(ctx, r.plain(), Toast.LENGTH_LONG).show()
            done()
        }
    }

    LazyColumn(modifier.fillMaxSize()) {
        val l = list
        when {
            error != null && l == null -> item { EmptyNote("불러오지 못했어요", error) }
            l == null -> Unit
            l.isEmpty() -> item { EmptyNote("읽지 못한 알림이 없어요") }
        }
        items(l.orEmpty(), key = { it.id }) { e ->
            UnreadRow(e, appLabel(ctx, e.sourcePackage), busy, onRecord = { recording = e }, onIgnore = { ignore(e) })
            Hairline(Modifier.padding(start = Gutter))
        }
    }

    recording?.let { e ->
        RecordUnreadSheet(e, cats, onDismiss = { recording = null }) {
            recording = null
            done()
        }
    }
}

@Composable
internal fun UnreadRow(e: RawEventDto, app: String, busy: Boolean, onRecord: () -> Unit, onIgnore: () -> Unit) {
    val m = LocalModo.current
    Column(Modifier.fillMaxWidth().padding(start = Gutter, end = Gutter - 8.dp, top = 14.dp, bottom = 4.dp), verticalArrangement = Arrangement.spacedBy(2.dp)) {
        Text("$app · ${shortTime(e.postedAt)}", fontSize = 13.sp, color = m.muted)
        if (e.title.isNotBlank()) Text(e.title, style = MaterialTheme.typography.titleSmall, fontWeight = FontWeight.Bold)
        val body = listOf(e.text, e.bigText).filter { it.isNotBlank() }.distinct().joinToString("\n")
        if (body.isNotBlank()) Text(body, style = MaterialTheme.typography.bodyMedium)
        Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.End, verticalAlignment = Alignment.CenterVertically) {
            TextButton(onClick = onIgnore, enabled = !busy) { Text("결제 아님", color = m.muted) }
            TextButton(onClick = onRecord, enabled = !busy) { Text("적기") }
        }
    }
}

/** 금액·나간/들어온·어디서·분류를 넣어 적는다. 분류는 나간 돈이면 지출 분류, 들어온 돈이면 수입·정산·이체 */
@Composable
private fun RecordUnreadSheet(e: RawEventDto, cats: List<Category>, onDismiss: () -> Unit, onSaved: () -> Unit) {
    val ctx = LocalContext.current
    val app = MyHomeApp.instance
    val scope = rememberCoroutineScope()
    val m = LocalModo.current
    var amount by remember { mutableStateOf("") }
    var where by remember { mutableStateOf(e.title.take(40)) }
    var out by remember { mutableStateOf(true) }
    var catId by remember { mutableStateOf<Long?>(null) }
    var busy by remember { mutableStateOf(false) }
    var err by remember { mutableStateOf<String?>(null) }
    val won = amount.toLongOrNull() ?: 0L
    ModoBottomSheet(onDismissRequest = onDismiss) {
        Column(Modifier.fillMaxWidth().heightIn(max = 680.dp).imePadding().navigationBarsPadding()) {
            Column(Modifier.weight(1f, fill = false).verticalScroll(rememberScrollState())) {
                Text("알림 적기", Modifier.padding(horizontal = Gutter), fontSize = 20.sp, fontWeight = FontWeight.Bold)
                Text(
                    listOf(e.title, e.text).filter { it.isNotBlank() }.joinToString(" · "),
                    Modifier.padding(horizontal = Gutter, vertical = 4.dp), fontSize = 13.sp, color = m.muted, maxLines = 3,
                )
                TextTabs(listOf("나간 돈", "들어온 돈"), if (out) 0 else 1, Modifier.padding(horizontal = Gutter, vertical = 8.dp)) {
                    out = it == 0
                    catId = null
                }
                Column(Modifier.padding(horizontal = Gutter, vertical = 4.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
                    LineField(amount, { amount = it.filter(Char::isDigit).take(10) }, if (won > 0) Money.korean(won) else "금액", keyboardType = KeyboardType.Number, suffix = "원", money = true)
                    LineField(where, { where = it.take(40) }, "어디서")
                }
                SectionLabel("분류")
                val tree = remember(cats) { CategoryTree(cats) }
                val kinds = if (out) setOf("expense") else setOf("income", "special")
                for ((c, depth) in tree.preorder { !it.archived }.filter { it.first.kind in kinds }) {
                    FlatRow(
                        c.name, chevron = false, bold = tree.children(c.id).isNotEmpty(), leading = indentLead(depth),
                        trailing = { CheckCircle(catId == c.id) }, onClick = { catId = c.id },
                    )
                }
                err?.let { Text(it, Modifier.padding(horizontal = Gutter, vertical = 8.dp), fontSize = 14.sp, color = m.over) }
            }
            PrimaryButton("적기", Modifier.padding(horizontal = Gutter, vertical = 12.dp), enabled = won > 0 && catId != null && !busy) {
                val id = catId ?: return@PrimaryButton
                busy = true
                scope.launch {
                    val r = withContext(Dispatchers.IO) {
                        app.api.recordUnread(e.id, ManualTransaction(amount = won, merchant = where.trim(), categoryId = id, flow = if (out) "out" else "in"))
                    }
                    busy = false
                    if (r is ApiResult.Ok) {
                        Toast.makeText(ctx, "${Money.won(won)}을 적었어요", Toast.LENGTH_SHORT).show()
                        onSaved()
                    } else {
                        err = "적지 못했어요. ${r.plain()}"
                    }
                }
            }
        }
    }
}

private fun appLabel(ctx: Context, pkg: String): String = runCatching {
    ctx.packageManager.getApplicationLabel(ctx.packageManager.getApplicationInfo(pkg, 0)).toString()
}.getOrDefault(pkg)

/** "2026-11-15T12:03:00+09:00" → "11/15 12:03" */
private fun shortTime(iso: String): String =
    runCatching { OffsetDateTime.parse(iso).atZoneSameInstant(ZoneId.of("Asia/Seoul")).let { "%d/%d %02d:%02d".format(it.monthValue, it.dayOfMonth, it.hour, it.minute) } }
        .getOrDefault(iso)
