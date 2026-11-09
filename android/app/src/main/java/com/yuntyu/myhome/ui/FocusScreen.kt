package com.yuntyu.myhome.ui

import android.widget.Toast
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
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
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.MoreVert
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableLongStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import com.yuntyu.myhome.BuildConfig
import com.yuntyu.myhome.MyHomeApp
import com.yuntyu.myhome.core.FocusDay
import com.yuntyu.myhome.core.FocusSpan
import com.yuntyu.myhome.core.FocusStats
import com.yuntyu.myhome.core.FocusSubjects
import com.yuntyu.myhome.core.formatHm
import com.yuntyu.myhome.core.formatHms
import com.yuntyu.myhome.data.FocusSession
import com.yuntyu.myhome.data.FocusSubject
import com.yuntyu.myhome.focus.FocusSync
import com.yuntyu.myhome.focus.FocusTimer
import com.yuntyu.myhome.focus.StudyPlaces
import com.yuntyu.myhome.health.GymGeofence
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import java.time.Instant
import java.time.LocalDate
import java.time.format.DateTimeFormatter

/*
 * 집중 타이머(열품타 느낌): 날짜 · 오늘 순공 큰 시간 · 타이머/통계/장소 탭.
 * 타이머 탭은 과목 줄(색 동그라미 재생 버튼 · 이름 · 오늘 시간 · ⋮)이고, 누르면 시작, 다시 누르면 멈춘다(한 번에 하나).
 * 하루는 05:00에 바뀐다(FocusDay). 기록은 폰에 먼저 남고 서버로 올라간다(FocusSync).
 */

private val Tabular = TextStyle(fontFeatureSettings = "tnum")
private val TimeFmt = DateTimeFormatter.ofPattern("HH:mm")
private val Dows = listOf("월", "화", "수", "목", "금", "토", "일")

private fun parseColor(hex: String): Color = runCatching { Color(android.graphics.Color.parseColor(hex)) }.getOrDefault(Color.Gray)
private fun clock(ms: Long): String = Instant.ofEpochMilli(ms).atZone(FocusDay.KST).format(TimeFmt)
private fun dayLabel(d: LocalDate): String = "${d.monthValue}. ${d.dayOfMonth} (${Dows[d.dayOfWeek.value - 1]})"

