package com.yuntyu.myhome.ui

import android.Manifest
import android.os.Build
import android.widget.Toast
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
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
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.material3.TextButton
import androidx.compose.material3.pulltorefresh.PullToRefreshBox
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.rotate
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.LocalUriHandler
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import com.yuntyu.myhome.MyHomeApp
import com.yuntyu.myhome.auth.AppLink
import com.yuntyu.myhome.core.Money
import com.yuntyu.myhome.core.WidgetModel
import com.yuntyu.myhome.core.api.CategoryBudget
import com.yuntyu.myhome.core.api.Summary
import com.yuntyu.myhome.core.api.TransactionDto
import com.yuntyu.myhome.data.SummaryRepository
import com.yuntyu.myhome.widget.WidgetRefresher
import kotlinx.coroutines.launch
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.IntrinsicSize
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.ui.draw.clip
import androidx.compose.ui.draw.drawBehind
import androidx.compose.ui.semantics.Role
import com.yuntyu.myhome.core.api.ApiResult
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import java.time.YearMonth

/**
 * 앱 홈. 처음 설정(로그인 → 알림 읽기 허용 → 앱 고르기)이 덜 끝났으면 할 일을 번호 순서로 보여주고 지금 할 일 하나만 버튼으로 둔다.
 * 다 끝났으면 이번 달 쓸 수 있는 돈, 해야 할 일 한 줄, 분류별 남은 돈, 최근 결제를 보여준다.
 */
@Composable
fun HomeScreen(
    modifier: Modifier,
    resumeTick: Int,
    onNavigate: (Screen) -> Unit,
    onTransactions: (tab: Int) -> Unit,
    /** 분류 줄을 누르면 그 분류 거래만 */
    onCategory: (Long) -> Unit = {},
    /** 최근 결제를 누르면 그 거래를 바로 연다 */
    onOpenTx: (TransactionDto) -> Unit = {},
) {
    val ctx = LocalContext.current
    val app = MyHomeApp.instance
    val state by app.summaryRepo.state.collectAsState()
    val settings by app.settings.state.collectAsState()
    val hasToken by app.tokenStore.hasToken.collectAsState()
    val listenerOn = remember(resumeTick) { PermissionStatus.listenerEnabled(ctx) }
    val batteryOk = remember(resumeTick) { PermissionStatus.batteryUnrestricted(ctx) }
    val authProblem = remember(resumeTick, hasToken, state.updatedAt) { app.settings.authProblem }
    val setupDone = hasToken && listenerOn && settings.selectedPackages.isNotEmpty()
    // 화면에 들어올 때마다 새로 받고 위젯도 맞춘다
    LaunchedEffect(resumeTick, hasToken) { if (hasToken) WidgetRefresher.refresh(ctx) }

    // 알림 읽기를 허용하고 돌아오면 배터리 절전 예외를 이어서 묻는다(한 흐름으로)
    var askBattery by rememberSaveable { mutableStateOf(false) }
    LaunchedEffect(resumeTick, listenerOn) {
        if (askBattery && listenerOn) {
            askBattery = false
            if (!batteryOk) PermissionStatus.requestBatteryExemption(ctx)
        }
    }
    // 처음 설정을 다 마치면 앱 알림(예산·매매 실패 알림) 권한을 한 번만 묻는다
    val notifLauncher = rememberLauncherForActivityResult(ActivityResultContracts.RequestPermission()) { }
    LaunchedEffect(setupDone) {
        if (setupDone && Build.VERSION.SDK_INT >= 33 && !app.settings.notifAsked && !PermissionStatus.canPostNotifications(ctx)) {
            app.settings.notifAsked = true
            notifLauncher.launch(Manifest.permission.POST_NOTIFICATIONS)
        }
    }
    val login = {
        if (!AppLink.start(ctx)) {
            Toast.makeText(ctx, "서버 주소가 없어요. 문제 해결에서 넣어 주세요", Toast.LENGTH_LONG).show()
            onNavigate(Screen.STATUS)
        }
    }

    if (!setupDone) {
        SetupSteps(
            modifier,
            hasToken = hasToken,
            listenerOn = listenerOn,
            selectedCount = settings.selectedPackages.size,
            budgetSet = (state.summary?.totals?.budget ?: 0L) > 0,
            onLogin = login,
            onListener = { askBattery = true; PermissionStatus.openListenerSettings(ctx) },
            onApps = { onNavigate(Screen.APPS) },
            onBudget = { onNavigate(Screen.BUDGETS) },
        )
        return
    }

    val problems = buildList {
        if (authProblem != null) add(Problem("로그인이 풀렸어요", "눌러서 다시 로그인", login))
        if (!batteryOk) add(Problem("절전 예외가 꺼져 있어요", "결제 알림을 놓칠 수 있어요") { PermissionStatus.requestBatteryExemption(ctx) })
    }
    // 위 "11월 ⌄"로 지난 기간을 볼 수 있다(null = 이번 기간, 위젯·알림은 늘 이번 기간)
    var month by rememberSaveable { mutableStateOf<String?>(null) }
    var past by remember { mutableStateOf<SummaryRepository.State?>(null) }
    LaunchedEffect(month) {
        val mo = month
        if (mo == null) { past = null; return@LaunchedEffect }
        past = SummaryRepository.State(null, 0, null)
        val r = withContext(Dispatchers.IO) { app.api.summary(recent = 1, month = mo) }
        past = if (r is ApiResult.Ok) SummaryRepository.State(r.value, System.currentTimeMillis(), null) else SummaryRepository.State(null, 0, r.plain())
    }
    val current = state.summary?.period?.label
    InUseHome(
        modifier, if (month == null) state else past ?: SummaryRepository.State(null, 0, null), problems, onNavigate, onTransactions, onCategory, onOpenTx, settings.serverUrl,
        currentMonth = current, pastMonth = month != null, onMonth = { mo -> month = if (mo == current) null else mo },
        saveTotal = { mo, amount ->
            withContext(Dispatchers.IO) { app.api.putTotalBudget(mo, amount) }.let { r ->
                if (r is ApiResult.Ok) { WidgetRefresher.refresh(ctx); null } else r.plain()
            }
        },
        loadMonthTx = { mo ->
            withContext(Dispatchers.IO) { app.api.transactions(limit = 300, month = mo) }.let { if (it is ApiResult.Ok) it.value.transactions else null }
        },
    )
}

