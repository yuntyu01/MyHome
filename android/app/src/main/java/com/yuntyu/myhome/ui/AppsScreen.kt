package com.yuntyu.myhome.ui

import android.content.Context
import android.content.Intent
import android.content.pm.PackageManager
import android.net.Uri
import android.os.Build
import android.provider.Settings
import androidx.compose.foundation.Image
import androidx.compose.foundation.gestures.detectDragGesturesAfterLongPress
import androidx.compose.foundation.gestures.detectTapGestures
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.offset
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.BasicTextField
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.produceState
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.geometry.Rect
import androidx.compose.ui.graphics.graphicsLayer
import androidx.compose.ui.hapticfeedback.HapticFeedbackType
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.ui.layout.boundsInWindow
import androidx.compose.ui.layout.onGloballyPositioned
import androidx.compose.ui.layout.positionInWindow
import androidx.compose.ui.platform.LocalHapticFeedback
import androidx.compose.ui.unit.IntOffset
import androidx.compose.ui.zIndex
import androidx.compose.ui.graphics.ImageBitmap
import androidx.compose.ui.graphics.SolidColor
import androidx.compose.ui.graphics.asImageBitmap
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.core.graphics.drawable.toBitmap
import com.yuntyu.myhome.MyHomeApp
import com.yuntyu.myhome.core.AppOrder
import com.yuntyu.myhome.core.NoticePolicy
import kotlin.math.roundToInt
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext

internal class AppEntry(val pkg: String, val label: String, val icon: ImageBitmap?)


/**
 * 알림 읽을 앱 고르기: 폰에 깔린 앱(런처에 보이는 앱)을 보여준다. 최근 알림 앱·은행·배달·쇼핑 앱을 먼저(AppOrder), 나머지는 펼치거나 검색해서 고른다.
 * 알림이 한 번도 안 온 앱도 고를 수 있어서 "목록이 비어 있어요" 상태가 없다. 고른 앱의 알림만 읽는다.
 */
