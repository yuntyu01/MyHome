package com.yuntyu.myhome.ui

import androidx.compose.foundation.background
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.widthIn
import androidx.compose.ui.draw.drawBehind
import androidx.compose.ui.graphics.PathEffect
import androidx.compose.ui.graphics.drawscope.Stroke
import androidx.compose.ui.platform.LocalContext
import com.yuntyu.myhome.core.api.Nutrition
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Icon
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.pulltorefresh.PullToRefreshBox
import androidx.compose.runtime.rememberCoroutineScope
import kotlinx.coroutines.launch
import androidx.compose.material3.Text
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
import com.yuntyu.myhome.core.api.describe
import com.yuntyu.myhome.health.MealOutbox
import com.yuntyu.myhome.health.Reminders
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import java.time.LocalDate
import java.time.LocalDateTime
import java.time.OffsetDateTime
import java.time.ZoneId
import kotlin.math.max

/** 건강 첫 화면(핏두잉 "오늘"): 요일 줄, 끼니 칸, 오늘 합계(탄단지 위주), 끼니마다 박스, 헬스 한 줄, 주간 그래프, 아래 촬영 버튼. */
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

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun HealthScreen(modifier: Modifier, resumeTick: Int, nav: HealthNav, onNavigate: (Screen) -> Unit) {
    var data by remember { mutableStateOf<HealthHome?>(null) }
    var error by remember { mutableStateOf<String?>(null) }
    var refreshing by remember { mutableStateOf(false) }
    val scope = rememberCoroutineScope()
    suspend fun load() {
        loadHealthHome().onSuccess { data = it; error = null }.onFailure { error = it.message }
    }
    LaunchedEffect(resumeTick) { load() }
    val m = LocalModo.current
    // 날짜·합계·그래프 막대를 누르면 그날 기록으로
    val openDay: (LocalDate) -> Unit = { date ->
        if (!date.isAfter(LocalDate.now())) { nav.recordsDate = date.toString(); onNavigate(Screen.HEALTH_RECORDS) }
    }
    Column(modifier.fillMaxSize()) {
        PullToRefreshBox(
            isRefreshing = refreshing,
            onRefresh = { scope.launch { refreshing = true; load(); refreshing = false } },
            modifier = Modifier.weight(1f),
        ) {
        Column(
            Modifier.fillMaxSize().verticalScroll(rememberScrollState()).padding(start = 16.dp, end = 16.dp, top = 4.dp, bottom = 16.dp),
            verticalArrangement = Arrangement.spacedBy(12.dp),
        ) {
            val d = data
            if (d == null) {
                Text(if (error != null) "불러오지 못했어요. 인터넷 연결을 확인해 주세요" else "불러오는 중…", color = m.muted, modifier = Modifier.padding(horizontal = 6.dp))
                return@Column
            }
            if (error != null) Text("새로 못 받았어요", style = MaterialTheme.typography.bodySmall, color = m.over, modifier = Modifier.padding(horizontal = 6.dp))
            WeekStrip(d, openDay)
            if (d.outbox.isNotEmpty()) {
                val failed = d.outbox.values.count { it.error != null }
                Text(
                    if (failed > 0) "못 보낸 끼니 ${failed}개"
                    else "보내는 중 ${d.outbox.size}개",
                    style = MaterialTheme.typography.bodySmall,
                    color = if (failed > 0) m.warn else m.muted,
                    modifier = Modifier.padding(horizontal = 6.dp),
                )
            }
            NutritionTotals(d.today.total, d.today.targets, null, null) { openDay(d.day) }
            MealCards(d, nav, onNavigate)
            GymLine(d) { onNavigate(Screen.HEALTH_GYM) }
            BodyLine(resumeTick) { onNavigate(Screen.BODY) }
            d.weekly?.let { WeeklyCard(it, openDay) }
        }
        }
        // 찍기 버튼은 내용 위에 떠 있지 않고 아래에 붙인다(표·그래프를 가리지 않게)
        val d = data
        if (d != null) {
            val next = currentIndex(d)
            PrimaryButton(
                if (next != null) "사진 찍어서 ${HealthPlan.label(next)} 기록" else "사진 찍어서 기록",
                Modifier.padding(horizontal = 16.dp, vertical = 10.dp),
            ) {
                nav.openCapture(d.day.toString(), next)
                onNavigate(Screen.HEALTH_CAPTURE)
            }
        }
    }
}

