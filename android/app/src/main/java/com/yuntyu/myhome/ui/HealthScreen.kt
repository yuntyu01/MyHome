package com.yuntyu.myhome.ui

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.horizontalScroll
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
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import com.yuntyu.myhome.MyHomeApp
import com.yuntyu.myhome.core.HealthPlan
import com.yuntyu.myhome.core.api.ApiResult
import com.yuntyu.myhome.core.api.DayCard
import com.yuntyu.myhome.core.api.HealthSettings
import com.yuntyu.myhome.core.api.HealthToday
import com.yuntyu.myhome.core.api.HealthWeekly
import com.yuntyu.myhome.core.api.MyHomeJson
import com.yuntyu.myhome.core.api.Nutrition
import com.yuntyu.myhome.core.api.describe
import com.yuntyu.myhome.health.MealOutbox
import com.yuntyu.myhome.health.Reminders
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import java.time.LocalDate
import java.time.LocalDateTime
import java.time.OffsetDateTime
import java.time.ZoneId
import kotlin.math.abs
import kotlin.math.max

/** 건강 첫 화면(핏두잉 "오늘"): 요일 줄, 끼니 칸, 오늘 합계, 식단 표, 주간 그래프, 촬영 버튼. */
data class HealthHome(
    val day: LocalDate,
    val settings: HealthSettings,
    val today: HealthToday,
    val weekly: HealthWeekly?,
    val card: DayCard?,
    /** 그날 아직 서버에 못 보낸 끼니(번호 → 기록). 칸에 "보내는 중"으로 보인다 */
    val outbox: Map<Int, MealOutbox.Entry> = emptyMap(),
)

/** 설정을 읽고, 끼니 일정 기준 날(자정 넘긴 끼니면 어제)의 오늘·주간·하루 기록을 받는다. 알림도 맞춘다. */
suspend fun loadHealthHome(): Result<HealthHome> = withContext(Dispatchers.IO) {
    val app = MyHomeApp.instance
    val api = app.api
    val st = when (val r = api.healthSettings()) {
        is ApiResult.Ok -> MyHomeJson.decodeFromJsonElement(HealthSettings.serializer(), r.value.settings)
        else -> return@withContext Result.failure(IllegalStateException(r.describe()))
    }
    app.healthLocal.updateFrom(st)
    val now = LocalDateTime.now()
    val day = HealthPlan.scheduleDay(st.meals, now)
    val nowText = if (day == now.toLocalDate()) nowHhmm(now) else "23:59"
    val today = when (val r = api.healthToday(day.toString(), nowText)) {
        is ApiResult.Ok -> r.value
        else -> return@withContext Result.failure(IllegalStateException(r.describe()))
    }
    val weekly = (api.healthWeekly(day.toString()) as? ApiResult.Ok)?.value
    val card = (api.healthDay(day.toString()) as? ApiResult.Ok)?.value
    val outbox = MealOutbox.forDay(app, day.toString())
    // 못 보낸 끼니도 찍은 것으로 쳐서 알림에서 뺀다. 헬스를 인증했거나 쉬는 날이면 헬스 알림도 뺀다
    card?.meals?.let { meals -> app.healthLocal.setLoggedForDay(day.toString(), meals.map { it.index } + outbox.keys) }
    card?.let { app.healthLocal.setGymDone(day.toString(), it.gymVerifiedBy != null) }
    Reminders.reschedule(app)
    Result.success(HealthHome(day, st, today, weekly, card, outbox))
}