@Composable
fun FocusScreen(modifier: Modifier, resumeTick: Int) {
    val ctx = LocalContext.current
    val scope = rememberCoroutineScope()
    val m = LocalModo.current
    val dao = remember { MyHomeApp.instance.db.focus() }
    val running by remember { FocusTimer.running(ctx) }.collectAsState()
    var now by remember { mutableLongStateOf(System.currentTimeMillis()) }
    // 장소 설정·들어와 있는 곳은 설정 파일에 있어서 바뀌면 다시 읽는다(지오펜스는 화면 밖에서 바꾼다)
    var placeTick by remember { mutableIntStateOf(0) }
    val openStays = remember(now / 5_000, placeTick, resumeTick) { StudyPlaces.openStays(ctx) }
    // 도는 동안·장소에 있는 동안은 1초마다, 아니면 날이 바뀌는 것만 보면 되니 30초마다
    val ticking = running != null || openStays.isNotEmpty()
    LaunchedEffect(ticking) {
        while (true) {
            now = System.currentTimeMillis()
            delay(if (ticking) 1_000 else 30_000)
        }
    }
    // 앱을 새로 깔았으면 서버 기록을 한 번 내려받는다
    LaunchedEffect(Unit) { FocusSync.restoreOnce(ctx) }

    val today = FocusDay.dayOf(now)
    val weekFrom = FocusDay.startOf(today.minusDays(6))
    val dayEnd = FocusDay.endOf(today)
    val subjects by remember { dao.subjects() }.collectAsState(initial = null)
    val allSubjects by remember { dao.allSubjects() }.collectAsState(initial = emptyList())
    val sessions by remember(weekFrom) { dao.sessionsBetween(weekFrom, dayEnd) }.collectAsState(initial = emptyList())
    val stays by remember(weekFrom) { dao.staysBetween(weekFrom, dayEnd) }.collectAsState(initial = emptyList())

    // 도는 타이머·들어와 있는 곳은 지금까지로 센다
    val sessionSpans = sessions.map { FocusSpan(it.subjectId, it.startedAt, it.endedAt) } +
        listOfNotNull(running?.let { FocusSpan(it.subjectId, it.startedAt, now) })
    // 5분 안에 다시 들어와 이어 가는 중이면 남겨 둔 줄 대신 들어와 있는 것(같은 시작~지금)으로 센다(두 번 보이지 않게)
    val resumedIds = openStays.filter { it.resumed }.map { it.stayId }.toSet()
    val staySpans = stays.filterNot { it.id in resumedIds }.map { FocusSpan(it.place, it.enteredAt, it.exitedAt) } +
        openStays.map { FocusSpan(it.name, it.enteredAt, now) }
    val bySubject = FocusStats.bySubject(sessionSpans, today)
    val focusToday = FocusStats.focusTotal(sessionSpans, today)
    val stayToday = FocusStats.stayTotal(staySpans, today)
    val runningName = running?.let { r -> allSubjects.firstOrNull { it.id == r.subjectId }?.name }

    var tab by rememberSaveable { mutableIntStateOf(0) }

    Column(modifier.fillMaxSize()) {
        // 머리: 가운데 날짜, 공부 장소에 있으면 왼쪽에 장소
        Box(Modifier.fillMaxWidth().height(56.dp).padding(horizontal = 16.dp)) {
            openStays.firstOrNull()?.let { o ->
                Row(
                    Modifier.align(Alignment.CenterStart).clip(RoundedCornerShape(12.dp)).background(m.pill)
                        .clickable { tab = 2 }.padding(horizontal = 10.dp, vertical = 6.dp),
                    verticalAlignment = Alignment.CenterVertically,
                    horizontalArrangement = Arrangement.spacedBy(4.dp),
                ) {
                    Icon(PinIcon, contentDescription = null, tint = m.muted, modifier = Modifier.size(14.dp))
                    Text(o.name, fontSize = 13.sp, color = m.muted, maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.width(72.dp))
                }
            }
            Text(dayLabel(today), Modifier.align(Alignment.Center), fontSize = 18.sp, fontWeight = FontWeight.Medium)
        }
        Text(
            formatHms(focusToday),
            Modifier.fillMaxWidth().padding(top = 20.dp),
            textAlign = TextAlign.Center,
            fontSize = 64.sp,
            fontWeight = FontWeight.SemiBold,
            letterSpacing = (-1).sp,
            style = Tabular,
            color = if (running != null) MaterialTheme.colorScheme.onBackground else m.accent,
        )
        val ratio = FocusStats.ratioPercent(focusToday, stayToday)
        Text(
            when {
                runningName != null -> "$runningName 하는 중"
                stayToday > 0 && ratio != null -> "머문 시간 ${formatHms(stayToday)} · 집중 $ratio%"
                else -> "오늘 순공 시간"
            },
            Modifier.fillMaxWidth().padding(top = 4.dp, bottom = 20.dp),
            textAlign = TextAlign.Center,
            fontSize = 14.sp,
            color = m.muted,
        )
        EvenTabs(listOf("타이머", "통계", "장소"), tab) { tab = it }
        Column(Modifier.weight(1f).verticalScroll(rememberScrollState())) {
            when (tab) {
                0 -> TimerTab(subjects, bySubject, running?.subjectId) { scope.launch { FocusTimer.toggle(ctx, it) } }
                1 -> StatsTab(today, sessions, sessionSpans, staySpans, allSubjects, bySubject, focusToday, stayToday)
                else -> PlacesTab(today, now, staySpans, resumeTick) { placeTick++ }
            }
        }
    }
}