internal class Problem(val title: String, val sub: String, val action: () -> Unit)

private class Step(val title: String, val sub: String, val done: Boolean, val button: String, val optional: Boolean = false, val action: () -> Unit)

@Composable
private fun SetupSteps(
    modifier: Modifier,
    hasToken: Boolean,
    listenerOn: Boolean,
    selectedCount: Int,
    budgetSet: Boolean,
    onLogin: () -> Unit,
    onListener: () -> Unit,
    onApps: () -> Unit,
    onBudget: () -> Unit,
) {
    val m = LocalModo.current
    val steps = listOf(
        Step("로그인", if (hasToken) "연결됐어요" else "이메일 코드로 로그인", hasToken, "이메일로 로그인하기", action = onLogin),
        Step(
            "결제 알림 읽기 허용",
            if (listenerOn) "허용했어요" else "설정에서 MyHome 켜기",
            listenerOn, "알림 읽기 허용하기", action = onListener,
        ),
        Step(
            "카드·은행 앱 고르기",
            if (selectedCount > 0) "${selectedCount}개 골랐어요" else "KB Pay, 토스 등",
            selectedCount > 0, "카드·은행 앱 고르기", action = onApps,
        ),
        Step("한 달 예산 정하기", if (budgetSet) "정했어요" else "나중에 해도 돼요", budgetSet, "예산 정하기", optional = true, action = onBudget),
    )
    val current = steps.indexOfFirst { !it.done && !it.optional }.coerceAtLeast(0)
    Column(modifier.fillMaxSize()) {
        Column(Modifier.weight(1f).verticalScroll(rememberScrollState())) {
            Column(Modifier.padding(start = Gutter, end = Gutter, top = 36.dp), verticalArrangement = Arrangement.spacedBy(10.dp)) {
                Text("쓴 돈을 자동으로\n적어 드릴게요", fontSize = 28.sp, fontWeight = FontWeight.Bold, lineHeight = 38.sp)
                Text("세 가지만 하면 끝나요", fontSize = 16.sp, color = m.muted, lineHeight = 24.sp)
            }
            Spacer(Modifier.height(28.dp))
            steps.forEachIndexed { i, st ->
                StepRow(i + 1, st, current = i == current)
                if (i < steps.lastIndex) Hairline(Modifier.padding(horizontal = Gutter))
            }
        }
        PrimaryButton(steps[current].button, Modifier.padding(horizontal = Gutter, vertical = 12.dp)) { steps[current].action() }
    }
}

@Composable
private fun StepRow(n: Int, st: Step, current: Boolean) {
    val m = LocalModo.current
    Row(
        Modifier.fillMaxWidth().tap(enabled = !st.done || st.optional, onClick = st.action).padding(horizontal = Gutter, vertical = 16.dp),
        horizontalArrangement = Arrangement.spacedBy(16.dp),
    ) {
        Box(Modifier.width(24.dp).padding(top = 1.dp), contentAlignment = Alignment.TopCenter) {
            if (st.done) {
                Icon(CheckIcon, contentDescription = "완료", tint = m.muted, modifier = Modifier.size(22.dp))
            } else {
                Text("$n", fontSize = 17.sp, fontWeight = if (current) FontWeight.Bold else FontWeight.Normal, color = if (current) MaterialTheme.colorScheme.onBackground else m.faint)
            }
        }
        Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(3.dp)) {
            Text(
                st.title,
                fontSize = 17.sp,
                fontWeight = if (current) FontWeight.Bold else FontWeight.Normal,
                color = when {
                    current -> MaterialTheme.colorScheme.onBackground
                    else -> m.muted
                },
            )
            Text(st.sub, fontSize = 14.sp, color = if (current) m.muted else m.faint, lineHeight = 21.sp)
        }
    }
}

