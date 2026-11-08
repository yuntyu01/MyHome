package com.yuntyu.myhome.ui

import android.content.Context
import android.content.pm.PackageManager
import android.location.Location
import android.location.LocationManager
import android.widget.Toast
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Button
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import com.yuntyu.myhome.BuildConfig
import com.yuntyu.myhome.health.GymGeofence
import androidx.compose.material3.FilledTonalButton
import androidx.compose.material3.Switch
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.unit.dp
import androidx.core.content.ContextCompat
import androidx.core.location.LocationManagerCompat
import com.yuntyu.myhome.MyHomeApp
import com.yuntyu.myhome.core.HealthPlan
import com.yuntyu.myhome.core.api.ApiResult
import com.yuntyu.myhome.core.api.DayCard
import com.yuntyu.myhome.core.api.HealthMonth
import com.yuntyu.myhome.core.api.HealthSettings
import com.yuntyu.myhome.core.api.HealthToday
import com.yuntyu.myhome.core.api.MyHomeJson
import com.yuntyu.myhome.core.api.describe
import com.yuntyu.myhome.health.MealPhotos
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import kotlinx.coroutines.suspendCancellableCoroutine
import kotlinx.coroutines.withContext
import kotlinx.coroutines.withTimeoutOrNull
import java.io.File
import java.time.LocalDate
import java.time.LocalDateTime
import java.time.YearMonth
import kotlin.coroutines.resume

/**
 * 헬스: 오늘 인증 상태, 이번 주 횟수, 도착까지 남은 시간, 사진 인증 / 오늘 쉬기(휴식권).
 * 인증 사진은 서버에 보내지도 폰에 남기지도 않는다(인증 표시만 보낸다).
 * 헬스장 위치를 정해 두면 그 근처에 머물 때 자동으로 출석한다(GymGeofence, 좌표는 폰에만).
 */
