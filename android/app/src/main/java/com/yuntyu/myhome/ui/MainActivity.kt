package com.yuntyu.myhome.ui

import android.content.Intent
import android.os.Bundle
import android.widget.Toast
import androidx.activity.ComponentActivity
import androidx.activity.compose.BackHandler
import androidx.activity.compose.setContent
import androidx.activity.enableEdgeToEdge
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.WindowInsets
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.statusBars
import androidx.compose.foundation.layout.statusBarsPadding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.layout.windowInsetsTopHeight
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.offset
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.DateRange
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.MutableState
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.key
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateListOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.LifecycleEventObserver
import androidx.lifecycle.compose.LocalLifecycleOwner
import androidx.lifecycle.lifecycleScope
import com.yuntyu.myhome.MyHomeApp
import com.yuntyu.myhome.auth.AppLink
import kotlinx.coroutines.launch

/** 앱 화면 목록. 단순한 상태 기반 내비게이션(라이브러리 없음). 제목이 빈 화면은 화면이 직접 머리를 그린다. */
enum class Screen(val title: String) {
    HOME(""),
    TRANSACTIONS("거래"),
    HEALTH("건강"),
    FOCUS("집중"),
    INVEST("투자"),
    SETTINGS("설정"),
    STATUS("문제 해결"),
    PERMISSIONS("절전 설정 안내"),
    APPS("알림 읽을 앱"),
    LOGS("보낸 기록"),
    BUDGETS("분류와 예산"),
    INVEST_VALUE("평가"),
    HEALTH_CAPTURE("끼니 기록"),
    HEALTH_RECORDS("기록"),
    HEALTH_GYM("헬스"),
    HEALTH_SETTINGS("건강 설정"),
    ;

    /** 하단바에 있는 첫 화면들. 투자는 자동이라 볼 일이 적어 홈 오른쪽 위 버튼으로 연다(2026-11-09, 칸은 집중 타이머에) */
    val topLevel: Boolean
        get() = this == HOME || this == TRANSACTIONS || this == HEALTH || this == FOCUS || this == SETTINGS

    /** 제목 줄을 화면이 직접 그리는 첫 화면(홈은 제목 없음, 거래는 달 넘기기가 제목 줄에 있다, 집중은 가운데 날짜) */
    val ownHeader: Boolean
        get() = this == HOME || this == TRANSACTIONS || this == FOCUS

    /** 뒤로 가기 대상(지나온 화면 기록이 없을 때) */
    val parent: Screen?
        get() = when (this) {
            HOME -> null
            PERMISSIONS, LOGS -> STATUS
            STATUS, APPS, BUDGETS, HEALTH_SETTINGS -> SETTINGS
            INVEST_VALUE -> INVEST
            HEALTH_CAPTURE, HEALTH_RECORDS, HEALTH_GYM -> HEALTH
            else -> HOME
        }
}

class MainActivity : ComponentActivity() {
    /** 위젯에서 연 화면 요청(screen, txId). 처리하면 null로 비운다. */
    private val launchRequest = mutableStateOf<Pair<String, Long?>?>(null)

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        enableEdgeToEdge()
        // 회전 등으로 다시 만들어질 때는 위젯 인텐트를 다시 처리하지 않는다(복원된 화면 유지).
        if (savedInstanceState == null) readIntent(intent)
        setContent { MyHomeTheme { AppRoot(launchRequest) } }
    }

    override fun onNewIntent(intent: Intent) {
        super.onNewIntent(intent)
        setIntent(intent)
        readIntent(intent)
    }

    private fun readIntent(intent: Intent?) {
        // 브라우저 로그인 뒤 myhome://auth?code&state로 돌아온 경우: 코드를 기기 토큰으로 바꾸고 홈(처음 설정 순서)을 연다
        val data = intent?.data
        if (data != null && AppLink.isRedirect(data)) {
            intent.data = null
            lifecycleScope.launch {
                val msg = AppLink.complete(this@MainActivity, data)
                Toast.makeText(this@MainActivity, msg, Toast.LENGTH_LONG).show()
            }
            launchRequest.value = "home" to null
            return
        }
        val screen = intent?.getStringExtra(EXTRA_SCREEN) ?: return
        val tx = intent.getLongExtra(EXTRA_TX_ID, 0L).takeIf { it > 0 }
            ?: intent.getIntExtra(EXTRA_MEAL_INDEX, 0).toLong().takeIf { it > 0 }
        launchRequest.value = screen to tx
    }

    companion object {
        const val EXTRA_SCREEN = "screen"
        const val EXTRA_TX_ID = "tx_id"
        /** 건강 알림: 촬영할 끼니 번호 */
        const val EXTRA_MEAL_INDEX = "meal_index"
    }
}

