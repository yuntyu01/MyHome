package com.yuntyu.myhome.ui

import android.os.Build
import androidx.compose.foundation.isSystemInDarkTheme
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.darkColorScheme
import androidx.compose.material3.dynamicDarkColorScheme
import androidx.compose.material3.dynamicLightColorScheme
import androidx.compose.material3.lightColorScheme
import androidx.compose.runtime.Composable
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalContext

private val Green = Color(0xFF2E7D5B)

@Composable
fun MyHomeTheme(content: @Composable () -> Unit) {
    val dark = isSystemInDarkTheme()
    val ctx = LocalContext.current
    val scheme = when {
        Build.VERSION.SDK_INT >= 31 -> if (dark) dynamicDarkColorScheme(ctx) else dynamicLightColorScheme(ctx)
        dark -> darkColorScheme(primary = Color(0xFF7FD3A8))
        else -> lightColorScheme(primary = Green)
    }
    MaterialTheme(colorScheme = scheme, content = content)
}

/** 예산 사용률 색: 80% 이상 경고, 100% 초과 빨강. */
@Composable
fun budgetColor(ratio: Double): Color = when {
    ratio > 1.0 -> MaterialTheme.colorScheme.error
    ratio >= 0.8 -> Color(0xFFE6A100)
    else -> MaterialTheme.colorScheme.primary
}
