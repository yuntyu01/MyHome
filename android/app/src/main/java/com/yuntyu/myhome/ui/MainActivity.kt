package com.yuntyu.myhome.ui

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
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalLifecycleOwner
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.LifecycleEventObserver

/** 앱 화면 목록. 단순한 상태 기반 내비게이션(라이브러리 없음). */
enum class Screen(val title: String) {
    HOME("MyHome 가계부"),
    PERMISSIONS("권한 안내"),
    APPS("최근 알림 · 수집 대상"),
    SETTINGS("설정"),
    LOGS("전송 로그"),
}

class MainActivity : ComponentActivity() {
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        enableEdgeToEdge()
        setContent { MyHomeTheme { AppRoot() } }
    }
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun AppRoot() {
    var screen by rememberSaveable { mutableStateOf(Screen.HOME) }
    // 설정 화면에서 돌아왔을 때 권한 상태를 다시 읽기 위한 갱신 신호
    var resumeTick by remember { mutableIntStateOf(0) }
    val lifecycleOwner = LocalLifecycleOwner.current
    DisposableEffect(lifecycleOwner) {
        val obs = LifecycleEventObserver { _, e -> if (e == Lifecycle.Event.ON_RESUME) resumeTick++ }
        lifecycleOwner.lifecycle.addObserver(obs)
        onDispose { lifecycleOwner.lifecycle.removeObserver(obs) }
    }

    BackHandler(enabled = screen != Screen.HOME) { screen = Screen.HOME }

    Scaffold(
        topBar = {
            TopAppBar(
                title = { Text(screen.title) },
                navigationIcon = {
                    if (screen != Screen.HOME) {
                        IconButton(onClick = { screen = Screen.HOME }) {
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
            Screen.PERMISSIONS -> PermissionScreen(mod, resumeTick)
            Screen.APPS -> AppsScreen(mod)
            Screen.SETTINGS -> SettingsScreen(mod)
            Screen.LOGS -> LogScreen(mod)
        }
    }
}
