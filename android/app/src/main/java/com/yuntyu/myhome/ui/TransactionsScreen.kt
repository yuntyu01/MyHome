package com.yuntyu.myhome.ui

import android.widget.Toast
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
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.background
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.ui.draw.clip
import androidx.compose.ui.focus.focusRequester
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.pulltorefresh.PullToRefreshBox
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
import com.yuntyu.myhome.core.TxMarks
import com.yuntyu.myhome.core.RunningBudget
import com.yuntyu.myhome.core.WidgetModel
import com.yuntyu.myhome.core.api.ApiResult
import com.yuntyu.myhome.core.CategoryTree
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

/** 한 달 목록을 한 번에 받는 수. 이만큼 오면 잘렸을 수 있어서 남은 돈을 안 보인다 */
private const val TX_LIMIT = 500

private val reviewReasonLabels = mapOf(
    "no_merchant" to "가게 이름이 없어요", "no_rule" to "처음 보는 가게예요", "payment_gateway" to "결제대행사 이름으로 찍혔어요",
    "transfer" to "송금이에요", "deposit" to "어떤 입금인지 골라 주세요", "unknown_kind" to "어떤 거래인지 몰라요",
    "cancel_unmatched" to "원래 결제를 못 찾은 취소예요", "cancel_partial" to "부분 취소예요",
    "foreign_currency" to "외화 결제예요(금액은 웹에서 고쳐 주세요)",
)