@Composable
fun HealthScreen(modifier: Modifier, resumeTick: Int, nav: HealthNav, onNavigate: (Screen) -> Unit) {
    var data by remember { mutableStateOf<HealthHome?>(null) }
    var error by remember { mutableStateOf<String?>(null) }
    LaunchedEffect(resumeTick) {
        loadHealthHome().onSuccess { data = it; error = null }.onFailure { error = it.message }
    }
    val m = LocalModo.current
    Box(modifier.fillMaxSize()) {
        Column(
            Modifier.fillMaxSize().verticalScroll(rememberScrollState()).padding(start = 16.dp, end = 16.dp, top = 4.dp, bottom = 96.dp),
            verticalArrangement = Arrangement.spacedBy(12.dp),
        ) {
            val d = data
            if (d == null) {
                Text(error?.let { "불러오지 못했어요: $it" } ?: "불러오는 중…", color = m.muted)
                return@Column
            }
            if (error != null) Text("새로 불러오지 못했어요: $error", style = MaterialTheme.typography.bodySmall, color = m.over)
            Row(Modifier.padding(horizontal = 6.dp), verticalAlignment = Alignment.CenterVertically) {
                Text(dayTitle(d.day), Modifier.weight(1f), style = MaterialTheme.typography.titleLarge)
                if (d.today.streak > 0) Text("${d.today.streak}일째 기록 중", style = MaterialTheme.typography.bodySmall, color = m.muted)
            }
            WeekStrip(d)
            if (d.outbox.isNotEmpty()) {
                val failed = d.outbox.values.count { it.error != null }
                Text(
                    if (failed > 0) "보내지 못한 끼니가 ${failed}개 있어요. 눌러서 다시 저장해 주세요"
                    else "보내는 중인 끼니가 ${d.outbox.size}개 있어요. 연결되면 자동으로 보내요",
                    style = MaterialTheme.typography.bodySmall,
                    color = if (failed > 0) m.warn else m.muted,
                    modifier = Modifier.padding(horizontal = 6.dp),
                )
            }
            MealTiles(d, nav, onNavigate)
            NutritionTotals(d.today.total, d.today.targets, "오늘 합계", "${d.today.loggedCount}끼 기록")
            MealTable(d, nav, onNavigate)
            d.weekly?.let { WeeklyCard(it) }
        }
        val d = data
        if (d != null) {
            Box(
                Modifier.align(Alignment.BottomEnd).padding(20.dp).size(60.dp).clip(CircleShape).background(m.accent)
                    .clickable(onClickLabel = "끼니 촬영") {
                        nav.openCapture(d.day.toString(), currentIndex(d))
                        onNavigate(Screen.HEALTH_CAPTURE)
                    },
                contentAlignment = Alignment.Center,
            ) {
                Icon(CameraIcon, contentDescription = "끼니 촬영", tint = m.onAccent, modifier = Modifier.size(26.dp))
            }
        }
    }
}

/** 지금 찍을 끼니: 마감 전 미기록 중 가장 이른 것, 없으면 다음 기타 번호 */
private fun currentIndex(d: HealthHome): Int? {
    val logged = d.card?.meals.orEmpty().map { it.index }.toSet()
    val st = HealthPlan.statuses(HealthPlan.deadlinesFor(d.day, d.settings.meals), logged, LocalDateTime.now())
    return st.firstOrNull { it.second == HealthPlan.TileState.CURRENT }?.first?.index ?: HealthPlan.nextExtraIndex(logged)
}

@Composable
private fun WeekStrip(d: HealthHome) {
    val m = LocalModo.current
    val days = d.weekly?.days.orEmpty()
    Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.SpaceBetween) {
        for (i in 0..6) {
            val wd = days.firstOrNull { it.index == i }
            val date = wd?.date?.let { runCatching { LocalDate.parse(it) }.getOrNull() } ?: d.day.minusDays((HealthPlan.weekdayMon0(d.day) - i).toLong())
            val isToday = date == d.day
            Column(
                Modifier.weight(1f).clip(RoundedCornerShape(12.dp)).background(if (isToday) m.card else androidx.compose.ui.graphics.Color.Transparent).padding(vertical = 8.dp),
                horizontalAlignment = Alignment.CenterHorizontally,
                verticalArrangement = Arrangement.spacedBy(4.dp),
            ) {
                Text(WEEKDAYS[i], style = MaterialTheme.typography.bodySmall, color = m.muted)
                when {
                    isToday -> Text("${date.dayOfMonth}", fontWeight = FontWeight.Bold)
                    wd?.state == "done" -> Text("✓", color = m.accent)
                    else -> Text("${date.dayOfMonth}", color = if (date.isAfter(d.day)) m.muted else MaterialTheme.colorScheme.onSurface)
                }
            }
        }
    }
}

