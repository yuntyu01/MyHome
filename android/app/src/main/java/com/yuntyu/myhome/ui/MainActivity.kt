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
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.offset
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material.icons.automirrored.filled.List
import androidx.compose.material.icons.filled.DateRange
import androidx.compose.material.icons.filled.Home
import androidx.compose.material.icons.filled.Settings
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.material3.TopAppBar
import androidx.compose.material3.TopAppBarDefaults
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

/** 앱 화면 목록. 단순한 상태 기반 내비게이션(라이브러리 없음). */
enum class Screen(val title: String) {
    HOME("내 가계부"),
    STATUS("수집 상태"),
    REVIEW("확인 필요"),
    TRANSACTIONS("이번 달 거래"),
    PERMISSIONS("권한 안내"),
    APPS("최근 알림 · 수집 대상"),
    SETTINGS("설정"),
    LOGS("전송 로그"),
    INVEST("투자"),
    INVEST_VALUE("평가"),
    HEALTH("건강"),
    HEALTH_CAPTURE("끼니 기록"),
    HEALTH_RECORDS("기록"),
    HEALTH_GYM("헬스"),
    HEALTH_SETTINGS("건강 설정"),
    ;

    /** 하단바에 있는 첫 화면들 */
    val topLevel: Boolean
        get() = this == HOME || this == TRANSACTIONS || this == HEALTH || this == INVEST || this == SETTINGS

