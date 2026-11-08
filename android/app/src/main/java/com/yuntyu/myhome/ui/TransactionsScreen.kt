package com.yuntyu.myhome.ui

import android.widget.Toast
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.imePadding
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.Text
import androidx.compose.material3.pulltorefresh.PullToRefreshBox
import androidx.compose.material3.rememberModalBottomSheetState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import com.yuntyu.myhome.MyHomeApp
import com.yuntyu.myhome.core.Money
import com.yuntyu.myhome.core.WidgetModel
import com.yuntyu.myhome.core.api.ApiResult
import com.yuntyu.myhome.core.api.Category
import com.yuntyu.myhome.core.api.ManualTransaction
import com.yuntyu.myhome.core.api.TransactionDto
import com.yuntyu.myhome.core.api.TransactionEdit
import com.yuntyu.myhome.widget.WidgetRefresher
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import java.time.LocalDate
import java.time.OffsetDateTime
import java.time.YearMonth
import java.time.ZoneId

private val KST: ZoneId = ZoneId.of("Asia/Seoul")

private val reviewReasonLabels = mapOf(
    "no_merchant" to "가게 이름이 없어요", "no_rule" to "처음 보는 가게예요", "payment_gateway" to "결제대행사 이름으로 찍혔어요",
    "transfer" to "송금이에요", "deposit" to "어떤 입금인지 골라 주세요", "unknown_kind" to "어떤 거래인지 몰라요",
    "cancel_unmatched" to "원래 결제를 못 찾은 취소예요", "cancel_partial" to "부분 취소예요",
    "foreign_currency" to "외화 결제예요(금액은 웹에서 고쳐 주세요)",
)