@Composable
private fun MealTiles(d: HealthHome, nav: HealthNav, onNavigate: (Screen) -> Unit) {
    val m = LocalModo.current
    val meals = d.card?.meals.orEmpty().associateBy { it.index }
    val now = LocalDateTime.now()
    val statuses = HealthPlan.statuses(HealthPlan.deadlinesFor(d.day, d.settings.meals), meals.keys + d.outbox.keys, now)
    Row(Modifier.fillMaxWidth().horizontalScroll(rememberScrollState()), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
        for ((dl, state) in statuses) {
            val current = state == HealthPlan.TileState.CURRENT
            val waiting = d.outbox[dl.index]?.takeIf { dl.index !in meals }
            val sub = if (waiting != null) {
                if (waiting.error != null) "보내지 못함" else "보내는 중"
            } else {
                when (state) {
                    HealthPlan.TileState.LOGGED -> meals[dl.index]?.loggedAt?.let(::localHhmm) ?: "기록함"
                    HealthPlan.TileState.CURRENT -> remainText(java.time.Duration.between(now, dl.at).toMinutes())
                    HealthPlan.TileState.MISSED -> "놓침"
                    HealthPlan.TileState.UPCOMING -> HealthPlan.hhmm(dl.at.hour * 60 + dl.at.minute)
                }
            }
            Tile(HealthPlan.label(dl.index), sub, current, done = state == HealthPlan.TileState.LOGGED && waiting == null) {
                nav.openCapture(d.day.toString(), dl.index, meals[dl.index])
                onNavigate(Screen.HEALTH_CAPTURE)
            }
        }
        val g = d.today.gym
        if (g.enabled) {
            // 휴식권을 쓴 날은 한 날로 보고 도착 시각 대신 "쉬는 날"
            val rest = d.card?.gymVerifiedBy == "rest"
            val sub = when {
                rest -> "쉬는 날"
                d.card?.gym == true -> "${g.done}/${g.planned}회"
                else -> "${g.done}/${g.planned}회" + if (g.scheduledToday) " · ${g.arriveBy}" else ""
            }
            Tile("헬스", sub, current = false, done = d.card?.gym == true || rest) {
                onNavigate(Screen.HEALTH_GYM)
            }
        }
    }
}

@Composable
private fun Tile(title: String, sub: String, current: Boolean, done: Boolean, onClick: () -> Unit) {
    val m = LocalModo.current
    val fg = if (current) m.onAccent else MaterialTheme.colorScheme.onSurface
    Column(
        Modifier.width(76.dp).height(84.dp).clip(RoundedCornerShape(16.dp)).background(if (current) m.accent else m.card)
            .clickable(onClick = onClick).padding(horizontal = 10.dp, vertical = 10.dp),
        verticalArrangement = Arrangement.SpaceBetween,
    ) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Text(title, Modifier.weight(1f), color = fg, fontWeight = FontWeight.SemiBold, maxLines = 1)
            if (done) Text("✓", color = if (current) m.onAccent else m.muted, fontSize = 12.sp)
        }
        Text(sub, color = if (current) m.onAccent else m.muted, style = MaterialTheme.typography.bodySmall, fontWeight = if (current) FontWeight.Bold else FontWeight.Normal, maxLines = 1)
    }
}

/** 서버 시각(ISO)을 폰 시각 "HH:mm"으로 */
fun localHhmm(iso: String): String? = runCatching {
    OffsetDateTime.parse(iso).atZoneSameInstant(ZoneId.systemDefault()).toLocalTime().toString().take(5)
}.getOrNull()

@Composable
private fun MealTable(d: HealthHome, nav: HealthNav, onNavigate: (Screen) -> Unit) {
    val m = LocalModo.current
    val meals = d.card?.meals.orEmpty().associateBy { it.index }
    Column(
        Modifier.fillMaxWidth().clip(RoundedCornerShape(16.dp)).background(m.card).padding(horizontal = 18.dp, vertical = 12.dp),
    ) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Text("오늘 식단", Modifier.weight(1f), style = MaterialTheme.typography.titleMedium)
            TextButton(onClick = { nav.recordsDate = d.day.toString(); onNavigate(Screen.HEALTH_RECORDS) }) { Text("전체 보기 ›", color = m.muted) }
        }
        NutritionTableRow("끼니", null, header = true)
        for (r in d.today.rows.orEmpty()) {
            val open = { nav.openCapture(d.day.toString(), r.index, meals[r.index]); onNavigate(Screen.HEALTH_CAPTURE) }
            when {
                r.logged -> NutritionTableRow(HealthPlan.label(r.index), r.nutrition, onClick = open)
                r.missed -> NutritionTableRow("${HealthPlan.label(r.index)} 놓침", null, faint = true, onClick = open)
                else -> NutritionTableRow("${HealthPlan.label(r.index)} 예정", r.target, faint = true, onClick = open)
            }
        }
        for (e in meals.values.filter { HealthPlan.isExtra(it.index) }.sortedBy { it.index }) {
            NutritionTableRow(HealthPlan.label(e.index), e.nutrition) {
                nav.openCapture(d.day.toString(), e.index, e); onNavigate(Screen.HEALTH_CAPTURE)
            }
        }
        NutritionTableRow("합계", d.today.total, bold = true)
    }
}