/** 칸을 똑같이 나눈 글자 탭(고른 칸 아래 짧은 막대) */
@Composable
private fun EvenTabs(items: List<String>, selected: Int, onSelect: (Int) -> Unit) {
    val m = LocalModo.current
    Column(Modifier.fillMaxWidth()) {
        Row(Modifier.fillMaxWidth().padding(horizontal = 8.dp)) {
            items.forEachIndexed { i, label ->
                val on = i == selected
                Column(
                    Modifier.weight(1f).clickable(role = Role.Tab) { onSelect(i) }.padding(top = 10.dp),
                    horizontalAlignment = Alignment.CenterHorizontally,
                ) {
                    Text(label, fontSize = 16.sp, fontWeight = if (on) FontWeight.SemiBold else FontWeight.Normal, color = if (on) MaterialTheme.colorScheme.onBackground else m.muted)
                    Spacer(Modifier.height(10.dp))
                    Box(Modifier.width(44.dp).height(3.dp).clip(RoundedCornerShape(2.dp)).background(if (on) MaterialTheme.colorScheme.onBackground else Color.Transparent))
                }
            }
        }
        Hairline()
    }
}

@Composable
private fun TimerTab(subjects: List<FocusSubject>?, bySubject: Map<String, Long>, runningId: String?, onToggle: (String) -> Unit) {
    val ctx = LocalContext.current
    val scope = rememberCoroutineScope()
    val m = LocalModo.current
    var editing by remember { mutableStateOf<FocusSubject?>(null) }
    var adding by remember { mutableStateOf(false) }
    var hiding by remember { mutableStateOf<FocusSubject?>(null) }
    val list = subjects ?: return

    if (list.isEmpty()) {
        EmptyNote("과목을 추가해 주세요", "KoDeploy, 코테처럼 할 일마다 시간을 따로 재요. 누르면 시작하고 다시 누르면 멈춰요.")
    }
    list.forEachIndexed { i, s ->
        SubjectRow(
            s,
            ms = bySubject[s.id] ?: 0L,
            running = s.id == runningId,
            first = i == 0,
            last = i == list.lastIndex,
            onToggle = { onToggle(s.id) },
            onEdit = { editing = s },
            onMove = { d -> scope.launch { FocusTimer.move(ctx, list, s.id, d) } },
            onHide = { hiding = s },
        )
    }
    Row(
        Modifier.padding(start = Gutter, top = 12.dp, bottom = 32.dp).clip(RoundedCornerShape(12.dp)).background(m.pill)
            .clickable(role = Role.Button) { adding = true }.padding(horizontal = 14.dp, vertical = 10.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(6.dp),
    ) {
        Icon(PlusIcon, contentDescription = null, tint = m.muted, modifier = Modifier.size(18.dp))
        Text("과목 추가", fontSize = 15.sp, color = m.muted)
    }

    if (adding || editing != null) {
        val e = editing
        SubjectDialog(
            initial = e,
            usedColors = list.map { it.color },
            onDismiss = { adding = false; editing = null },
            onSave = { name, color ->
                scope.launch {
                    val ok = if (e == null) FocusTimer.addSubject(ctx, name, color) != null else FocusTimer.editSubject(ctx, e, name, color)
                    if (!ok) Toast.makeText(ctx, "이름은 1~${FocusSubjects.MAX_NAME}자로 적어 주세요", Toast.LENGTH_SHORT).show()
                }
                adding = false
                editing = null
            },
        )
    }
    hiding?.let { s ->
        AlertDialog(
            onDismissRequest = { hiding = null },
            title = { Text("'${s.name}' 숨기기") },
            text = { Text("목록에서 빼요. 지난 기록과 통계는 그대로 남아요.") },
            confirmButton = { TextButton(onClick = { scope.launch { FocusTimer.archive(ctx, s) }; hiding = null }) { Text("숨기기") } },
            dismissButton = { TextButton(onClick = { hiding = null }) { Text("취소") } },
        )
    }
}

@Composable
private fun SubjectRow(
    s: FocusSubject,
    ms: Long,
    running: Boolean,
    first: Boolean,
    last: Boolean,
    onToggle: () -> Unit,
    onEdit: () -> Unit,
    onMove: (Int) -> Unit,
    onHide: () -> Unit,
) {
    val m = LocalModo.current
    var menu by remember { mutableStateOf(false) }
    Row(
        Modifier.fillMaxWidth().heightIn(min = 76.dp).clickable(onClickLabel = if (running) "멈추기" else "시작", onClick = onToggle)
            .padding(start = Gutter, end = 4.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Box(Modifier.size(44.dp).clip(CircleShape).background(parseColor(s.color)), contentAlignment = Alignment.Center) {
            Icon(
                if (running) PauseFilledIcon else PlayFilledIcon,
                contentDescription = if (running) "멈추기" else "시작",
                tint = Color.White,
                modifier = Modifier.size(22.dp),
            )
        }
        Spacer(Modifier.width(16.dp))
        Text(
            s.name,
            Modifier.weight(1f),
            fontSize = 18.sp,
            fontWeight = if (running) FontWeight.SemiBold else FontWeight.Normal,
            maxLines = 1,
            overflow = TextOverflow.Ellipsis,
        )
        Text(
            formatHms(ms),
            fontSize = 18.sp,
            style = Tabular,
            color = if (running || ms > 0) MaterialTheme.colorScheme.onBackground else m.muted,
        )
        Box {
            IconButton(onClick = { menu = true }) { Icon(Icons.Filled.MoreVert, contentDescription = "${s.name} 더보기", tint = m.muted) }
            DropdownMenu(expanded = menu, onDismissRequest = { menu = false }) {
                DropdownMenuItem(text = { Text("이름·색 바꾸기") }, onClick = { menu = false; onEdit() })
                if (!first) DropdownMenuItem(text = { Text("위로") }, onClick = { menu = false; onMove(-1) })
                if (!last) DropdownMenuItem(text = { Text("아래로") }, onClick = { menu = false; onMove(1) })
                DropdownMenuItem(text = { Text("숨기기") }, onClick = { menu = false; onHide() })
            }
        }
    }
}

@Composable
private fun SubjectDialog(initial: FocusSubject?, usedColors: List<String>, onDismiss: () -> Unit, onSave: (String, String) -> Unit) {
    var name by remember { mutableStateOf(initial?.name ?: "") }
    var color by remember { mutableStateOf(initial?.color ?: FocusSubjects.nextColor(usedColors)) }
    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text(if (initial == null) "과목 추가" else "과목 바꾸기") },
        text = {
            Column(verticalArrangement = Arrangement.spacedBy(16.dp)) {
                LineField(name, { name = it.take(FocusSubjects.MAX_NAME) }, "이름 (예: 코테)")
                Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.SpaceBetween) {
                    for (c in FocusSubjects.PALETTE) {
                        val on = c.equals(color, ignoreCase = true)
                        Box(
                            Modifier.size(28.dp).clip(CircleShape).background(parseColor(c))
                                .then(if (on) Modifier.border(2.dp, MaterialTheme.colorScheme.onBackground, CircleShape) else Modifier)
                                .clickable(role = Role.RadioButton, onClickLabel = "이 색") { color = c },
                            contentAlignment = Alignment.Center,
                        ) {
                            if (on) Icon(CheckIcon, contentDescription = "고른 색", tint = Color.White, modifier = Modifier.size(16.dp))
                        }
                    }
                }
            }
        },
        confirmButton = { TextButton(onClick = { onSave(name, color) }, enabled = FocusSubjects.cleanName(name) != null) { Text("저장") } },
        dismissButton = { TextButton(onClick = onDismiss) { Text("취소") } },
    )
}

