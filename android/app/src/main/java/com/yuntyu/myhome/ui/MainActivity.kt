package com.yuntyu.myhome.ui

import android.content.Intent
import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.compose.BackHandler
import androidx.activity.compose.setContent
import androidx.activity.enableEdgeToEdge
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.automirrored.filled.List
import androidx.compose.material.icons.filled.CheckCircle
import androidx.compose.material.icons.filled.Home
import androidx.compose.material.icons.filled.Settings
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.TopAppBarDefaults
import androidx.compose.runtime.collectAsState
import androidx.compose.ui.Alignment
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.unit.dp
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.sp
import androidx.compose.foundation.layout.Column
import com.yuntyu.myhome.MyHomeApp
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.material3.TopAppBar
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.MutableState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.LifecycleEventObserver
import androidx.lifecycle.compose.LocalLifecycleOwner

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
    ;

    /** 하단바에 있는 첫 화면들 */
    val topLevel: Boolean
        get() = this == HOME || this == TRANSACTIONS || this == REVIEW || this == SETTINGS

    /** 뒤로 가기 대상 */
    val parent: Screen?
        get() = when (this) {
            HOME -> null
            PERMISSIONS, APPS, LOGS -> STATUS
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
        val screen = intent?.getStringExtra(EXTRA_SCREEN) ?: return
        val tx = intent.getLongExtra(EXTRA_TX_ID, 0L).takeIf { it > 0 }
        launchRequest.value = screen to tx
    }

    companion object {
        const val EXTRA_SCREEN = "screen"
        const val EXTRA_TX_ID = "tx_id"
    }
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun AppRoot(launchRequest: MutableState<Pair<String, Long?>?>) {
    var screen by rememberSaveable { mutableStateOf(Screen.HOME) }
    var openTxId by remember { mutableStateOf<Long?>(null) }
    // 위젯 탭: 확인 필요 항목이면 분류 화면으로, 그 외는 해당 화면으로
    LaunchedEffect(launchRequest.value) {
        val req = launchRequest.value ?: return@LaunchedEffect
        screen = when (req.first) {
            "review" -> Screen.REVIEW
            "transactions" -> Screen.TRANSACTIONS
            else -> Screen.HOME
        }
        openTxId = req.second
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

    BackHandler(enabled = screen.parent != null) { screen = screen.parent ?: Screen.HOME }
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
                        if (!screen.topLevel && screen.parent != null) {
                            IconButton(onClick = { screen = screen.parent ?: Screen.HOME }) {
                                Icon(Icons.AutoMirrored.Filled.ArrowBack, contentDescription = "뒤로")
                            }
                        }
                    },
                    colors = TopAppBarDefaults.topAppBarColors(containerColor = Color.Transparent, scrolledContainerColor = Color.Transparent),
                )
            },
            bottomBar = {
                if (screen.topLevel) {
                    ModoBottomBar(screen, summaryState.summary?.needsReview ?: 0L) { screen = it }
                }
            },
        ) { padding ->
            val mod = Modifier.padding(padding)
            when (screen) {
                Screen.HOME -> HomeScreen(mod, resumeTick, onNavigate = { screen = it })
                Screen.STATUS -> StatusScreen(mod, resumeTick, onNavigate = { screen = it })
                Screen.REVIEW -> ReviewScreen(mod, openTxId, onOpened = { openTxId = null })
                Screen.TRANSACTIONS -> TransactionsScreen(mod)
                Screen.PERMISSIONS -> PermissionScreen(mod, resumeTick)
                Screen.APPS -> AppsScreen(mod)
                Screen.SETTINGS -> SettingsScreen(mod)
                Screen.LOGS -> LogScreen(mod)
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
        Triple(Screen.REVIEW, Icons.Filled.CheckCircle, "확인"),
        Triple(Screen.SETTINGS, Icons.Filled.Settings, "설정"),
    )
    Box(Modifier.fillMaxWidth().navigationBarsPadding().padding(horizontal = 16.dp, vertical = 10.dp)) {
        Row(
            Modifier.fillMaxWidth().height(64.dp).clip(RoundedCornerShape(32.dp)).background(m.nav).padding(horizontal = 12.dp),
            horizontalArrangement = Arrangement.SpaceBetween,
            verticalAlignment = Alignment.CenterVertically,
        ) {
            for ((s, icon, label) in items) {
                val selected = current == s
                Box(
                    Modifier.size(width = 72.dp, height = 52.dp).clip(RoundedCornerShape(26.dp))
                        .background(if (selected) m.selected else Color.Transparent)
                        .clickable(onClickLabel = label) { onSelect(s) },
                    contentAlignment = Alignment.Center,
                ) {
                    val tint = if (selected) m.accent else m.muted
                    Column(horizontalAlignment = Alignment.CenterHorizontally) {
                        Icon(icon, contentDescription = null, tint = tint, modifier = Modifier.size(22.dp))
                        Text(label, color = tint, fontSize = 11.sp, fontWeight = FontWeight.SemiBold, lineHeight = 14.sp)
                    }
                    if (s == Screen.REVIEW && reviewCount > 0) {
                        Box(Modifier.align(Alignment.TopEnd).padding(top = 6.dp, end = 20.dp).size(8.dp).clip(CircleShape).background(m.accent))
                    }
                }
            }
        }
    }
}