/**
 * 거래: 달 넘기기, 탭(전체 · 분류 고를 것 · 들어온 돈), 날짜별 줄. 줄을 누르면 분류를 고른다(확인 필요 건이면 AI 추천도).
 * 아래에 "현금으로 쓴 돈 직접 적기". 떠 있는 버튼 대신 목록 아래 줄로 둔다(내용을 가리지 않게).
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun TransactionsScreen(modifier: Modifier, resumeTick: Int, tab: Int, onTab: (Int) -> Unit, openTxId: Long?, onOpened: () -> Unit) {
    val ctx = LocalContext.current
    val app = MyHomeApp.instance
    val scope = rememberCoroutineScope()
    val m = LocalModo.current
    val summary by app.summaryRepo.state.collectAsState()
    val current = summary.summary?.period?.label ?: YearMonth.now(KST).toString()
    // null이면 이번 기간
    var month by rememberSaveable { mutableStateOf<String?>(null) }
    val shown = month ?: current
    var items by remember { mutableStateOf<List<TransactionDto>?>(null) }
    var review by remember { mutableStateOf<List<TransactionDto>?>(null) }
    var cats by remember { mutableStateOf<List<Category>>(emptyList()) }
    var error by remember { mutableStateOf<String?>(null) }
    var refreshing by remember { mutableStateOf(false) }
    var selected by remember { mutableStateOf<TransactionDto?>(null) }
    var cashOpen by remember { mutableStateOf(false) }
    var reloadTick by remember { mutableIntStateOf(0) }

    suspend fun load() {
        val list = withContext(Dispatchers.IO) { app.api.transactions(500, month = shown) }
        val q = withContext(Dispatchers.IO) { app.api.reviewQueue() }
        val c = withContext(Dispatchers.IO) { app.api.categories() }
        if (list is ApiResult.Ok) { items = list.value.transactions; error = null } else error = list.plain()
        if (q is ApiResult.Ok) review = q.value.transactions
        if (c is ApiResult.Ok) cats = c.value.categories.filter { !it.archived }
    }
    LaunchedEffect(shown, resumeTick, reloadTick) { load() }
    // 위젯에서 확인 필요 항목을 눌러 들어왔으면 바로 분류 고르기를 연다
    LaunchedEffect(openTxId, review) {
        val id = openTxId ?: return@LaunchedEffect
        val q = review ?: return@LaunchedEffect
        selected = q.firstOrNull { it.id == id } ?: items?.firstOrNull { it.id == id }
        onOpened()
    }

    fun submit(t: TransactionDto, edit: TransactionEdit) {
        scope.launch {
            val r = withContext(Dispatchers.IO) { app.api.editTransaction(t.id, edit) }
            if (r is ApiResult.Ok) {
                val extra = if (r.value.alsoUpdated > 0) " 같은 가게 ${r.value.alsoUpdated}건도 같이 바꿨어요." else ""
                Toast.makeText(ctx, "바꿨어요.$extra", Toast.LENGTH_SHORT).show()
                selected = null
                reloadTick++
                WidgetRefresher.refresh(ctx)
            } else {
                Toast.makeText(ctx, "바꾸지 못했어요. ${r.plain()}", Toast.LENGTH_LONG).show()
            }
        }
    }

    Column(modifier.fillMaxSize()) {
        PageTitle("거래") {
            // 분류 고를 것은 달과 상관없이 모두 보여주므로 달 넘기기를 숨긴다
            if (tab != 1) {
                IconButton(onClick = { month = YearMonth.parse(shown).minusMonths(1).toString() }) {
                    Icon(ChevronLeftIcon, contentDescription = "지난달", tint = m.muted)
                }
                Text(WidgetModel.monthName(shown), fontSize = 17.sp, fontWeight = FontWeight.SemiBold)
                val atCurrent = shown >= current
                IconButton(
                    onClick = { val next = YearMonth.parse(shown).plusMonths(1).toString(); month = if (next >= current) null else next },
                    enabled = !atCurrent,
                ) {
                    Icon(ChevronRightIcon, contentDescription = "다음 달", tint = if (atCurrent) m.line else m.muted)
                }
            }
        }
        val reviewCount = review?.size ?: 0
        TextTabs(listOf("전체", if (reviewCount > 0) "분류 고를 것 $reviewCount" else "분류 고를 것", "들어온 돈"), tab, onSelect = onTab)
        PullToRefreshBox(
            isRefreshing = refreshing,
            onRefresh = { scope.launch { refreshing = true; load(); refreshing = false } },
            modifier = Modifier.weight(1f),
        ) {
            val list = when (tab) {
                1 -> review
                2 -> items?.filter { it.flow == "in" }
                else -> items
            }
            val monthName = WidgetModel.monthName(shown)
            LazyColumn(Modifier.fillMaxSize()) {
                when {
                    list == null -> item { EmptyNote(if (error != null) "불러오지 못했어요" else "불러오는 중이에요", error) }
                    list.isEmpty() -> item {
                        when (tab) {
                            1 -> EmptyNote("분류 고를 결제가 없어요", "모든 결제가 분류돼 있어요")
                            2 -> EmptyNote("${monthName}에 들어온 돈이 없어요")
                            else -> EmptyNote("${monthName} 거래가 아직 없어요", "결제 알림이 오면 여기에 바로 적혀요")
                        }
                    }
                    else -> {
                        val groups = list.groupBy { kstDate(it.occurredAt) }
                        for ((day, txs) in groups) {
                            item(key = "d$day") { DayHeader(day, txs) }
                            items(txs, key = { it.id }) { t ->
                                // 전체 목록에는 AI 추천이 없어서 확인 목록의 같은 거래로 연다
                                TxRow(t, withDay = false) { selected = review?.firstOrNull { r -> r.id == t.id } ?: t }
                            }
                        }
                        item { Spacer(Modifier.height(12.dp)) }
                    }
                }
            }
        }
        Hairline()
        AddRow("현금으로 쓴 돈 직접 적기") { cashOpen = true }
    }

    selected?.let { t -> ClassifySheet(t, cats, onDismiss = { selected = null }) { edit -> submit(t, edit) } }
    if (cashOpen) {
        CashSheet(cats, onDismiss = { cashOpen = false }) {
            cashOpen = false
            reloadTick++
            scope.launch { WidgetRefresher.refresh(ctx) }
        }
    }
}

/** 서버 시각(KST)의 날짜. 형식이 다르면 오늘 */
private fun kstDate(iso: String): LocalDate =
    runCatching { OffsetDateTime.parse(iso).atZoneSameInstant(KST).toLocalDate() }.getOrElse { LocalDate.now(KST) }

/** "오늘 · 11월 8일", "어제 · 11월 7일", "11월 6일 목요일", 다른 해면 연도까지 */
private fun dayLabel(d: LocalDate, today: LocalDate = LocalDate.now(KST)): String {
    val md = "${d.monthValue}월 ${d.dayOfMonth}일"
    return when {
        d == today -> "오늘 · $md"
        d == today.minusDays(1) -> "어제 · $md"
        d.year != today.year -> "${d.year}년 $md"
        else -> "$md ${WEEKDAYS[d.dayOfWeek.value - 1]}요일"
    }
}

