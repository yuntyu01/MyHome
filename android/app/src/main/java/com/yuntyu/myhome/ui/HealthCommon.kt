package com.yuntyu.myhome.ui

import androidx.compose.foundation.background
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
import androidx.compose.runtime.saveable.Saver
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import com.yuntyu.myhome.core.Money
import com.yuntyu.myhome.core.api.MealView
import com.yuntyu.myhome.core.api.MyHomeJson
import com.yuntyu.myhome.core.api.Nutrition
import java.time.LocalDate
import java.time.LocalDateTime
import java.time.format.DateTimeFormatter

/**
 * 건강 화면 사이에 넘기는 값(라이브러리 없는 상태 내비게이션). 카메라가 열린 사이 Activity가 다시 만들어져도
 * 같은 끼니 화면으로 돌아와 찍은 사진을 받도록 저장해 둔다(HealthNav.Saver, 촬영 화면의 key와 rememberSaveable이 이 값에 묶여 있다).
 */
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

    companion object {
        val Saver: Saver<HealthNav, List<String?>> = Saver(
            save = { n ->
                arrayListOf(n.captureDate, n.captureIndex?.toString(), n.editing?.let { MyHomeJson.encodeToString(MealView.serializer(), it) }, n.recordsDate)
            },
            restore = { v ->
                HealthNav().apply {
                    captureDate = v.getOrNull(0)
                    captureIndex = v.getOrNull(1)?.toIntOrNull()
                    editing = v.getOrNull(2)?.let { runCatching { MyHomeJson.decodeFromString(MealView.serializer(), it) }.getOrNull() }
                    recordsDate = v.getOrNull(3)
                }
            },
        )
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

/**
 * 하루 합계: 탄수화물·단백질·지방을 크게, 칼로리는 그 아래 한 줄로. 목표를 넘어도 경고색을 쓰지 않는다
 * (핏두잉 규칙: 많이 먹은 것을 탓하지 않는다).
 */
@Composable
fun NutritionTotals(total: Nutrition, targets: Nutrition, title: String, trailing: String?, onClick: (() -> Unit)? = null) {
    val m = LocalModo.current
    Column(
        Modifier.fillMaxWidth().clip(RoundedCornerShape(16.dp)).background(m.card)
            .then(if (onClick != null) Modifier.tap(onClick = onClick) else Modifier)
            .padding(horizontal = 18.dp, vertical = 16.dp),
        verticalArrangement = Arrangement.spacedBy(14.dp),
    ) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Text(title, Modifier.weight(1f), style = MaterialTheme.typography.titleMedium)
            if (trailing != null) Text(trailing, style = MaterialTheme.typography.bodySmall, color = m.muted)
        }
        Row(horizontalArrangement = Arrangement.spacedBy(14.dp)) {
            Macro("탄수화물", total.carbs, targets.carbs, Modifier.weight(1f))
            Macro("단백질", total.protein, targets.protein, Modifier.weight(1f))
            Macro("지방", total.fat, targets.fat, Modifier.weight(1f))
        }
        Column(verticalArrangement = Arrangement.spacedBy(6.dp)) {
            Row(verticalAlignment = Alignment.Bottom) {
                Text("칼로리", Modifier.weight(1f), color = m.muted, style = MaterialTheme.typography.bodyMedium)
                Text(num(total.kcal), style = MaterialTheme.typography.titleMedium)
                Text(" / ${num(targets.kcal)} kcal", color = m.muted, style = MaterialTheme.typography.bodySmall)
            }
            ModoBar(ratioOf(total.kcal, targets.kcal), m.accent)
        }
    }
}

private fun ratioOf(v: Int, t: Int): Double = if (t <= 0) 0.0 else v.toDouble() / t

@Composable
private fun Macro(label: String, value: Int, target: Int, modifier: Modifier) {
    val m = LocalModo.current
    Column(modifier, verticalArrangement = Arrangement.spacedBy(6.dp)) {
        Text(label, style = MaterialTheme.typography.bodySmall, color = m.muted, maxLines = 1)
        Row(verticalAlignment = Alignment.Bottom) {
            Text(num(value), fontSize = 24.sp, fontWeight = FontWeight.Bold, maxLines = 1)
            Text("g", fontSize = 14.sp, color = m.muted, modifier = Modifier.padding(start = 2.dp, bottom = 3.dp))
        }
        ModoBar(ratioOf(value, target), m.accent)
        Text("목표 ${num(target)}g", fontSize = 12.sp, color = m.muted, maxLines = 1)
    }
}

/** 끼니 영양 한 줄: "탄 74g · 단 28g · 지 22g · 620kcal" */
fun macroLine(n: Nutrition): String = "탄 ${num(n.carbs)}g · 단 ${num(n.protein)}g · 지 ${num(n.fat)}g · ${num(n.kcal)}kcal"

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
        Modifier.fillMaxWidth().then(if (onClick != null) Modifier.tap(onClick = onClick) else Modifier).padding(vertical = 6.dp),
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
        modifier.clip(RoundedCornerShape(12.dp)).background(if (selected) m.accent else m.pill).tap(onClick = onClick)
            .padding(horizontal = 10.dp, vertical = 10.dp),
        contentAlignment = Alignment.Center,
    ) {
        // 좁은 칸(weight)에서 글자가 잘리지 않게 여백을 줄이고 두 줄까지 허용한다
        Text(
            text, color = if (selected) m.onAccent else MaterialTheme.colorScheme.onSurface,
            fontWeight = if (selected) FontWeight.SemiBold else FontWeight.Normal, maxLines = 2, textAlign = TextAlign.Center,
        )
    }
}

/** 카드 안 한 줄: 제목(+설명) 왼쪽, 값 오른쪽, 누르면 동작 */
@Composable
fun ItemRow(title: String, value: String = "", sub: String? = null, valueColor: Color? = null, onClick: (() -> Unit)? = null) {
    val m = LocalModo.current
    Row(
        Modifier.fillMaxWidth().then(if (onClick != null) Modifier.tap(onClick = onClick) else Modifier).padding(vertical = 4.dp),
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
