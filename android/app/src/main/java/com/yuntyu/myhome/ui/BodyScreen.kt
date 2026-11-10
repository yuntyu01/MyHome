package com.yuntyu.myhome.ui

import android.graphics.BitmapFactory
import android.widget.Toast
import androidx.compose.foundation.Canvas
import androidx.compose.foundation.Image
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.aspectRatio
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.graphics.StrokeCap
import androidx.compose.ui.graphics.asImageBitmap
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import com.yuntyu.myhome.MyHomeApp
import com.yuntyu.myhome.core.api.ApiResult
import com.yuntyu.myhome.core.api.Weight
import com.yuntyu.myhome.health.BodyPhotos
import com.yuntyu.myhome.health.MealPhotos
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import java.io.File
import java.time.LocalDate

/**
 * 몸 기록(사용자 요청, 2026-11-10): 몸무게(서버)와 몸 사진(폰에만).
 * 저울을 찍으면 AI가 화면 숫자를 읽어 채운다(저울 사진은 저장하지 않는다). 몸 사진은 날짜별로 모은다.
 */
@Composable
fun BodyScreen(modifier: Modifier) {
    val ctx = LocalContext.current
    val app = MyHomeApp.instance
    val scope = rememberCoroutineScope()
    val m = LocalModo.current
    var weights by remember { mutableStateOf<List<Weight>?>(null) }
    var error by remember { mutableStateOf<String?>(null) }
    var tick by remember { mutableIntStateOf(0) }
    var photosTick by remember { mutableIntStateOf(0) }
    var busy by remember { mutableStateOf<String?>(null) }
    // 몸무게 넣기·고치기 대화상자: (날짜, 처음 값, 고치는 것인지)
    var editing by remember { mutableStateOf<Triple<LocalDate, String, Boolean>?>(null) }
    var viewing by remember { mutableStateOf<Pair<LocalDate, File>?>(null) }
    var showAll by remember { mutableStateOf(false) }
    val today = LocalDate.now()

    LaunchedEffect(tick) {
        when (val r = withContext(Dispatchers.IO) { app.api.bodyWeights() }) {
            is ApiResult.Ok -> { weights = r.value.weights; error = null }
            else -> error = r.plain()
        }
    }
    val photos = remember(photosTick) { BodyPhotos.all(ctx) }

    // 저울 찍기: 소리 없이 찍고 → 서버가 숫자를 읽고 → 맞는지 확인하고 저장
    val scaleCam = rememberSilentCamera { f ->
        busy = "저울을 읽는 중이에요…"
        scope.launch {
            val bytes = withContext(Dispatchers.IO) { MealPhotos.downscaled(f).also { f.delete() } }
            val r = if (bytes != null) withContext(Dispatchers.IO) { app.api.scanScale(bytes) } else null
            busy = null
            when {
                r is ApiResult.Ok && r.value.weightKg > 0 -> editing = Triple(today, kgText(r.value.weightKg), false)
                r is ApiResult.Ok || r == null -> {
                    Toast.makeText(ctx, "숫자를 못 읽었어요. 직접 넣어 주세요", Toast.LENGTH_SHORT).show()
                    editing = Triple(today, "", false)
                }
                else -> {
                    Toast.makeText(ctx, r.plain(), Toast.LENGTH_LONG).show()
                    editing = Triple(today, "", false)
                }
            }
        }
    }
    val bodyCam = rememberSilentCamera { f ->
        scope.launch {
            val kept = withContext(Dispatchers.IO) { BodyPhotos.keep(ctx, f, LocalDate.now()) }
            if (kept == null) Toast.makeText(ctx, "사진을 저장하지 못했어요", Toast.LENGTH_SHORT).show()
            photosTick++
        }
    }

    Column(modifier.fillMaxSize().verticalScroll(rememberScrollState()).padding(bottom = 32.dp)) {
        val list = weights.orEmpty()
        val last = list.lastOrNull()
        val prev = list.dropLast(1).lastOrNull()
        Column(Modifier.padding(start = Gutter, end = Gutter, top = 8.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
            Text("몸무게", fontSize = 15.sp, color = m.muted)
            Text(last?.let { "${kgText(it.weightKg)}kg" } ?: if (weights == null && error == null) "…" else "아직 없어요", fontSize = 36.sp, fontWeight = FontWeight.Bold)
            if (last != null) {
                val diff = prev?.let { last.weightKg - it.weightKg }
                Text(
                    listOfNotNull(dateText(last.date, today), diff?.let { d -> "지난번보다 " + (if (d > 0) "+" else if (d < 0) "−" else "±") + kgText(kotlin.math.abs(d)) + "kg" }).joinToString(" · "),
                    fontSize = 14.sp, color = m.muted,
                )
            }
            error?.let { Text(it, fontSize = 14.sp, color = m.over) }
        }
        if (list.size >= 2) WeightChart(list.takeLast(90), Modifier.padding(horizontal = Gutter, vertical = 16.dp).fillMaxWidth().height(140.dp))

        Row(Modifier.fillMaxWidth().padding(horizontal = Gutter, vertical = 12.dp), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            ActionPill(CameraIcon, "저울 찍기", Modifier.weight(1f), enabled = busy == null) { scaleCam() }
            ActionPill(PencilIcon, "직접 입력", Modifier.weight(1f)) { editing = Triple(today, last?.let { kgText(it.weightKg) } ?: "", false) }
            ActionPill(GalleryIcon, "몸 사진", Modifier.weight(1f)) { bodyCam() }
        }
        busy?.let { HintLine(it) }

        if (list.isNotEmpty()) {
            SectionLabel("기록", action = if (list.size > 7) (if (showAll) "접기" else "전체") else null, onAction = { showAll = !showAll })
            val shown = list.reversed().let { if (showAll) it else it.take(7) }
            for (w in shown) {
                val d = LocalDate.parse(w.date)
                FlatRow(dateText(w.date, today), value = "${kgText(w.weightKg)}kg", valueColor = MaterialTheme.colorScheme.onBackground, onClick = { editing = Triple(d, kgText(w.weightKg), true) })
            }
        }

        SectionLabel("몸 사진", help = "몸 사진은 이 폰에만 저장돼요. 서버로 보내지 않아요.")
        if (photos.isEmpty()) {
            Text("아직 없어요", Modifier.padding(horizontal = Gutter, vertical = 8.dp), fontSize = 14.sp, color = m.muted)
        } else {
            for (row in photos.chunked(3)) {
                Row(Modifier.fillMaxWidth().padding(horizontal = Gutter, vertical = 4.dp), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    for (p in row) {
                        Column(Modifier.weight(1f).tap(onClickLabel = "크게 보기") { viewing = p }) {
                            PhotoBox(p.second, Modifier.fillMaxWidth().aspectRatio(3f / 4f).clip(RoundedCornerShape(12.dp)))
                            Text(dateText(p.first.toString(), today), Modifier.padding(top = 4.dp), fontSize = 12.sp, color = m.muted)
                        }
                    }
                    repeat(3 - row.size) { Spacer(Modifier.weight(1f)) }
                }
            }
        }
    }

    editing?.let { (date, initial, existing) ->
        var text by remember(date, initial) { mutableStateOf(initial) }
        val kg = text.toDoubleOrNull()?.takeIf { it in 20.0..400.0 }
        ModoAlertDialog(
            onDismissRequest = { editing = null },
            title = { Text(if (existing) dateText(date.toString(), today) else "오늘 몸무게") },
            text = { LineField(text, { v -> text = v.filter { it.isDigit() || it == '.' }.take(5) }, "몸무게", keyboardType = KeyboardType.Decimal, suffix = "kg") },
            confirmButton = {
                TextButton(enabled = kg != null, onClick = {
                    val v = kg ?: return@TextButton
                    editing = null
                    scope.launch {
                        val r = withContext(Dispatchers.IO) { app.api.saveWeight(date.toString(), v) }
                        if (r is ApiResult.Ok) tick++ else Toast.makeText(ctx, r.plain(), Toast.LENGTH_LONG).show()
                    }
                }) { Text("저장") }
            },
            dismissButton = {
                Row {
                    if (existing) TextButton(onClick = {
                        editing = null
                        scope.launch {
                            val r = withContext(Dispatchers.IO) { app.api.deleteWeight(date.toString()) }
                            if (r is ApiResult.Ok) tick++ else Toast.makeText(ctx, r.plain(), Toast.LENGTH_LONG).show()
                        }
                    }) { Text("지우기", color = m.over) }
                    TextButton(onClick = { editing = null }) { Text("닫기") }
                }
            },
        )
    }
    viewing?.let { (date, file) ->
        ModoAlertDialog(
            onDismissRequest = { viewing = null },
            title = { Text(dateText(date.toString(), today)) },
            text = { PhotoBox(file, Modifier.fillMaxWidth().aspectRatio(3f / 4f).clip(RoundedCornerShape(12.dp))) },
            confirmButton = { TextButton(onClick = { viewing = null }) { Text("닫기") } },
            dismissButton = { TextButton(onClick = { BodyPhotos.delete(file); viewing = null; photosTick++ }) { Text("지우기", color = m.over) } },
        )
    }
}

/** 건강 첫 화면의 몸무게 줄: 최근 몸무게와 변화. 누르면 몸 기록 */
@Composable
fun BodyLine(resumeTick: Int, onClick: () -> Unit) {
    val app = MyHomeApp.instance
    val m = LocalModo.current
    var weights by remember { mutableStateOf<List<Weight>?>(null) }
    LaunchedEffect(resumeTick) {
        val r = withContext(Dispatchers.IO) { app.api.bodyWeights(from = LocalDate.now().minusDays(120).toString()) }
        if (r is ApiResult.Ok) weights = r.value.weights
    }
    val last = weights?.lastOrNull()
    val prev = weights?.dropLast(1)?.lastOrNull()
    Row(
        Modifier.fillMaxWidth().clip(RoundedCornerShape(16.dp)).background(m.card).tap(onClick = onClick).padding(horizontal = 18.dp, vertical = 16.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(2.dp)) {
            Text("몸무게", style = MaterialTheme.typography.titleMedium)
            Text(last?.let { dateText(it.date, LocalDate.now()) } ?: "저울을 찍으면 숫자를 읽어요", style = MaterialTheme.typography.bodySmall, color = m.muted)
        }
        if (last != null) {
            Column(horizontalAlignment = Alignment.End) {
                Text("${kgText(last.weightKg)}kg", style = MaterialTheme.typography.titleMedium, fontWeight = FontWeight.Bold)
                prev?.let { p ->
                    val d = last.weightKg - p.weightKg
                    Text((if (d > 0) "+" else if (d < 0) "−" else "±") + kgText(kotlin.math.abs(d)), style = MaterialTheme.typography.bodySmall, color = m.muted)
                }
            }
        }
    }
}

@Composable
private fun ActionPill(icon: ImageVector, label: String, modifier: Modifier, enabled: Boolean = true, onClick: () -> Unit) {
    val m = LocalModo.current
    Column(
        modifier.clip(RoundedCornerShape(14.dp)).background(m.card).tap(enabled = enabled, role = Role.Button, onClick = onClick).padding(vertical = 14.dp),
        horizontalAlignment = Alignment.CenterHorizontally,
        verticalArrangement = Arrangement.spacedBy(6.dp),
    ) {
        Icon(icon, contentDescription = null, modifier = Modifier.size(22.dp), tint = if (enabled) MaterialTheme.colorScheme.onBackground else m.faint)
        Text(label, fontSize = 14.sp, fontWeight = FontWeight.SemiBold, color = if (enabled) MaterialTheme.colorScheme.onBackground else m.faint)
    }
}

@Composable
private fun PhotoBox(file: File, modifier: Modifier) {
    val m = LocalModo.current
    val bmp = remember(file.path) { runCatching { BitmapFactory.decodeFile(file.path)?.asImageBitmap() }.getOrNull() }
    Box(modifier.background(m.card)) {
        if (bmp != null) Image(bmp, contentDescription = null, contentScale = ContentScale.Crop, modifier = Modifier.fillMaxSize())
    }
}

/** 몸무게 꺾은선(최근 90번). 위아래는 최저·최고에 0.5kg 여유 */
@Composable
private fun WeightChart(list: List<Weight>, modifier: Modifier) {
    val m = LocalModo.current
    val line = MaterialTheme.colorScheme.onBackground
    val lo = list.minOf { it.weightKg } - 0.5
    val hi = list.maxOf { it.weightKg } + 0.5
    Box(modifier.clip(RoundedCornerShape(16.dp)).background(m.card).padding(16.dp)) {
        Canvas(Modifier.fillMaxSize()) {
            val n = list.size
            fun pt(i: Int) = Offset(
                if (n == 1) size.width / 2 else size.width * i / (n - 1),
                (size.height * (1 - (list[i].weightKg - lo) / (hi - lo))).toFloat(),
            )
            for (i in 1 until n) drawLine(line, pt(i - 1), pt(i), strokeWidth = 2.5.dp.toPx(), cap = StrokeCap.Round)
            drawCircle(line, 4.dp.toPx(), pt(n - 1))
        }
    }
}

/** 72.0 → "72", 72.35 → "72.4" */
fun kgText(kg: Double): String {
    val r = Math.round(kg * 10) / 10.0
    return if (r % 1.0 == 0.0) r.toLong().toString() else r.toString()
}

/** "오늘", "어제", "11월 6일" */
private fun dateText(iso: String, today: LocalDate): String {
    val d = runCatching { LocalDate.parse(iso) }.getOrNull() ?: return iso
    return when (d) {
        today -> "오늘"
        today.minusDays(1) -> "어제"
        else -> if (d.year != today.year) "${d.year}년 ${d.monthValue}월 ${d.dayOfMonth}일" else "${d.monthValue}월 ${d.dayOfMonth}일"
    }
}