    /** 뒤로 가기 대상(지나온 화면 기록이 없을 때) */
    val parent: Screen?
        get() = when (this) {
            HOME -> null
            PERMISSIONS, APPS, LOGS -> STATUS
            INVEST_VALUE -> INVEST
            HEALTH_CAPTURE, HEALTH_RECORDS, HEALTH_GYM -> HEALTH
            HEALTH_SETTINGS -> SETTINGS
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
        // 브라우저 로그인 뒤 myhome://auth?code&state로 돌아온 경우: 코드를 기기 토큰으로 바꾸고 설정 화면을 연다
        val data = intent?.data
        if (data != null && AppLink.isRedirect(data)) {
            intent.data = null
            lifecycleScope.launch {
                val msg = AppLink.complete(this@MainActivity, data)
                Toast.makeText(this@MainActivity, msg, Toast.LENGTH_LONG).show()
            }
            launchRequest.value = "settings" to null
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

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun AppRoot(launchRequest: MutableState<Pair<String, Long?>?>) {
    var screen by rememberSaveable { mutableStateOf(Screen.HOME) }
    // 지나온 화면(하단바 칸으로 가면 비운다). 뒤로 가기는 여기서 꺼내고, 없으면 parent로 간다
    val backStack = remember { mutableStateListOf<Screen>() }
    val healthNav = rememberSaveable(saver = HealthNav.Saver) { HealthNav() }
    fun go(to: Screen) {
        if (to == screen) return
        if (to.topLevel) backStack.clear() else backStack.add(screen)
        screen = to
    }
    fun back() {
        screen = backStack.removeLastOrNull() ?: screen.parent ?: Screen.HOME
    }
    var openTxId by remember { mutableStateOf<Long?>(null) }
    // 위젯·알림 탭: 확인 필요 항목이면 분류 화면으로, 건강 알림이면 촬영·헬스로, 그 외는 해당 화면으로
    LaunchedEffect(launchRequest.value) {
        val req = launchRequest.value ?: return@LaunchedEffect
        backStack.clear()
        when (req.first) {
            "review" -> { screen = Screen.HOME; go(Screen.REVIEW); openTxId = req.second }
            "transactions" -> screen = Screen.TRANSACTIONS
            "settings" -> screen = Screen.SETTINGS
            "health" -> screen = Screen.HEALTH
            "invest" -> screen = Screen.INVEST
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

    ModoBackground {
        Scaffold(
            containerColor = Color.Transparent,
            // 바탕이 투명하면 Scaffold가 글자색을 정하지 못해 기본값(검정)이 된다. 다크 모드에서도 보이게 직접 준다.
            contentColor = MaterialTheme.colorScheme.onBackground,
            topBar = {
                TopAppBar(
                    title = {
                        Text(
                            screen.title,
                            style = if (screen.topLevel) MaterialTheme.typography.headlineMedium else MaterialTheme.typography.titleLarge,
                        )
                    },
                    navigationIcon = {
                        if (!screen.topLevel && (screen.parent != null || backStack.isNotEmpty())) {
                            IconButton(onClick = { back() }) {
                                Icon(Icons.AutoMirrored.Filled.ArrowBack, contentDescription = "뒤로")
                            }
                        }
                    },
                    actions = {
                        if (screen == Screen.HEALTH) {
                            IconButton(onClick = { healthNav.recordsDate = null; go(Screen.HEALTH_RECORDS) }) {
                                Icon(Icons.Filled.DateRange, contentDescription = "기록 달력")
                            }
                            IconButton(onClick = { go(Screen.HEALTH_SETTINGS) }) {
                                Icon(Icons.Filled.Settings, contentDescription = "건강 설정")
                            }
                        }
                    },
                    colors = TopAppBarDefaults.topAppBarColors(containerColor = Color.Transparent, scrolledContainerColor = Color.Transparent),
                )
            },
            bottomBar = {
                if (screen.topLevel) {
                    ModoBottomBar(screen, summaryState.summary?.needsReview ?: 0L) { go(it) }
                }
            },
        ) { padding ->
            val mod = Modifier.padding(padding)
            when (screen) {
                Screen.HOME -> HomeScreen(mod, resumeTick, onNavigate = { go(it) })
                Screen.STATUS -> StatusScreen(mod, resumeTick, onNavigate = { go(it) })
                Screen.REVIEW -> ReviewScreen(mod, openTxId, onOpened = { openTxId = null })
                Screen.TRANSACTIONS -> TransactionsScreen(mod, summaryState.summary?.needsReview ?: 0L, onNavigate = { go(it) })
                Screen.PERMISSIONS -> PermissionScreen(mod, resumeTick)
                Screen.APPS -> AppsScreen(mod)
                Screen.SETTINGS -> SettingsScreen(mod, onNavigate = { go(it) })
                Screen.LOGS -> LogScreen(mod)
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

/** modo처럼 화면 아래에 떠 있는 알약 모양 하단바. 선택한 칸은 옅은 강조색 알약으로 표시한다. */
@Composable
private fun ModoBottomBar(current: Screen, reviewCount: Long, onSelect: (Screen) -> Unit) {
    val m = LocalModo.current
    val items = listOf(
        Triple(Screen.HOME, Icons.Filled.Home, "홈"),
        Triple(Screen.TRANSACTIONS, Icons.AutoMirrored.Filled.List, "거래"),
        Triple(Screen.HEALTH, HealthIcon, "건강"),
        Triple(Screen.INVEST, InvestIcon, "투자"),
        Triple(Screen.SETTINGS, Icons.Filled.Settings, "설정"),
    )
    Box(Modifier.fillMaxWidth().navigationBarsPadding().padding(horizontal = 16.dp, vertical = 10.dp)) {
        Row(
            Modifier.fillMaxWidth().height(64.dp).clip(RoundedCornerShape(32.dp)).background(m.nav).padding(horizontal = 8.dp),
            horizontalArrangement = Arrangement.spacedBy(2.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            for ((s, icon, label) in items) {
                val selected = current == s
                Box(
                    // 5칸이라 고정 폭(72dp)이면 좁은 폰에서 넘친다. 칸을 똑같이 나눈다
                    Modifier.weight(1f).height(52.dp).clip(RoundedCornerShape(26.dp))
                        .background(if (selected) m.selected else Color.Transparent)
                        .clickable(onClickLabel = label) { onSelect(s) },
                    contentAlignment = Alignment.Center,
                ) {
                    val tint = if (selected) m.accent else m.muted
                    Column(horizontalAlignment = Alignment.CenterHorizontally) {
                        Icon(icon, contentDescription = null, tint = tint, modifier = Modifier.size(22.dp))
                        Text(label, color = tint, fontSize = 11.sp, fontWeight = FontWeight.SemiBold, lineHeight = 14.sp)
                    }
                    // 확인할 거래가 있으면 거래 칸에 점(확인 화면은 홈 카드·거래 위 띠·위젯에서 연다)
                    if (s == Screen.TRANSACTIONS && reviewCount > 0) {
                        Box(Modifier.align(Alignment.TopCenter).offset(x = 12.dp, y = 6.dp).size(8.dp).clip(CircleShape).background(m.accent))
                    }
                }
            }
        }
    }
}
