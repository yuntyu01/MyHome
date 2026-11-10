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
            SpendCard(
                s, if (pastMonth) month else "이번 달", spent,
                onClick = if (pastMonth) null else ({ onTransactions(0) }),
            )

            // 해야 할 일: 박스 대신 위아래 선으로 나눈 줄(이번 기간만)
            val todo = buildList<@Composable () -> Unit> {
                if (pastMonth) return@buildList
                if (s.needsReview > 0) add {
                    FlatRow("분류를 골라야 할 결제 ${s.needsReview}건", bold = true, leading = { Dot(m.over) }, onClick = { onTransactions(1) })
                }
                for (p in problems) add { FlatRow(p.title, sub = p.sub, leading = { Dot(m.over) }, onClick = p.action) }
                if (s.failedEvents > 0) add {
                    // 눌러서 웹 관리 화면의 '읽지 못한 알림' 탭을 연다(서버 주소가 있을 때)
                    val admin = serverUrl?.trimEnd('/')?.let { "$it/admin?tab=failed" }
                    FlatRow(
                        "읽지 못한 결제 알림 ${s.failedEvents}건",
                        help = "금액을 못 찾아서 가계부에 안 적힌 알림이에요. ${if (admin != null) "눌러서 " else ""}웹 관리 화면에서 볼 수 있어요. 원문은 남아 있어서 읽는 방법을 고치면 다시 적을 수 있어요.",
                        leading = { Dot(m.warn) },
                        onClick = admin?.let { a -> { runCatching { uri.openUri(a) } } },
                    )
                }
                if (s.totals.budget <= 0) add {
                    FlatRow("한 달 예산 정하기", onClick = { onNavigate(Screen.BUDGETS) })
                }
            }
            if (todo.isNotEmpty()) {
                Spacer(Modifier.height(16.dp))
                todo.forEachIndexed { i, row ->
                    row()
                    if (i < todo.lastIndex) Hairline(Modifier.padding(horizontal = Gutter))
                }
            }

            val cats = WidgetModel.homeCategories(s, topCount = 3)
            if (cats.top.isNotEmpty() || cats.rest.isNotEmpty()) {
                // 펼친 분류가 없으면(기간 시작 직후) 처음부터 펼쳐 둔다
                var showRest by rememberSaveable { mutableStateOf(false) }
                val open = showRest || cats.top.isEmpty()
                val more = cats.rest.isNotEmpty() && cats.top.isNotEmpty()
                HomeSection(
                    "분류별 남은 돈",
                    action = if (more) (if (open) "접기" else "전체 보기") else "예산",
                    onAction = { if (more) showRest = !open else onNavigate(Screen.BUDGETS) },
                )
                // 폴더는 누르면 안 분류가 펼쳐진다
                var openFolders by remember { mutableStateOf(emptySet<Long>()) }
                val toggle: (Long) -> Unit = { id -> openFolders = if (id in openFolders) openFolders - id else openFolders + id }
                for (c in cats.top) CategoryTreeLine(s, c, 0, openFolders, toggle, onCategory)
                if (open) for (c in cats.rest) CategoryTreeLine(s, c, 0, openFolders, toggle, onCategory)
            }

            if (!pastMonth) {
                val recent = WidgetModel.recentPayments(s, 3)
                HomeSection("최근 거래", action = "전체 보기", onAction = { onTransactions(0) })
                if (recent.isEmpty()) {
                    Text("아직 결제가 없어요", Modifier.padding(horizontal = Gutter, vertical = 12.dp), fontSize = 14.sp, color = m.muted)
                }
                for (t in recent) {
                    TxRow(t, withDay = true) { onOpenTx(t) }
                    Hairline(Modifier.padding(horizontal = Gutter))
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
        Modifier.padding(horizontal = Gutter).fillMaxWidth().clip(RoundedCornerShape(20.dp)).background(m.card)
            .then(if (onClick != null) Modifier.tap(onClickLabel = "거래 보기", onClick = onClick) else Modifier)
            .padding(horizontal = 22.dp, vertical = 22.dp),
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
            val r = spent.toDouble() / s.totals.budget
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
    }
}