/**
 * 쓰는 중인 홈(2026-11-10 다듬음): 위 "11월 ⌄"(지난 기간 보기)·"투자 ↗", 카드(쓸 수 있는 돈, 막대, 예산 | 사용),
 * 해야 할 일, "분류별 남은 돈"·"최근 거래" 묶음(굵은 머리 + 전체 보기).
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
internal fun InUseHome(
    modifier: Modifier,
    state: SummaryRepository.State,
    problems: List<Problem>,
    onNavigate: (Screen) -> Unit,
    onTransactions: (Int) -> Unit,
    onCategory: (Long) -> Unit,
    onOpenTx: (TransactionDto) -> Unit,
    serverUrl: String?,
    /** 이번 기간("2026-11"). 달 고르기 목록의 맨 위 */
    currentMonth: String? = state.summary?.period?.label,
    /** 지난 기간을 보는 중이면 할 일·최근 거래를 숨긴다 */
    pastMonth: Boolean = false,
    onMonth: (String) -> Unit = {},
    /** 분류 박스를 펼칠 때 그 기간 결제를 받는다(못 받으면 null → 요약의 최근 결제만) */
    loadMonthTx: suspend (String) -> List<TransactionDto>? = { null },
    /** 한 달 전체 예산 저장(기간, 금액 또는 null = 분류 합계). 실패하면 이유 */
    saveTotal: suspend (String, Long?) -> String? = { _, _ -> "저장할 수 없어요" },
) {
    val ctx = LocalContext.current
    val scope = rememberCoroutineScope()
    val m = LocalModo.current
    val uri = LocalUriHandler.current
    var refreshing by remember { mutableStateOf(false) }
    val refresh: () -> Unit = { scope.launch { refreshing = true; WidgetRefresher.refresh(ctx); refreshing = false } }
    PullToRefreshBox(
        isRefreshing = refreshing,
        onRefresh = refresh,
        modifier = modifier.fillMaxSize(),
    ) {
        Column(Modifier.fillMaxSize().verticalScroll(rememberScrollState())) {
            val s = state.summary
            HomeHeader(
                label = s?.period?.label ?: currentMonth,
                months = currentMonth?.let { cur -> runCatching { (0 until 12).map { YearMonth.parse(cur).minusMonths(it.toLong()).toString() } }.getOrNull() }.orEmpty(),
                onMonth = onMonth,
                onInvest = { onNavigate(Screen.INVEST) },
            )
            if (s == null) {
                EmptyNote(if (state.error != null) "가계부를 불러오지 못했어요" else "불러오는 중이에요", if (state.error != null) "아래로 당겨서 다시 받아 보세요" else null)
                return@Column
            }
            val month = WidgetModel.monthName(s.period.label)
            val spent = s.totals.spent - s.totals.settled
            // 누르면 한 달 예산 고치기(사용자 요청, 2026-11-10). 지난 기간은 보기만
            var editTotal by remember { mutableStateOf(false) }
            SpendCard(
                s, if (pastMonth) month else "이번 달", spent,
                onClick = if (pastMonth) null else ({ editTotal = true }),
            )
            if (editTotal) {
                TotalBudgetDialog(s.totals, onDismiss = { editTotal = false }) { amount ->
                    scope.launch {
                        val err = saveTotal(s.period.label, amount) // 저장하면 요약도 새로 받는다(HomeScreen)
                        if (err == null) {
                            editTotal = false
                        } else {
                            Toast.makeText(ctx, err, Toast.LENGTH_LONG).show()
                        }
                    }
                }
            }

            // 해야 할 일: 박스 대신 위아래 선으로 나눈 줄(이번 기간만)
            val todo = buildList<@Composable () -> Unit> {
                if (pastMonth) return@buildList
                if (s.needsReview > 0) add {
                    FlatRow("분류를 골라야 할 결제 ${s.needsReview}건", bold = true, leading = { Dot(m.over) }, onClick = { onTransactions(1) })
                }
                for (p in problems) add { FlatRow(p.title, sub = p.sub, leading = { Dot(m.over) }, onClick = p.action) }
                // 읽지 못한 결제 알림은 홈 맨 밑으로(사용자 요청, 2026-11-15)
                if (s.totals.budget <= 0) add {
                    FlatRow("한 달 예산 정하기", onClick = { onNavigate(Screen.BUDGETS) })
                }
            }
            // 덩어리마다 둥근 박스(토스 홈처럼, 사용자 요청 2026-11-10)
            if (todo.isNotEmpty()) {
                Spacer(Modifier.height(CardGap))
                HomeCard {
                    todo.forEachIndexed { i, row ->
                        row()
                        if (i < todo.lastIndex) Hairline(Modifier.padding(horizontal = Gutter))
                    }
                }
            }

            // 분류별 예산(사용자 요청 2026-11-10, 시안 2026-11-12): 맨 위 분류마다 박스 하나. 누르면 그 박스가 펼쳐져
            // 안의 분류·폴더 트리와 그 분류의 최근 결제 3개가 박스 안에 보인다. "더 보기·접기" 줄은 두지 않는다
            val cats = WidgetModel.homeCategories(s, topCount = 3)
            val shown = cats.top + cats.rest
            if (shown.isNotEmpty()) {
                HomeSection("분류별 예산", action = "예산 변경", chevron = false, top = 28.dp, onAction = { onNavigate(Screen.BUDGETS) })
                var opened by rememberSaveable { mutableStateOf(listOf<Long>()) }
                // 펼친 박스의 최근 결제: 처음엔 요약에 온 최근 결제로, 이번 기간 결제를 받으면 그것으로
                var monthTx by remember(s.period.label, state.updatedAt) { mutableStateOf<List<TransactionDto>?>(null) }
                LaunchedEffect(opened.isNotEmpty(), s.period.label, state.updatedAt) {
                    if (opened.isNotEmpty() && monthTx == null) monthTx = loadMonthTx(s.period.label)
                }
                val pool = monthTx ?: s.recent
                for (c in shown) {
                    Spacer(Modifier.height(CardGap))
                    val isOpen = c.id in opened
                    CategoryBox(
                        c, isOpen,
                        kidsOf = { WidgetModel.childrenOf(s, it) },
                        recentFor = { WidgetModel.recentIn(s, pool, it, 3) },
                        onToggle = { opened = if (isOpen) opened - c.id else opened + c.id },
                        onCategory = onCategory,
                        onOpenTx = onOpenTx,
                    )
                }
            }

            if (!pastMonth) {
                val recent = WidgetModel.recentPayments(s, 3)
                Spacer(Modifier.height(CardGap))
                HomeCard {
                    HomeSection("최근 거래", action = "전체 보기", top = 20.dp, onAction = { onTransactions(0) })
                    if (recent.isEmpty()) {
                        Text("아직 결제가 없어요", Modifier.padding(horizontal = Gutter, vertical = 12.dp), fontSize = 14.sp, color = m.muted)
                    }
                    for (t in recent) TxRow(t, withDay = true) { onOpenTx(t) }
                    Spacer(Modifier.height(8.dp))
                }
            }

            // 새로 받지 못했을 때만 한 줄(눌러도 새로 받는다)
            if (state.error != null) {
                Text(
                    "새로 받지 못했어요 · 다시 받기",
                    Modifier.fillMaxWidth().tap(onClickLabel = "새로 받기", onClick = refresh).padding(horizontal = Gutter, vertical = 20.dp),
                    fontSize = 13.sp, color = m.faint,
                )
            }

            // 읽지 못한 결제 알림: 홈 맨 밑(사용자 요청, 2026-11-15). 누르면 웹 관리 화면의 '읽지 못한 알림' 탭(서버 주소가 있을 때)
            if (!pastMonth && s.failedEvents > 0) {
                val admin = serverUrl?.trimEnd('/')?.let { "$it/admin?tab=failed" }
                Spacer(Modifier.height(CardGap))
                HomeCard {
                    FlatRow(
                        "읽지 못한 결제 알림 ${s.failedEvents}건",
                        help = "금액을 못 찾아서 가계부에 안 적힌 알림이에요. ${if (admin != null) "눌러서 " else ""}웹 관리 화면에서 볼 수 있어요. 원문은 남아 있어서 읽는 방법을 고치면 다시 적을 수 있어요.",
                        leading = { Dot(m.warn) },
                        onClick = admin?.let { a -> { runCatching { uri.openUri(a) } } },
                    )
                }
            }
            Spacer(Modifier.height(24.dp))
        }
    }
}

