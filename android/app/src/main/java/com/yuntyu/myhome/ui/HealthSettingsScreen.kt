package com.yuntyu.myhome.ui

import android.Manifest
import android.app.TimePickerDialog
import android.os.Build
import android.widget.Toast
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Button
import androidx.compose.material3.FilledTonalButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Switch
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.unit.dp
import com.yuntyu.myhome.MyHomeApp
import com.yuntyu.myhome.core.HealthPlan
import com.yuntyu.myhome.core.api.ApiResult
import com.yuntyu.myhome.core.api.GymPlan
import com.yuntyu.myhome.core.api.HealthSettings
import com.yuntyu.myhome.core.api.MealSlot
import com.yuntyu.myhome.core.api.MyHomeJson
import com.yuntyu.myhome.core.api.Nutrition
import com.yuntyu.myhome.core.api.Recommendation
import com.yuntyu.myhome.core.api.SleepPlan
import com.yuntyu.myhome.core.api.describe
import com.yuntyu.myhome.health.MealPhotos
import com.yuntyu.myhome.health.Reminders
import com.yuntyu.myhome.notify.Notifications
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import kotlinx.serialization.builtins.ListSerializer
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive

private val GOALS = listOf("lose" to "감량", "keep" to "유지", "gain" to "린매스업", "bulk" to "벌크업")
private val ACTIVITIES = listOf(
    "sedentary" to "비활동적 · 운동 없이 대부분 앉아서",
    "low" to "저활동적 · 가벼운 걷기·운동 조금",
    "active" to "활동적 · 걷기·운동이 꽤 많음",
    "veryActive" to "매우 활동적 · 고강도 운동·몸 쓰는 일",
)

/**
 * 건강 설정: 끼니 수·마감, 헬스 요일·횟수·도착 시간, 취침·기상, 영양 목표(몸·활동으로 추천받거나 직접), 알림, 사진 보관.
 * 서버 설정 원본에 바꾼 필드만 덮어써서 저장한다.
 */
