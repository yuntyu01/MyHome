package com.yuntyu.myhome.ui

import android.graphics.BitmapFactory
import android.net.Uri
import android.widget.Toast
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.PickVisualMediaRequest
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.Image
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
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
import androidx.compose.runtime.mutableStateListOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.asImageBitmap
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.unit.dp
import com.yuntyu.myhome.MyHomeApp
import com.yuntyu.myhome.core.HealthPlan
import com.yuntyu.myhome.core.api.ApiResult
import com.yuntyu.myhome.core.api.FoodItem
import com.yuntyu.myhome.core.api.FoodResult
import com.yuntyu.myhome.core.api.HealthSettings
import com.yuntyu.myhome.core.api.MealRequest
import com.yuntyu.myhome.core.api.MyHomeJson
import com.yuntyu.myhome.core.api.describe
import com.yuntyu.myhome.health.MealPhotos
import com.yuntyu.myhome.health.Reminders
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import java.io.File
import java.time.LocalDateTime

private enum class Phase { PICK, ANALYZING, REVIEW }

fun portionLabel(p: Double): String = when (p) {
    0.25 -> "1/4"
    0.5 -> "반"
    0.75 -> "3/4"
    1.0 -> "다"
    1.5 -> "1.5배"
    2.0 -> "2배"
    else -> "${p}배"
}

/**
 * 끼니 촬영 → AI 분석 → 기록 확인(음식 고치기, 먹은 양, 나눠 먹음) → 저장.
 * 사진은 분석할 때만 서버를 거치고(서버는 저장하지 않음) 폰에만 남는다. 저장된 끼니를 누르면 이 화면에서 고친다.
 */