@Composable
fun HealthGymScreen(modifier: Modifier, resumeTick: Int) {
    val ctx = LocalContext.current
    val app = MyHomeApp.instance
    val scope = rememberCoroutineScope()
    val m = LocalModo.current
    val today = LocalDate.now()
    var settings by remember { mutableStateOf<HealthSettings?>(null) }
    var todayView by remember { mutableStateOf<HealthToday?>(null) }
    var card by remember { mutableStateOf<DayCard?>(null) }
    var month by remember { mutableStateOf<HealthMonth?>(null) }
    var error by remember { mutableStateOf<String?>(null) }
    var reload by remember { mutableIntStateOf(0) }
    var confirmRest by remember { mutableStateOf(false) }
    var pending by remember { mutableStateOf<File?>(null) }

    LaunchedEffect(resumeTick, reload) {
        withContext(Dispatchers.IO) {
            when (val r = app.api.healthSettings()) {
                is ApiResult.Ok -> settings = MyHomeJson.decodeFromJsonElement(HealthSettings.serializer(), r.value.settings)
                else -> error = r.describe()
            }
            todayView = (app.api.healthToday(today.toString(), nowHhmm()) as? ApiResult.Ok)?.value
            card = (app.api.healthDay(today.toString()) as? ApiResult.Ok)?.value
            month = (app.api.healthMonth(YearMonth.from(today).toString()) as? ApiResult.Ok)?.value
        }
    }

    fun checkIn(by: String) = scope.launch {
        when (val r = withContext(Dispatchers.IO) { app.api.gymCheckIn(today.toString(), by) }) {
            is ApiResult.Ok -> { Toast.makeText(ctx, "오늘 헬스를 기록했어요", Toast.LENGTH_SHORT).show(); reload++ }
            else -> Toast.makeText(ctx, "기록하지 못했어요: ${r.describe()}", Toast.LENGTH_LONG).show()
        }
    }

    val camera = rememberLauncherForActivityResult(ActivityResultContracts.TakePicture()) { ok ->
        val f = pending
        pending = null
        if (ok && f != null && f.length() > 0) {
            // 사진은 인증 표시로만 쓰고 서버에 보내지 않는다. 폰에도 남기지 않는다
            f.delete()
            checkIn("photo")
        } else f?.delete()
    }

    val g = settings?.gym
    val week = todayView?.gym
    val verified = card?.gymVerifiedBy
    Column(
        modifier.fillMaxSize().verticalScroll(rememberScrollState()).padding(16.dp),
        verticalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        error?.let { Text("불러오지 못했어요: $it", color = m.over, style = MaterialTheme.typography.bodySmall) }
        if (g == null || !g.enabled) {
            SectionCard("헬스") { Text("건강 설정에서 헬스 일정을 켜면 여기서 인증해요.", color = m.muted) }
            return@Column
        }
        val gymDay = HealthPlan.weekdayMon0(today) in g.days
        Column(Modifier.padding(horizontal = 6.dp)) {
            Text("오늘 인증", style = MaterialTheme.typography.titleSmall, color = m.muted)
            Row(verticalAlignment = Alignment.Bottom) {
                Text(
                    when (verified) {
                        "geofence", "photo" -> "인증 완료" // geofence는 위치 인증을 쓰던 예전 기록
                        "rest" -> "쉬는 날"
                        else -> if (gymDay) "인증 전" else "헬스 없는 날"
                    },
                    Modifier.weight(1f),
                    style = MaterialTheme.typography.headlineMedium,
                )
                Text("${g.arriveBy}까지", color = m.muted)
            }
        }
        if (week != null) {
            Column(
                Modifier.fillMaxWidth().clip(RoundedCornerShape(16.dp)).background(m.card).padding(horizontal = 18.dp, vertical = 16.dp),
                verticalArrangement = Arrangement.spacedBy(10.dp),
            ) {
                Text("이번 주", style = MaterialTheme.typography.bodySmall, color = m.muted)
                Row(verticalAlignment = Alignment.Bottom) {
                    Text("${week.done}", style = MaterialTheme.typography.headlineSmall)
                    Text(" / ${week.planned}회", color = m.muted)
                }
                Row(horizontalArrangement = Arrangement.spacedBy(6.dp)) {
                    for (i in 0 until week.planned.coerceAtLeast(1)) {
                        Box(Modifier.weight(1f).height(6.dp).clip(RoundedCornerShape(3.dp)).background(if (i < week.done) m.accent else m.raised))
                    }
                }
            }
        }
        SectionCard("이번 주 일정") {
            val monday = today.minusDays(HealthPlan.weekdayMon0(today).toLong())
            val gymDates = month?.days.orEmpty().filter { it.gym }.map { it.date }.toSet()
            Row(Modifier.fillMaxWidth()) {
                for (i in 0..6) {
                    val d = monday.plusDays(i.toLong())
                    Column(Modifier.weight(1f), horizontalAlignment = Alignment.CenterHorizontally, verticalArrangement = Arrangement.spacedBy(4.dp)) {
                        Text(WEEKDAYS[i], style = MaterialTheme.typography.bodySmall, color = m.muted)
                        Text("${d.dayOfMonth}", color = if (d == today) MaterialTheme.colorScheme.onSurface else m.muted)
                        Text(
                            when {
                                d.toString() in gymDates -> "✓"
                                i in g.days && !d.isBefore(today) -> "–"
                                else -> " "
                            },
                            color = m.muted,
                        )
                    }
                }
            }
        }
        if (gymDay && verified == null) {
            SectionCard("오늘") { ItemRow("도착까지 남은 시간", remainText(HealthPlan.minutesUntil(LocalDateTime.now(), g.arriveBy))) }
        }
        if (verified == null) {
            Button(onClick = {
                val (f, uri) = MealPhotos.newCapture(ctx)
                pending = f
                camera.launch(uri)
            }, modifier = Modifier.fillMaxWidth().height(52.dp)) { Text("사진으로 인증") }
            val rest = todayView?.coupons?.rest
            TextButton(onClick = { confirmRest = true }, enabled = (rest?.left ?: 0) > 0, modifier = Modifier.fillMaxWidth()) {
                Text("오늘 쉬기 · 휴식권 ${rest?.left ?: 0}/${rest?.total ?: 0}", color = m.muted)
            }
        }
        GymLocationSection { reload++ }
    }

    if (confirmRest) {
        AlertDialog(
            onDismissRequest = { confirmRest = false },
            title = { Text("오늘 헬스를 쉴까요?") },
            text = { Text("휴식권을 1장 써요. 휴식권은 월요일에 다시 채워져요.") },
            confirmButton = {
                TextButton(onClick = {
                    confirmRest = false
                    scope.launch {
                        when (val r = withContext(Dispatchers.IO) { app.api.useRest(today.toString()) }) {
                            is ApiResult.Ok -> reload++
                            else -> Toast.makeText(ctx, "쓰지 못했어요: ${r.describe()}", Toast.LENGTH_LONG).show()
                        }
                    }
                }) { Text("쉬기") }
            },
            dismissButton = { TextButton(onClick = { confirmRest = false }) { Text("취소") } },
        )
    }
}

/**
 * 헬스장 자동 출석 설정. 헬스장에 있을 때 지금 위치를 저장하고, "항상 허용"을 받으면 지오펜스를 건다.
 * Android 11부터 "항상 허용"은 앱 안 창이 아니라 설정 화면에서 고른다(권한 요청이 설정으로 보낸다).
 */
