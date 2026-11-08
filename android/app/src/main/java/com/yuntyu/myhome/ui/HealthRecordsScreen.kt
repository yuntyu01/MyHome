package com.yuntyu.myhome.ui

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import com.yuntyu.myhome.MyHomeApp
import com.yuntyu.myhome.core.HealthPlan
import com.yuntyu.myhome.core.api.ApiResult
import com.yuntyu.myhome.core.api.DayCard
import com.yuntyu.myhome.core.api.HealthMonth
import com.yuntyu.myhome.core.api.describe
import com.yuntyu.myhome.health.MealPhotos
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import java.time.LocalDate
import java.time.YearMonth

/** 기록: 달력(다 찍은 날·일부·헬스) + 고른 날 카드(합계, 끼니, 사진, 헬스). 끼니를 누르면 고친다. */
@Composable
fun HealthRecordsScreen(modifier: Modifier, resumeTick: Int, nav: HealthNav, onNavigate: (Screen) -> Unit) {
    val api = MyHomeApp.instance.api
    val m = LocalModo.current
    val ctx = LocalContext.current
    val start = nav.recordsDate?.let { runCatching { LocalDate.parse(it) }.getOrNull() } ?: LocalDate.now()
    var month by remember { mutableStateOf(YearMonth.from(start)) }
    var selected by remember { mutableStateOf(start) }
    var data by remember { mutableStateOf<HealthMonth?>(null) }
    var card by remember { mutableStateOf<DayCard?>(null) }
    var error by remember { mutableStateOf<String?>(null) }

    LaunchedEffect(month, resumeTick) {
        when (val r = withContext(Dispatchers.IO) { api.healthMonth(month.toString()) }) {
            is ApiResult.Ok -> { data = r.value; error = null }
            else -> error = r.describe()
        }
    }
    LaunchedEffect(selected, resumeTick) {
        nav.recordsDate = selected.toString()
        card = (withContext(Dispatchers.IO) { api.healthDay(selected.toString()) } as? ApiResult.Ok)?.value
    }

    Column(
        modifier.fillMaxSize().verticalScroll(rememberScrollState()).padding(16.dp),
        verticalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        error?.let { Text("불러오지 못했어요: $it", color = m.over, style = MaterialTheme.typography.bodySmall) }
        Column(
            Modifier.fillMaxWidth().clip(RoundedCornerShape(16.dp)).background(m.card).padding(horizontal = 14.dp, vertical = 14.dp),
            verticalArrangement = Arrangement.spacedBy(8.dp),
        ) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Text("${month.year}년 ${month.monthValue}월", Modifier.weight(1f).padding(start = 4.dp), style = MaterialTheme.typography.titleMedium)
                TextButton(onClick = { month = month.minusMonths(1) }) { Text("‹", color = m.muted) }
                TextButton(onClick = { month = month.plusMonths(1) }, enabled = month < YearMonth.now()) { Text("›", color = m.muted) }
            }
            data?.summary?.let { s -> Text("다 찍은 날 ${s.fullDays} · 헬스 간 날 ${s.gymDays}", style = MaterialTheme.typography.bodySmall, color = m.muted, modifier = Modifier.padding(start = 4.dp)) }
            Row(Modifier.fillMaxWidth()) {
                for (w in WEEKDAYS) Text(w, Modifier.weight(1f), style = MaterialTheme.typography.bodySmall, color = m.muted, textAlign = androidx.compose.ui.text.style.TextAlign.Center)
            }
            val days = data?.days.orEmpty().associateBy { it.date }
            val first = month.atDay(1)
            val lead = first.dayOfWeek.value - 1
            val cells = lead + month.lengthOfMonth()
            for (week in 0 until (cells + 6) / 7) {
                Row(Modifier.fillMaxWidth()) {
                    for (c in 0..6) {
                        val n = week * 7 + c - lead + 1
                        if (n < 1 || n > month.lengthOfMonth()) {
                            Spacer(Modifier.weight(1f))
                            continue
                        }
                        val date = month.atDay(n)
                        val info = days[date.toString()]
                        val isSel = date == selected
                        Column(
                            Modifier.weight(1f).clip(RoundedCornerShape(12.dp)).background(if (isSel) m.selected else androidx.compose.ui.graphics.Color.Transparent)
                                .clickable(enabled = !date.isAfter(LocalDate.now())) { selected = date }.padding(vertical = 6.dp),
                            horizontalAlignment = Alignment.CenterHorizontally,
                            verticalArrangement = Arrangement.spacedBy(3.dp),
                        ) {
                            Text("$n", color = if (date.isAfter(LocalDate.now())) m.muted else MaterialTheme.colorScheme.onSurface, fontWeight = if (date == LocalDate.now()) FontWeight.Bold else FontWeight.Normal)
                            Row(Modifier.height(8.dp), horizontalArrangement = Arrangement.spacedBy(3.dp), verticalAlignment = Alignment.CenterVertically) {
                                when (info?.state) {
                                    "full" -> Box(Modifier.size(7.dp).clip(CircleShape).background(m.accent))
                                    "partial" -> Box(Modifier.size(7.dp).border(1.dp, m.muted, CircleShape))
                                }
                                if (info?.gym == true) Box(Modifier.width(8.dp).height(3.dp).clip(RoundedCornerShape(2.dp)).background(m.muted))
                            }
                        }
                    }
                }
            }
            Row(horizontalArrangement = Arrangement.spacedBy(12.dp), verticalAlignment = Alignment.CenterVertically, modifier = Modifier.padding(start = 4.dp, top = 4.dp)) {
                Box(Modifier.size(7.dp).clip(CircleShape).background(m.accent)); Text("모두 기록", style = MaterialTheme.typography.bodySmall, color = m.muted)
                Box(Modifier.size(7.dp).border(1.dp, m.muted, CircleShape)); Text("일부 기록", style = MaterialTheme.typography.bodySmall, color = m.muted)
                Box(Modifier.width(8.dp).height(3.dp).background(m.muted)); Text("헬스", style = MaterialTheme.typography.bodySmall, color = m.muted)
            }
        }

        Row(Modifier.padding(horizontal = 6.dp), verticalAlignment = Alignment.CenterVertically) {
            Text(dayTitle(selected), Modifier.weight(1f), style = MaterialTheme.typography.titleMedium)
            card?.let { c -> Text("${c.meals.orEmpty().count { !HealthPlan.isExtra(it.index) }}끼 기록", style = MaterialTheme.typography.bodySmall, color = m.muted) }
        }
        val c = card
        if (c != null) {
            NutritionTotals(c.total, c.targets, "이날 합계", null)
            SectionCard("끼니") {
                val meals = c.meals.orEmpty().sortedBy { it.index }
                if (meals.isEmpty()) Text("기록한 끼니가 없어요", color = m.muted)
                for (meal in meals) {
                    Row(
                        Modifier.fillMaxWidth().clickable {
                            nav.openCapture(selected.toString(), meal.index, meal)
                            onNavigate(Screen.HEALTH_CAPTURE)
                        },
                        horizontalArrangement = Arrangement.spacedBy(10.dp),
                        verticalAlignment = Alignment.CenterVertically,
                    ) {
                        MealPhotos.photosOf(ctx, selected.toString(), meal.index).firstOrNull()?.let { Thumb(it, 48) }
                        Column(Modifier.weight(1f)) {
                            Text(HealthPlan.label(meal.index) + (localHhmm(meal.loggedAt)?.let { " · $it" } ?: ""), fontWeight = FontWeight.SemiBold)
                            Text(
                                meal.items.orEmpty().joinToString(", ") { it.name }.ifBlank { "직접 입력" },
                                style = MaterialTheme.typography.bodySmall,
                                color = m.muted,
                                maxLines = 2,
                            )
                        }
                        Text("${num(meal.nutrition.kcal)} kcal", color = m.muted)
                    }
                }
            }
            SectionCard("헬스") {
                Text(
                    when (c.gymVerifiedBy) {
                        "geofence" -> "갔어요 · 위치로 확인"
                        "photo" -> "갔어요 · 사진으로 인증"
                        "rest" -> "쉬었어요 · 휴식권"
                        else -> if (c.gym) "갔어요" else "기록 없음"
                    },
                    color = if (c.gym || c.gymVerifiedBy != null) MaterialTheme.colorScheme.onSurface else m.muted,
                )
            }
            if (!selected.isAfter(LocalDate.now())) {
                TextButton(onClick = {
                    val used = c.meals.orEmpty().map { it.index }
                    nav.openCapture(selected.toString(), HealthPlan.nextExtraIndex(used) ?: HealthPlan.EXTRA_BASE)
                    onNavigate(Screen.HEALTH_CAPTURE)
                }) { Text("이날 끼니 추가하기", color = m.muted) }
            }
        }
        Spacer(Modifier.height(24.dp))
    }
}