@Composable
private fun StatsTab(
    today: LocalDate,
    sessions: List<FocusSession>,
    sessionSpans: List<FocusSpan>,
    staySpans: List<FocusSpan>,
    allSubjects: List<FocusSubject>,
    bySubject: Map<String, Long>,
    focusToday: Long,
    stayToday: Long,
) {
    val ctx = LocalContext.current
    val scope = rememberCoroutineScope()
    val m = LocalModo.current
    val byId = allSubjects.associateBy { it.id }
    var deleting by remember { mutableStateOf<FocusSession?>(null) }

    SectionLabel("오늘")
    FlatRow("순공 시간", value = formatHms(focusToday), valueColor = MaterialTheme.colorScheme.onBackground)
    FlatRow("머문 시간", sub = if (stayToday == 0L) "장소 탭에서 공부 장소를 정하면 자동으로 재요" else null, value = formatHms(stayToday))
    FlatRow("집중률", sub = "머문 시간 중 타이머를 켠 비율", value = FocusStats.ratioPercent(focusToday, stayToday)?.let { "$it%" } ?: "—")

    SectionLabel("과목별")
    val rows = bySubject.entries.sortedByDescending { it.value }
    if (rows.isEmpty()) {
        Text("오늘 잰 시간이 아직 없어요", Modifier.padding(horizontal = Gutter, vertical = 12.dp), fontSize = 14.sp, color = m.muted)
    }
    val max = rows.maxOfOrNull { it.value } ?: 1L
    for ((id, ms) in rows) {
        val s = byId[id]
        Column(Modifier.fillMaxWidth().padding(horizontal = Gutter, vertical = 10.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Dot(parseColor(s?.color ?: "#888888"), 10.dp)
                Spacer(Modifier.width(10.dp))
                Text(s?.name ?: "지운 과목", Modifier.weight(1f), fontSize = 16.sp, maxLines = 1, overflow = TextOverflow.Ellipsis)
                Text(formatHms(ms), fontSize = 16.sp, style = Tabular)
            }
            ModoBar(ms.toDouble() / max, parseColor(s?.color ?: "#888888"))
        }
    }

    SectionLabel("최근 7일")
    WeekBars(today, sessionSpans, staySpans)

    SectionLabel("오늘 기록")
    val todays = sessions.filter { FocusDay.overlap(it.startedAt, it.endedAt, today) > 0 }.sortedByDescending { it.startedAt }
    if (todays.isEmpty()) {
        Text("멈춘 기록이 여기에 쌓여요", Modifier.padding(horizontal = Gutter, vertical = 12.dp), fontSize = 14.sp, color = m.muted)
    }
    for (x in todays) {
        val s = byId[x.subjectId]
        FlatRow(
            s?.name ?: "지운 과목",
            sub = "${clock(x.startedAt)} – ${clock(x.endedAt)}",
            value = formatHms(x.endedAt - x.startedAt),
            leading = { Dot(parseColor(s?.color ?: "#888888"), 10.dp) },
            trailing = {
                var menu by remember { mutableStateOf(false) }
                Box {
                    IconButton(onClick = { menu = true }) { Icon(Icons.Filled.MoreVert, contentDescription = "기록 더보기", tint = m.muted) }
                    DropdownMenu(expanded = menu, onDismissRequest = { menu = false }) {
                        DropdownMenuItem(text = { Text("지우기") }, onClick = { menu = false; deleting = x })
                    }
                }
            },
        )
    }
    Spacer(Modifier.height(32.dp))

    deleting?.let { x ->
        AlertDialog(
            onDismissRequest = { deleting = null },
            title = { Text("기록 지우기") },
            text = { Text("${clock(x.startedAt)} – ${clock(x.endedAt)} (${formatHm(x.endedAt - x.startedAt)}) 기록을 지울까요?") },
            confirmButton = { TextButton(onClick = { scope.launch { FocusTimer.deleteSession(ctx, x) }; deleting = null }) { Text("지우기") } },
            dismissButton = { TextButton(onClick = { deleting = null }) { Text("취소") } },
        )
    }
}