/**
 * 거래: 달 넘기기, 탭(전체 · 분류 고를 것 · 들어온 돈), 날짜별 줄. 줄을 누르면 분류를 고른다(확인 필요 건이면 AI 추천도).
 * 아래에 "현금으로 쓴 돈 직접 적기". 떠 있는 버튼 대신 목록 아래 줄로 둔다(내용을 가리지 않게).
 * 돋보기: 가게 이름·메모로 전체 기간을 찾는다(서버 q). 홈에서 분류를 눌러 들어오면 그 분류(안 분류 포함)만 본다.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun TransactionsScreen(
    modifier: Modifier,
    resumeTick: Int,
    tab: Int,
    onTab: (Int) -> Unit,
    openTxId: Long?,
    onOpened: () -> Unit,
    categoryFilter: Long? = null,
    onCategoryFilter: (Long?) -> Unit = {},
) {
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
    // 검색: null이면 닫힘. 글자가 있으면 전체 기간에서 찾은 것(found)을 보여준다
    var query by rememberSaveable { mutableStateOf<String?>(null) }
    var found by remember { mutableStateOf<List<TransactionDto>?>(null) }
    val searching = !query.isNullOrBlank()

    suspend fun load() {
        val list = withContext(Dispatchers.IO) { app.api.transactions(TX_LIMIT, month = shown) }
        val q = withContext(Dispatchers.IO) { app.api.reviewQueue() }
        val c = withContext(Dispatchers.IO) { app.api.categories() }
        if (list is ApiResult.Ok) { items = list.value.transactions; error = null } else error = list.plain()
        if (q is ApiResult.Ok) review = q.value.transactions
        if (c is ApiResult.Ok) cats = c.value.categories.filter { !it.archived }
    }
    suspend fun search(text: String) {
        val r = withContext(Dispatchers.IO) {
            app.api.transactions(SEARCH_LIMIT, from = SEARCH_FROM, to = LocalDate.now(KST).toString(), search = text)
        }
        if (r is ApiResult.Ok) { found = r.value.transactions; error = null } else { found = emptyList(); error = r.plain() }
    }
    LaunchedEffect(shown, resumeTick, reloadTick) { load() }
    // 글자를 칠 때마다 부르지 않게 잠깐 기다렸다가 찾는다
    LaunchedEffect(query, reloadTick) {
        val text = query?.trim().orEmpty()
        if (text.isEmpty()) { found = null; return@LaunchedEffect }
        kotlinx.coroutines.delay(300)
        search(text)
    }
    // 위젯·홈에서 거래를 눌러 들어왔으면 바로 그 거래를 연다
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

    // 고른 분류와 그 안 분류들(폴더면 안까지)
    val tree = remember(cats) { CategoryTree(cats) }
    val catIds = remember(tree, categoryFilter) { categoryFilter?.let { descendantsOf(tree, it) } }
    val catName = categoryFilter?.let { id -> cats.firstOrNull { it.id == id }?.name ?: "고른 분류" }

    Column(modifier.fillMaxSize()) {
        PageTitle("거래") {
            IconButton(onClick = { query = if (query == null) "" else null }) {
                Icon(if (query == null) SearchIcon else CloseIcon, contentDescription = if (query == null) "거래 찾기" else "찾기 닫기", tint = MaterialTheme.colorScheme.onBackground)
            }
        }
        // 미분류·찾기는 달과 상관없이 보여주므로 달 넘기기를 숨긴다
        if (tab != 1 && !searching) {
            val atCurrent = shown >= current
            val shownItems = items?.let { l -> if (catIds != null) l.filter { it.categoryId in catIds } else l }
            val total = shownItems?.filter { it.status != "excluded" && it.flow == (if (tab == 2) "in" else "out") }?.sumOf { it.amount }
            TxMonthRow(
                WidgetModel.monthName(shown), total?.let { (if (tab == 2) "+" else "") + Money.won(it) }, atCurrent,
                onPrev = { month = YearMonth.parse(shown).minusMonths(1).toString() },
                onNext = { val next = YearMonth.parse(shown).plusMonths(1).toString(); month = if (next >= current) null else next },
                onCurrent = { month = null },
            )
        }
        if (query != null) SearchField(query.orEmpty()) { query = it }
        val reviewCount = review?.size ?: 0
        TextTabs(listOf("전체", if (reviewCount > 0) "미분류 $reviewCount" else "미분류", "수입"), tab, onSelect = onTab)
        if (catName != null) FilterChipRow("$catName 거래만 보는 중") { onCategoryFilter(null) }
        PullToRefreshBox(
            isRefreshing = refreshing,
            onRefresh = { scope.launch { refreshing = true; load(); query?.trim()?.takeIf { it.isNotEmpty() }?.let { search(it) }; refreshing = false } },
            modifier = Modifier.weight(1f),
        ) {
            val needle = query?.trim().orEmpty()
            val base = when {
                tab == 1 -> review?.let { r -> if (searching) r.filter { matches(it, needle) } else r }
                searching -> found
                else -> items
            }
            val list = base
                ?.let { l -> if (tab == 2) l.filter { it.flow == "in" } else l }
                ?.let { l -> if (catIds != null && tab != 1) l.filter { it.categoryId in catIds } else l }
            val monthName = WidgetModel.monthName(shown)
            // 이번 달 전체 목록을 받았으면 결제마다 "그 뒤 남은 돈"(예산이 있을 때만). 지난달·잘린 목록은 셈이 안 맞아서 안 보인다
            val budget = summary.summary?.totals?.budget ?: 0L
            val left = remember(items, budget, cats, month) {
                val all = items
                if (all == null || month != null || budget <= 0 || all.size >= TX_LIMIT) emptyMap()
                else RunningBudget.remainingAfter(all, budget, cats.filter { it.code == "settlement" }.map { it.id }.toSet())
            }
            LazyColumn(Modifier.fillMaxSize()) {
                when {
                    searching && list == null -> item { EmptyNote("찾는 중이에요") }
                    list == null -> item { EmptyNote(if (error != null) "불러오지 못했어요" else "불러오는 중이에요", error) }
                    list.isEmpty() -> item {
                        when {
                            searching -> EmptyNote("'$needle'이(가) 든 거래가 없어요", "가게 이름과 메모에서 찾아요")
                            catName != null -> EmptyNote("${monthName}에 $catName 거래가 없어요")
                            tab == 1 -> EmptyNote("미분류 결제가 없어요")
                            tab == 2 -> EmptyNote("${monthName}에 들어온 돈이 없어요")
                            else -> EmptyNote("${monthName} 거래가 아직 없어요")
                        }
                    }
                    else -> {
                        // 찾기·분류로 거른 때는 합계를 맨 위에(몇 건, 얼마)
                        if (searching || catName != null) item(key = "sum") { TotalsLine(list, if (searching) "전체 기간" else monthName, searching && list.size >= SEARCH_LIMIT) }
                        // 전체 목록에는 AI 추천이 없어서 확인 목록의 같은 거래로 연다
                        txDays(list, left) { t -> selected = review?.firstOrNull { r -> r.id == t.id } ?: t }
                    }
                }
                // 현금 지출은 목록 끝에(떠 있는 버튼 대신)
                if (!searching && tab != 1) item(key = "cash") { CashRow { cashOpen = true } }
                item { Spacer(Modifier.height(12.dp)) }
            }
        }
    }

    selected?.let { t ->
        ClassifySheet(
            t, cats,
            onDismiss = { selected = null },
            onSearch = { text -> selected = null; onTab(0); query = text },
        ) { edit -> submit(t, edit) }
    }
    if (cashOpen) {
        CashSheet(cats, onDismiss = { cashOpen = false }) {
            cashOpen = false
            reloadTick++
            scope.launch { WidgetRefresher.refresh(ctx) }
        }
    }
}

/** 찾기는 서버에서 전체 기간을 이만큼까지 받는다 */
private const val SEARCH_LIMIT = 200
private const val SEARCH_FROM = "2000-01-01"