@Composable
fun HealthCaptureScreen(modifier: Modifier, nav: HealthNav, onDone: () -> Unit) {
    val ctx = LocalContext.current
    val app = MyHomeApp.instance
    val scope = rememberCoroutineScope()
    val m = LocalModo.current
    val editing = nav.editing

    var settings by remember { mutableStateOf<HealthSettings?>(null) }
    var date by remember { mutableStateOf(nav.captureDate) }
    var index by remember { mutableStateOf(editing?.index ?: nav.captureIndex) }
    var phase by remember { mutableStateOf(if (editing != null) Phase.REVIEW else Phase.PICK) }
    val files = remember { mutableStateListOf<File>() }
    var photoBytes by remember { mutableStateOf<List<ByteArray>>(emptyList()) }
    var ai by remember {
        mutableStateOf(
            editing?.let { FoodResult(it.items.orEmpty(), it.base.kcal, it.base.carbs, it.base.protein, it.base.fat, it.confidence) }
                ?: FoodResult(),
        )
    }
    var portion by remember { mutableStateOf(editing?.portion ?: 1.0) }
    var shared by remember { mutableStateOf(editing?.sharedMeal ?: false) }
    var manualTotals by remember {
        mutableStateOf(
            editing != null && !(editing.portion == 1.0 && HealthPlan.canPortionPerFood(editing.items.orEmpty()) &&
                HealthPlan.sumEaten(editing.items.orEmpty()).kcal == editing.base.kcal),
        )
    }
    var message by remember { mutableStateOf<String?>(null) }
    var editItem by remember { mutableStateOf<Int?>(null) } // -1 = 새 음식
    var editTotals by remember { mutableStateOf(false) }
    var confirmDelete by remember { mutableStateOf(false) }
    var busy by remember { mutableStateOf(false) }
    var pendingCapture by remember { mutableStateOf<File?>(null) }

    LaunchedEffect(Unit) {
        val r = withContext(Dispatchers.IO) { app.api.healthSettings() }
        if (r is ApiResult.Ok) {
            val st = MyHomeJson.decodeFromJsonElement(HealthSettings.serializer(), r.value.settings)
            settings = st
            if (date == null) date = HealthPlan.scheduleDay(st.meals, LocalDateTime.now()).toString()
        } else {
            message = "설정을 불러오지 못했어요: ${r.describe()}"
            if (date == null) date = java.time.LocalDate.now().toString()
        }
    }

    val camera = rememberLauncherForActivityResult(ActivityResultContracts.TakePicture()) { ok ->
        val f = pendingCapture
        if (ok && f != null && f.length() > 0) files += f else f?.delete()
        pendingCapture = null
    }
    val gallery = rememberLauncherForActivityResult(ActivityResultContracts.PickMultipleVisualMedia(3)) { uris: List<Uri> ->
        for (u in uris) if (files.size < 3) MealPhotos.copyIn(ctx, u)?.let { files += it }
    }

    fun analyze() {
        phase = Phase.ANALYZING
        message = null
        scope.launch {
            val bytes = withContext(Dispatchers.IO) { files.mapNotNull(MealPhotos::downscaled) }
            photoBytes = bytes
            when (val r = withContext(Dispatchers.IO) { app.api.analyzeMeal(bytes) }) {
                is ApiResult.Ok -> {
                    ai = r.value
                    manualTotals = false
                    portion = 1.0
                    message = when {
                        r.value.items.isEmpty() -> "음식을 찾지 못했어요. 직접 추가해 주세요"
                        r.value.confidence < 0.5 -> "AI가 확신하지 못했어요. 음식을 확인해 주세요"
                        else -> null
                    }
                }
                else -> message = "음식 인식에 실패했어요. 직접 입력해 주세요 (${r.describe()})"
            }
            phase = Phase.REVIEW
        }
    }

    val (toSave, savePortion) = HealthPlan.mealToSave(ai, portion, manualTotals)
    val perFood = !manualTotals && HealthPlan.canPortionPerFood(ai.items)
    val factor = HealthPlan.eatenFactor(savePortion, shared)
    fun eaten(v: Int) = HealthPlan.jsRound(v * factor)

    Column(
        modifier.fillMaxSize().verticalScroll(rememberScrollState()).padding(16.dp),
        verticalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        // 끼니 고르기 (고칠 때는 고정)
        val st = settings
        if (editing == null && st != null) {
            SectionCard("어느 끼니인가요?") {
                Row(Modifier.horizontalScroll(rememberScrollState()), horizontalArrangement = Arrangement.spacedBy(6.dp)) {
                    for (slot in st.meals) Chip(HealthPlan.label(slot.index), index == slot.index) { index = slot.index }
                    val extra = if (index != null && HealthPlan.isExtra(index!!)) index!! else HealthPlan.EXTRA_BASE
                    Chip("기타", index != null && HealthPlan.isExtra(index!!)) { index = extra }
                }
            }
        } else if (editing != null) {
            Text("${date ?: ""} · ${HealthPlan.label(editing.index)}", style = MaterialTheme.typography.titleMedium, modifier = Modifier.padding(horizontal = 6.dp))
        }

        // 사진
        val thumbs = if (files.isNotEmpty()) files.toList() else if (date != null && index != null) MealPhotos.photosOf(ctx, date!!, index!!) else emptyList()
        if (thumbs.isNotEmpty()) {
            Row(Modifier.horizontalScroll(rememberScrollState()), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                for (f in thumbs) Thumb(f, 96)
            }
        }

        when (phase) {
            Phase.PICK -> {
                SectionCard("끼니 사진") {
                    Text("한 끼를 사진 3장까지 찍을 수 있어요. 영양성분표를 찍어도 돼요.", style = MaterialTheme.typography.bodySmall, color = m.muted)
                    Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                        Button(onClick = {
                            val (f, uri) = MealPhotos.newCapture(ctx)
                            pendingCapture = f
                            camera.launch(uri)
                        }, enabled = files.size < 3) { Text("사진 찍기") }
                        FilledTonalButton(onClick = {
                            gallery.launch(PickVisualMediaRequest(ActivityResultContracts.PickVisualMedia.ImageOnly))
                        }, enabled = files.size < 3) { Text("앨범에서") }
                    }
                    if (files.isNotEmpty()) TextButton(onClick = { files.forEach { it.delete() }; files.clear() }) { Text("사진 지우기", color = m.muted) }
                }
                Button(onClick = { analyze() }, enabled = files.isNotEmpty() && index != null, modifier = Modifier.fillMaxWidth().height(52.dp)) { Text("음식 알아보기") }
                TextButton(onClick = { phase = Phase.REVIEW; manualTotals = true }, enabled = index != null) { Text("사진 없이 직접 입력", color = m.muted) }
            }
            Phase.ANALYZING -> Text("음식을 알아보는 중이에요…", color = m.muted, modifier = Modifier.padding(6.dp))
            Phase.REVIEW -> {
                message?.let { Text(it, color = m.warn, style = MaterialTheme.typography.bodyMedium, modifier = Modifier.padding(horizontal = 6.dp)) }
                SectionCard("이런 음식이에요") {
                    if (ai.items.isEmpty()) Text("아직 음식이 없어요", color = m.muted)
                    ai.items.forEachIndexed { i, it ->
                        val sub = listOf(it.amount, if (perFood && it.portion != null && it.portion != 1.0) "${portionLabel(it.portion!!)} 먹음" else "").filter { s -> s.isNotBlank() }.joinToString(" · ")
                        ItemRow(it.name, it.kcal?.let { k -> "${num(HealthPlan.jsRound(k * HealthPlan.itemPortion(it)))} kcal" } ?: "", sub) { editItem = i }
                    }
                    ItemRow("음식 추가", "+") { editItem = -1 }
                }
                if (!perFood) {
                    SectionCard("얼마나 먹었나요?") {
                        Row(horizontalArrangement = Arrangement.spacedBy(6.dp)) {
                            for (p in HealthPlan.PORTIONS) Chip(portionLabel(p), portion == p, Modifier.weight(1f)) { portion = p }
                        }
                    }
                }
                SectionCard("나눠 먹었어요") {
                    Row(verticalAlignment = Alignment.CenterVertically) {
                        Text("함께 먹었다면 내가 먹은 만큼(절반)으로 기록해요", Modifier.weight(1f), style = MaterialTheme.typography.bodySmall, color = m.muted)
                        Switch(checked = shared, onCheckedChange = { shared = it })
                    }
                }
                SectionCard("추정 영양") {
                    Row(verticalAlignment = Alignment.Bottom) {
                        Text("약 ", color = m.muted)
                        Text(num(eaten(toSave.kcal)), style = MaterialTheme.typography.headlineSmall)
                        Text(" kcal", color = m.muted)
                    }
                    Row(horizontalArrangement = Arrangement.spacedBy(16.dp)) {
                        Text("탄수화물 ${num(eaten(toSave.carbs))}g", style = MaterialTheme.typography.bodySmall)
                        Text("단백질 ${num(eaten(toSave.protein))}g", style = MaterialTheme.typography.bodySmall)
                        Text("지방 ${num(eaten(toSave.fat))}g", style = MaterialTheme.typography.bodySmall)
                    }
                    TextButton(onClick = { editTotals = true }) { Text(if (manualTotals) "합계 고치기 (직접 입력함)" else "합계 직접 고치기", color = m.muted) }
                }
                Button(
                    onClick = {
                        val d = date ?: return@Button
                        val i = index ?: return@Button
                        busy = true
                        scope.launch {
                            val r = withContext(Dispatchers.IO) { app.api.saveMeal(MealRequest(d, i, toSave, savePortion, shared)) }
                            busy = false
                            if (r is ApiResult.Ok) {
                                withContext(Dispatchers.IO) {
                                    if (photoBytes.isNotEmpty()) MealPhotos.keep(ctx, d, i, photoBytes)
                                    files.forEach { it.delete() }
                                }
                                app.healthLocal.markLogged(d, i, true)
                                Reminders.reschedule(ctx)
                                Toast.makeText(ctx, "${HealthPlan.label(i)}을 기록했어요", Toast.LENGTH_SHORT).show()
                                onDone()
                            } else {
                                message = "저장하지 못했어요: ${r.describe()}"
                            }
                        }
                    },
                    enabled = !busy && date != null && index != null && (ai.items.isNotEmpty() || ai.kcal > 0),
                    modifier = Modifier.fillMaxWidth().height(52.dp),
                ) { Text("기록하기") }
                if (editing != null) {
                    TextButton(onClick = { confirmDelete = true }) { Text("이 끼니 기록 지우기", color = m.over) }
                }
            }
        }
    }

    // 음식 고치기·추가
    editItem?.let { i ->
        val orig = if (i >= 0) ai.items.getOrNull(i) else null
        FoodDialog(
            orig = orig,
            perFood = perFood || (orig == null && HealthPlan.canPortionPerFood(ai.items)),
            onDismiss = { editItem = null },
            onDelete = if (orig != null) ({
                ai = ai.copy(items = ai.items.filterIndexed { j, _ -> j != i })
                if (!manualTotals && HealthPlan.canPortionPerFood(ai.items)) ai = HealthPlan.sumEaten(ai.items).copy(confidence = ai.confidence)
                editItem = null
            }) else null,
        ) { item ->
            val items = ai.items.toMutableList()
            if (i >= 0) items[i] = item else items += item
            ai = ai.copy(items = items)
            // 음식별 숫자가 모두 있으면 합계는 음식 합으로 다시 낸다. 숫자 없는 음식이 섞이면 합계를 직접 고쳐야 한다
            if (!manualTotals && HealthPlan.canPortionPerFood(items)) {
                ai = HealthPlan.sumEaten(items).copy(confidence = ai.confidence)
            } else if (!HealthPlan.canPortionPerFood(items)) {
                manualTotals = true
                val add = HealthPlan.sumEaten(listOf(item.copy(portion = null)))
                if (i < 0) ai = ai.copy(kcal = ai.kcal + add.kcal, carbs = ai.carbs + add.carbs, protein = ai.protein + add.protein, fat = ai.fat + add.fat)
            }
            editItem = null
        }
    }
    if (editTotals) {
        TotalsDialog(ai, onDismiss = { editTotals = false }) { k, c, p, f ->
            ai = ai.copy(kcal = k, carbs = c, protein = p, fat = f)
            manualTotals = true
            editTotals = false
        }
    }
    if (confirmDelete && editing != null) {
        AlertDialog(
            onDismissRequest = { confirmDelete = false },
            title = { Text("${HealthPlan.label(editing.index)} 기록을 지울까요?") },
            text = { Text("폰에 있는 이 끼니 사진도 같이 지워요.") },
            confirmButton = {
                TextButton(onClick = {
                    confirmDelete = false
                    scope.launch {
                        val r = withContext(Dispatchers.IO) { app.api.deleteMeal(editing.date, editing.index) }
                        if (r is ApiResult.Ok) {
                            MealPhotos.delete(ctx, editing.date, editing.index)
                            app.healthLocal.markLogged(editing.date, editing.index, false)
                            Reminders.reschedule(ctx)
                            onDone()
                        } else {
                            message = "지우지 못했어요: ${r.describe()}"
                        }
                    }
                }) { Text("지우기", color = m.over) }
            },
            dismissButton = { TextButton(onClick = { confirmDelete = false }) { Text("취소") } },
        )
    }
}