/** 묶음 머리: 굵은 제목, 오른쪽 "전체 보기 ›" */
@Composable
private fun HomeSection(title: String, action: String?, onAction: () -> Unit) {
    val m = LocalModo.current
    Row(Modifier.fillMaxWidth().padding(start = Gutter, end = Gutter - 6.dp, top = 32.dp, bottom = 6.dp), verticalAlignment = Alignment.CenterVertically) {
        Text(title, Modifier.weight(1f), fontSize = 20.sp, fontWeight = FontWeight.Bold)
        if (action != null) {
            Row(
                Modifier.clip(RoundedCornerShape(10.dp)).tap(role = Role.Button, onClick = onAction).padding(horizontal = 6.dp, vertical = 6.dp),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                Text(action, fontSize = 15.sp, color = m.muted)
                Icon(ChevronRightIcon, contentDescription = null, tint = m.muted, modifier = Modifier.padding(start = 2.dp).size(16.dp))
            }
        }
    }
}

/** 분류와, 펼친 폴더면 그 안의 분류들(들여 써서). 폴더는 누르면 펼치고, 펼친 폴더 맨 아래 "폴더 전체 거래"로 거래를 본다. 분류는 누르면 그 거래만 */
@Composable
private fun CategoryTreeLine(s: Summary, c: CategoryBudget, depth: Int, open: Set<Long>, toggle: (Long) -> Unit, onCategory: (Long) -> Unit) {
    val kids = WidgetModel.childrenOf(s, c.id)
    val isOpen = c.id in open
    CategoryLine(c, depth, folder = if (kids.isEmpty()) null else isOpen, onClick = if (kids.isEmpty()) ({ onCategory(c.id) }) else ({ toggle(c.id) }))
    if (isOpen) {
        for (k in kids) CategoryTreeLine(s, k, depth + 1, open, toggle, onCategory)
        val m = LocalModo.current
        Text(
            "${c.name} 거래 모두 보기",
            Modifier.fillMaxWidth().tap(onClickLabel = "${c.name} 거래 보기") { onCategory(c.id) }
                .padding(start = Gutter + 20.dp * (depth + 1), end = Gutter, top = 10.dp, bottom = 10.dp),
            fontSize = 14.sp, color = m.muted,
        )
    }
}

/** 분류 한 줄: 이름과 남은 돈, 아래 얇은 막대. 예산이 없으면 쓴 돈만. folder가 null이 아니면 폴더(펼침 여부) */
@Composable
private fun CategoryLine(c: CategoryBudget, depth: Int = 0, folder: Boolean? = null, onClick: (() -> Unit)? = null) {
    val m = LocalModo.current
    Column(
        Modifier.fillMaxWidth()
            .then(if (onClick != null) Modifier.tap(onClick = onClick) else Modifier)
            .padding(start = Gutter + 20.dp * depth, end = Gutter, top = 10.dp, bottom = 10.dp),
        verticalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Text(c.name, Modifier.weight(1f), fontSize = if (depth > 0) 16.sp else 17.sp, color = if (depth > 0) m.muted else MaterialTheme.colorScheme.onBackground, maxLines = 1)
            val (text, color) = when {
                c.budget <= 0 -> "${Money.won(c.spent)} 씀" to m.muted
                c.remaining < 0 -> "${Money.won(-c.remaining)} 넘음" to m.over
                c.ratio >= 0.8 -> Money.won(c.remaining) to m.warn
                else -> Money.won(c.remaining) to MaterialTheme.colorScheme.onBackground
            }
            Text(text, fontSize = 17.sp, fontWeight = FontWeight.SemiBold, color = color, maxLines = 1)
            if (onClick != null) {
                Icon(
                    ChevronRightIcon, contentDescription = if (folder == null) null else if (folder) "접기" else "펼치기", tint = m.faint,
                    modifier = Modifier.padding(start = 8.dp).size(18.dp).rotate(if (folder == true) 90f else 0f),
                )
            }
        }
        if (c.budget > 0) ModoBar(c.ratio, budgetColor(c.ratio))
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