/** 분류 고를 것 목록은 폰에서 거른다(서버 q와 같게 가게 이름·메모, 대소문자 무시) */
private fun matches(t: TransactionDto, needle: String): Boolean =
    needle.isEmpty() || t.merchant.contains(needle, ignoreCase = true) || t.memo.contains(needle, ignoreCase = true)

/** 분류와 그 안 분류 전부 */
private fun descendantsOf(tree: CategoryTree, id: Long): Set<Long> {
    val out = mutableSetOf(id)
    for (c in tree.children(id)) out += descendantsOf(tree, c.id)
    return out
}

/** 찾기 칸: 밑줄 입력칸. 빈 칸으로 열리면 바로 칠 수 있게 포커스(가게 이름을 넣어 열면 키보드를 띄우지 않는다) */
@Composable
private fun SearchField(value: String, onChange: (String) -> Unit) {
    val focus = remember { androidx.compose.ui.focus.FocusRequester() }
    val startEmpty = remember { value.isEmpty() }
    LaunchedEffect(Unit) { if (startEmpty) runCatching { focus.requestFocus() } }
    Column(Modifier.padding(horizontal = Gutter)) {
        LineField(value, { onChange(it.take(40)) }, "가게 이름이나 메모로 찾기 (전체 기간)", Modifier.focusRequester(focus))
    }
}

/** "‹ 11월 ›"와 오른쪽 "총 ○원". 달 이름을 누르면 이번 달로 */
@Composable
internal fun TxMonthRow(label: String, total: String?, atCurrent: Boolean, onPrev: () -> Unit, onNext: () -> Unit, onCurrent: () -> Unit) {
    val m = LocalModo.current
    val fg = MaterialTheme.colorScheme.onBackground
    Row(Modifier.fillMaxWidth().padding(start = Gutter - 12.dp, end = Gutter, bottom = 6.dp), verticalAlignment = Alignment.CenterVertically) {
        IconButton(onClick = onPrev) { Icon(ChevronLeftIcon, contentDescription = "지난달", tint = fg) }
        Text(
            label,
            Modifier.tap(enabled = !atCurrent, onClickLabel = "이번 달로", onClick = onCurrent).padding(vertical = 6.dp),
            fontSize = 22.sp, fontWeight = FontWeight.Bold,
        )
        IconButton(onClick = onNext, enabled = !atCurrent) { Icon(ChevronRightIcon, contentDescription = "다음 달", tint = if (atCurrent) m.line else fg) }
        Spacer(Modifier.weight(1f))
        if (total != null) Text("총 $total", fontSize = 16.sp, color = m.muted, maxLines = 1)
    }
}

/** 날짜별 묶음: 날짜 머리(날짜 사이에 선) + 그날 결제들 */
internal fun androidx.compose.foundation.lazy.LazyListScope.txDays(list: List<TransactionDto>, left: Map<Long, Long>, onOpen: (TransactionDto) -> Unit) {
    val groups = list.groupBy { kstDate(it.occurredAt) }
    groups.entries.forEachIndexed { i, (day, txs) ->
        item(key = "d$day") {
            if (i > 0) Hairline(Modifier.padding(start = Gutter, end = Gutter, top = 10.dp))
            DayHeader(day, txs)
        }
        items(txs, key = { it.id }) { t -> TxRow(t, withDay = false, remaining = left[t.id]) { onOpen(t) } }
    }
}