@Composable
fun AppsScreen(modifier: Modifier, onDone: () -> Unit) {
    val ctx = LocalContext.current
    val app = MyHomeApp.instance
    val m = LocalModo.current
    val settings by app.settings.state.collectAsState()
    val recent by remember { app.db.recentApps().all() }.collectAsState(initial = emptyList())
    val counts = recent.associate { it.packageName to it.count }
    val lastSeen = recent.associate { it.packageName to it.lastSeenAt }
    var query by rememberSaveable { mutableStateOf("") }
    var showAll by rememberSaveable { mutableStateOf(false) }
    val installed by produceState<List<AppEntry>?>(null) { value = withContext(Dispatchers.IO) { loadInstalled(ctx) } }
    val selected = settings.selectedPackages
    // 고른 앱인데 폰에서 지웠거나 런처에 없는 앱도 보이게(고른 것을 풀 수 있게)
    // 런처에 없는 앱도: 알림을 보낸 적 있는 앱(최근 알림 기록)과 이미 고른 앱은 찾을 수 있게 넣는다
    val all = installed.orEmpty().let { list ->
        val known = list.map { it.pkg }.toSet()
        val extra = (recent.map { it.packageName } + selected).distinct().filter { it !in known && it != ctx.packageName }
        list + extra.map { pkg -> AppEntry(pkg, recent.firstOrNull { it.packageName == pkg }?.appLabel ?: pkg, null) }
    }
    val q = query.trim().lowercase()
    // 화면을 열 때 이미 고른 앱도 위에 둔다. 고르는 도중에는 순서를 바꾸지 않는다(누른 줄이 튀지 않게):
    // 앱 목록·최근 알림 기록이 처음 들어왔을 때만 다시 정한다
    val pinned = remember(installed == null) { selected }
    val (likely, others) = remember(installed == null, recent.isEmpty(), all.size) {
        val byPkg = all.associateBy { it.pkg }
        val (top, rest) = AppOrder.split(all.map { AppOrder.App(it.pkg, it.label, lastSeen[it.pkg] ?: 0L) }, pinned, System.currentTimeMillis())
        top.mapNotNull { byPkg[it.pkg] } to rest.mapNotNull { byPkg[it.pkg] }
    }
    fun toggle(pkg: String) = app.settings.setPackageSelected(pkg, pkg !in selected)
    val drag = remember { ModeDrag() }

    Box(modifier.fillMaxSize().onGloballyPositioned { drag.rootWin = it.positionInWindow() }) {
    Column(Modifier.fillMaxSize()) {
        // 항목마다 key를 준다: 검색어를 치면 위 칸들이 사라지는데, key가 없으면 검색칸이 새로 만들어져
        // 한 글자("ㅂ")마다 키보드가 내려갔다(2026-11-15)
        LazyColumn(Modifier.weight(1f)) {
            item(key = "title") {
                Column(Modifier.padding(start = Gutter, end = Gutter, top = 8.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                    Text("결제 알림이 오는 앱을\n골라 주세요", fontSize = 24.sp, fontWeight = FontWeight.Bold, lineHeight = 33.sp)
                    Text("고른 앱 알림만 읽어요", fontSize = 15.sp, color = m.muted, lineHeight = 22.sp)
                }
            }
            // 고른 앱을 지우기 방식 묶음에 꾹 눌러 끌어 넣는다(사용자 요청, 2026-11-15). 눌러도 고를 수 있다(폰이 좁을 때)
            if (q.isEmpty() && installed != null && selected.isNotEmpty()) {
                item(key = "modes") {
                    SectionLabel(
                        "결제 알림 지우기",
                        help = "앱을 꾹 눌러 다른 칸으로 끌어 놓거나, 눌러서 고르세요. 다 지움: 보낸 알림은 전부 치워요. " +
                            "알아본 것만: 가계부가 알아본 결제와 위젯에서 고를 결제는 AI 판정 뒤에 치우고, 못 읽은 알림·송금은 직접 처리하면 치워요. 안 지움: 그대로 둬요.",
                    )
                    ModeGroups(all.filter { it.pkg in selected }, settings::modeOf, drag) { pkg, mode -> app.settings.setDeleteMode(pkg, mode) }
                }
            }
            // 고른 앱마다: 알림이 실제로 오고 있는지, 안 오면 켜는 곳(폰 설정 + 앱 안 설정)
            if (q.isEmpty() && installed != null && selected.isNotEmpty()) {
                item(key = "check-title") {
                    SectionLabel(
                        "고른 앱 알림 확인",
                        help = "결제 알림을 읽으려면 두 곳이 켜져 있어야 해요. 폰 설정의 그 앱 알림, 그리고 그 앱 안의 결제(승인) 알림이에요. 줄을 누르면 폰 설정이 열려요.",
                    )
                }
                val chosen = all.filter { it.pkg in selected }
                items(chosen, key = { "check-" + it.pkg }) { a ->
                    val n = counts[a.pkg] ?: 0
                    // 쇼핑·배달 앱 알림은 주문·배달 진행이라 금액이 없다(2026-11-16 쿠팡 15건 모두 금액 없음). 돈은 결제 수단 앱에서 온다
                    val shop = AppOrder.group(a.label).let { it == AppOrder.Group.DELIVERY || it == AppOrder.Group.SHOP }
                    FlatRow(
                        a.label,
                        sub = when {
                            n == 0 -> "아직 알림 없음"
                            shop -> "알림 ${n}건 · 금액은 안 와요"
                            else -> "알림 ${n}건 받음"
                        },
                        help = when {
                            shop -> SHOP_HINT
                            n > 0 -> null
                            else -> payAppHint(a.label)
                        },
                        subColor = if (n > 0) m.muted else m.over,
                        leading = { Dot(if (n > 0) m.faint else m.over) },
                        onClick = { openAppNotificationSettings(ctx, a.pkg) },
                    )
                }
                item(key = "check-end") { Hairline(Modifier.padding(top = 8.dp)) }
            }
            item(key = "search") { SearchBox(query) { query = it } }
            when {
                installed == null -> item(key = "loading") { EmptyNote("폰에 있는 앱을 찾는 중이에요") }
                q.isNotEmpty() -> {
                    val found = all.filter { it.label.lowercase().contains(q) || it.pkg.lowercase().contains(q) }
                    if (found.isEmpty()) item(key = "none") { EmptyNote("'$query' 이름의 앱이 없어요") }
                    items(found, key = { it.pkg }) { a -> AppRow(a, a.pkg in selected, counts[a.pkg] ?: 0) { toggle(a.pkg) } }
                }
                else -> {
                    // 결제 앱 같은 게 없으면 접지 않고 전부 보여준다
                    val first = likely.ifEmpty { others }
                    item(key = "first-title") { SectionLabel(if (likely.isEmpty()) "이 폰의 앱" else "최근 알림·결제·배달 앱") }
                    items(first, key = { it.pkg }) { a -> AppRow(a, a.pkg in selected, counts[a.pkg] ?: 0) { toggle(a.pkg) } }
                    if (likely.isNotEmpty() && others.isNotEmpty()) {
                        item(key = "more") {
                            Hairline(Modifier.padding(top = 8.dp))
                            FlatRow(
                                if (showAll) "다른 앱 접기" else "다른 앱 모두 보기",
                                value = "${others.size}개",
                                titleColor = m.muted,
                                chevron = false,
                                onClick = { showAll = !showAll },
                            )
                        }
                        if (showAll) items(others, key = { it.pkg }) { a -> AppRow(a, a.pkg in selected, counts[a.pkg] ?: 0) { toggle(a.pkg) } }
                    }
                }
            }
        }
        PrimaryButton(
            if (selected.isEmpty()) "앱을 골라 주세요" else "${selected.size}개 골랐어요",
            Modifier.padding(horizontal = Gutter, vertical = 12.dp),
            enabled = selected.isNotEmpty(),
            onClick = onDone,
        )
    }
    // 끄는 앱은 손가락을 따라 떠 있다
    drag.pkg?.let { pkg ->
        val a = all.firstOrNull { it.pkg == pkg }
        if (a != null) {
            Box(
                Modifier.zIndex(1f).offset {
                    IntOffset((drag.finger.x - drag.rootWin.x - 40.dp.toPx()).roundToInt(), (drag.finger.y - drag.rootWin.y - 56.dp.toPx()).roundToInt())
                },
            ) { AppChip(a, Modifier.graphicsLayer { shadowElevation = 12f }) }
        }
    }
    }
}

/** 지우기 방식 묶음으로 끄는 중인 앱. 좌표는 전부 창 기준 */
internal class ModeDrag {
    var pkg by mutableStateOf<String?>(null)
    var finger by mutableStateOf(Offset.Zero)
    var rootWin = Offset.Zero
    val boxes = HashMap<NoticePolicy.Mode, Rect>()
    fun target(): NoticePolicy.Mode? = boxes.entries.firstOrNull { it.value.contains(finger) }?.key
}

@OptIn(ExperimentalLayoutApi::class)
@Composable
internal fun ModeGroups(apps: List<AppEntry>, modeOf: (String) -> NoticePolicy.Mode, drag: ModeDrag, onSet: (String, NoticePolicy.Mode) -> Unit) {
    val m = LocalModo.current
    val hover = if (drag.pkg != null) drag.target() else null
    Column(Modifier.padding(horizontal = Gutter), verticalArrangement = Arrangement.spacedBy(8.dp)) {
        for (mode in NoticePolicy.Mode.entries) {
            val inBox = apps.filter { modeOf(it.pkg) == mode }
            Column(
                Modifier.fillMaxWidth()
                    .onGloballyPositioned { drag.boxes[mode] = it.boundsInWindow() }
                    .clip(RoundedCornerShape(16.dp))
                    .background(if (hover == mode) m.selected else m.card)
                    .padding(12.dp),
                verticalArrangement = Arrangement.spacedBy(8.dp),
            ) {
                Text("${mode.label} ${inBox.size}", fontSize = 13.sp, color = m.muted)
                FlowRow(horizontalArrangement = Arrangement.spacedBy(6.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
                    for (a in inBox) ModeChip(a, mode, drag, onSet)
                    if (inBox.isEmpty()) Text("여기로 끌어 놓기", fontSize = 13.sp, color = m.faint, modifier = Modifier.padding(vertical = 6.dp))
                }
            }
        }
    }
}

/** 묶음 안 앱 하나: 꾹 눌러 끌기, 눌러서 방식 고르기 */
@Composable
private fun ModeChip(a: AppEntry, mode: NoticePolicy.Mode, drag: ModeDrag, onSet: (String, NoticePolicy.Mode) -> Unit) {
    val haptic = LocalHapticFeedback.current
    var menu by remember { mutableStateOf(false) }
    var origin by remember { mutableStateOf(Offset.Zero) }
    Box {
        AppChip(
            a,
            Modifier
                .onGloballyPositioned { origin = it.positionInWindow() }
                .graphicsLayer { alpha = if (drag.pkg == a.pkg) 0.3f else 1f }
                .pointerInput(a.pkg) { detectTapGestures(onTap = { menu = true }) }
                .pointerInput(a.pkg) {
                    detectDragGesturesAfterLongPress(
                        onDragStart = { o ->
                            haptic.performHapticFeedback(HapticFeedbackType.LongPress)
                            drag.finger = origin + o
                            drag.pkg = a.pkg
                        },
                        onDrag = { change, d -> change.consume(); drag.finger += d },
                        onDragEnd = {
                            drag.target()?.let { if (it != mode) onSet(a.pkg, it) }
                            drag.pkg = null
                        },
                        onDragCancel = { drag.pkg = null },
                    )
                },
        )
        DropdownMenu(expanded = menu, onDismissRequest = { menu = false }) {
            for (x in NoticePolicy.Mode.entries) {
                DropdownMenuItem(text = { Text((if (x == mode) "✓ " else "") + x.label) }, onClick = { menu = false; onSet(a.pkg, x) })
            }
        }
    }
}

@Composable
private fun AppChip(a: AppEntry, modifier: Modifier = Modifier) {
    val m = LocalModo.current
    Row(
        modifier.clip(RoundedCornerShape(20.dp)).background(m.raised).padding(start = 6.dp, end = 12.dp, top = 6.dp, bottom = 6.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(6.dp),
    ) {
        AppIcon(a, 22.dp, 6.dp)
        Text(a.label, fontSize = 14.sp, maxLines = 1)
    }
}

@Composable
private fun AppIcon(a: AppEntry, size: androidx.compose.ui.unit.Dp, corner: androidx.compose.ui.unit.Dp) {
    val m = LocalModo.current
    if (a.icon != null) {
        Image(a.icon, contentDescription = null, modifier = Modifier.size(size).clip(RoundedCornerShape(corner)))
    } else {
        Box(Modifier.size(size).clip(RoundedCornerShape(corner)).background(m.card), contentAlignment = Alignment.Center) {
            Text(a.label.take(1), fontWeight = FontWeight.Bold, color = m.muted, fontSize = (size.value * 0.45f).sp)
        }
    }
}

@Composable
private fun SearchBox(value: String, onChange: (String) -> Unit) {
    val m = LocalModo.current
    Row(
        Modifier.fillMaxWidth().padding(start = Gutter, end = Gutter, top = 20.dp).height(48.dp).clip(RoundedCornerShape(12.dp)).background(m.card).padding(horizontal = 14.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(10.dp),
    ) {
        Icon(SearchIcon, contentDescription = null, tint = m.muted, modifier = Modifier.size(18.dp))
        Box(Modifier.weight(1f)) {
            if (value.isEmpty()) Text("앱 이름으로 찾기", fontSize = 16.sp, color = m.faint)
            BasicTextField(
                value = value,
                onValueChange = onChange,
                singleLine = true,
                textStyle = TextStyle(fontSize = 16.sp, color = MaterialTheme.colorScheme.onBackground),
                cursorBrush = SolidColor(MaterialTheme.colorScheme.onBackground),
                modifier = Modifier.fillMaxWidth(),
            )
        }
    }
}

@Composable
private fun AppRow(a: AppEntry, checked: Boolean, notified: Int, onClick: () -> Unit) {
    val m = LocalModo.current
    Row(
        Modifier.fillMaxWidth().tap(onClick = onClick).padding(horizontal = Gutter, vertical = 10.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(14.dp),
    ) {
        AppIcon(a, 40.dp, 10.dp)
        Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(2.dp)) {
            Text(a.label, fontSize = 16.sp, maxLines = 2)
            if (notified > 0) Text("최근 알림 ${notified}건", fontSize = 13.sp, color = m.faint)
        }
        CheckCircle(checked)
    }
}

/**
 * 앱 안에서 결제 알림을 켜는 곳 안내. 메뉴 이름은 앱 업데이트마다 바뀌어서 "앱 안 설정 → 알림" 정도로만 적는다.
 * docs/DECISIONS.md 「분류 폴더」 옆 「알림 읽을 앱 안내」
 */
/** 쇼핑·배달 앱을 골랐을 때 "?" 설명 */
private const val SHOP_HINT = "주문·배달 알림엔 금액이 없어서 가계부에 안 적혀요. 돈은 결제한 곳 알림으로 잡혀요. " +
    "쿠페이·배민페이를 계좌로 내면 그 은행 앱(토스 등), 카드로 내면 카드 앱(KB Pay 등)을 골라 주세요. 삼성페이 알림은 매장에서 낸 것만 와요."

private fun payAppHint(label: String): String {
    val l = label.lowercase()
    return when {
        "kb" in l || "국민" in l -> "앱 안 설정의 알림에서 결제(승인) 푸시 알림을 켜 주세요. 승인 문자(SMS)만 받고 있으면 읽을 수 없어요"
        "토스" in l || "toss" in l -> "토스 앱 안 설정의 알림에서 결제·입출금 알림을 켜 주세요"
        "카카오" in l || "kakao" in l -> "앱 안 설정의 알림에서 결제 알림을 켜 주세요"
        "네이버" in l || "naver" in l -> "앱 안 설정의 알림에서 페이 결제 알림을 켜 주세요"
        else -> "앱 안 설정의 알림에서 결제 알림을 켜 주세요"
    }
}

/** 폰 설정의 그 앱 알림 화면을 연다 */
private fun openAppNotificationSettings(ctx: Context, pkg: String) {
    val i = Intent(Settings.ACTION_APP_NOTIFICATION_SETTINGS).putExtra(Settings.EXTRA_APP_PACKAGE, pkg).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
    runCatching { ctx.startActivity(i) }.onFailure {
        runCatching { ctx.startActivity(Intent(Settings.ACTION_APPLICATION_DETAILS_SETTINGS, Uri.parse("package:$pkg")).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)) }
    }
}

/** 런처에 보이는 앱(이름·아이콘). 매니페스트 <queries>의 LAUNCHER 선언으로 볼 수 있는 범위다 */
private fun loadInstalled(ctx: Context): List<AppEntry> {
    val pm = ctx.packageManager
    val intent = Intent(Intent.ACTION_MAIN).addCategory(Intent.CATEGORY_LAUNCHER)
    val acts = if (Build.VERSION.SDK_INT >= 33) {
        pm.queryIntentActivities(intent, PackageManager.ResolveInfoFlags.of(0))
    } else {
        @Suppress("DEPRECATION") pm.queryIntentActivities(intent, 0)
    }
    val px = (40 * ctx.resources.displayMetrics.density).toInt()
    return acts.map { it.activityInfo.packageName }.distinct().filter { it != ctx.packageName }.mapNotNull { pkg ->
        runCatching {
            val info = pm.getApplicationInfo(pkg, 0)
            val label = pm.getApplicationLabel(info).toString()
            val icon = runCatching { pm.getApplicationIcon(info).toBitmap(px, px).asImageBitmap() }.getOrNull()
            AppEntry(pkg, label, icon)
        }.getOrNull()
    }.sortedBy { it.label.lowercase() }
}