@Composable
fun HealthSettingsScreen(modifier: Modifier, onDone: () -> Unit) {
    val ctx = LocalContext.current
    val app = MyHomeApp.instance
    val scope = rememberCoroutineScope()
    val m = LocalModo.current
    var s by remember { mutableStateOf<HealthSettings?>(null) }
    var error by remember { mutableStateOf<String?>(null) }
    var busy by remember { mutableStateOf(false) }
    var rec by remember { mutableStateOf<Recommendation?>(null) }
    var keepDays by remember { mutableStateOf(app.healthLocal.photoKeepDays) }
    // 숫자 칸은 입력 중인 글자 그대로
    var height by remember { mutableStateOf("") }
    var weight by remember { mutableStateOf("") }
    var age by remember { mutableStateOf("") }
    var kcal by remember { mutableStateOf("") }
    var carbs by remember { mutableStateOf("") }
    var protein by remember { mutableStateOf("") }
    var fat by remember { mutableStateOf("") }

    LaunchedEffect(Unit) {
        when (val r = withContext(Dispatchers.IO) { app.api.healthSettings() }) {
            is ApiResult.Ok -> {
                val st = MyHomeJson.decodeFromJsonElement(HealthSettings.serializer(), r.value.settings)
                s = st
                height = st.heightCm.takeIf { it > 0 }?.toString() ?: ""
                weight = st.weightKg.takeIf { it > 0 }?.let { if (it % 1.0 == 0.0) it.toInt().toString() else it.toString() } ?: ""
                age = st.age?.toString() ?: ""
                (r.value.settings["photoKeepDays"] as? JsonPrimitive)?.content?.toIntOrNull()?.let { keepDays = it }
                st.targets?.let { kcal = "${it.kcal}"; carbs = "${it.carbs}"; protein = "${it.protein}"; fat = "${it.fat}" }
            }
            else -> error = r.describe()
        }
    }
    val notifPermission = rememberLauncherForActivityResult(ActivityResultContracts.RequestPermission()) { }

    fun time(initial: String, onPick: (String) -> Unit) {
        val mins = runCatching { HealthPlan.minutes(initial) }.getOrDefault(12 * 60)
        TimePickerDialog(ctx, { _, h, mi -> onPick(HealthPlan.hhmm(h * 60 + mi)) }, mins / 60, mins % 60, true).show()
    }

    val st = s
    Column(
        modifier.fillMaxSize().verticalScroll(rememberScrollState()).padding(16.dp),
        verticalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        error?.let { Text("불러오지 못했어요: $it", color = m.over) }
        if (st == null) {
            if (error == null) Text("불러오는 중…", color = m.muted)
            return@Column
        }
        SectionCard("끼니") {
            Text("하루 끼니 수", style = MaterialTheme.typography.bodySmall, color = m.muted)
            Row(horizontalArrangement = Arrangement.spacedBy(6.dp)) {
                for (n in 3..6) Chip("${n}끼", st.meals.size == n, Modifier.weight(1f)) { s = st.copy(meals = HealthPlan.resizeMeals(st.meals, n)) }
            }
            for (slot in st.meals) {
                ItemRow("${slot.index}끼 마감", slot.deadline) {
                    time(slot.deadline) { t -> s = s!!.copy(meals = s!!.meals.map { if (it.index == slot.index) MealSlot(it.index, t) else it }) }
                }
            }
        }
        val g = st.gym ?: GymPlan()
        SectionCard("헬스") {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Text("헬스 일정", Modifier.weight(1f))
                Switch(checked = g.enabled, onCheckedChange = { s = st.copy(gym = g.copy(enabled = it)) })
            }
            if (g.enabled) {
                Row(horizontalArrangement = Arrangement.spacedBy(4.dp)) {
                    WEEKDAYS.forEachIndexed { i, w ->
                        Chip(w, i in g.days, Modifier.weight(1f)) {
                            val days = if (i in g.days) g.days - i else (g.days + i).sorted()
                            s = st.copy(gym = g.copy(days = days, weeklyTarget = g.weeklyTarget.coerceAtMost(days.size.coerceAtLeast(1))))
                        }
                    }
                }
                Text("주 목표 횟수", style = MaterialTheme.typography.bodySmall, color = m.muted)
                Row(Modifier.horizontalScroll(rememberScrollState()), horizontalArrangement = Arrangement.spacedBy(4.dp)) {
                    for (n in 1..7) Chip("${n}회", (g.weeklyTarget.takeIf { it > 0 } ?: g.days.size) == n) { s = st.copy(gym = g.copy(weeklyTarget = n)) }
                }
                ItemRow("도착 시간", g.arriveBy) { time(g.arriveBy) { t -> s = s!!.copy(gym = g.copy(arriveBy = t)) } }
            }
        }
        val sl = st.sleep ?: SleepPlan()
        SectionCard("취침") {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Text("취침 알림", Modifier.weight(1f))
                Switch(checked = sl.enabled, onCheckedChange = { s = st.copy(sleep = sl.copy(enabled = it)) })
            }
            if (sl.enabled) {
                ItemRow("취침 시각", sl.bedTime) { time(sl.bedTime) { t -> s = s!!.copy(sleep = sl.copy(bedTime = t)) } }
                ItemRow("기상 시각", sl.wakeTime) { time(sl.wakeTime) { t -> s = s!!.copy(sleep = sl.copy(wakeTime = t)) } }
            }
        }
        SectionCard("영양 목표") {
            Row(horizontalArrangement = Arrangement.spacedBy(6.dp)) {
                for ((k, l) in GOALS) Chip(l, st.goal == k, Modifier.weight(1f)) { s = st.copy(goal = k) }
            }
            Row(horizontalArrangement = Arrangement.spacedBy(6.dp)) {
                Chip("남성", st.sex == "male", Modifier.weight(1f)) { s = st.copy(sex = "male") }
                Chip("여성", st.sex == "female", Modifier.weight(1f)) { s = st.copy(sex = "female") }
            }
            Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                Field("키(cm)", height, Modifier.weight(1f)) { height = it }
                Field("몸무게(kg)", weight, Modifier.weight(1f), decimal = true) { weight = it }
                Field("나이", age, Modifier.weight(1f)) { age = it }
            }
            Text("평소 활동", style = MaterialTheme.typography.bodySmall, color = m.muted)
            for ((k, l) in ACTIVITIES) Chip(l, st.activity == k, Modifier.fillMaxWidth()) { s = st.copy(activity = k) }
            FilledTonalButton(onClick = {
                val h = height.toIntOrNull(); val w = weight.toDoubleOrNull(); val a = age.toIntOrNull()
                val sex = st.sex; val act = st.activity
                if (h == null || w == null || a == null || sex == null || act == null) {
                    Toast.makeText(ctx, "성별·키·몸무게·나이·활동을 모두 넣어 주세요", Toast.LENGTH_SHORT).show()
                    return@FilledTonalButton
                }
                scope.launch {
                    when (val r = withContext(Dispatchers.IO) { app.api.recommendTargets(st.goal, w, h, a, sex, act, if (st.goal == "keep" || st.goal == "lose") "balanced" else "training") }) {
                        is ApiResult.Ok -> rec = r.value
                        else -> Toast.makeText(ctx, "추천받지 못했어요: ${r.describe()}", Toast.LENGTH_LONG).show()
                    }
                }
            }) { Text("목표 추천받기") }
            rec?.let { r ->
                Text(
                    "유지 칼로리 약 ${num(r.maintenanceKcal.toInt())} kcal → 목표 ${num(r.kcal)} kcal · 탄 ${r.carbs}g · 단 ${r.protein}g · 지 ${r.fat}g",
                    style = MaterialTheme.typography.bodySmall,
                )
                TextButton(onClick = { kcal = "${r.kcal}"; carbs = "${r.carbs}"; protein = "${r.protein}"; fat = "${r.fat}"; rec = null }) { Text("이 값으로 채우기") }
            }
            Text("하루 목표 (직접 고칠 수 있어요)", style = MaterialTheme.typography.bodySmall, color = m.muted)
            Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                Field("kcal", kcal, Modifier.weight(1.2f)) { kcal = it }
                Field("탄(g)", carbs, Modifier.weight(1f)) { carbs = it }
                Field("단(g)", protein, Modifier.weight(1f)) { protein = it }
                Field("지(g)", fat, Modifier.weight(1f)) { fat = it }
            }
        }
        SectionCard("알림") {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Column(Modifier.weight(1f)) {
                    Text("끼니·헬스·취침 알림")
                    Text("끼니 마감 30분 전·마감 시각, 헬스 도착 1시간 전, 취침 시각", style = MaterialTheme.typography.bodySmall, color = m.muted)
                }
                Switch(checked = st.notifications != false, onCheckedChange = {
                    s = st.copy(notifications = it)
                    if (it && Build.VERSION.SDK_INT >= 33 && !Notifications.canPost(ctx)) notifPermission.launch(Manifest.permission.POST_NOTIFICATIONS)
                })
            }
        }
        SectionCard("폰에 사진 보관") {
            Row(horizontalArrangement = Arrangement.spacedBy(6.dp)) {
                for ((d, l) in listOf(0 to "계속", 30 to "30일", 90 to "90일", 180 to "180일")) Chip(l, keepDays == d, Modifier.weight(1f)) { keepDays = d }
            }
            Text("이 폰의 사진만 지워요. 기록과 사진은 서버에 남고, 다시 볼 때 받아 와요.", style = MaterialTheme.typography.bodySmall, color = m.muted)
        }
        Button(
            onClick = {
                val k = kcal.toIntOrNull()
                if (k != null && k < 1200) {
                    Toast.makeText(ctx, "목표 칼로리는 1,200 kcal 이상이어야 해요", Toast.LENGTH_SHORT).show()
                    return@Button
                }
                val next = st.copy(
                    heightCm = height.toIntOrNull() ?: st.heightCm,
                    weightKg = weight.toDoubleOrNull() ?: st.weightKg,
                    age = age.toIntOrNull() ?: st.age,
                    targets = if (k != null) Nutrition(k, carbs.toIntOrNull() ?: 0, protein.toIntOrNull() ?: 0, fat.toIntOrNull() ?: 0) else st.targets,
                )
                busy = true
                scope.launch {
                    val body = patch(next, keepDays)
                    val r = withContext(Dispatchers.IO) { app.api.putHealthSettings(body) }
                    busy = false
                    if (r is ApiResult.Ok) {
                        app.healthLocal.updateFrom(MyHomeJson.decodeFromJsonElement(HealthSettings.serializer(), r.value.settings))
                        app.healthLocal.photoKeepDays = keepDays
                        withContext(Dispatchers.IO) { MealPhotos.purge(ctx, keepDays) }
                        Reminders.reschedule(ctx)
                        Toast.makeText(ctx, "저장했어요", Toast.LENGTH_SHORT).show()
                        onDone()
                    } else {
                        Toast.makeText(ctx, "저장하지 못했어요: ${r.describe()}", Toast.LENGTH_LONG).show()
                    }
                }
            },
            enabled = !busy,
            modifier = Modifier.fillMaxWidth().height(52.dp),
        ) { Text("저장") }
    }
}

