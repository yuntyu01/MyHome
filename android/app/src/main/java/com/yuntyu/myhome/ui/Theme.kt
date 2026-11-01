package com.yuntyu.myhome.ui

import androidx.compose.foundation.background
import androidx.compose.foundation.isSystemInDarkTheme
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.ColorScheme
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Shapes
import androidx.compose.material3.Typography
import androidx.compose.material3.darkColorScheme
import androidx.compose.material3.lightColorScheme
import androidx.compose.runtime.Composable
import androidx.compose.runtime.Immutable
import androidx.compose.runtime.staticCompositionLocalOf
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.Font
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import com.yuntyu.myhome.R

/*
 * modo 디자인 언어:
 * - 다크: 검정 바탕 + 위쪽 남색 그라데이션, 살짝 비치는 카드(#1A1E25), 강조색 #85ADE2
 * - 라이트: 거의 흰 바탕(#F7F9FC) + 옅은 하늘빛 그라데이션, 흰 카드
 * - 테두리·그림자 없이 바탕색 차이로만 구분, 둥근 모서리(카드 16dp), Pretendard 글꼴
 * 휴대폰 배경화면 색을 따라가는 동적 색(Material You)은 쓰지 않는다(디자인을 고정하기 위해).
 */

/** Material 색 체계에 없는 modo 색(그라데이션, 하단바, 경고색 등). */
@Immutable
data class ModoColors(
    val gradientTop: Color,
    val gradientMid: Color,
    val bg: Color,
    val card: Color,
    val raised: Color,
    val composer: Color,
    val nav: Color,
    val pill: Color,
    val accent: Color,
    val accentInk: Color,
    val onAccent: Color,
    val muted: Color,
    val warn: Color,
    val over: Color,
    val income: Color,
    val dark: Boolean,
)

val ModoAccent = Color(0xFF85ADE2)

private val DarkModo = ModoColors(
    gradientTop = Color(0xFF051324), gradientMid = Color(0xFF020910), bg = Color(0xFF000000),
    card = Color(0xFF1A1E25), raised = Color(0xFF24282C), composer = Color(0xFF1A1B1F),
    nav = Color(0xFF161A20), pill = Color(0xFF2C3747),
    accent = ModoAccent, accentInk = ModoAccent, onAccent = Color(0xFF0B0C0E),
    muted = Color(0xFF929398), warn = Color(0xFFF5C451), over = Color(0xFFFF6B6B), income = Color(0xFF5FD38D),
    dark = true,
)

private val LightModo = ModoColors(
    gradientTop = Color(0xFFE6F0FD), gradientMid = Color(0xFFEEF4FD), bg = Color(0xFFF7F9FC),
    card = Color(0xFFFFFFFF), raised = Color(0xFFE9ECF1), composer = Color(0xFFEEF2F8),
    nav = Color(0xFFEDF3F8), pill = Color(0xFFDCE8F4),
    accent = ModoAccent, accentInk = Color(0xFF3F78C2), onAccent = Color(0xFF0B0C0E),
    muted = Color(0xFF7A7F87), warn = Color(0xFFC98A00), over = Color(0xFFE5484D), income = Color(0xFF2F8F6B),
    dark = false,
)

val LocalModo = staticCompositionLocalOf { DarkModo }