@Composable
fun Thumb(f: File, sizeDp: Int) {
    val bmp = remember(f.path, f.lastModified()) {
        runCatching { BitmapFactory.decodeFile(f.path, BitmapFactory.Options().apply { inSampleSize = 4 })?.asImageBitmap() }.getOrNull()
    }
    if (bmp != null) {
        Image(bmp, contentDescription = null, contentScale = ContentScale.Crop, modifier = Modifier.size(sizeDp.dp).clip(RoundedCornerShape(12.dp)))
    }
}

@Composable
private fun NumField(label: String, value: String, modifier: Modifier = Modifier, onChange: (String) -> Unit) {
    OutlinedTextField(
        value = value,
        onValueChange = { onChange(it.filter(Char::isDigit).take(5)) },
        label = { Text(label) },
        singleLine = true,
        keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Number),
        modifier = modifier,
    )
}

@Composable
private fun FoodDialog(orig: FoodItem?, perFood: Boolean, onDismiss: () -> Unit, onDelete: (() -> Unit)?, onSave: (FoodItem) -> Unit) {
    var name by remember { mutableStateOf(orig?.name ?: "") }
    var amount by remember { mutableStateOf(orig?.amount ?: "") }
    var kcal by remember { mutableStateOf(orig?.kcal?.toString() ?: "") }
    var carbs by remember { mutableStateOf(orig?.carbs?.toString() ?: "") }
    var protein by remember { mutableStateOf(orig?.protein?.toString() ?: "") }
    var fat by remember { mutableStateOf(orig?.fat?.toString() ?: "") }
    var portion by remember { mutableStateOf(orig?.portion ?: 1.0) }
    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text(if (orig == null) "음식 추가" else "음식 고치기") },
        text = {
            Column(verticalArrangement = Arrangement.spacedBy(8.dp), modifier = Modifier.verticalScroll(rememberScrollState())) {
                OutlinedTextField(name, { name = it.take(40) }, label = { Text("이름") }, singleLine = true)
                OutlinedTextField(amount, { amount = it.take(30) }, label = { Text("양 (예: 1공기)") }, singleLine = true)
                Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    NumField("kcal", kcal, Modifier.weight(1f)) { kcal = it }
                    NumField("탄(g)", carbs, Modifier.weight(1f)) { carbs = it }
                }
                Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    NumField("단(g)", protein, Modifier.weight(1f)) { protein = it }
                    NumField("지(g)", fat, Modifier.weight(1f)) { fat = it }
                }
                Text("숫자를 비우면 이 음식은 합계에 더하지 않아요(합계를 직접 고쳐 주세요).", style = MaterialTheme.typography.bodySmall, color = LocalModo.current.muted)
                if (perFood) {
                    Text("이 음식을 얼마나 먹었나요?", style = MaterialTheme.typography.bodyMedium, fontWeight = FontWeight.SemiBold)
                    Row(Modifier.horizontalScroll(rememberScrollState()), horizontalArrangement = Arrangement.spacedBy(6.dp)) {
                        for (p in HealthPlan.ITEM_PORTIONS) Chip(portionLabel(p), portion == p) { portion = p }
                    }
                }
                if (onDelete != null) TextButton(onClick = onDelete) { Text("이 음식 빼기", color = LocalModo.current.over) }
            }
        },
        confirmButton = {
            TextButton(onClick = {
                onSave(
                    FoodItem(
                        name.trim(), amount.trim(), kcal.toIntOrNull(), carbs.toIntOrNull(), protein.toIntOrNull(), fat.toIntOrNull(),
                        portion.takeIf { perFood && it != 1.0 },
                    ),
                )
            }, enabled = name.isNotBlank()) { Text("확인") }
        },
        dismissButton = { TextButton(onClick = onDismiss) { Text("취소") } },
    )
}