/** 시각 "08:42", withDay면 "오늘 08:42"·"어제 21:10"·"11/06 12:00" */
fun txTime(iso: String, withDay: Boolean): String {
    val t = runCatching { OffsetDateTime.parse(iso).atZoneSameInstant(KST) }.getOrNull() ?: return WidgetModel.shortTime(iso)
    val hm = "%02d:%02d".format(t.hour, t.minute)
    if (!withDay) return hm
    val today = LocalDate.now(KST)
    return when (t.toLocalDate()) {
        today -> "오늘 $hm"
        today.minusDays(1) -> "어제 $hm"
        else -> "${t.monthValue}/${t.dayOfMonth} $hm"
    }
}

@Composable
private fun DayHeader(day: LocalDate, txs: List<TransactionDto>) {
    val m = LocalModo.current
    val spent = txs.filter { it.flow == "out" && it.status != "excluded" }.sumOf { it.amount }
    Row(Modifier.fillMaxWidth().padding(start = Gutter, end = Gutter, top = 20.dp, bottom = 2.dp)) {
        Text(dayLabel(day), Modifier.weight(1f), fontSize = 14.sp, color = m.muted)
        if (spent > 0) Text(Money.won(spent), fontSize = 14.sp, color = m.muted)
    }
}

/** 거래 한 줄: 가게 이름, 분류·시각(분류를 골라야 하면 살구색), 금액. 박스 없이 가로로 꽉 찬다 */
@Composable
fun TxRow(t: TransactionDto, withDay: Boolean, onClick: (() -> Unit)? = null) {
    val m = LocalModo.current
    val review = t.status == "needs_review"
    val excluded = t.status == "excluded"
    val time = txTime(t.occurredAt, withDay)
    val (sub, subColor) = when {
        review -> (t.suggestion?.let { "AI 추천 ${it.category} · 눌러서 확인" } ?: "분류를 골라 주세요") + " · $time" to m.over
        excluded -> "가계부에서 뺌 · $time" to m.faint
        else -> "${t.category.ifBlank { "분류 없음" }} · $time" to m.muted
    }
    Row(
        Modifier.fillMaxWidth().clickable(enabled = onClick != null) { onClick?.invoke() }.padding(horizontal = Gutter, vertical = 12.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(2.dp)) {
            Text(t.merchant.ifBlank { "이름 없는 결제" }, fontSize = 16.sp, maxLines = 2, color = if (excluded) m.faint else MaterialTheme.colorScheme.onBackground)
            Text(sub, fontSize = 14.sp, color = subColor, maxLines = 1, overflow = TextOverflow.Ellipsis)
        }
        Text(
            (if (t.flow == "in") "+" else "") + Money.won(t.amount),
            fontSize = 16.sp,
            color = when {
                excluded -> m.faint
                t.flow == "in" -> m.income
                else -> MaterialTheme.colorScheme.onBackground
            },
        )
    }
}

