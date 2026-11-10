package com.yuntyu.myhome.ui

import android.graphics.BitmapFactory
import android.net.Uri
import android.widget.Toast
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.PickVisualMediaRequest
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.Image
import androidx.compose.foundation.background
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
import androidx.compose.runtime.key
import androidx.compose.runtime.mutableStateListOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.saveable.listSaver
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.runtime.snapshots.SnapshotStateList
import androidx.compose.runtime.toMutableStateList
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.asImageBitmap
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.unit.dp
import com.google.android.gms.common.moduleinstall.ModuleInstall
import com.google.android.gms.common.moduleinstall.ModuleInstallRequest
import com.yuntyu.myhome.MyHomeApp
import com.yuntyu.myhome.core.HealthPlan
import com.yuntyu.myhome.core.api.ApiResult
import com.yuntyu.myhome.core.api.FoodItem
import com.yuntyu.myhome.core.api.FoodResult
import com.yuntyu.myhome.core.api.HealthSettings
import com.yuntyu.myhome.core.api.MealRequest
import com.yuntyu.myhome.core.api.MyHomeJson
import com.yuntyu.myhome.core.api.UploadDecision
import com.yuntyu.myhome.core.api.describe
import com.yuntyu.myhome.core.api.toUploadDecision
import com.yuntyu.myhome.health.MealOutbox
import com.yuntyu.myhome.health.MealPhotos
import com.yuntyu.myhome.health.Reminders
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import java.io.File
import java.time.LocalDateTime
import androidx.compose.foundation.layout.statusBarsPadding
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.material3.IconButton
import androidx.compose.material3.Icon
import androidx.compose.foundation.layout.Box
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.unit.sp
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.IntrinsicSize
import androidx.compose.foundation.layout.width
import androidx.compose.ui.text.style.TextAlign

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
 * 사진은 분석할 때 서버를 거치고, 기록을 저장하면 서버(VM 디스크)에도 남는다. 폰 사진은 보기용 캐시다. 저장된 끼니를 누르면 이 화면에서 고친다.
 * 새로 찍는 끼니가 이미 기록이 있는 번호면 저장 전에 덮어쓸지 묻는다. "기타"는 그날 비어 있는 다음 번호를 쓴다.
 * 신호가 없거나 서버가 잠깐 안 되면 기록을 폰에 남겨 두고 연결되면 보낸다(MealOutbox).
 * 찍는 중인 사진 경로는 rememberSaveable로 둔다: 카메라가 열린 사이 Activity가 다시 만들어져도 사진을 잃지 않게.
 */