/** 홈 맨 위: 왼쪽 "11월 ⌄"(누르면 지난 기간 고르기), 오른쪽 "투자 ↗" */
@Composable
private fun HomeHeader(label: String?, months: List<String>, onMonth: (String) -> Unit, onInvest: () -> Unit) {
    val m = LocalModo.current
    var menu by remember { mutableStateOf(false) }
    Row(
        Modifier.fillMaxWidth().heightIn(min = 64.dp).padding(start = Gutter - 4.dp, end = Gutter - 6.dp, top = 6.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Box {
            Row(
                Modifier.clip(RoundedCornerShape(10.dp)).tap(enabled = months.isNotEmpty(), onClickLabel = "달 고르기", role = Role.Button) { menu = true }
                    .padding(horizontal = 4.dp, vertical = 6.dp),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                Text(label?.let(WidgetModel::monthName) ?: "", fontSize = 26.sp, fontWeight = FontWeight.Bold, maxLines = 1)
                if (months.isNotEmpty()) Icon(ChevronDownIcon, contentDescription = null, modifier = Modifier.padding(start = 6.dp).size(22.dp))
            }
            ModoDropdownMenu(expanded = menu, onDismissRequest = { menu = false }) {
                val year = months.firstOrNull()?.take(4)
                for (mo in months) {
                    val name = WidgetModel.monthName(mo)
                    DropdownMenuItem(
                        text = { Text(if (mo.take(4) != year) "${mo.take(4)}년 $name" else name, fontWeight = if (mo == label) FontWeight.Bold else FontWeight.Normal) },
                        onClick = { menu = false; onMonth(mo) },
                    )
                }
            }
        }
        Spacer(Modifier.weight(1f))
        Row(
            Modifier.clip(RoundedCornerShape(10.dp)).tap(onClickLabel = "투자 열기", role = Role.Button, onClick = onInvest).padding(horizontal = 8.dp, vertical = 8.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Text("투자", fontSize = 17.sp, color = m.muted)
            Icon(ArrowUpRightIcon, contentDescription = null, tint = m.muted, modifier = Modifier.padding(start = 4.dp).size(18.dp))
        }
    }
}

/** 카드: "이번 달 쓸 수 있는 돈" 큰 숫자, 막대, 아래 예산 | 사용(예산이 없으면 쓴 돈 | 들어온 돈) */
@Composable
private fun SpendCard(s: Summary, monthWord: String, spent: Long, onClick: (() -> Unit)?) {
    val m = LocalModo.current
    val hasBudget = s.totals.budget > 0
    val over = hasBudget && s.totals.remaining < 0
    Column(
        Modifier.padding(horizontal = CardMargin).fillMaxWidth().clip(RoundedCornerShape(CardRadius)).background(m.card)
            .then(if (onClick != null) Modifier.tap(onClickLabel = "거래 보기", onClick = onClick) else Modifier)
            .padding(horizontal = Gutter, vertical = 22.dp),
    ) {
        Text(
            when {
                !hasBudget -> "$monthWord 쓴 돈"
                over -> "$monthWord 예산을 넘었어요"
                else -> "$monthWord 쓸 수 있는 돈"
            },
            fontSize = 15.sp, color = m.muted,
        )
        Text(
            when {
                !hasBudget -> Money.won(spent)
                over -> "${Money.won(-s.totals.remaining)} 넘음"
                else -> Money.won(s.totals.remaining)
            },
            Modifier.padding(top = 6.dp),
            fontSize = 36.sp, fontWeight = FontWeight.Bold, color = if (over) m.over else MaterialTheme.colorScheme.onBackground, maxLines = 1,
        )
        if (hasBudget) {
            val r = (spent + s.totals.fixed).toDouble() / s.totals.budget // 고정으로 빠지는 돈도 쓴 쪽으로
            ModoBar(r, budgetColor(r), Modifier.padding(top = 18.dp))
        }
        Row(Modifier.padding(top = 18.dp).height(IntrinsicSize.Min), verticalAlignment = Alignment.CenterVertically) {
            val (l1, v1, l2, v2) = if (hasBudget) listOf("예산", Money.won(s.totals.budget), "사용", Money.won(spent))
            else listOf("사용", Money.won(spent), "들어온 돈", Money.won(s.totals.income))
            Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(4.dp)) {
                Text(l1, fontSize = 14.sp, color = m.muted)
                Text(v1, fontSize = 17.sp, fontWeight = FontWeight.SemiBold, maxLines = 1)
            }
            Box(Modifier.width(1.dp).fillMaxHeight().background(m.line))
            Column(Modifier.weight(1f).padding(start = 20.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
                Text(l2, fontSize = 14.sp, color = m.muted)
                Text(v2, fontSize = 17.sp, fontWeight = FontWeight.SemiBold, maxLines = 1)
            }
        }
        // 고정으로 빠지는 돈(주식 적립 등)이 있으면 한 줄
        if (hasBudget && s.totals.fixed > 0) {
            Text("고정으로 빠지는 돈 -${Money.won(s.totals.fixed)}", Modifier.padding(top = 14.dp), fontSize = 14.sp, color = m.muted)
        }
    }
}

/** 홈 박스: 화면 옆 14dp, 모서리 22dp, 카드 바탕. 안의 줄은 평소처럼 Gutter만큼 들여 쓴다 */
private val CardMargin = 14.dp
private val CardRadius = 22.dp
private val CardGap = 12.dp

@Composable
private fun HomeCard(content: @Composable () -> Unit) {
    val m = LocalModo.current
    Column(Modifier.padding(horizontal = CardMargin).fillMaxWidth().clip(RoundedCornerShape(CardRadius)).background(m.card)) { content() }
}

/**
 * 한 달 예산 고치기: 숫자 칸(+1천·+1만·+10만, "분류 합계"), 아래에 그러면 쓸 수 있는 돈.
 * 분류 예산 합계와 같은 값으로 저장하면 null(분류 합계를 따라감)로 보낸다.
 */
@Composable
private fun TotalBudgetDialog(t: com.yuntyu.myhome.core.api.Totals, onDismiss: () -> Unit, onSave: (Long?) -> Unit) {
    val m = LocalModo.current
    var text by remember { mutableStateOf(t.budget.takeIf { it > 0 }?.toString().orEmpty()) }
    val won = text.toLongOrNull() ?: 0L
    val left = won - t.fixed - t.spent + t.settled
    ModoAlertDialog(
        onDismissRequest = onDismiss,
        title = { Text("이번 달 예산") },
        text = {
            Column(verticalArrangement = Arrangement.spacedBy(6.dp)) {
                LineField(
                    text, { v -> text = v.filter(Char::isDigit).trimStart('0').take(12).let { d -> if ((d.toLongOrNull() ?: 0L) > MONEY_MAX) MONEY_MAX.toString() else d } },
                    if (won > 0) Money.korean(won) else "예산", keyboardType = KeyboardType.Number, suffix = "원", money = true,
                )
                MoneyQuickAdd(won, { text = it.toString() }, extra = if (t.categoryBudget > 0) "분류 합계" to t.categoryBudget else null)
                Text(
                    if (left >= 0) "쓸 수 있는 돈 ${Money.won(left)}" else "${Money.won(-left)} 넘음",
                    Modifier.padding(top = 6.dp), fontSize = 14.sp, color = if (left >= 0) m.muted else m.over,
                )
            }
        },
        confirmButton = {
            TextButton(enabled = text.isNotEmpty(), onClick = { onSave(if (t.categoryBudget > 0 && won == t.categoryBudget) null else won) }) { Text("저장") }
        },
        dismissButton = { TextButton(onClick = onDismiss) { Text("닫기") } },
    )
}

/** 묶음 머리: 굵은 제목, 오른쪽 "전체 보기 ›" */
@Composable
private fun HomeSection(title: String, action: String?, chevron: Boolean = true, top: androidx.compose.ui.unit.Dp = 32.dp, onAction: () -> Unit) {
    val m = LocalModo.current
    Row(Modifier.fillMaxWidth().padding(start = Gutter, end = Gutter - 6.dp, top = top, bottom = 6.dp), verticalAlignment = Alignment.CenterVertically) {
        Text(title, Modifier.weight(1f), fontSize = 20.sp, fontWeight = FontWeight.Bold)
        if (action != null) {
            Row(
                Modifier.clip(RoundedCornerShape(10.dp)).tap(role = Role.Button, onClick = onAction).padding(horizontal = 6.dp, vertical = 6.dp),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                Text(action, fontSize = 15.sp, color = m.muted)
                if (chevron) Icon(ChevronRightIcon, contentDescription = null, tint = m.muted, modifier = Modifier.padding(start = 2.dp).size(16.dp))
            }
        }
    }
}

/** 한도 없는 분류 꼬리표: 예산 없음(한 달 예산에서 안 깎임) / 자유(한도 없이, 깎임). 한도가 있으면 없음 */
private fun budgetTag(c: CategoryBudget): String? = when {
    !c.inBudget -> "예산 없음"
    c.budget <= 0 -> "자유"
    else -> null
}

/**
 * 쓴 돈 / 남은 돈 글자(2026-11-12 사용자 요청): "11,900 / 588,100원". 쓴 돈은 진하게, " / 남은 돈원"은 회색
 * (80% 넘게 쓰면 주의색). 넘으면 " / 12,000원 넘음"을 초과색으로. 예산이 없으면(자유·예산 없음) 쓴 돈만 "11,900원"
 */
@Composable
private fun spentAndLeft(c: CategoryBudget, strong: FontWeight): androidx.compose.ui.text.AnnotatedString {
    val m = LocalModo.current
    val (tail, tailColor) = when {
        c.budget <= 0 -> "원" to m.muted
        c.remaining < 0 -> " / ${Money.won(-c.remaining)} 넘음" to m.over
        c.ratio >= 0.8 -> " / ${Money.won(c.remaining)}" to m.warn
        else -> " / ${Money.won(c.remaining)}" to m.muted
    }
    return androidx.compose.ui.text.buildAnnotatedString {
        pushStyle(androidx.compose.ui.text.SpanStyle(color = MaterialTheme.colorScheme.onBackground, fontWeight = strong))
        append(Money.grouped(c.spent))
        pop()
        pushStyle(androidx.compose.ui.text.SpanStyle(color = tailColor, fontWeight = FontWeight.Normal))
        append(tail)
        pop()
    }
}

// 분류 박스 안쪽 여백과 트리 치수(시안을 잰 값). 세로선은 박스 이름보다 4dp 왼쪽, 가지 18dp, 가지 끝에서 8dp 띄워 이름
private val BoxPad = 16.dp
private val RailX = 12.dp
private val RailStep = 22.dp
private val BranchLen = 18.dp
private val NameOffset = 26.dp
private val TreeNameLine = 22.dp
private val TreeRowPadTop = 4.dp
/** 고른 줄을 칠할 때 글자 둘레 여백(글자 자리는 그대로 두고 칠만 넓힌다) */
private val HlPad = 6.dp

/**
 * 맨 위 분류 박스 하나: 굵은 이름, 오른쪽 "쓴 돈 / 남은 돈", 막대. 누르면 박스 안에서 펼쳐진다(2026-11-12 사용자 시안):
 * 안의 분류·폴더를 세로선과 가지로 이은 트리(줄마다 이름·쓴 돈 / 남은 돈·막대, 폴더 안은 한 단계 더 들여서)
 * → 최근 결제 3개 → "○○ 거래 모두 보기". 트리의 줄을 누르면 고른 것이 되고(다시 누르면 풀림)
 * 최근 결제와 "거래 모두 보기"가 그것으로 바뀐다.
 */
@Composable
private fun CategoryBox(
    c: CategoryBudget,
    open: Boolean,
    kidsOf: (Long) -> List<CategoryBudget>,
    recentFor: (Long) -> List<TransactionDto>,
    onToggle: () -> Unit,
    onCategory: (Long) -> Unit,
    onOpenTx: (TransactionDto) -> Unit,
) {
    val m = LocalModo.current
    var selected by rememberSaveable(c.id) { mutableStateOf<Long?>(null) }
    val select: (Long) -> Unit = { id -> selected = if (selected == id) null else id }
    HomeCard {
        Column(
            Modifier.fillMaxWidth().tap(onClickLabel = if (open) "접기" else "펼치기", onClick = onToggle)
                .padding(start = BoxPad, end = BoxPad, top = 20.dp, bottom = if (open) 4.dp else 20.dp),
            verticalArrangement = Arrangement.spacedBy(5.dp),
        ) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                // 이름이 남는 칸을 다 차지하고 돈은 오른쪽 끝. 이름 옆 화살표는 두지 않는다(사용자 요청)
                Row(Modifier.weight(1f), verticalAlignment = Alignment.CenterVertically) {
                    Text(c.name, Modifier.weight(1f, fill = false), fontSize = 18.sp, fontWeight = FontWeight.Bold, maxLines = 1, overflow = TextOverflow.Ellipsis)
                    budgetTag(c)?.let { tag -> Tag(tag) }
                }
                Spacer(Modifier.width(12.dp))
                Text(spentAndLeft(c, FontWeight.Bold), fontSize = 17.sp, maxLines = 1)
            }
            if (c.budget > 0) ModoBar(c.ratio, budgetColor(c.ratio))
        }
        if (open) {
            val rows = treeRows(c.id, kidsOf)
            if (rows.isNotEmpty()) {
                Column(Modifier.fillMaxWidth().padding(top = 5.dp, end = BoxPad - HlPad)) {
                    for (r in rows) TreeRow(r, selected == r.c.id) { select(r.c.id) }
                }
            }
            val focus = selected ?: c.id
            val focusName = if (selected == null) c.name else rows.firstOrNull { it.c.id == selected }?.c?.name ?: c.name
            val recent = recentFor(focus)
            if (recent.isNotEmpty()) {
                Hairline(Modifier.padding(start = BoxPad, end = BoxPad, top = 15.dp, bottom = 4.dp))
                var lastDay = ""
                for (t in recent) {
                    val day = WidgetModel.shortTime(t.occurredAt).take(5).replace('/', '.')
                    PreviewTxRow(t, if (day != lastDay) day else "") { onOpenTx(t) }
                    lastDay = day
                }
            }
            Row(
                Modifier.padding(start = BoxTxPad - 6.dp, top = 4.dp, bottom = 12.dp).clip(RoundedCornerShape(10.dp))
                    .tap(onClickLabel = "$focusName 거래 보기") { onCategory(focus) }.padding(horizontal = 6.dp, vertical = 8.dp),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                Text("$focusName 거래 모두 보기", fontSize = 15.sp, color = m.muted)
                Icon(ChevronRightIcon, contentDescription = null, tint = m.muted, modifier = Modifier.padding(start = 2.dp).size(16.dp))
            }
        }
    }
}

