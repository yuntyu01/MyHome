package com.yuntyu.myhome.ui

import android.Manifest
import android.os.Build
import android.widget.Toast
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.clickable
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
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import com.yuntyu.myhome.MyHomeApp
import com.yuntyu.myhome.auth.AppLink
import com.yuntyu.myhome.core.Money
import com.yuntyu.myhome.core.WidgetModel
import com.yuntyu.myhome.core.api.CategoryBudget
import com.yuntyu.myhome.core.api.Summary
import com.yuntyu.myhome.data.SummaryRepository
import com.yuntyu.myhome.widget.WidgetRefresher
import kotlinx.coroutines.launch

/**
 * 앱 홈. 처음 설정(로그인 → 알림 읽기 허용 → 앱 고르기)이 덜 끝났으면 할 일을 번호 순서로 보여주고 지금 할 일 하나만 버튼으로 둔다.
 * 다 끝났으면 이번 달 쓸 수 있는 돈, 해야 할 일 한 줄, 분류별 남은 돈, 최근 결제를 보여준다.
 */
@Composable
fun HomeScreen(modifier: Modifier, resumeTick: Int, onNavigate: (Screen) -> Unit, onTransactions: (tab: Int) -> Unit) {
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
        if (authProblem != null) add(Problem("로그인이 풀렸어요", "눌러서 다시 로그인해 주세요", login))
        if (!batteryOk) add(Problem("배터리 절전 예외가 꺼져 있어요", "결제 알림을 놓칠 수 있어요. 눌러서 허용해 주세요") { PermissionStatus.requestBatteryExemption(ctx) })
    }
    InUseHome(modifier, state, problems, onNavigate, onTransactions)
}