@Composable
fun AppRoot(launchRequest: MutableState<Pair<String, Long?>?>) {
    var screen by rememberSaveable { mutableStateOf(Screen.HOME) }
    // 지나온 화면(하단바 칸으로 가면 비운다). 뒤로 가기는 여기서 꺼내고, 없으면 parent로 간다
    val backStack = remember { mutableStateListOf<Screen>() }
    val healthNav = rememberSaveable(saver = HealthNav.Saver) { HealthNav() }
    // 거래 화면 탭(0 전체, 1 분류 고를 것, 2 들어온 돈)과 바로 열 거래(위젯에서 확인 필요 항목을 탭했을 때)
    var txTab by rememberSaveable { mutableIntStateOf(0) }
    var openTxId by remember { mutableStateOf<Long?>(null) }
    fun go(to: Screen) {
        if (to == screen) return
        if (to.topLevel) backStack.clear() else backStack.add(screen)
        screen = to
    }
    fun back() {
        screen = backStack.removeLastOrNull() ?: screen.parent ?: Screen.HOME
    }
    // 위젯·알림 탭: 확인 필요 항목이면 거래 화면의 분류 고르기로, 건강 알림이면 촬영·헬스로, 그 외는 해당 화면으로
    LaunchedEffect(launchRequest.value) {
        val req = launchRequest.value ?: return@LaunchedEffect
        backStack.clear()
        when (req.first) {
            "review" -> { txTab = 1; openTxId = req.second; screen = Screen.TRANSACTIONS }
            "transactions" -> { txTab = 0; screen = Screen.TRANSACTIONS }
            "settings" -> screen = Screen.SETTINGS
            "budgets" -> { screen = Screen.SETTINGS; go(Screen.BUDGETS) }
            "apps" -> { screen = Screen.SETTINGS; go(Screen.APPS) }
            "health" -> screen = Screen.HEALTH
            "invest" -> screen = Screen.INVEST
            "focus" -> screen = Screen.FOCUS
            "health_gym" -> { screen = Screen.HEALTH; go(Screen.HEALTH_GYM) }
            "health_capture" -> {
                screen = Screen.HEALTH
                healthNav.openCapture(null, req.second?.toInt()?.takeIf { it > 0 })
                go(Screen.HEALTH_CAPTURE)
            }
            else -> screen = Screen.HOME
        }
        launchRequest.value = null
    }
    // 설정 화면에서 돌아왔을 때 권한 상태를 다시 읽기 위한 갱신 신호
    var resumeTick by remember { mutableIntStateOf(0) }
    val lifecycleOwner = LocalLifecycleOwner.current
    DisposableEffect(lifecycleOwner) {
        val obs = LifecycleEventObserver { _, e -> if (e == Lifecycle.Event.ON_RESUME) resumeTick++ }
        lifecycleOwner.lifecycle.addObserver(obs)
        onDispose { lifecycleOwner.lifecycle.removeObserver(obs) }
    }

    BackHandler(enabled = screen.parent != null || backStack.isNotEmpty()) { back() }
    val summaryState by MyHomeApp.instance.summaryRepo.state.collectAsState()
    val m = LocalModo.current

    ModoBackground {
        Scaffold(
            containerColor = Color.Transparent,
            // 바탕이 투명하면 Scaffold가 글자색을 정하지 못해 기본값(검정)이 된다. 다크 모드에서도 보이게 직접 준다.
            contentColor = MaterialTheme.colorScheme.onBackground,
            topBar = {
                when {
                    !screen.topLevel -> SubTopBar(screen.title, showBack = screen.parent != null || backStack.isNotEmpty()) { back() }
                    // 홈: 오른쪽 위에 투자 버튼(하단바 칸은 집중 타이머가 쓴다)
                    screen == Screen.HOME -> Row(
                        Modifier.fillMaxWidth().statusBarsPadding().height(48.dp).padding(end = 16.dp),
                        horizontalArrangement = Arrangement.End,
                        verticalAlignment = Alignment.CenterVertically,
                    ) {
                        Row(
                            Modifier.clip(RoundedCornerShape(12.dp)).background(m.pill)
                                .clickable(onClickLabel = "투자 열기") { go(Screen.INVEST) }
                                .padding(horizontal = 12.dp, vertical = 7.dp),
                            verticalAlignment = Alignment.CenterVertically,
                            horizontalArrangement = Arrangement.spacedBy(5.dp),
                        ) {
                            Icon(InvestIcon, contentDescription = null, tint = m.muted, modifier = Modifier.size(16.dp))
                            Text("투자", fontSize = 13.sp, color = m.muted)
                        }
                    }
                    screen.ownHeader -> Spacer(Modifier.fillMaxWidth().windowInsetsTopHeight(WindowInsets.statusBars))
                    else -> Column(Modifier.statusBarsPadding()) {
                        PageTitle(screen.title) {
                            if (screen == Screen.HEALTH) {
                                IconButton(onClick = { healthNav.recordsDate = null; go(Screen.HEALTH_RECORDS) }) {
                                    Icon(Icons.Filled.DateRange, contentDescription = "지난 기록", tint = m.muted)
                                }
                                IconButton(onClick = { go(Screen.HEALTH_SETTINGS) }) {
                                    Icon(GearLineIcon, contentDescription = "건강 설정", tint = m.muted)
                                }
                            }
                        }
                    }
                }
            },
            bottomBar = {
                if (screen.topLevel) {
                    FlatBottomBar(screen, summaryState.summary?.needsReview ?: 0L) { go(it) }
                }
            },
        ) { padding ->
            val mod = Modifier.padding(padding)
            when (screen) {
                Screen.HOME -> HomeScreen(mod, resumeTick, onNavigate = { go(it) }, onTransactions = { txTab = it; go(Screen.TRANSACTIONS) })
                Screen.STATUS -> StatusScreen(mod, resumeTick, onNavigate = { go(it) })
                Screen.TRANSACTIONS -> TransactionsScreen(
                    mod, resumeTick, txTab, onTab = { txTab = it }, openTxId = openTxId, onOpened = { openTxId = null },
                )
                Screen.PERMISSIONS -> PermissionScreen(mod, resumeTick)
                Screen.APPS -> AppsScreen(mod, onDone = { back() })
                Screen.SETTINGS -> SettingsScreen(mod, resumeTick, onNavigate = { go(it) })
                Screen.LOGS -> LogScreen(mod)
                Screen.BUDGETS -> BudgetsScreen(mod)
                Screen.FOCUS -> FocusScreen(mod, resumeTick)
                Screen.INVEST -> InvestScreen(mod, resumeTick, onNavigate = { go(it) })
                Screen.INVEST_VALUE -> InvestValueScreen(mod)
                Screen.HEALTH -> HealthScreen(mod, resumeTick, healthNav, onNavigate = { go(it) })
                Screen.HEALTH_CAPTURE -> key(healthNav.captureDate, healthNav.captureIndex, healthNav.editing) {
                    HealthCaptureScreen(mod, healthNav, onDone = { back() })
                }
                Screen.HEALTH_RECORDS -> HealthRecordsScreen(mod, resumeTick, healthNav, onNavigate = { go(it) })
                Screen.HEALTH_GYM -> HealthGymScreen(mod, resumeTick)
                Screen.HEALTH_SETTINGS -> HealthSettingsScreen(mod, onDone = { back() })
            }
        }
    }
}