/** 이름 옆 작은 꼬리표(자유·예산 없음) */
@Composable
private fun Tag(text: String) {
    val m = LocalModo.current
    Text(
        text, Modifier.padding(start = 8.dp).clip(RoundedCornerShape(6.dp)).background(m.raised).padding(horizontal = 6.dp, vertical = 2.dp),
        fontSize = 12.sp, color = m.muted, maxLines = 1,
    )
}

/** 트리 한 줄: 분류와 그 깊이, 위 단계마다 세로선이 이어지는지(그 단계에 뒤 형제가 있는지), 자기 뒤에 형제가 있는지 */
private class TreeLine(val c: CategoryBudget, val depth: Int, val through: List<Boolean>, val hasNext: Boolean)

/** 박스 안 분류를 위에서부터 차례로(폴더는 바로 아래에 그 안을 한 단계 더 들여서) */
private fun treeRows(rootId: Long, kidsOf: (Long) -> List<CategoryBudget>): List<TreeLine> {
    val out = mutableListOf<TreeLine>()
    fun walk(id: Long, depth: Int, through: List<Boolean>) {
        val kids = kidsOf(id)
        kids.forEachIndexed { i, k ->
            val hasNext = i < kids.lastIndex
            out += TreeLine(k, depth, through, hasNext)
            walk(k.id, depth + 1, through + hasNext)
        }
    }
    walk(rootId, 0, emptyList())
    return out
}