private class Problem(val title: String, val sub: String, val action: () -> Unit)

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
        Step("로그인", if (hasToken) "연결됐어요" else "브라우저에서 이메일로 받은 코드를 넣으면 끝나요", hasToken, "이메일로 로그인하기", action = onLogin),
        Step(
            "결제 알림 읽기 허용",
            if (listenerOn) "허용했어요" else "설정 화면에서 MyHome을 켜 주세요. 알림을 놓치지 않게 배터리 절전 예외도 같이 물어봐요.",
            listenerOn, "알림 읽기 허용하기", action = onListener,
        ),
        Step(
            "카드·은행 앱 고르기",
            if (selectedCount > 0) "${selectedCount}개 골랐어요" else "KB Pay, 토스처럼 결제 알림이 오는 앱",
            selectedCount > 0, "카드·은행 앱 고르기", action = onApps,
        ),
        Step("한 달 예산 정하기", if (budgetSet) "정했어요" else "나중에 해도 돼요", budgetSet, "예산 정하기", optional = true, action = onBudget),
    )
    val current = steps.indexOfFirst { !it.done && !it.optional }.coerceAtLeast(0)
    Column(modifier.fillMaxSize()) {
        Column(Modifier.weight(1f).verticalScroll(rememberScrollState())) {
            Column(Modifier.padding(start = Gutter, end = Gutter, top = 36.dp), verticalArrangement = Arrangement.spacedBy(10.dp)) {
                Text("쓴 돈을 자동으로\n적어 드릴게요", fontSize = 28.sp, fontWeight = FontWeight.Bold, lineHeight = 38.sp)
                Text("카드·은행 앱 알림을 읽어서 가계부에 바로 적어요. 세 가지만 하면 끝나요.", fontSize = 16.sp, color = m.muted, lineHeight = 24.sp)
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
        Modifier.fillMaxWidth().clickable(enabled = !st.done || st.optional, onClick = st.action).padding(horizontal = Gutter, vertical = 16.dp),
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

@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun InUseHome(
    modifier: Modifier,
    state: SummaryRepository.State,
    problems: List<Problem>,
    onNavigate: (Screen) -> Unit,
    onTransactions: (Int) -> Unit,
) {
    val ctx = LocalContext.current
    val scope = rememberCoroutineScope()
    val m = LocalModo.current
    var refreshing by remember { mutableStateOf(false) }
    PullToRefreshBox(
        isRefreshing = refreshing,
        onRefresh = { scope.launch { refreshing = true; WidgetRefresher.refresh(ctx); refreshing = false } },
        modifier = modifier.fillMaxSize(),
    ) {
        Column(Modifier.fillMaxSize().verticalScroll(rememberScrollState())) {
            val s = state.summary
            if (s == null) {
                EmptyNote(if (state.error != null) "가계부를 불러오지 못했어요" else "불러오는 중이에요", if (state.error != null) "아래로 당겨서 다시 받아 보세요" else null)
                return@Column
            }
            val month = WidgetModel.monthName(s.period.label)
            val spent = s.totals.spent - s.totals.settled
            Column(Modifier.padding(start = Gutter, end = Gutter, top = 4.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
                if (s.totals.budget > 0) {
                    val over = s.totals.remaining < 0
                    Text(if (over) "${month} 예산을 넘었어요" else "${month}에 쓸 수 있는 돈", fontSize = 16.sp, color = m.muted)
                    Text(
                        if (over) "${Money.won(-s.totals.remaining)} 넘음" else Money.won(s.totals.remaining),
                        fontSize = 36.sp, fontWeight = FontWeight.Bold, color = if (over) m.over else MaterialTheme.colorScheme.onBackground,
                    )
                    Text("예산 ${Money.won(s.totals.budget)} 중 ${Money.won(spent)} 썼어요", fontSize = 15.sp, color = m.muted)
                    val r = spent.toDouble() / s.totals.budget
                    ModoBar(r, budgetColor(r), Modifier.padding(top = 10.dp))
                } else {
                    Text("${month}에 쓴 돈", fontSize = 16.sp, color = m.muted)
                    Text(Money.won(spent), fontSize = 36.sp, fontWeight = FontWeight.Bold)
                    if (s.totals.income > 0) Text("들어온 돈 ${Money.won(s.totals.income)}", fontSize = 15.sp, color = m.muted)
                }
            }

            // 해야 할 일: 박스 대신 위아래 선으로 나눈 줄
            val todo = buildList<@Composable () -> Unit> {
                if (s.needsReview > 0) add {
                    FlatRow("분류를 골라야 할 결제 ${s.needsReview}건", bold = true, leading = { Dot(m.over) }, onClick = { onTransactions(1) })
                }
                for (p in problems) add { FlatRow(p.title, sub = p.sub, leading = { Dot(m.over) }, onClick = p.action) }
                if (s.failedEvents > 0) add {
                    FlatRow("읽지 못한 결제 알림 ${s.failedEvents}건", sub = "가계부에 안 적혔을 수 있어요. 웹 관리 화면에서 확인해 주세요", leading = { Dot(m.warn) })
                }
                if (s.totals.budget <= 0) add {
                    FlatRow("한 달 예산 정하기", sub = "분류마다 쓸 돈을 정하면 남은 돈을 알려 드려요", onClick = { onNavigate(Screen.BUDGETS) })
                }
            }
            Spacer(Modifier.height(24.dp))
            if (todo.isNotEmpty()) {
                Hairline()
                todo.forEachIndexed { i, row ->
                    row()
                    if (i < todo.lastIndex) Hairline(Modifier.padding(start = Gutter))
                }
                Hairline()
            }

            val cats = WidgetModel.homeCategories(s, topCount = 3)
            if (cats.top.isNotEmpty() || cats.rest.isNotEmpty()) {
                SectionLabel("분류별 남은 돈", action = "예산 바꾸기", onAction = { onNavigate(Screen.BUDGETS) })
                // 펼친 분류가 없으면(기간 시작 직후) 처음부터 펼쳐 둔다
                var showRest by rememberSaveable { mutableStateOf(false) }
                val open = showRest || cats.top.isEmpty()
                // 폴더는 누르면 안 분류가 펼쳐진다
                var openFolders by remember { mutableStateOf(emptySet<Long>()) }
                val toggle: (Long) -> Unit = { id -> openFolders = if (id in openFolders) openFolders - id else openFolders + id }
                for (c in cats.top) CategoryTreeLine(s, c, 0, openFolders, toggle)
                if (open) for (c in cats.rest) CategoryTreeLine(s, c, 0, openFolders, toggle)
                if (cats.rest.isNotEmpty() && cats.top.isNotEmpty()) {
                    Text(
                        if (open) "접기" else "분류 ${cats.rest.size}개 더 보기",
                        Modifier.fillMaxWidth().clickable { showRest = !open }.padding(horizontal = Gutter, vertical = 12.dp),
                        fontSize = 15.sp, color = m.muted,
                    )
                }
            }

            val recent = WidgetModel.recentPayments(s, 3)
            SectionLabel("최근 결제", action = "전체", onAction = { onTransactions(0) })
            if (recent.isEmpty()) {
                Text("아직 적힌 결제가 없어요. 결제 알림이 오면 바로 적혀요.", Modifier.padding(horizontal = Gutter, vertical = 12.dp), fontSize = 14.sp, color = m.muted)
            }
            for (t in recent) TxRow(t, withDay = true) { onTransactions(if (t.status == "needs_review") 1 else 0) }

            Text(
                if (state.error != null) "새로 받지 못했어요. 아래로 당겨서 다시 받아 보세요" else "${agoText(state.updatedAt)} 새로 받음",
                Modifier.padding(horizontal = Gutter, vertical = 24.dp),
                fontSize = 13.sp, color = m.faint,
            )
        }
    }
}

/** 분류와, 펼친 폴더면 그 안의 분류들(들여 써서) */
@Composable
private fun CategoryTreeLine(s: Summary, c: CategoryBudget, depth: Int, open: Set<Long>, toggle: (Long) -> Unit) {
    val kids = WidgetModel.childrenOf(s, c.id)
    val isOpen = c.id in open
    CategoryLine(c, depth, folder = if (kids.isEmpty()) null else isOpen, onClick = if (kids.isEmpty()) null else ({ toggle(c.id) }))
    if (isOpen) for (k in kids) CategoryTreeLine(s, k, depth + 1, open, toggle)
}

/** 분류 한 줄: 이름과 남은 돈, 아래 얇은 막대. 예산이 없으면 쓴 돈만. folder가 null이 아니면 폴더(펼침 여부) */
@Composable
private fun CategoryLine(c: CategoryBudget, depth: Int = 0, folder: Boolean? = null, onClick: (() -> Unit)? = null) {
    val m = LocalModo.current
    Column(
        Modifier.fillMaxWidth()
            .then(if (onClick != null) Modifier.clickable(onClick = onClick) else Modifier)
            .padding(start = Gutter + 20.dp * depth, end = Gutter, top = 10.dp, bottom = 10.dp),
        verticalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Text(c.name, fontSize = if (depth > 0) 15.sp else 16.sp, color = if (depth > 0) m.muted else MaterialTheme.colorScheme.onBackground)
            if (folder != null) {
                Icon(
                    ChevronRightIcon, contentDescription = if (folder) "접기" else "펼치기", tint = m.faint,
                    modifier = Modifier.padding(start = 4.dp).size(16.dp).rotate(if (folder) 90f else 0f),
                )
            }
            Spacer(Modifier.weight(1f))
            val (text, color) = when {
                c.budget <= 0 -> "${Money.won(c.spent)} 씀" to m.muted
                c.remaining < 0 -> "${Money.won(-c.remaining)} 넘음" to m.over
                c.ratio >= 0.8 -> Money.won(c.remaining) to m.warn
                else -> Money.won(c.remaining) to MaterialTheme.colorScheme.onBackground
            }
            Text(text, fontSize = 16.sp, color = color)
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