/** 분류 고르기: 아래에서 올라오는 판에 분류를 줄로 늘어놓는다. 고르면 바로 저장 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun ClassifySheet(t: TransactionDto, cats: List<Category>, onDismiss: () -> Unit, onSubmit: (TransactionEdit) -> Unit) {
    val m = LocalModo.current
    var learn by remember { mutableStateOf(t.merchant.isNotBlank()) }
    // 들어온 돈이면 수입·정산·내 계좌 같은 특수 분류를 먼저 보여준다
    val ordered = if (t.flow == "in") cats.sortedBy { if (it.kind == "expense") 1 else 0 } else cats.sortedBy { if (it.kind == "expense") 0 else 1 }
    val excluded = t.status == "excluded"
    ModalBottomSheet(onDismissRequest = onDismiss, sheetState = rememberModalBottomSheetState(skipPartiallyExpanded = true), containerColor = m.bg) {
        Column(Modifier.fillMaxWidth().heightIn(max = 640.dp).verticalScroll(rememberScrollState()).navigationBarsPadding()) {
            Column(Modifier.padding(horizontal = Gutter).padding(bottom = 8.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
                Text(t.merchant.ifBlank { "이름 없는 결제" }, fontSize = 20.sp, fontWeight = FontWeight.Bold)
                Text(
                    listOfNotNull(
                        (if (t.flow == "in") "+" else "") + Money.won(t.amount),
                        txTime(t.occurredAt, withDay = true),
                        reviewReasonLabels[t.reviewReason],
                    ).joinToString(" · "),
                    fontSize = 14.sp, color = m.muted,
                )
            }
            if (t.merchant.isNotBlank()) {
                FlatRow(
                    "다음부터 이 가게는 자동으로",
                    sub = "'${t.merchant}' 결제는 고른 분류로 바로 적어요",
                    trailing = { CheckCircle(learn) },
                    onClick = { learn = !learn },
                )
            }
            Hairline()
            t.suggestion?.let { sg ->
                FlatRow("AI 추천: ${sg.category}", bold = true, leading = { Dot(m.warn) }, chevron = false, onClick = {
                    onSubmit(TransactionEdit(categoryId = sg.categoryId, rememberRule = learn))
                })
                Hairline(Modifier.padding(start = Gutter))
            }
            SectionLabel(if (t.flow == "in") "어떤 돈인가요?" else "어디에 쓴 돈인가요?")
            for (c in ordered) {
                FlatRow(
                    c.name,
                    value = if (c.id == t.categoryId && t.status != "needs_review") "지금 분류" else null,
                    chevron = false,
                    onClick = { onSubmit(TransactionEdit(categoryId = c.id, rememberRule = learn)) },
                )
            }
            Hairline(Modifier.padding(top = 8.dp))
            FlatRow(
                if (excluded) "가계부에 다시 넣기" else "이 결제는 가계부에서 빼기",
                sub = if (excluded) null else "같은 결제가 두 번 적혔거나 내 돈이 아닐 때",
                titleColor = m.muted,
                chevron = false,
                onClick = { onSubmit(TransactionEdit(status = if (excluded) "confirmed" else "excluded")) },
            )
            Spacer(Modifier.height(16.dp))
        }
    }
}

/** 현금으로 쓴 돈 적기: 금액, 어디서(선택), 분류 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun CashSheet(cats: List<Category>, onDismiss: () -> Unit, onSaved: () -> Unit) {
    val ctx = LocalContext.current
    val app = MyHomeApp.instance
    val scope = rememberCoroutineScope()
    val m = LocalModo.current
    var amount by remember { mutableStateOf("") }
    var where by remember { mutableStateOf("") }
    var catId by remember { mutableStateOf<Long?>(null) }
    var busy by remember { mutableStateOf(false) }
    var err by remember { mutableStateOf<String?>(null) }
    val won = amount.toLongOrNull() ?: 0L
    ModalBottomSheet(onDismissRequest = onDismiss, sheetState = rememberModalBottomSheetState(skipPartiallyExpanded = true), containerColor = m.bg) {
        Column(Modifier.fillMaxWidth().heightIn(max = 680.dp).imePadding().navigationBarsPadding()) {
            Column(Modifier.weight(1f, fill = false).verticalScroll(rememberScrollState())) {
                Text("현금으로 쓴 돈", Modifier.padding(horizontal = Gutter), fontSize = 20.sp, fontWeight = FontWeight.Bold)
                Column(Modifier.padding(horizontal = Gutter, vertical = 8.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
                    LineField(amount, { amount = it.filter(Char::isDigit).take(10) }, if (won > 0) "금액 · ${Money.won(won)}" else "금액(원)", keyboardType = KeyboardType.Number)
                    LineField(where, { where = it.take(40) }, "어디서 썼나요 (안 써도 돼요)")
                }
                SectionLabel("분류")
                for (c in cats.filter { it.kind == "expense" }) {
                    FlatRow(c.name, chevron = false, trailing = { CheckCircle(catId == c.id) }, onClick = { catId = c.id })
                }
                err?.let { Text(it, Modifier.padding(horizontal = Gutter, vertical = 8.dp), fontSize = 14.sp, color = m.over) }
            }
            PrimaryButton("적기", Modifier.padding(horizontal = Gutter, vertical = 12.dp), enabled = won > 0 && catId != null && !busy) {
                val id = catId ?: return@PrimaryButton
                busy = true
                scope.launch {
                    val r = withContext(Dispatchers.IO) {
                        app.api.createTransaction(ManualTransaction(amount = won, merchant = where.trim().ifBlank { "현금" }, categoryId = id))
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