@Composable
private fun GymLocationSection(onChanged: () -> Unit) {
    val ctx = LocalContext.current
    val scope = rememberCoroutineScope()
    val m = LocalModo.current
    var tick by remember { mutableIntStateOf(0) }
    var busy by remember { mutableStateOf(false) }
    val spot = remember(tick) { GymGeofence.spot(ctx) }
    val enabled = remember(tick) { GymGeofence.enabled(ctx) }
    val fg = remember(tick) { GymGeofence.hasForeground(ctx) }
    val bg = remember(tick) { GymGeofence.hasBackground(ctx) }

    fun saveHere() {
        busy = true
        scope.launch {
            val loc = GymGeofence.currentLocation(ctx)
            busy = false
            if (loc == null) {
                Toast.makeText(ctx, "위치를 잡지 못했어요. 위치(GPS)를 켜고 다시 해 주세요", Toast.LENGTH_LONG).show()
            } else {
                GymGeofence.save(ctx, loc)
                Toast.makeText(ctx, "여기를 헬스장으로 저장했어요 (오차 ${loc.accuracy.toInt()}m)", Toast.LENGTH_SHORT).show()
            }
            tick++
        }
    }
    val askFine = rememberLauncherForActivityResult(ActivityResultContracts.RequestMultiplePermissions()) { r ->
        tick++
        if (r[android.Manifest.permission.ACCESS_FINE_LOCATION] == true) saveHere()
        else Toast.makeText(ctx, "정확한 위치 권한이 있어야 헬스장 위치를 정할 수 있어요", Toast.LENGTH_LONG).show()
    }
    val askBackground = rememberLauncherForActivityResult(ActivityResultContracts.RequestPermission()) {
        tick++
        GymGeofence.register(ctx)
        onChanged()
    }
    // 설정 화면에서 "항상 허용"을 고르고 돌아오면 다시 확인한다
    val lifecycle = androidx.lifecycle.compose.LocalLifecycleOwner.current.lifecycle
    LaunchedEffect(lifecycle) {
        lifecycle.currentStateFlow.collect { if (it == androidx.lifecycle.Lifecycle.State.RESUMED) { GymGeofence.register(ctx); tick++ } }
    }

    SectionCard(if (BuildConfig.GYM_AUTO) "헬스장 위치로 자동 출석" else "헬스장 위치로 출석") {
        Text(
            if (BuildConfig.GYM_AUTO) "헬스장에 있을 때 위치를 저장해 두면, 그 근처에 5분 넘게 머물 때 자동으로 출석을 남겨요. 위치는 이 폰에만 저장돼요."
            else "헬스장에 있을 때 위치를 저장해 두면, 다음부터 헬스장에서 '위치로 출석'을 눌러 사진 없이 출석해요. 위치는 이 폰에만 저장돼요.",
            style = MaterialTheme.typography.bodySmall,
            color = m.muted,
        )
        if (spot == null) {
            Button(onClick = {
                if (fg) saveHere()
                else askFine.launch(arrayOf(android.Manifest.permission.ACCESS_FINE_LOCATION, android.Manifest.permission.ACCESS_COARSE_LOCATION))
            }, enabled = !busy) { Text(if (busy) "위치 잡는 중…" else "지금 위치를 헬스장으로") }
            return@SectionCard
        }
        ItemRow("저장한 위치", "반경 ${spot.radiusM}m")
        Row(horizontalArrangement = Arrangement.spacedBy(6.dp)) {
            for (r in listOf(100, 150, 250)) Chip("${r}m", spot.radiusM == r, Modifier.weight(1f)) { GymGeofence.setRadius(ctx, r); tick++ }
        }
        if (!BuildConfig.GYM_AUTO) {
            Button(onClick = {
                busy = true
                scope.launch {
                    val r = GymGeofence.checkInHere(ctx)
                    busy = false
                    val msg = when (r) {
                        is GymGeofence.HereResult.Done -> "출석했어요 (헬스장에서 ${r.distanceM}m)"
                        is GymGeofence.HereResult.TooFar -> "헬스장에서 ${r.distanceM}m 떨어져 있어요 (${r.allowedM}m 안이어야 해요)"
                        is GymGeofence.HereResult.Failed -> r.why
                    }
                    Toast.makeText(ctx, msg, Toast.LENGTH_LONG).show()
                    if (r is GymGeofence.HereResult.Done) onChanged()
                }
            }, enabled = !busy && fg, modifier = Modifier.fillMaxWidth()) { Text(if (busy) "위치 확인 중…" else "위치로 출석") }
        }
        if (BuildConfig.GYM_AUTO) Row(Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
            Text("자동 출석", Modifier.weight(1f))
            Switch(checked = enabled, onCheckedChange = { GymGeofence.setEnabled(ctx, it); tick++ })
        }
        if (BuildConfig.GYM_AUTO && enabled && !bg) {
            Text("앱이 꺼져 있어도 알려면 위치 권한을 '항상 허용'으로 바꿔야 해요.", style = MaterialTheme.typography.bodySmall, color = m.warn)
            FilledTonalButton(onClick = {
                if (android.os.Build.VERSION.SDK_INT >= android.os.Build.VERSION_CODES.Q) askBackground.launch(android.Manifest.permission.ACCESS_BACKGROUND_LOCATION)
            }) { Text("'항상 허용' 켜기") }
        } else if (BuildConfig.GYM_AUTO && enabled) {
            Text("켜져 있어요. 헬스장에 5분 머물면 출석이 남고 알림이 와요.", style = MaterialTheme.typography.bodySmall, color = m.muted)
        }
        Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            TextButton(onClick = { if (fg) saveHere() else askFine.launch(arrayOf(android.Manifest.permission.ACCESS_FINE_LOCATION)) }, enabled = !busy) {
                Text("지금 위치로 다시 정하기", color = m.muted)
            }
            TextButton(onClick = { GymGeofence.clear(ctx); tick++ }) { Text("위치 지우기", color = m.muted) }
        }
    }
}
