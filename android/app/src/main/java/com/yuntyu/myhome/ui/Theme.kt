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
 * - 다크: 차콜 바탕(#151515) + 카드(#222222), 평소 막대·선택 상태는 무채색 #CFCFCF
 * - 라이트: 밝은 회색 바탕(#F6F6F5) + 흰 카드, 평소 막대·선택 상태는 무채색 #5E5E5E
 * - 색은 주의가 필요한 금액에만 쓴다. 예산 80% 이상과 확인 필요는 라벤더, 초과는 살구. 빨강·파랑은 쓰지 않는다
 * - 테두리·그림자 없이 바탕색 차이로만 구분, 둥근 모서리(카드 16dp), Pretendard 글꼴
 * - 가계부·설정 화면은 카드 대신 가로로 꽉 찬 줄과 1dp 구분선으로 나눈다(박스 안 박스 금지). 주인공 버튼은 화면당 하나, 가로로 꽉 차게
 * 휴대폰 배경화면 색을 따라가는 동적 색(Material You)은 쓰지 않는다(디자인을 고정하기 위해).
 */

/** Material 색 체계에 없는 색(하단바, 선택 상태, 경고·초과 막대 등). */
@Immutable
data class ModoColors(
    val bg: Color,
    val card: Color,
    val raised: Color,
    val composer: Color,
    val nav: Color,
    val pill: Color,
    val selected: Color,
    val accent: Color,
    val accentInk: Color,
    val onAccent: Color,
    val muted: Color,
    val warn: Color,
    val warnBar: Color,
    val over: Color,
    val overBar: Color,
    val income: Color,
    /** 줄 구분선(박스 대신 1dp 선으로 나눈다) */
    val line: Color,
    /** 아주 옅은 글자(설명, 비활성) */
    val faint: Color,
    /** 화면당 하나인 주인공 버튼 바탕·글자 */
    val cta: Color,
    val onCta: Color,
    /** 정상 상태 점 */
    val ok: Color,
    val dark: Boolean,
)

private val DarkModo = ModoColors(
    bg = Color(0xFF151515),
    card = Color(0xFF222222), raised = Color(0xFF2E2E2E), composer = Color(0xFF2A2A2A),
    nav = Color(0xFF222222), pill = Color(0xFF333333), selected = Color(0xFF484848),
    accent = Color(0xFFCFCFCF), accentInk = Color(0xFFCFCFCF), onAccent = Color(0xFF151515),
    muted = Color(0xFF949494),
    warn = Color(0xFFAFA5D4), warnBar = Color(0xFFAFA5D4),
    over = Color(0xFFD89777), overBar = Color(0xFFD89777),
    income = Color(0xFFCFCFCF),
    line = Color(0xFF262626), faint = Color(0xFF6E6E6E),
    cta = Color(0xFFEDEDED), onCta = Color(0xFF151515),
    ok = Color(0xFF9FC2A8),
    dark = true,
)

private val LightModo = ModoColors(
    bg = Color(0xFFF6F6F5),
    card = Color(0xFFFFFFFF), raised = Color(0xFFEAEAE9), composer = Color(0xFFEEEEED),
    nav = Color(0xFFFFFFFF), pill = Color(0xFFEDEDEC), selected = Color(0xFFDCDCDC),
    accent = Color(0xFF5E5E5E), accentInk = Color(0xFF5E5E5E), onAccent = Color(0xFFFFFFFF),
    muted = Color(0xFF777777),
    warn = Color(0xFF6F6698), warnBar = Color(0xFF8F85B9),
    over = Color(0xFFA15A36), overBar = Color(0xFFC07B58),
    income = Color(0xFF5E5E5E),
    line = Color(0xFFE4E4E2), faint = Color(0xFF9A9A9A),
    cta = Color(0xFF1F1F1F), onCta = Color(0xFFFFFFFF),
    ok = Color(0xFF4F8A5E),
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
        surfaceContainer = m.card, surfaceContainerHigh = Color(0xFF2A2A2A), surfaceContainerHighest = m.raised,
        outline = Color(0xFF5C5C5C), outlineVariant = Color(0x14FFFFFF),
        error = m.over,
    )
} else {
    lightColorScheme(
        // 흰 바탕 위 글자·버튼 대비를 위해 라이트에서는 강조색을 한 단계 진하게 쓴다
        primary = m.accentInk, onPrimary = m.onAccent,
        primaryContainer = m.pill, onPrimaryContainer = Color(0xFF171717),
        secondaryContainer = m.pill, onSecondaryContainer = Color(0xFF171717),
        background = m.bg, onBackground = Color(0xFF171717),
        surface = m.card, onSurface = Color(0xFF171717),
        surfaceVariant = m.raised, onSurfaceVariant = m.muted,
        surfaceContainer = m.card, surfaceContainerHigh = Color(0xFFF3F3F2), surfaceContainerHighest = m.raised,
        outline = Color(0xFFBBBBBB), outlineVariant = Color(0x14000000),
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

/** 화면 바탕: 그라데이션 없는 무채색. */
@Composable
fun ModoBackground(content: @Composable () -> Unit) {
    val m = LocalModo.current
    Box(Modifier.fillMaxSize().background(m.bg)) { content() }
}

/** 예산 막대 색: 평소 무채색, 80% 이상 라벤더, 초과 살구. */
@Composable
fun budgetColor(ratio: Double): Color {
    val m = LocalModo.current
    return when {
        ratio > 1.0 -> m.overBar
        ratio >= 0.8 -> m.warnBar
        else -> m.accent
    }
}