/** 최근 7일 막대: 진한 막대는 순공, 뒤의 옅은 막대는 머문 시간 */
@Composable
private fun WeekBars(today: LocalDate, sessionSpans: List<FocusSpan>, staySpans: List<FocusSpan>) {
    val m = LocalModo.current
    val days = (6 downTo 0).map { today.minusDays(it.toLong()) }
    val data = days.map { d -> Triple(d, FocusStats.focusTotal(sessionSpans, d), FocusStats.stayTotal(staySpans, d)) }
    val max = maxOf(3_600_000L, data.maxOf { maxOf(it.second, it.third) })
    val barMax = 120.dp
    Row(Modifier.fillMaxWidth().padding(horizontal = Gutter, vertical = 8.dp), verticalAlignment = Alignment.Bottom) {
        for ((d, f, st) in data) {
            val isToday = d == today
            Column(Modifier.weight(1f), horizontalAlignment = Alignment.CenterHorizontally, verticalArrangement = Arrangement.spacedBy(6.dp)) {
                Text(if (f > 0) "%.1f".format(f / 3_600_000.0) else "", fontSize = 11.sp, color = m.muted, style = Tabular)
                Box(Modifier.width(18.dp).height(barMax), contentAlignment = Alignment.BottomCenter) {
                    if (st > 0) Box(Modifier.fillMaxWidth().height(barMax * (st.toFloat() / max)).clip(RoundedCornerShape(4.dp)).background(m.line))
                    if (f > 0) Box(Modifier.fillMaxWidth().height(barMax * (f.toFloat() / max)).clip(RoundedCornerShape(4.dp)).background(if (isToday) m.cta else m.accent))
                }
                Text(Dows[d.dayOfWeek.value - 1], fontSize = 12.sp, color = if (isToday) MaterialTheme.colorScheme.onBackground else m.muted, fontWeight = if (isToday) FontWeight.SemiBold else FontWeight.Normal)
            }
        }
    }
    Text("숫자는 순공 시간(시간), 옅은 막대는 머문 시간", Modifier.padding(horizontal = Gutter, vertical = 4.dp), fontSize = 12.sp, color = m.faint)
}