private val METRICS = listOf("kcal" to "칼로리", "protein" to "단백질", "carbs" to "탄수화물", "fat" to "지방")

private fun pick(n: Nutrition?, key: String): Int? = n?.let {
    when (key) {
        "kcal" -> it.kcal
        "protein" -> it.protein
        "carbs" -> it.carbs
        else -> it.fat
    }
}

/** 주간 그래프: 지난주(옅게)와 이번 주를 요일별로, 목표선과 함께. 목표를 넘은 것을 경고로 그리지 않는다. */
@Composable
private fun WeeklyCard(w: HealthWeekly) {
    val m = LocalModo.current
    var metric by rememberSaveable { mutableStateOf("kcal") }
    val unit = if (metric == "kcal") "kcal" else "g"
    val target = pick(w.targets, metric) ?: 0
    val days = w.days.orEmpty().sortedBy { it.index }
    val top = max(1, max(target * 13 / 10, days.maxOfOrNull { max(pick(it.current, metric) ?: 0, pick(it.last, metric) ?: 0) } ?: 0))
    val start = runCatching { LocalDate.parse(w.weekStart) }.getOrNull()
    Column(
        Modifier.fillMaxWidth().clip(RoundedCornerShape(16.dp)).background(m.card).padding(horizontal = 18.dp, vertical = 16.dp),
        verticalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Text("주간 그래프", Modifier.weight(1f), style = MaterialTheme.typography.titleMedium)
            if (start != null) Text("${start.monthValue}.${start.dayOfMonth} - ${start.plusDays(6).monthValue}.${start.plusDays(6).dayOfMonth}", style = MaterialTheme.typography.bodySmall, color = m.muted)
        }
        val s = w.summary?.get(metric)
        if (s != null) Text("완료한 ${s.completed}일 중 ${s.achieved}일 달성", style = MaterialTheme.typography.bodyMedium)
        Row(Modifier.fillMaxWidth().height(130.dp), horizontalArrangement = Arrangement.SpaceBetween, verticalAlignment = Alignment.Bottom) {
            for (wd in days) {
                Column(Modifier.weight(1f), horizontalAlignment = Alignment.CenterHorizontally) {
                    Box(Modifier.height(110.dp).fillMaxWidth(), contentAlignment = Alignment.BottomCenter) {
                        // 목표선
                        if (target > 0) Box(Modifier.align(Alignment.BottomCenter).padding(bottom = (110f * target / top).dp).fillMaxWidth().height(1.dp).background(m.muted.copy(alpha = 0.5f)))
                        Row(horizontalArrangement = Arrangement.spacedBy(3.dp), verticalAlignment = Alignment.Bottom) {
                            Bar(pick(wd.last, metric), top, m.selected)
                            Bar(pick(wd.current, metric), top, if (wd.state == "today") m.accent.copy(alpha = 0.6f) else m.accent)
                        }
                    }
                    Text(WEEKDAYS.getOrElse(wd.index) { "" }, style = MaterialTheme.typography.bodySmall, color = if (wd.state == "today") MaterialTheme.colorScheme.onSurface else m.muted)
                }
            }
        }
        Row(horizontalArrangement = Arrangement.spacedBy(6.dp)) {
            for ((k, label) in METRICS) Chip(label, metric == k, Modifier.weight(1f)) { metric = k }
        }
        val label = METRICS.first { it.first == metric }.second
        s?.furthest?.let { f ->
            val more = f.diff > 0
            Text("${WEEKDAYS.getOrElse(f.index) { "" }}요일은 $label 목표보다 ${num(abs(f.diff))} $unit ${if (more) "많았어요" else "적었어요"}", style = MaterialTheme.typography.bodyMedium)
        }
        val avg = pick(w.avg, metric)
        val lastAvg = pick(w.lastAvg, metric)
        if (avg != null || lastAvg != null) {
            Text(
                listOfNotNull(avg?.let { "이번 주 평균 ${num(it)} $unit" }, lastAvg?.let { "지난주 ${num(it)} $unit" }).joinToString(" · "),
                style = MaterialTheme.typography.bodySmall,
                color = m.muted,
            )
        }
    }
}

@Composable
private fun Bar(value: Int?, top: Int, color: androidx.compose.ui.graphics.Color) {
    val h = if (value == null || value <= 0) 0f else max(2f, 110f * value / top)
    Box(Modifier.width(9.dp).height(h.dp).clip(RoundedCornerShape(topStart = 3.dp, topEnd = 3.dp)).background(color))
}