/**
 * 트리 줄: 왼쪽에 세로선·가지(├ └), 오른쪽에 이름과 "쓴 돈 / 남은 돈", 아래 막대. 줄끼리 틈 없이 붙여 세로선이 이어진다.
 * 고른 줄은 오른쪽 칸을 옅게 칠한다.
 */
@Composable
private fun TreeRow(r: TreeLine, selected: Boolean, onClick: () -> Unit) {
    val m = LocalModo.current
    val rail = m.muted.copy(alpha = 0.2f)
    val lead = RailX + RailStep * r.depth + NameOffset - HlPad
    Row(Modifier.fillMaxWidth().height(IntrinsicSize.Min)) {
        Box(
            Modifier.width(lead).fillMaxHeight().drawBehind {
                val w = 1.dp.toPx()
                val branchY = (TreeRowPadTop + TreeNameLine / 2).toPx()
                // 위 단계 세로선: 그 단계에 뒤 형제가 남아 있으면 줄 전체를 지나간다
                r.through.forEachIndexed { lvl, goes ->
                    if (goes) {
                        val x = (RailX + RailStep * lvl).toPx()
                        drawLine(rail, androidx.compose.ui.geometry.Offset(x, 0f), androidx.compose.ui.geometry.Offset(x, size.height), w)
                    }
                }
                val x = (RailX + RailStep * r.depth).toPx()
                drawLine(rail, androidx.compose.ui.geometry.Offset(x, 0f), androidx.compose.ui.geometry.Offset(x, if (r.hasNext) size.height else branchY), w)
                drawLine(rail, androidx.compose.ui.geometry.Offset(x, branchY), androidx.compose.ui.geometry.Offset(x + BranchLen.toPx(), branchY), w)
            },
        )
        // 줄 간격(시안): 이름 줄 22 · 2 · 막대 5 · 8 → 다음 이름. 칠하는 칸은 위 4·아래 2만 품는다
        Column(
            Modifier.weight(1f).padding(bottom = 2.dp)
                .clip(RoundedCornerShape(10.dp)).background(if (selected) m.raised else androidx.compose.ui.graphics.Color.Transparent)
                .tap(onClickLabel = "${r.c.name} 고르기", onClick = onClick)
                .padding(start = HlPad, end = HlPad, top = TreeRowPadTop, bottom = 2.dp),
        ) {
            Row(Modifier.height(TreeNameLine), verticalAlignment = Alignment.CenterVertically) {
                Row(Modifier.weight(1f), verticalAlignment = Alignment.CenterVertically) {
                    Text(r.c.name, Modifier.weight(1f, fill = false), fontSize = 15.sp, fontWeight = FontWeight.SemiBold, maxLines = 1, overflow = TextOverflow.Ellipsis)
                    budgetTag(r.c)?.let { Tag(it) }
                }
                Spacer(Modifier.width(10.dp))
                Text(spentAndLeft(r.c, FontWeight.Medium), fontSize = 15.sp, maxLines = 1)
            }
            if (r.c.budget > 0) ModoBar(r.c.ratio, budgetColor(r.c.ratio), Modifier.padding(top = 2.dp)) else Spacer(Modifier.height(7.dp))
        }
    }
}