@Composable
private fun PlacesTab(today: LocalDate, now: Long, staySpans: List<FocusSpan>, resumeTick: Int, onChanged: () -> Unit) {
    val ctx = LocalContext.current
    val scope = rememberCoroutineScope()
    val m = LocalModo.current
    var tick by remember { mutableIntStateOf(0) }
    fun changed() { tick++; onChanged() }
    val places = remember(tick) { StudyPlaces.places(ctx) }
    val enabled = remember(tick) { StudyPlaces.enabled(ctx) }
    val fg = remember(tick, resumeTick) { GymGeofence.hasForeground(ctx) }
    val bg = remember(tick, resumeTick) { GymGeofence.hasBackground(ctx) }
    val regError = remember(tick, resumeTick) { StudyPlaces.registrationError(ctx) }
    val open = remember(tick, now / 5_000) { StudyPlaces.openStays(ctx) }
    var busy by remember { mutableStateOf(false) }
    // 지금 위치를 잡은 뒤 이름을 받는다
    var naming by remember { mutableStateOf<android.location.Location?>(null) }
    var renaming by remember { mutableStateOf<StudyPlaces.Place?>(null) }
    var removing by remember { mutableStateOf<StudyPlaces.Place?>(null) }
    // Play 정책: 백그라운드 위치를 요청하기 전에 앱 안에서 먼저 알리고 동의를 받는다
    var disclosure by remember { mutableStateOf(false) }

    fun locateHere() {
        busy = true
        scope.launch {
            val loc = GymGeofence.currentLocation(ctx)
            busy = false
            if (loc == null) Toast.makeText(ctx, "위치를 잡지 못했어요. 위치(GPS)를 켜고 다시 해 주세요", Toast.LENGTH_LONG).show()
            else naming = loc
        }
    }
    val askFine = rememberLauncherForActivityResult(ActivityResultContracts.RequestMultiplePermissions()) { r ->
        changed()
        if (r[android.Manifest.permission.ACCESS_FINE_LOCATION] == true) locateHere()
        else Toast.makeText(ctx, "정확한 위치 권한이 있어야 공부 장소를 정할 수 있어요", Toast.LENGTH_LONG).show()
    }
    val askBackground = rememberLauncherForActivityResult(ActivityResultContracts.RequestPermission()) {
        StudyPlaces.register(ctx) { changed() }
        changed()
    }
    // 설정에서 '항상 허용'을 고르거나 위치를 켜고 돌아오면 다시 건다
    LaunchedEffect(resumeTick) { StudyPlaces.register(ctx) { changed() } }

    Text(
        "도서관·카페처럼 공부하는 곳에서 위치를 저장해 두면, 그곳에 머문 시간을 자동으로 재서 순공 시간과 견줘요. 위치는 이 폰에만 저장되고 서버에는 장소 이름과 머문 시각만 가요.",
        Modifier.padding(horizontal = Gutter, vertical = 16.dp),
        fontSize = 14.sp,
        color = m.muted,
        lineHeight = 20.sp,
    )
    Hairline()
    for (p in places) {
        val inside = open.any { it.placeId == p.id }
        val ms = FocusStats.stayTotal(staySpans.filter { it.key == p.name }, today)
        var menu by remember(p.id) { mutableStateOf(false) }
        FlatRow(
            p.name,
            sub = "반경 ${p.radiusM}m" + if (inside) " · 지금 여기 있어요" else "",
            subColor = if (inside) m.ok else null,
            value = formatHms(ms),
            leading = { Icon(PinIcon, contentDescription = null, tint = if (inside) m.ok else m.muted, modifier = Modifier.size(20.dp)) },
            trailing = {
                Box {
                    IconButton(onClick = { menu = true }) { Icon(Icons.Filled.MoreVert, contentDescription = "${p.name} 더보기", tint = m.muted) }
                    DropdownMenu(expanded = menu, onDismissRequest = { menu = false }) {
                        DropdownMenuItem(text = { Text("이름 바꾸기") }, onClick = { menu = false; renaming = p })
                        for (r in listOf(100, 150, 250)) {
                            if (r != p.radiusM) DropdownMenuItem(text = { Text("반경 ${r}m") }, onClick = { menu = false; StudyPlaces.setRadius(ctx, p.id, r); changed() })
                        }
                        DropdownMenuItem(text = { Text("지우기") }, onClick = { menu = false; removing = p })
                    }
                }
            },
        )
        Hairline(Modifier.padding(start = Gutter))
    }
    if (places.size < StudyPlaces.MAX_PLACES) {
        AddRow(if (busy) "위치 잡는 중…" else "지금 위치를 공부 장소로") {
            if (busy) return@AddRow
            if (fg) locateHere()
            else askFine.launch(arrayOf(android.Manifest.permission.ACCESS_FINE_LOCATION, android.Manifest.permission.ACCESS_COARSE_LOCATION))
        }
    }
    Hairline()

    if (places.isNotEmpty()) {
        SwitchRow(
            "머문 시간 자동 기록",
            checked = enabled,
            sub = when {
                !BuildConfig.GYM_AUTO -> "이 빌드는 앱이 꺼져 있을 때 위치를 쓰지 않아요"
                enabled && !bg -> "위치 권한을 '항상 허용'으로 바꿔야 앱이 꺼져 있어도 재요"
                enabled && regError != null -> regError
                enabled -> "켜져 있어요. 장소에 들어오고 나가는 시각을 남겨요(3분 넘게 머문 것만)"
                else -> "켜면 장소에 들어오고 나가는 시각을 자동으로 남겨요"
            },
        ) { on ->
            if (on) disclosure = true
            else scope.launch { StudyPlaces.setEnabled(ctx, false); changed() }
        }
        if (enabled && !bg && BuildConfig.GYM_AUTO) {
            Text(
                "'항상 허용' 켜기",
                Modifier.padding(horizontal = Gutter, vertical = 8.dp).clickable { disclosure = true },
                fontSize = 15.sp,
                color = m.warn,
            )
        }
    }

    SectionLabel("오늘 머문 기록")
    val todaysStays = staySpans.filter { FocusDay.overlap(it.startMs, it.endMs, today) > 0 }.sortedByDescending { it.startMs }
    if (todaysStays.isEmpty()) {
        Text("공부 장소에 들어오고 나간 기록이 여기에 쌓여요", Modifier.padding(horizontal = Gutter, vertical = 12.dp), fontSize = 14.sp, color = m.muted)
    }
    for (s in todaysStays) {
        val isOpen = s.endMs >= now - 1_000 && open.any { it.name == s.key && it.enteredAt == s.startMs }
        FlatRow(s.key, sub = "${clock(s.startMs)} – ${if (isOpen) "지금" else clock(s.endMs)}", value = formatHms(s.endMs - s.startMs))
    }
    Spacer(Modifier.height(32.dp))

    naming?.let { loc ->
        NameDialog("공부 장소 이름", "", onDismiss = { naming = null }) { name ->
            val p = StudyPlaces.add(ctx, name, loc)
            if (p == null) Toast.makeText(ctx, "이름은 1~${FocusSubjects.MAX_NAME}자로 적어 주세요", Toast.LENGTH_SHORT).show()
            else Toast.makeText(ctx, "여기를 '${p.name}'(으)로 저장했어요 (오차 ${loc.accuracy.toInt()}m)", Toast.LENGTH_SHORT).show()
            naming = null
            changed()
        }
    }
    renaming?.let { p ->
        NameDialog("이름 바꾸기", p.name, onDismiss = { renaming = null }) { name ->
            if (!StudyPlaces.rename(ctx, p.id, name)) Toast.makeText(ctx, "이름은 1~${FocusSubjects.MAX_NAME}자로 적어 주세요", Toast.LENGTH_SHORT).show()
            renaming = null
            changed()
        }
    }
    removing?.let { p ->
        AlertDialog(
            onDismissRequest = { removing = null },
            title = { Text("'${p.name}' 지우기") },
            text = { Text("이 장소의 지오펜스를 지워요. 지난 머문 기록은 남아요.") },
            confirmButton = { TextButton(onClick = { scope.launch { StudyPlaces.remove(ctx, p.id); changed() }; removing = null }) { Text("지우기") } },
            dismissButton = { TextButton(onClick = { removing = null }) { Text("취소") } },
        )
    }
    if (disclosure) {
        AlertDialog(
            onDismissRequest = { disclosure = false },
            title = { Text("위치 정보 사용 안내") },
            text = {
                Text(
                    "MyHome은 공부 장소에 머문 시간을 재기 위해 앱이 닫혀 있거나 사용 중이 아닐 때에도 위치 정보를 수집합니다.\n\n" +
                        "위치는 저장한 공부 장소에 들어오고 나갔는지 확인하는 데만 쓰고, 장소 좌표는 이 폰에만 저장합니다. " +
                        "서버에는 장소 이름과 들어오고 나간 시각만 보내고 위치나 이동 경로는 보내지 않습니다.\n\n" +
                        "다음 화면에서 위치 권한을 '항상 허용'으로 골라 주세요. 언제든 이 화면에서 끌 수 있어요.",
                )
            },
            confirmButton = {
                TextButton(onClick = {
                    disclosure = false
                    scope.launch {
                        StudyPlaces.setEnabled(ctx, true)
                        changed()
                        if (!bg && android.os.Build.VERSION.SDK_INT >= android.os.Build.VERSION_CODES.Q) {
                            askBackground.launch(android.Manifest.permission.ACCESS_BACKGROUND_LOCATION)
                        }
                    }
                }) { Text("동의하고 계속") }
            },
            dismissButton = { TextButton(onClick = { disclosure = false }) { Text("취소") } },
        )
    }
}

@Composable
private fun NameDialog(title: String, initial: String, onDismiss: () -> Unit, onSave: (String) -> Unit) {
    var name by remember { mutableStateOf(initial) }
    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text(title) },
        text = { LineField(name, { name = it.take(FocusSubjects.MAX_NAME) }, "이름 (예: 도서관)") },
        confirmButton = { TextButton(onClick = { onSave(name) }, enabled = FocusSubjects.cleanName(name) != null) { Text("저장") } },
        dismissButton = { TextButton(onClick = onDismiss) { Text("취소") } },
    )
}