@Composable
private fun TotalsDialog(ai: FoodResult, onDismiss: () -> Unit, onSave: (Int, Int, Int, Int) -> Unit) {
    var kcal by remember { mutableStateOf(ai.kcal.toString()) }
    var carbs by remember { mutableStateOf(ai.carbs.toString()) }
    var protein by remember { mutableStateOf(ai.protein.toString()) }
    var fat by remember { mutableStateOf(ai.fat.toString()) }
    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text("한 끼 합계 (1인분 전체)") },
        text = {
            Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                Text("먹은 양·나눠 먹음은 저장할 때 곱해요.", style = MaterialTheme.typography.bodySmall, color = LocalModo.current.muted)
                Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    NumField("kcal", kcal, Modifier.weight(1f)) { kcal = it }
                    NumField("탄(g)", carbs, Modifier.weight(1f)) { carbs = it }
                }
                Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    NumField("단(g)", protein, Modifier.weight(1f)) { protein = it }
                    NumField("지(g)", fat, Modifier.weight(1f)) { fat = it }
                }
            }
        },
        confirmButton = {
            TextButton(onClick = { onSave(kcal.toIntOrNull() ?: 0, carbs.toIntOrNull() ?: 0, protein.toIntOrNull() ?: 0, fat.toIntOrNull() ?: 0) }) { Text("확인") }
        },
        dismissButton = { TextButton(onClick = onDismiss) { Text("취소") } },
    )
}