/** 박스 안 최근 결제 줄의 옆 여백(시안: 박스 이름보다 조금 안쪽) */
private val BoxTxPad = 22.dp

/** 박스 안 최근 결제 한 줄: "11.10  가게 이름  1,800원"(같은 날이면 날짜는 첫 줄에만) */
@Composable
private fun PreviewTxRow(t: TransactionDto, day: String, onClick: () -> Unit) {
    val m = LocalModo.current
    Row(
        Modifier.fillMaxWidth().tap(onClickLabel = "${t.merchant} 결제 보기", onClick = onClick).padding(horizontal = BoxTxPad, vertical = 5.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Text(day, Modifier.width(47.dp), fontSize = 13.sp, color = m.muted, maxLines = 1)
        Text(t.merchant.ifBlank { "이름 없는 결제" }, Modifier.weight(1f), fontSize = 15.sp, maxLines = 1, overflow = TextOverflow.Ellipsis)
        Spacer(Modifier.width(12.dp))
        Text((if (t.flow == "in") "+" else "") + Money.won(t.amount), fontSize = 15.sp, color = if (t.flow == "in") m.income else MaterialTheme.colorScheme.onBackground, maxLines = 1)
    }
}

/** "방금", "5분 전", "3시간 전", 그보다 오래면 날짜 */
fun agoText(at: Long, now: Long = System.currentTimeMillis()): String {
    if (at <= 0) return "아직"
    val min = (now - at) / 60_000
    return when {
        min < 1 -> "방금"
        min < 60 -> "${min}분 전"
        min < 24 * 60 -> "${min / 60}시간 전"
        else -> formatTime(at, "M월 d일 HH:mm")
    }
}