/** 지금 찍을 끼니: 찍은 순서대로 쌓인다(마감과 상관없이 빈 앞 번호, 다 찼으면 기타) */
private fun currentIndex(d: HealthHome): Int? {
    val logged = d.card?.meals.orEmpty().map { it.index }.toSet() + d.outbox.keys
    return HealthPlan.nextMealIndex(d.settings.meals.map { it.index }, logged)
}

@Composable
private fun WeekStrip(d: HealthHome, onDay: (LocalDate) -> Unit) {
    val m = LocalModo.current
    val days = d.weekly?.days.orEmpty()
    Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.SpaceBetween) {
        for (i in 0..6) {
            val wd = days.firstOrNull { it.index == i }
            val date = wd?.date?.let { runCatching { LocalDate.parse(it) }.getOrNull() } ?: d.day.minusDays((HealthPlan.weekdayMon0(d.day) - i).toLong())
            val isToday = date == d.day
            Column(
                Modifier.weight(1f).clip(RoundedCornerShape(12.dp)).background(if (isToday) m.card else androidx.compose.ui.graphics.Color.Transparent)
                    .tap(enabled = !date.isAfter(d.day), onClickLabel = "${date.monthValue}월 ${date.dayOfMonth}일 기록") { onDay(date) }
                    .padding(vertical = 8.dp),
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

/** 서버 시각(ISO)을 폰 시각 "HH:mm"으로 */
fun localHhmm(iso: String): String? = runCatching {
    OffsetDateTime.parse(iso).atZoneSameInstant(ZoneId.systemDefault()).toLocalTime().toString().take(5)
}.getOrNull()

/**
 * 끼니 박스(사용자 요청, 2026-11-10): 찍은 끼니가 찍은 순서대로 1끼·2끼처럼 쌓이고, 그 아래 다음 끼니 하나만 점선 박스로.
 * 점선 박스에는 오늘 남은 탄·단·지·칼로리만 보인다. 누르면 바로 촬영 화면.
 */
@Composable
private fun MealCards(d: HealthHome, nav: HealthNav, onNavigate: (Screen) -> Unit) {
    val day = d.day.toString()
    val meals = d.card?.meals.orEmpty().associateBy { it.index }
    fun open(i: Int) { nav.openCapture(day, i, meals[i]); onNavigate(Screen.HEALTH_CAPTURE) }
    // 먹은 끼니, 먹은 시각 순
    val eaten = meals.values.sortedWith(compareBy({ it.loggedAt }, { it.index }))
    for (meal in eaten) EatenMealCard(day, meal) { open(meal.index) }
    // 못 보낸 끼니
    for ((i, e) in d.outbox.filterKeys { it !in meals }.toSortedMap()) {
        PlanCard(HealthPlan.label(i), if (e.error != null) "못 보냄 · 눌러서 다시" else "보내는 중", null, dashed = false) { open(i) }
    }
    // 다음 끼니 하나: 남은 양만
    currentIndex(d)?.let { next ->
        PlanCard("${HealthPlan.label(next)} 예정", leftLine(HealthPlan.remaining(d.today.targets, d.today.total)), null, dashed = true) { open(next) }
    }
}

/** "남은 탄 120g · 단 80g · 지 30g · 900kcal", 넘은 것은 "넘음" */
private fun leftLine(r: Nutrition): String {
    fun part(name: String, v: Int, unit: String) = if (v >= 0) "$name ${num(v)}$unit" else "$name ${num(-v)}$unit 넘음"
    return "남은 " + listOf(part("탄", r.carbs, "g"), part("단", r.protein, "g"), part("지", r.fat, "g"), part("", r.kcal, "kcal").trim()).joinToString(" · ")
}

@Composable
private fun EatenMealCard(day: String, meal: com.yuntyu.myhome.core.api.MealView, onClick: () -> Unit) {
    val ctx = LocalContext.current
    val m = LocalModo.current
    // 폰에 사진이 없으면 서버에서 받아 온다(받으면 다시 그린다)
    var photosTick by remember { mutableStateOf(0) }
    LaunchedEffect(day, meal.index, meal.loggedAt) {
        if (withContext(Dispatchers.IO) { com.yuntyu.myhome.health.MealPhotos.ensureLocal(ctx, day, meal.index) }) photosTick++
    }
    val photos = remember(day, meal.index, photosTick) { com.yuntyu.myhome.health.MealPhotos.photosOf(ctx, day, meal.index) }
    Column(
        Modifier.fillMaxWidth().clip(RoundedCornerShape(16.dp)).background(m.card).tap(onClick = onClick)
            .padding(horizontal = 18.dp, vertical = 16.dp),
        verticalArrangement = Arrangement.spacedBy(10.dp),
    ) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Text(HealthPlan.label(meal.index), style = MaterialTheme.typography.titleMedium)
            Text(localHhmm(meal.loggedAt)?.let { "  ${it}에 먹음" } ?: "", Modifier.weight(1f), style = MaterialTheme.typography.bodySmall, color = m.muted)
            Text("${num(meal.nutrition.kcal)} kcal", style = MaterialTheme.typography.bodyMedium, color = m.muted)
        }
        if (photos.isNotEmpty()) {
            Row(Modifier.horizontalScroll(rememberScrollState()), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                for (f in photos) Thumb(f, 76)
            }
        }
        val foods = meal.items.orEmpty().map { it.name }.filter { it.isNotBlank() }
        if (foods.isNotEmpty()) Text(foods.joinToString(", "), style = MaterialTheme.typography.bodyLarge)
        Row(horizontalArrangement = Arrangement.spacedBy(16.dp)) {
            MacroCell("탄수화물", meal.nutrition.carbs)
            MacroCell("단백질", meal.nutrition.protein)
            MacroCell("지방", meal.nutrition.fat)
        }
    }
}

@Composable
private fun MacroCell(label: String, grams: Int) {
    val m = LocalModo.current
    Row(verticalAlignment = Alignment.Bottom) {
        Text(label, style = MaterialTheme.typography.bodySmall, color = m.muted)
        Text(" ${num(grams)}g", style = MaterialTheme.typography.bodyMedium, fontWeight = FontWeight.SemiBold)
    }
}

/** 아직 안 먹은 끼니(점선) 또는 보내는 중인 끼니 박스. target이 있으면 이 끼니에 먹으면 좋은 양 */
@Composable
private fun PlanCard(title: String, sub: String, target: Nutrition?, dashed: Boolean, faint: Boolean = false, onClick: () -> Unit) {
    val m = LocalModo.current
    val shape = RoundedCornerShape(16.dp)
    val lineColor = m.faint
    Column(
        Modifier.fillMaxWidth().clip(shape)
            .then(
                if (dashed) Modifier.drawBehind {
                    val w = 1.5.dp.toPx()
                    drawRoundRect(
                        color = lineColor,
                        topLeft = androidx.compose.ui.geometry.Offset(w / 2, w / 2),
                        size = androidx.compose.ui.geometry.Size(size.width - w, size.height - w),
                        cornerRadius = androidx.compose.ui.geometry.CornerRadius(16.dp.toPx()),
                        style = Stroke(width = w, pathEffect = PathEffect.dashPathEffect(floatArrayOf(10.dp.toPx(), 6.dp.toPx()))),
                    )
                } else Modifier.background(m.card),
            )
            .tap(onClick = onClick).padding(horizontal = 18.dp, vertical = 16.dp),
        verticalArrangement = Arrangement.spacedBy(6.dp),
    ) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Text(title, Modifier.weight(1f), style = MaterialTheme.typography.titleMedium, color = if (faint) m.muted else MaterialTheme.colorScheme.onSurface)
            Icon(CameraIcon, contentDescription = "기록", tint = m.muted, modifier = Modifier.size(20.dp))
        }
        Text(sub, style = MaterialTheme.typography.bodySmall, color = m.muted)
    }
}