private fun scheme(m: ModoColors): ColorScheme = if (m.dark) {
    darkColorScheme(
        primary = m.accent, onPrimary = m.onAccent,
        primaryContainer = m.pill, onPrimaryContainer = Color.White,
        secondaryContainer = m.pill, onSecondaryContainer = Color.White,
        background = m.bg, onBackground = Color.White,
        surface = m.card, onSurface = Color.White,
        surfaceVariant = m.raised, onSurfaceVariant = m.muted,
        surfaceContainer = m.card, surfaceContainerHigh = Color(0xFF1F232A), surfaceContainerHighest = m.raised,
        outline = Color(0xFF5A5E66), outlineVariant = Color(0x14FFFFFF),
        error = m.over,
    )
} else {
    lightColorScheme(
        // 흰 바탕 위 글자·버튼 대비를 위해 라이트에서는 강조색을 한 단계 진하게 쓴다
        primary = m.accentInk, onPrimary = Color.White,
        primaryContainer = m.pill, onPrimaryContainer = Color(0xFF15171A),
        secondaryContainer = m.pill, onSecondaryContainer = Color(0xFF15171A),
        background = m.bg, onBackground = Color(0xFF15171A),
        surface = m.card, onSurface = Color(0xFF15171A),
        surfaceVariant = m.raised, onSurfaceVariant = m.muted,
        surfaceContainer = m.card, surfaceContainerHigh = Color(0xFFF2F4F7), surfaceContainerHighest = m.raised,
        outline = Color(0xFFB9BEC6), outlineVariant = Color(0x1415171A),
        error = m.over,
    )
}

val Pretendard = FontFamily(
    Font(R.font.pretendard_regular, FontWeight.Normal),
    Font(R.font.pretendard_semibold, FontWeight.SemiBold),
    Font(R.font.pretendard_bold, FontWeight.Bold),
)

private fun typography(): Typography {
    val base = Typography()
    fun TextStyle.p() = copy(fontFamily = Pretendard, letterSpacing = (-0.2).sp)
    return Typography(
        displayLarge = base.displayLarge.p(), displayMedium = base.displayMedium.p(), displaySmall = base.displaySmall.p(),
        headlineLarge = base.headlineLarge.p().copy(fontWeight = FontWeight.Bold),
        headlineMedium = base.headlineMedium.p().copy(fontWeight = FontWeight.Bold),
        headlineSmall = base.headlineSmall.p().copy(fontWeight = FontWeight.Bold),
        titleLarge = base.titleLarge.p().copy(fontWeight = FontWeight.Bold),
        titleMedium = base.titleMedium.p().copy(fontWeight = FontWeight.SemiBold),
        titleSmall = base.titleSmall.p(),
        bodyLarge = base.bodyLarge.p(), bodyMedium = base.bodyMedium.p(), bodySmall = base.bodySmall.p(),
        labelLarge = base.labelLarge.p().copy(fontWeight = FontWeight.SemiBold),
        labelMedium = base.labelMedium.p(), labelSmall = base.labelSmall.p(),
    )
}

private val ModoShapes = Shapes(
    extraSmall = RoundedCornerShape(8.dp),
    small = RoundedCornerShape(12.dp),
    medium = RoundedCornerShape(16.dp),
    large = RoundedCornerShape(22.dp),
    extraLarge = RoundedCornerShape(28.dp),
)

@Composable
fun MyHomeTheme(content: @Composable () -> Unit) {
    val m = if (isSystemInDarkTheme()) DarkModo else LightModo
    androidx.compose.runtime.CompositionLocalProvider(LocalModo provides m) {
        MaterialTheme(colorScheme = scheme(m), typography = typography(), shapes = ModoShapes, content = content)
    }
}

/** 화면 바탕: 위쪽 테마색 그라데이션 → 바탕색(다크는 화면 2/3, 라이트는 40% 지점에서 바탕색). */
@Composable
fun ModoBackground(content: @Composable () -> Unit) {
    val m = LocalModo.current
    val stops = if (m.dark) arrayOf(0f to m.gradientTop, 0.32f to m.gradientMid, 0.68f to m.bg)
    else arrayOf(0f to m.gradientTop, 0.18f to m.gradientMid, 0.42f to m.bg)
    Box(Modifier.fillMaxSize().background(m.bg).background(Brush.verticalGradient(*stops))) { content() }
}

/** 예산 사용률 색: 80% 이상 경고, 100% 초과 빨강, 그 밖은 강조색. */
@Composable
fun budgetColor(ratio: Double): Color {
    val m = LocalModo.current
    return when {
        ratio > 1.0 -> m.over
        ratio >= 0.8 -> m.warn
        else -> m.accent
    }
}
