package com.yuntyu.myhome.ui

import android.content.Intent
import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.compose.BackHandler
import androidx.activity.compose.setContent
import androidx.activity.enableEdgeToEdge
import androidx.compose.foundation.layout.padding
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
    HOME("MyHome 가계부"),
    STATUS("수집 상태"),
    REVIEW("확인 필요"),
    TRANSACTIONS("이번 달 거래"),
    PERMISSIONS("권한 안내"),
    APPS("최근 알림 · 수집 대상"),
    SETTINGS("설정"),
    LOGS("전송 로그"),
    ;

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

    Scaffold(
        topBar = {
            TopAppBar(
                title = { Text(screen.title) },
                navigationIcon = {
                    if (screen.parent != null) {
                        IconButton(onClick = { screen = screen.parent ?: Screen.HOME }) {
                            Icon(Icons.AutoMirrored.Filled.ArrowBack, contentDescription = "뒤로")
                        }
                    }
                },
            )
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