@Composable
fun HealthCaptureScreen(modifier: Modifier, nav: HealthNav, onDone: () -> Unit, onBack: () -> Unit = onDone) {
    val ctx = LocalContext.current
    val app = MyHomeApp.instance
    val scope = rememberCoroutineScope()
    val m = LocalModo.current
    val editing = nav.editing

    var settings by remember { mutableStateOf<HealthSettings?>(null) }
    var date by rememberSaveable { mutableStateOf(nav.captureDate) }
    var index by rememberSaveable { mutableStateOf(editing?.index ?: nav.captureIndex) }
    var phase by remember { mutableStateOf(if (editing != null) Phase.REVIEW else Phase.PICK) }
    val files = rememberSaveable(
        saver = listSaver<SnapshotStateList<File>, String>(
            save = { it.map(File::getPath) },
            restore = { paths -> paths.map(::File).filter(File::exists).toMutableStateList() },
        ),
    ) { mutableStateListOf() }
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
    // 바코드로 찾은 제품이면 그 번호를 끼니와 같이 저장한다. basis는 영양값의 기준 양(예: "100g(ml)당")
    var barcode by remember { mutableStateOf(editing?.barcode) }
    var barcodeBasis by remember { mutableStateOf<String?>(null) }
    // AI 분석이 실패했는지(다시 알아보기 버튼), 크레딧이 없어서인지(다시 해도 안 됨)
    var analyzeFailed by remember { mutableStateOf(false) }
    var noCredit by remember { mutableStateOf(false) }
    // 그날 이미 기록이 있는 끼니 번호(서버, 못 보낸 기록 포함). 덮어쓰기 확인과 "기타" 번호 고르기에 쓴다
    var used by remember { mutableStateOf<Set<Int>>(emptySet()) }
    var confirmOverwrite by remember { mutableStateOf(false) }
    // 먹은 시각("HH:mm"). 새 기록은 지금, 고칠 때는 저장된 시각. 기록을 늦게 해도 먹은 시각을 따로 남긴다
    var eatenHhmm by rememberSaveable { mutableStateOf(editing?.loggedAt?.let(::localHhmm) ?: nowHhmm()) }

    // 바코드 스캐너 모듈을 미리 받아 둔다(처음 찍을 때 모듈을 받느라 실패하지 않게)
    LaunchedEffect(Unit) { if (editing == null) prefetchBarcodeScanner(ctx) }
    LaunchedEffect(date) {
        val d = date ?: return@LaunchedEffect
        used = withContext(Dispatchers.IO) {
            // 서버에 못 닿으면 폰 사본(최근 2주 기록한 끼니)으로
            val saved = (app.api.healthDay(d) as? ApiResult.Ok)?.value?.meals?.map { it.index }
                ?: app.healthLocal.logged.filter { it.startsWith("$d#") }.mapNotNull { it.substringAfter('#').toIntOrNull() }
            (saved + MealOutbox.forDay(ctx, d).keys).toSet()
        }
    }

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

    val gallery = rememberLauncherForActivityResult(ActivityResultContracts.PickMultipleVisualMedia(3)) { uris: List<Uri> ->
        for (u in uris) if (files.size < 3) MealPhotos.copyIn(ctx, u)?.let { files += it }
    }

    fun analyze() {
        phase = Phase.ANALYZING
        message = null
        analyzeFailed = false
        noCredit = false
        barcode = null // 사진으로 알아본 끼니는 바코드 제품이 아니다
        barcodeBasis = null
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
                else -> {
                    analyzeFailed = true
                    noCredit = (r as? ApiResult.HttpError)?.errorCode == "AI_NO_CREDIT"
                    // 크레딧이 없으면 다시 해도 같으니 직접 입력만 안내한다
                    message = if (noCredit) "AI 크레딧이 없어요 - 직접 입력해 주세요" else "음식 인식에 실패했어요. 다시 알아보거나 직접 입력해 주세요 (${r.describe()})"
                }
            }
            phase = Phase.REVIEW
        }
    }

    fun startBarcode() {
        scanBarcode(ctx) { code ->
            if (code == null) return@scanBarcode
            busy = true
            scope.launch {
                val r = withContext(Dispatchers.IO) { app.api.barcode(code) }
                busy = false
                barcode = code
                barcodeBasis = null
                when {
                    r is ApiResult.Ok && r.value.result != null -> {
                        ai = r.value.result!!; manualTotals = false; message = null
                        // Open Food Facts는 100g(ml)당 값일 때가 많다. 기준을 보여 주고 먹은 양으로 고치게 한다
                        barcodeBasis = ai.items.firstOrNull()?.amount?.ifBlank { null } ?: r.value.product.amount.ifBlank { null }
                    }
                    r is ApiResult.Ok -> {
                        ai = FoodResult(listOf(FoodItem(name = r.value.product.name.ifBlank { "바코드 $code" })))
                        manualTotals = true
                        message = "${r.value.product.name}: 영양성분이 없어요. 합계를 직접 넣어 주세요"
                    }
                    else -> {
                        ai = FoodResult(listOf(FoodItem(name = "바코드 $code")))
                        manualTotals = true
                        message = "제품을 찾지 못했어요(${r.describe()}). 영양성분표를 찍거나 직접 넣어 주세요"
                    }
                }
                phase = Phase.REVIEW
            }
        }
    }

    // 저장: 서버에 바로 보내고, 연결·서버 문제면 폰에 남겨 두고 연결되면 보낸다(MealOutbox)
    fun save() {
        val d = date ?: return
        val i = index ?: return
        val (aiSave, portionSave) = HealthPlan.mealToSave(ai, portion, manualTotals)
        val eatenAt = runCatching {
            HealthPlan.eatenAt(java.time.LocalDate.parse(d), eatenHhmm, java.time.LocalDate.now())
                .atZone(java.time.ZoneId.systemDefault()).toOffsetDateTime().toString()
        }.getOrNull()
        val req = MealRequest(d, i, aiSave, portionSave, shared, barcode, loggedAt = eatenAt)
        val bytes = photoBytes
        busy = true
        scope.launch {
            val r = withContext(Dispatchers.IO) { app.api.saveMeal(req) }
            busy = false
            when {
                r is ApiResult.Ok -> {
                    withContext(Dispatchers.IO) {
                        MealOutbox.remove(ctx, d, i) // 전에 못 보낸 옛 기록이 나중에 덮어쓰지 않게
                        if (bytes.isNotEmpty()) {
                            MealPhotos.keep(ctx, d, i, bytes)
                            MealPhotos.markPending(ctx, d, i) // 올리는 중에 앱이 죽어도 다음 주기에 다시
                        }
                        files.forEach { it.delete() }
                    }
                    // 서버에도 사진을 올려 둔다(화면은 기다리지 않는다. 실패하면 다음 갱신 때 다시)
                    if (bytes.isNotEmpty()) app.ioScope.launch { MealPhotos.upload(ctx.applicationContext, d, i) }
                    app.healthLocal.markLogged(d, i, true)
                    Reminders.reschedule(ctx)
                    Toast.makeText(ctx, "${HealthPlan.label(i)}을 기록했어요", Toast.LENGTH_SHORT).show()
                    onDone()
                }
                r.toUploadDecision() == UploadDecision.RETRY -> {
                    withContext(Dispatchers.IO) {
                        // 사진은 끼니 사진으로 옮겨 둔다(임시 사진은 하루 뒤 지워진다)
                        if (bytes.isNotEmpty()) MealPhotos.keep(ctx, d, i, bytes)
                        files.forEach { it.delete() }
                        MealOutbox.add(ctx, req, hasPhotos = bytes.isNotEmpty())
                    }
                    app.healthLocal.markLogged(d, i, true) // 찍은 끼니는 알림에서 뺀다
                    Reminders.reschedule(ctx)
                    Toast.makeText(ctx, "지금은 보내지 못했어요. 연결되면 ${HealthPlan.label(i)} 기록을 보내요", Toast.LENGTH_LONG).show()
                    onDone()
                }
                else -> message = "저장하지 못했어요: ${r.describe()}"
            }
        }
    }

    val (toSave, savePortion) = HealthPlan.mealToSave(ai, portion, manualTotals)
    val perFood = !manualTotals && HealthPlan.canPortionPerFood(ai.items)
    val factor = HealthPlan.eatenFactor(savePortion, shared)
    fun eaten(v: Int) = HealthPlan.jsRound(v * factor)

    // 위: "‹ 2끼 기록 ⌄"(끼니 바꾸기는 여기서, 고칠 때는 고정)
    val st = settings
    val choices = if (editing == null && st != null) st.meals.map { it.index } + listOf(HealthPlan.extraChipIndex(index, used)) else emptyList()
    Column(modifier.fillMaxSize()) {
    CaptureHeader(
        title = (index ?: editing?.index)?.let { "${HealthPlan.label(it)} 기록" } ?: "끼니 기록",
        sub = if (editing != null) date else null,
        choices = choices.distinct(),
        onPick = { index = it },
        onBack = onBack,
    )
    if (phase == Phase.PICK && editing == null) {
        CameraPick(
            files = files,
            onAlbum = { gallery.launch(PickVisualMediaRequest(ActivityResultContracts.PickVisualMedia.ImageOnly)) },
            onAnalyze = { analyze() },
            onBarcode = { startBarcode() },
            onManual = { phase = Phase.REVIEW; manualTotals = true },
            enabled = index != null && !busy,
            modifier = Modifier.weight(1f),
        )
    } else
    Column(
        Modifier.weight(1f).verticalScroll(rememberScrollState()).padding(16.dp),
        verticalArrangement = Arrangement.spacedBy(12.dp),
    ) {

        // 사진 (폰에 없으면 서버에서 받아 온다)
        var savedTick by remember { mutableStateOf(0) }
        LaunchedEffect(date, index) {
            val d = date; val i = index
            if (d != null && i != null && withContext(Dispatchers.IO) { MealPhotos.ensureLocal(ctx, d, i) }) savedTick++
        }
        val thumbs = key(savedTick) { if (files.isNotEmpty()) files.toList() else if (date != null && index != null) MealPhotos.photosOf(ctx, date!!, index!!) else emptyList() }
        if (thumbs.isNotEmpty()) {
            Row(Modifier.horizontalScroll(rememberScrollState()), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                for (f in thumbs) Thumb(f, 96)
            }
        }

        when (phase) {
            Phase.PICK -> Unit // 카메라 화면(CameraPick)이 맡는다
            Phase.ANALYZING -> Text("음식을 알아보는 중이에요…", color = m.muted, modifier = Modifier.padding(6.dp))
            Phase.REVIEW -> {
                message?.let { Text(it, color = m.warn, style = MaterialTheme.typography.bodyMedium, modifier = Modifier.padding(horizontal = 6.dp)) }
                if (analyzeFailed && !noCredit && files.isNotEmpty()) {
                    FilledTonalButton(onClick = { analyze() }, modifier = Modifier.padding(horizontal = 6.dp)) { Text("다시 알아보기") }
                }
                barcodeBasis?.let { basis ->
                    Text(
                        "영양성분 기준: $basis · 다르면 음식을 눌러 고치기",
                        color = if (basis.contains("100")) m.warn else m.muted,
                        style = MaterialTheme.typography.bodySmall,
                        modifier = Modifier.padding(horizontal = 6.dp),
                    )
                }
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
                SectionCard("먹은 시각") {
                    ItemRow("${eatenHhmm}에 먹었어요", "바꾸기") {
                        val (h, mi) = eatenHhmm.split(":").map { it.toInt() }
                        android.app.TimePickerDialog(ctx, { _, hh, mm -> eatenHhmm = "%02d:%02d".format(hh, mm) }, h, mi, true).show()
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
                        val i = index ?: return@Button
                        if (HealthPlan.needsOverwriteConfirm(i, used, editing?.index)) confirmOverwrite = true else save()
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
        ModoAlertDialog(
            onDismissRequest = { confirmDelete = false },
            title = { Text("${HealthPlan.label(editing.index)} 기록을 지울까요?") },
            text = { Text("폰에 있는 이 끼니 사진도 같이 지워요.") },
            confirmButton = {
                TextButton(onClick = {
                    confirmDelete = false
                    scope.launch {
                        val r = withContext(Dispatchers.IO) { app.api.deleteMeal(editing.date, editing.index) }
                        if (r is ApiResult.Ok) {
                            withContext(Dispatchers.IO) { MealOutbox.remove(ctx, editing.date, editing.index) }
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

    if (confirmOverwrite) {
        ModoAlertDialog(
            onDismissRequest = { confirmOverwrite = false },
            title = { Text("이미 기록이 있어요. 바꿀까요?") },
            text = { Text("${index?.let(HealthPlan::label) ?: ""}에 저장된 기록을 이 기록으로 바꿔요.") },
            confirmButton = { TextButton(onClick = { confirmOverwrite = false; save() }) { Text("바꾸기") } },
            dismissButton = { TextButton(onClick = { confirmOverwrite = false }) { Text("취소") } },
        )
    }
}

/** 촬영 화면 머리: ‹ 뒤로, "2끼 기록 ⌄"(누르면 끼니 바꾸기), 고칠 때는 아래 날짜 */
@Composable
private fun CaptureHeader(title: String, sub: String?, choices: List<Int>, onPick: (Int) -> Unit, onBack: () -> Unit) {
    val m = LocalModo.current
    var menu by remember { mutableStateOf(false) }
    Row(Modifier.fillMaxWidth().statusBarsPadding().height(64.dp).padding(start = 4.dp, end = 16.dp), verticalAlignment = Alignment.CenterVertically) {
        IconButton(onClick = onBack) { Icon(ChevronLeftIcon, contentDescription = "뒤로", modifier = Modifier.size(26.dp)) }
        Box {
            Row(
                Modifier.clip(RoundedCornerShape(10.dp)).tap(enabled = choices.isNotEmpty(), onClickLabel = "끼니 바꾸기", role = Role.Button) { menu = true }
                    .padding(horizontal = 6.dp, vertical = 6.dp),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                Text(title, fontSize = 22.sp, fontWeight = FontWeight.Bold, maxLines = 1)
                if (choices.isNotEmpty()) Icon(ChevronDownIcon, contentDescription = null, modifier = Modifier.padding(start = 6.dp).size(22.dp))
            }
            ModoDropdownMenu(expanded = menu, onDismissRequest = { menu = false }) {
                for (i in choices) DropdownMenuItem(text = { Text(HealthPlan.label(i)) }, onClick = { menu = false; onPick(i) })
            }
        }
        Spacer(Modifier.weight(1f))
        if (sub != null) Text(sub, color = m.muted, fontSize = 14.sp)
    }
}

/**
 * 끼니 촬영 화면(사용자 요청, 2026-11-10): 열자마자 카메라. 앨범 · 셔터 · 찍은 장 수,
 * 아래 바코드 스캔 | 직접 입력. 한 장 이상 찍으면 "음식 알아보기". 셔터는 소리 없이 미리보기 한 장을 뜬다(SilentCamera.kt).
 */
@Composable
private fun CameraPick(
    files: MutableList<File>,
    onAlbum: () -> Unit,
    onAnalyze: () -> Unit,
    onBarcode: () -> Unit,
    onManual: () -> Unit,
    enabled: Boolean,
    modifier: Modifier = Modifier,
) {
    val ctx = LocalContext.current
    val scope = rememberCoroutineScope()
    val m = LocalModo.current
    val shooter = remember { CameraShooter() }
    var torch by remember { mutableStateOf(false) }
    var flash by remember { mutableStateOf(false) }
    LaunchedEffect(flash) { if (flash) { kotlinx.coroutines.delay(120); flash = false } }
    val full = files.size >= 3
    Column(modifier.fillMaxWidth()) {
        Text("음식을 화면에 담아주세요", Modifier.padding(horizontal = Gutter), fontSize = 16.sp, color = m.muted)
        Box(Modifier.weight(1f).fillMaxWidth().padding(horizontal = Gutter, vertical = 14.dp).clip(RoundedCornerShape(20.dp))) {
            SilentCameraView(shooter, Modifier.fillMaxSize())
            CornerMarks(Modifier.fillMaxSize().padding(28.dp))
            if (flash) Box(Modifier.fillMaxSize().background(androidx.compose.ui.graphics.Color.White.copy(alpha = 0.5f)))
            if (shooter.hasTorch) {
                IconButton(onClick = { torch = !torch; shooter.torch(torch) }, modifier = Modifier.align(Alignment.TopEnd).padding(6.dp)) {
                    Icon(if (torch) FlashIcon else FlashOffIcon, contentDescription = if (torch) "손전등 끄기" else "손전등 켜기", tint = androidx.compose.ui.graphics.Color.White)
                }
            }
            // 찍은 사진: 왼쪽 아래 작게(누르면 뺀다)
            if (files.isNotEmpty()) {
                Row(Modifier.align(Alignment.BottomStart).padding(12.dp), horizontalArrangement = Arrangement.spacedBy(6.dp)) {
                    for (f in files.toList()) {
                        Box(Modifier.clip(RoundedCornerShape(8.dp)).tap(onClickLabel = "이 사진 빼기") { files.remove(f); f.delete() }) { Thumb(f, 52) }
                    }
                }
            }
        }
        Row(Modifier.fillMaxWidth().padding(horizontal = Gutter), verticalAlignment = Alignment.CenterVertically) {
            Column(
                Modifier.weight(1f).clip(RoundedCornerShape(12.dp)).tap(enabled = !full, onClickLabel = "앨범에서 고르기", role = Role.Button, onClick = onAlbum).padding(vertical = 8.dp),
                horizontalAlignment = Alignment.CenterHorizontally,
            ) {
                Icon(GalleryIcon, contentDescription = null, modifier = Modifier.size(28.dp))
                Text("앨범", fontSize = 14.sp)
            }
            ShutterButton(enabled = shooter.ready && !full && enabled, ring = MaterialTheme.colorScheme.onBackground) {
                val bmp = shooter.shot() ?: return@ShutterButton
                flash = true
                scope.launch {
                    val f = saveShot(ctx, bmp)
                    if (f != null && files.size < 3) files += f else f?.delete()
                }
            }
            Box(Modifier.weight(1f), contentAlignment = Alignment.Center) {
                Text("${files.size} / 3", fontSize = 18.sp, color = m.muted)
            }
        }
        if (files.isEmpty()) {
            Text("최대 3장 · 영양성분표도 가능", Modifier.fillMaxWidth().padding(top = 10.dp, bottom = 12.dp), fontSize = 14.sp, color = m.muted, textAlign = TextAlign.Center)
        } else {
            PrimaryButton("음식 알아보기", Modifier.padding(horizontal = Gutter, vertical = 10.dp), enabled = enabled, onClick = onAnalyze)
        }
        Hairline(Modifier.padding(horizontal = Gutter))
        Row(Modifier.fillMaxWidth().height(IntrinsicSize.Min).padding(vertical = 6.dp), verticalAlignment = Alignment.CenterVertically) {
            BottomAction(BarcodeIcon, "바코드 스캔", enabled, Modifier.weight(1f), onBarcode)
            Box(Modifier.width(1.dp).height(28.dp).background(m.line))
            BottomAction(PencilIcon, "직접 입력", enabled, Modifier.weight(1f), onManual)
        }
        Spacer(Modifier.navigationBarsPadding())
    }
}

@Composable
private fun BottomAction(icon: androidx.compose.ui.graphics.vector.ImageVector, label: String, enabled: Boolean, modifier: Modifier, onClick: () -> Unit) {
    Row(
        modifier.tap(enabled = enabled, role = Role.Button, onClick = onClick).padding(vertical = 16.dp),
        horizontalArrangement = Arrangement.Center,
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Icon(icon, contentDescription = null, modifier = Modifier.size(24.dp))
        Text(label, Modifier.padding(start = 10.dp), fontSize = 16.sp, fontWeight = FontWeight.SemiBold)
    }
}

/** 미리보기 네 귀퉁이의 흰 꺾쇠 */
@Composable
private fun CornerMarks(modifier: Modifier) {
    androidx.compose.foundation.Canvas(modifier) {
        val l = 22.dp.toPx(); val w = 2.5.dp.toPx(); val c = androidx.compose.ui.graphics.Color.White
        fun corner(x: Float, y: Float, dx: Float, dy: Float) {
            drawLine(c, androidx.compose.ui.geometry.Offset(x, y), androidx.compose.ui.geometry.Offset(x + dx * l, y), w, cap = androidx.compose.ui.graphics.StrokeCap.Round)
            drawLine(c, androidx.compose.ui.geometry.Offset(x, y), androidx.compose.ui.geometry.Offset(x, y + dy * l), w, cap = androidx.compose.ui.graphics.StrokeCap.Round)
        }
        corner(0f, 0f, 1f, 1f); corner(size.width, 0f, -1f, 1f); corner(0f, size.height, 1f, -1f); corner(size.width, size.height, -1f, -1f)
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
        label = { Text(label, maxLines = 1, overflow = androidx.compose.ui.text.style.TextOverflow.Ellipsis) },
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
    ModoAlertDialog(
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
    ModoAlertDialog(
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

/** 코드 스캐너 모듈이 폰에 없으면 미리 받아 둔다(이미 있으면 아무것도 안 한다). 실패해도 찍을 때 다시 받는다. */
private fun prefetchBarcodeScanner(context: android.content.Context) {
    runCatching {
        val scanner = com.google.mlkit.vision.codescanner.GmsBarcodeScanning.getClient(context)
        val modules = ModuleInstall.getClient(context)
        modules.areModulesAvailable(scanner).addOnSuccessListener { r ->
            if (!r.areModulesAvailable()) modules.installModules(ModuleInstallRequest.newBuilder().addApi(scanner).build())
        }
    }
}

/** Play 서비스 코드 스캐너로 상품 바코드(EAN·UPC)를 찍는다. 카메라 권한이 필요 없다. 취소·실패면 null. */
private fun scanBarcode(context: android.content.Context, onResult: (String?) -> Unit) {
    val opts = com.google.mlkit.vision.codescanner.GmsBarcodeScannerOptions.Builder()
        .setBarcodeFormats(
            com.google.mlkit.vision.barcode.common.Barcode.FORMAT_EAN_13,
            com.google.mlkit.vision.barcode.common.Barcode.FORMAT_EAN_8,
            com.google.mlkit.vision.barcode.common.Barcode.FORMAT_UPC_A,
            com.google.mlkit.vision.barcode.common.Barcode.FORMAT_UPC_E,
        )
        .build()
    com.google.mlkit.vision.codescanner.GmsBarcodeScanning.getClient(context, opts).startScan()
        .addOnSuccessListener { b -> onResult(b.rawValue?.filter { it.isDigit() }?.takeIf { it.length in 8..14 }) }
        .addOnFailureListener { e ->
            android.widget.Toast.makeText(context, "바코드를 찍지 못했어요: ${e.message}", android.widget.Toast.LENGTH_SHORT).show()
            onResult(null)
        }
        .addOnCanceledListener { onResult(null) }
}