/** 하위 화면 머리: 뒤로 + 제목. 바탕과 같은 색, 선 없음 */
@Composable
private fun SubTopBar(title: String, showBack: Boolean, onBack: () -> Unit) {
    Row(
        Modifier.fillMaxWidth().statusBarsPadding().height(56.dp).padding(start = 4.dp, end = 16.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        if (showBack) {
            IconButton(onClick = onBack) { Icon(ChevronLeftIcon, contentDescription = "뒤로", modifier = Modifier.size(24.dp)) }
        } else {
            Spacer(Modifier.width(20.dp))
        }
        Text(title, fontSize = 18.sp, fontWeight = FontWeight.SemiBold, maxLines = 1)
    }
}

/** 화면 아래에 붙은 평평한 하단바: 위쪽 1dp 선, 고른 칸은 진한 글자. 확인할 거래가 있으면 거래 칸에 살구색 점 */
@Composable
private fun FlatBottomBar(current: Screen, reviewCount: Long, onSelect: (Screen) -> Unit) {
    val m = LocalModo.current
    val items = listOf(
        Triple(Screen.HOME, HomeLineIcon, "홈"),
        Triple(Screen.TRANSACTIONS, ListLineIcon, "거래"),
        Triple(Screen.HEALTH, HealthIcon, "건강"),
        Triple(Screen.FOCUS, TimerIcon, "집중"),
        Triple(Screen.SETTINGS, GearLineIcon, "설정"),
    )
    Column(Modifier.fillMaxWidth().background(m.bg).navigationBarsPadding()) {
        Hairline()
        Row(Modifier.fillMaxWidth().height(60.dp), verticalAlignment = Alignment.CenterVertically) {
            for ((s, icon, label) in items) {
                val selected = current == s
                val tint = if (selected) MaterialTheme.colorScheme.onBackground else m.muted
                Column(
                    Modifier.weight(1f).fillMaxHeight().clickable(onClickLabel = label) { onSelect(s) },
                    horizontalAlignment = Alignment.CenterHorizontally,
                    verticalArrangement = Arrangement.Center,
                ) {
                    Box {
                        Icon(icon, contentDescription = null, tint = tint, modifier = Modifier.size(22.dp))
                        if (s == Screen.TRANSACTIONS && reviewCount > 0) {
                            Box(Modifier.align(Alignment.TopEnd).offset(x = 4.dp, y = (-2).dp).size(7.dp).clip(CircleShape).background(m.over))
                        }
                    }
                    Spacer(Modifier.height(3.dp))
                    Text(label, color = tint, fontSize = 11.sp, fontWeight = if (selected) FontWeight.SemiBold else FontWeight.Normal, lineHeight = 14.sp)
                }
            }
        }
    }
}
