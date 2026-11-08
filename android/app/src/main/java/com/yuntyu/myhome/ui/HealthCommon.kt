package com.yuntyu.myhome.ui

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import com.yuntyu.myhome.core.Money
import com.yuntyu.myhome.core.api.MealView
import com.yuntyu.myhome.core.api.Nutrition
import java.time.LocalDate
import java.time.LocalDateTime
import java.time.format.DateTimeFormatter

/** 건강 화면 사이에 넘기는 값(라이브러리 없는 상태 내비게이션). */
class HealthNav {
    /** 촬영·기록 확인 화면이 열 끼니. 날짜가 null이면 오늘(끼니 일정 기준) */
    var captureDate by mutableStateOf<String?>(null)
    var captureIndex by mutableStateOf<Int?>(null)
    /** 저장된 끼니를 고칠 때 */
    var editing by mutableStateOf<MealView?>(null)
    /** 기록 화면에서 고른 날 */
    var recordsDate by mutableStateOf<String?>(null)

    fun openCapture(date: String?, index: Int?, meal: MealView? = null) {
        captureDate = date
        captureIndex = index
        editing = meal
    }
}

val WEEKDAYS = listOf("월", "화", "수", "목", "금", "토", "일")

fun dayTitle(d: LocalDate): String = "${d.monthValue}월 ${d.dayOfMonth}일 ${WEEKDAYS[d.dayOfWeek.value - 1]}요일"

fun nowHhmm(now: LocalDateTime = LocalDateTime.now()): String = now.format(DateTimeFormatter.ofPattern("HH:mm"))

/** 칼로리·그램 숫자: 천 단위 쉼표 */
fun num(n: Int): String = Money.grouped(n.toLong())

/** 남은 분을 "1시간 20분", "20분"으로 */
fun remainText(minutes: Long): String = when {
    minutes <= 0 -> "지남"
    minutes < 60 -> "${minutes}분"
    minutes % 60 == 0L -> "${minutes / 60}시간"
    else -> "${minutes / 60}시간 ${minutes % 60}분"
}

/** 약 1,160 / 1,600 kcal 같은 줄과 막대. 목표를 넘어도 경고색을 쓰지 않는다(핏두잉 규칙: 많이 먹은 것을 탓하지 않는다). */
@Composable
fun NutritionTotals(total: Nutrition, targets: Nutrition, title: String, trailing: String?) {
    val m = LocalModo.current
    Column(
        Modifier.fillMaxWidth().clip(RoundedCornerShape(16.dp)).background(m.card).padding(horizontal = 18.dp, vertical = 16.dp),
        verticalArrangement = Arrangement.spacedBy(10.dp),
    ) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Text(title, Modifier.weight(1f), style = MaterialTheme.typography.titleMedium)
            if (trailing != null) Text(trailing, style = MaterialTheme.typography.bodySmall, color = m.muted)
        }
        Row(verticalAlignment = Alignment.Bottom) {
            Text("칼로리", Modifier.weight(1f), color = m.muted, style = MaterialTheme.typography.bodyMedium)
            Text("약 ", color = m.muted, style = MaterialTheme.typography.bodySmall)
            Text(num(total.kcal), style = MaterialTheme.typography.titleLarge)
            Text(" / ${num(targets.kcal)} kcal", color = m.muted, style = MaterialTheme.typography.bodySmall)
        }
        ModoBar(ratioOf(total.kcal, targets.kcal), m.accent)
        Row(horizontalArrangement = Arrangement.spacedBy(12.dp)) {
            Macro("탄수화물", total.carbs, targets.carbs, Modifier.weight(1f))
            Macro("단백질", total.protein, targets.protein, Modifier.weight(1f))
            Macro("지방", total.fat, targets.fat, Modifier.weight(1f))
        }
    }
}

private fun ratioOf(v: Int, t: Int): Double = if (t <= 0) 0.0 else v.toDouble() / t

@Composable
private fun Macro(label: String, value: Int, target: Int, modifier: Modifier) {
    val m = LocalModo.current
    Column(modifier, verticalArrangement = Arrangement.spacedBy(4.dp)) {
        Text(label, style = MaterialTheme.typography.bodySmall, color = m.muted)
        Row(verticalAlignment = Alignment.Bottom) {
            Text(num(value), style = MaterialTheme.typography.titleMedium)
            Text(" / ${num(target)}g", style = MaterialTheme.typography.bodySmall, color = m.muted)
        }
        ModoBar(ratioOf(value, target), m.accent)
    }
}

/** 식단 표 머리줄과 줄: 끼니 | kcal | 탄 | 단 | 지 */
@Composable
fun NutritionTableRow(label: String, n: Nutrition?, faint: Boolean = false, bold: Boolean = false, header: Boolean = false, onClick: (() -> Unit)? = null) {
    val m = LocalModo.current
    val color = when {
        header || faint -> m.muted
        else -> MaterialTheme.colorScheme.onSurface
    }
    val style = if (header) MaterialTheme.typography.bodySmall else MaterialTheme.typography.bodyMedium
    val w = if (bold) FontWeight.SemiBold else FontWeight.Normal
    Row(
        Modifier.fillMaxWidth().then(if (onClick != null) Modifier.clickable(onClick = onClick) else Modifier).padding(vertical = 6.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Text(label, Modifier.weight(1.4f), color = color, style = style, fontWeight = w, maxLines = 1)
        val cells = if (header) listOf("kcal", "탄(g)", "단(g)", "지(g)") else listOf(n?.kcal, n?.carbs, n?.protein, n?.fat).map { it?.let(::num) ?: "–" }
        cells.forEachIndexed { i, c ->
            Text(c, Modifier.weight(if (i == 0) 1.1f else 0.9f), color = color, style = style, fontWeight = w, textAlign = TextAlign.End)
        }
    }
}

/** 선택 칩(먹은 양, 요일, 끼니 수 등). 고른 것은 강조색 바탕. */
@Composable
fun Chip(text: String, selected: Boolean, modifier: Modifier = Modifier, onClick: () -> Unit) {
    val m = LocalModo.current
    Box(
        modifier.clip(RoundedCornerShape(12.dp)).background(if (selected) m.accent else m.pill).clickable(onClick = onClick)
            .padding(horizontal = 14.dp, vertical = 10.dp),
        contentAlignment = Alignment.Center,
    ) {
        Text(text, color = if (selected) m.onAccent else MaterialTheme.colorScheme.onSurface, fontWeight = if (selected) FontWeight.SemiBold else FontWeight.Normal, maxLines = 1)
    }
}

/** 카드 안 한 줄: 제목(+설명) 왼쪽, 값 오른쪽, 누르면 동작 */
@Composable
fun ItemRow(title: String, value: String = "", sub: String? = null, valueColor: Color? = null, onClick: (() -> Unit)? = null) {
    val m = LocalModo.current
    Row(
        Modifier.fillMaxWidth().then(if (onClick != null) Modifier.clickable(onClick = onClick) else Modifier).padding(vertical = 4.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        Column(Modifier.weight(1f)) {
            Text(title)
            if (!sub.isNullOrBlank()) Text(sub, style = MaterialTheme.typography.bodySmall, color = m.muted)
        }
        if (value.isNotEmpty()) Text(value, color = valueColor ?: m.muted, textAlign = TextAlign.End)
        if (onClick != null) Text("›", color = m.muted)
    }
}