/** 목록 끝 "+ 현금 지출 기록"(가운데) */
@Composable
internal fun CashRow(onClick: () -> Unit) {
    val m = LocalModo.current
    Column {
        Hairline(Modifier.padding(start = Gutter, end = Gutter, top = 10.dp))
        Row(
            Modifier.fillMaxWidth().tap(role = androidx.compose.ui.semantics.Role.Button, onClick = onClick).padding(vertical = 22.dp),
            horizontalArrangement = Arrangement.Center,
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Icon(PlusIcon, contentDescription = null, tint = m.muted, modifier = Modifier.size(20.dp))
            Text("현금 지출 기록", Modifier.padding(start = 6.dp), fontSize = 16.sp, color = m.muted)
        }
    }
}

/** 거른 조건을 알리는 줄. 오른쪽 ×로 푼다 */
@Composable
private fun FilterChipRow(text: String, onClear: () -> Unit) {
    val m = LocalModo.current
    Row(
        Modifier.fillMaxWidth().tap(onClickLabel = "거르기 풀기", onClick = onClear).padding(horizontal = Gutter, vertical = 10.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Text(text, Modifier.weight(1f), fontSize = 14.sp, color = MaterialTheme.colorScheme.onBackground, fontWeight = FontWeight.SemiBold)
        Text("모두 보기", fontSize = 14.sp, color = m.muted)
        Icon(CloseIcon, contentDescription = null, tint = m.muted, modifier = Modifier.padding(start = 4.dp).size(16.dp))
    }
    Hairline()
}

/** 몇 건 · 쓴 돈 · 들어온 돈(가계부에서 뺀 건 빼고) */
@Composable
private fun TotalsLine(list: List<TransactionDto>, scope: String, capped: Boolean) {
    val m = LocalModo.current
    val live = list.filter { it.status != "excluded" }
    val out = live.filter { it.flow == "out" }.sumOf { it.amount }
    val inn = live.filter { it.flow == "in" }.sumOf { it.amount }
    Text(
        listOfNotNull(
            "$scope ${list.size}건" + if (capped) "(최근 것만)" else "",
            if (out > 0) "쓴 돈 ${Money.won(out)}" else null,
            if (inn > 0) "들어온 돈 ${Money.won(inn)}" else null,
        ).joinToString(" · "),
        Modifier.padding(start = Gutter, end = Gutter, top = 14.dp),
        fontSize = 14.sp, color = m.muted,
    )
}

/** 서버 시각(KST)의 날짜. 형식이 다르면 오늘 */
private fun kstDate(iso: String): LocalDate =
    runCatching { OffsetDateTime.parse(iso).atZoneSameInstant(KST).toLocalDate() }.getOrElse { LocalDate.now(KST) }

/** 날짜 머리: 큰 글자("오늘", "어제", "11월 6일")와 옆 작은 글자("11월 8일", "목요일") */
internal fun dayParts(d: LocalDate, today: LocalDate = LocalDate.now(KST)): Pair<String, String> {
    val md = "${d.monthValue}월 ${d.dayOfMonth}일"
    val wd = "${WEEKDAYS[d.dayOfWeek.value - 1]}요일"
    return when {
        d == today -> "오늘" to md
        d == today.minusDays(1) -> "어제" to md
        d.year != today.year -> "${d.year}년 $md" to wd
        else -> md to wd
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
internal fun DayHeader(day: LocalDate, txs: List<TransactionDto>) {
    val m = LocalModo.current
    val spent = txs.filter { it.flow == "out" && it.status != "excluded" }.sumOf { it.amount }
    val (big, small) = dayParts(day)
    // 날짜와 그날 나간 돈은 크게(사용자 요청, 2026-11-10)
    Row(Modifier.fillMaxWidth().padding(start = Gutter, end = Gutter, top = 22.dp, bottom = 4.dp), verticalAlignment = Alignment.Bottom) {
        Text(big, fontSize = 20.sp, fontWeight = FontWeight.Bold)
        Text(small, Modifier.weight(1f).padding(start = 8.dp, bottom = 2.dp), fontSize = 14.sp, color = m.muted, maxLines = 1)
        if (spent > 0) Text(Money.won(spent), fontSize = 18.sp, fontWeight = FontWeight.Bold)
    }
}

/** 거래 한 줄: 가게 이름, 분류·시각(분류를 골라야 하면 살구색), 금액. 박스 없이 가로로 꽉 찬다 */
@Composable
fun TxRow(t: TransactionDto, withDay: Boolean, remaining: Long? = null, onClick: (() -> Unit)? = null) {
    val m = LocalModo.current
    val review = t.status == "needs_review"
    val excluded = t.status == "excluded"
    val time = txTime(t.occurredAt, withDay)
    val (sub, subColor) = when {
        review -> (t.suggestion?.let { "AI 추천 ${it.category}" } ?: "분류 고르기") + " · $time" to m.over
        excluded -> "가계부에서 뺌 · $time" to m.faint
        else -> "${t.category.ifBlank { "분류 없음" }} · $time" to m.muted
    }.let { (text, color) -> (if (t.memo.isNotBlank()) "$text · ${t.memo}" else text) to color }
    Row(
        Modifier.fillMaxWidth().tap(enabled = onClick != null) { onClick?.invoke() }.padding(horizontal = Gutter, vertical = 12.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        // 앞 동그라미: 아는 가게면 가게 색·글자, 아니면 분류 그림(토스 거래 목록처럼)
        TxMarkView(remember(t.merchant, t.category, t.status, t.direction, t.flow) { TxMarks.of(t) }, dim = excluded)
        Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(2.dp)) {
            Text(t.merchant.ifBlank { "이름 없는 결제" }, fontSize = 17.sp, maxLines = 2, color = if (excluded) m.faint else MaterialTheme.colorScheme.onBackground)
            Text(sub, fontSize = 14.sp, color = subColor, maxLines = 1, overflow = TextOverflow.Ellipsis)
        }
        // 나간 돈은 크게, 그 뒤 이번 달 남은 돈은 밑에 작게(사용자 요청, 2026-11-10)
        Column(horizontalAlignment = Alignment.End, verticalArrangement = Arrangement.spacedBy(2.dp)) {
            Text(
                (if (t.flow == "in") "+" else "") + Money.won(t.amount),
                fontSize = 17.sp,
                fontWeight = if (excluded) FontWeight.Normal else FontWeight.SemiBold,
                color = when {
                    excluded -> m.faint
                    t.flow == "in" -> m.income
                    else -> MaterialTheme.colorScheme.onBackground
                },
            )
            if (remaining != null) {
                Text(
                    if (remaining >= 0) "${Money.won(remaining)} 남음" else "${Money.won(-remaining)} 넘음",
                    fontSize = 12.sp, color = if (remaining >= 0) m.faint else m.over, maxLines = 1,
                )
            }
        }
    }
}

/** 분류 고르기: 아래에서 올라오는 판에 분류를 줄로 늘어놓는다. 고르면 바로 저장 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun ClassifySheet(
    t: TransactionDto,
    cats: List<Category>,
    onDismiss: () -> Unit,
    /** "이 가게 거래 모두 보기": 판을 닫고 가게 이름으로 찾는다 */
    onSearch: (String) -> Unit,
    onSubmit: (TransactionEdit) -> Unit,
) {
    val m = LocalModo.current
    var learn by remember { mutableStateOf(t.merchant.isNotBlank()) }
    var memoOpen by remember { mutableStateOf(false) }
    var fixOpen by remember { mutableStateOf(false) }
    // 폴더 순서대로 들여 쓴다. 들어온 돈이면 수입·정산·내 계좌 같은 특수 분류를 먼저 보여준다
    val tree = remember(cats) { CategoryTree(cats) }
    val (exp, other) = tree.preorder().partition { it.first.kind == "expense" }
    val ordered = if (t.flow == "in") other + exp else exp + other
    val excluded = t.status == "excluded"
    ModoBottomSheet(onDismissRequest = onDismiss) {
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
                if (t.memo.isNotBlank()) Text("메모 · ${t.memo}", fontSize = 14.sp, color = MaterialTheme.colorScheme.onBackground)
                // 자주 쓰는 일: 메모, 이름·금액 고치기, 같은 가게 거래 보기
                Row(Modifier.padding(top = 8.dp), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    SheetPill(if (t.memo.isBlank()) "메모 남기기" else "메모 고치기") { memoOpen = true }
                    SheetPill("이름·금액 고치기") { fixOpen = true }
                    if (t.merchant.isNotBlank()) SheetPill("이 가게 거래") { onSearch(t.merchant) }
                }
            }
            if (t.merchant.isNotBlank()) {
                FlatRow(
                    "이 가게는 항상 이 분류로",
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
            for ((c, depth) in ordered) {
                FlatRow(
                    c.name,
                    value = if (c.id == t.categoryId && t.status != "needs_review") "지금 분류" else null,
                    bold = tree.children(c.id).isNotEmpty(),
                    leading = indentLead(depth),
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
    if (memoOpen) {
        var memo by remember { mutableStateOf(t.memo) }
        ModoAlertDialog(
            onDismissRequest = { memoOpen = false },
            title = { Text("메모") },
            text = { LineField(memo, { memo = it.take(MEMO_MAX) }, "예: 점심 더치페이, 선물", maxLines = 3) },
            confirmButton = {
                TextButton(onClick = { memoOpen = false; onSubmit(TransactionEdit(memo = memo.trim())) }, enabled = memo.trim() != t.memo) { Text("저장") }
            },
            dismissButton = { TextButton(onClick = { memoOpen = false }) { Text("취소") } },
        )
    }
    if (fixOpen) {
        var name by remember { mutableStateOf(t.merchant) }
        var amount by remember { mutableStateOf(t.amount.toString()) }
        val won = amount.toLongOrNull() ?: 0L
        val edit = TransactionEdit(
            merchant = name.trim().takeIf { it.isNotEmpty() && it != t.merchant },
            amount = won.takeIf { it > 0 && it != t.amount },
        )
        ModoAlertDialog(
            onDismissRequest = { fixOpen = false },
            title = { Text("이름·금액 고치기") },
            text = {
                Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                    LineField(name, { name = it.take(MERCHANT_MAX) }, "가게 이름")
                    LineField(amount, { amount = it.filter(Char::isDigit).take(10) }, if (won > 0) "금액 · ${Money.won(won)}" else "금액(원)", keyboardType = KeyboardType.Number)
                }
            },
            confirmButton = {
                TextButton(onClick = { fixOpen = false; onSubmit(edit) }, enabled = edit.merchant != null || edit.amount != null) { Text("저장") }
            },
            dismissButton = { TextButton(onClick = { fixOpen = false }) { Text("취소") } },
        )
    }
}

/** 메모·가게 이름 글자 수(서버는 따로 막지 않아서 앱에서 적당히 자른다) */
private const val MEMO_MAX = 200
private const val MERCHANT_MAX = 60

/** 판 안의 작은 알약 버튼 */
@Composable
private fun SheetPill(text: String, onClick: () -> Unit) {
    val m = LocalModo.current
    androidx.compose.foundation.text.selection.DisableSelection {
        Text(
            text,
            Modifier.clip(RoundedCornerShape(12.dp)).background(m.pill).tap(role = androidx.compose.ui.semantics.Role.Button, onClick = onClick)
                .padding(horizontal = 12.dp, vertical = 8.dp),
            fontSize = 14.sp, color = MaterialTheme.colorScheme.onBackground, maxLines = 1,
        )
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
    ModoBottomSheet(onDismissRequest = onDismiss) {
        Column(Modifier.fillMaxWidth().heightIn(max = 680.dp).imePadding().navigationBarsPadding()) {
            Column(Modifier.weight(1f, fill = false).verticalScroll(rememberScrollState())) {
                Text("현금으로 쓴 돈", Modifier.padding(horizontal = Gutter), fontSize = 20.sp, fontWeight = FontWeight.Bold)
                Column(Modifier.padding(horizontal = Gutter, vertical = 8.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
                    LineField(amount, { amount = it.filter(Char::isDigit).take(10) }, if (won > 0) "금액 · ${Money.won(won)}" else "금액(원)", keyboardType = KeyboardType.Number)
                    LineField(where, { where = it.take(40) }, "어디서 썼나요 (안 써도 돼요)")
                }
                SectionLabel("분류")
                val tree = remember(cats) { CategoryTree(cats) }
                for ((c, depth) in tree.preorder().filter { it.first.kind == "expense" }) {
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