/**
 * 이 화면이 다루는 필드만 보낸다. 서버는 보낸 키만 바꾸고, targetHistory를 빼고 보내야 목표가 바뀐 날을 기록한다
 * (받은 원본을 통째로 돌려보내면 지난날이 새 목표로 판정된다).
 */
private fun patch(s: HealthSettings, keepDays: Int): JsonObject {
    val j = MyHomeJson
    val out = mutableMapOf<String, kotlinx.serialization.json.JsonElement>()
    out["photoKeepDays"] = if (keepDays > 0) JsonPrimitive(keepDays) else kotlinx.serialization.json.JsonNull
    out["goal"] = JsonPrimitive(s.goal)
    out["heightCm"] = JsonPrimitive(s.heightCm)
    out["weightKg"] = JsonPrimitive(s.weightKg)
    s.sex?.let { out["sex"] = JsonPrimitive(it) }
    s.age?.let { out["age"] = JsonPrimitive(it) }
    s.activity?.let { out["activity"] = JsonPrimitive(it) }
    out["meals"] = j.encodeToJsonElement(ListSerializer(MealSlot.serializer()), s.meals)
    s.gym?.let { out["gym"] = j.encodeToJsonElement(GymPlan.serializer(), it) }
    s.sleep?.let { out["sleep"] = j.encodeToJsonElement(SleepPlan.serializer(), it) }
    s.targets?.let { out["targets"] = j.encodeToJsonElement(Nutrition.serializer(), it) }
    s.notifications?.let { out["notifications"] = JsonPrimitive(it) }
    return JsonObject(out)
}

@Composable
private fun Field(label: String, value: String, modifier: Modifier, decimal: Boolean = false, onChange: (String) -> Unit) {
    OutlinedTextField(
        value = value,
        onValueChange = { v -> onChange(v.filter { it.isDigit() || (decimal && it == '.') }.take(6)) },
        label = { Text(label) },
        singleLine = true,
        keyboardOptions = KeyboardOptions(keyboardType = if (decimal) KeyboardType.Decimal else KeyboardType.Number),
        modifier = modifier,
    )
}