/** 헬스: 위 끼니 칸에서 빼서 여기 한 줄로 */
@Composable
private fun GymLine(d: HealthHome, onClick: () -> Unit) {
    val g = d.today.gym
    if (!g.enabled) return
    val m = LocalModo.current
    val rest = d.card?.gymVerifiedBy == "rest"
    val today = when {
        rest -> "오늘은 쉬는 날"
        d.card?.gym == true -> "오늘 다녀왔어요"
        g.scheduledToday -> "오늘 ${g.arriveBy}까지 가는 날"
        else -> "오늘은 안 가는 날"
    }
    Row(
        Modifier.fillMaxWidth().clip(RoundedCornerShape(16.dp)).background(m.card).tap(onClick = onClick).padding(horizontal = 18.dp, vertical = 16.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(2.dp)) {
            Text("헬스", style = MaterialTheme.typography.titleMedium)
            Text(today, style = MaterialTheme.typography.bodySmall, color = m.muted)
        }
        Text("이번 주 ${g.done}/${g.planned}회", style = MaterialTheme.typography.bodyMedium, color = m.muted)
    }
}

private val METRICS = listOf("carbs" to "탄수화물", "protein" to "단백질", "fat" to "지방", "kcal" to "칼로리")

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
private fun WeeklyCard(w: HealthWeekly, onDay: (LocalDate) -> Unit) {
    val m = LocalModo.current
    var metric by rememberSaveable { mutableStateOf("carbs") }
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
        if (s != null) Text("${s.achieved}/${s.completed}일 달성", style = MaterialTheme.typography.bodyMedium)
        Row(Modifier.fillMaxWidth().height(130.dp), horizontalArrangement = Arrangement.SpaceBetween, verticalAlignment = Alignment.Bottom) {
            for (wd in days) {
                val date = runCatching { LocalDate.parse(wd.date) }.getOrNull() ?: start?.plusDays(wd.index.toLong())
                Column(
                    Modifier.weight(1f).then(if (date != null) Modifier.tap { onDay(date) } else Modifier),
                    horizontalAlignment = Alignment.CenterHorizontally,
                ) {
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
        MetricTabs(metric) { metric = it }
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


/** 주간 그래프 기준: 글자 탭(고른 것은 진하게 + 밑줄). 좁은 폰에서도 잘리지 않는다 */
@Composable
private fun MetricTabs(metric: String, onPick: (String) -> Unit) {
    val m = LocalModo.current
    Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.spacedBy(18.dp)) {
        for ((k, label) in METRICS) {
            val on = metric == k
            Column(Modifier.tap { onPick(k) }.padding(vertical = 6.dp), horizontalAlignment = Alignment.CenterHorizontally) {
                Text(label, fontWeight = if (on) FontWeight.Bold else FontWeight.Normal, color = if (on) MaterialTheme.colorScheme.onSurface else m.muted, maxLines = 1)
                Spacer(Modifier.height(6.dp))
                Box(Modifier.height(2.dp).width(24.dp).background(if (on) MaterialTheme.colorScheme.onSurface else androidx.compose.ui.graphics.Color.Transparent))
            }
        }
    }
}
