package com.yuntyu.myhome.ui

import android.content.Context
import android.content.Intent
import android.content.pm.PackageManager
import android.os.Build
import androidx.compose.foundation.Image
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
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
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext

private class AppEntry(val pkg: String, val label: String, val icon: ImageBitmap?)

/**
 * 이름에 이런 말이 들어간 앱을 "결제 앱 같아요"로 먼저 보여준다. 순서만 바꿀 뿐 고르는 건 사용자다
 * (패키지명은 코드에 박지 않는다: 앱 업데이트·지역에 따라 다를 수 있어서). docs/DECISIONS.md 「앱 화면 단순화」
 */
private val PAY_WORDS = listOf(
    "카드", "페이", "pay", "뱅크", "bank", "은행", "토스", "toss", "국민", "kb", "신한", "하나", "우리", "농협", "nh",
    "기업", "ibk", "쿠팡", "coupang", "네이버", "카카오", "월렛", "wallet", "티머니", "증권", "머니", "money",
)

private fun payLike(label: String): Boolean = label.lowercase().let { l -> PAY_WORDS.any { l.contains(it) } }

/**
 * 알림 읽을 앱 고르기: 폰에 깔린 앱(런처에 보이는 앱)을 보여준다. 결제 앱 같은 것을 먼저, 나머지는 펼치거나 검색해서 고른다.
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
    var query by rememberSaveable { mutableStateOf("") }
    var showAll by rememberSaveable { mutableStateOf(false) }
    val installed by produceState<List<AppEntry>?>(null) { value = withContext(Dispatchers.IO) { loadInstalled(ctx) } }
    val selected = settings.selectedPackages
    // 고른 앱인데 폰에서 지웠거나 런처에 없는 앱도 보이게(고른 것을 풀 수 있게)
    val all = installed.orEmpty().let { list ->
        val known = list.map { it.pkg }.toSet()
        list + selected.filter { it !in known }.map { pkg -> AppEntry(pkg, recent.firstOrNull { it.packageName == pkg }?.appLabel ?: pkg, null) }
    }
    val q = query.trim().lowercase()
    // 화면을 열 때 이미 고른 앱도 위에 둔다. 고르는 도중에는 순서를 바꾸지 않는다(누른 줄이 튀지 않게)
    val pinned = remember(installed == null) { selected }
    val (likely, others) = all.partition { payLike(it.label) || it.pkg in pinned }
    fun toggle(pkg: String) = app.settings.setPackageSelected(pkg, pkg !in selected)

    Column(modifier.fillMaxSize()) {
        LazyColumn(Modifier.weight(1f)) {
            item {
                Column(Modifier.padding(start = Gutter, end = Gutter, top = 8.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                    Text("결제 알림이 오는 앱을\n골라 주세요", fontSize = 24.sp, fontWeight = FontWeight.Bold, lineHeight = 33.sp)
                    Text("고른 앱의 알림만 읽어요. 다른 앱 알림은 보지 않아요.", fontSize = 15.sp, color = m.muted, lineHeight = 22.sp)
                }
            }
            item { SearchBox(query) { query = it } }
            when {
                installed == null -> item { EmptyNote("폰에 있는 앱을 찾는 중이에요") }
                q.isNotEmpty() -> {
                    val found = all.filter { it.label.lowercase().contains(q) || it.pkg.lowercase().contains(q) }
                    if (found.isEmpty()) item { EmptyNote("'$query' 이름의 앱이 없어요") }
                    items(found, key = { it.pkg }) { a -> AppRow(a, a.pkg in selected, counts[a.pkg] ?: 0) { toggle(a.pkg) } }
                }
                else -> {
                    // 결제 앱 같은 게 없으면 접지 않고 전부 보여준다
                    val first = likely.ifEmpty { others }
                    item { SectionLabel(if (likely.isEmpty()) "이 폰의 앱" else "결제 앱 같아요") }
                    items(first, key = { it.pkg }) { a -> AppRow(a, a.pkg in selected, counts[a.pkg] ?: 0) { toggle(a.pkg) } }
                    if (likely.isNotEmpty() && others.isNotEmpty()) {
                        item {
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
        Modifier.fillMaxWidth().clickable(onClick = onClick).padding(horizontal = Gutter, vertical = 10.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(14.dp),
    ) {
        if (a.icon != null) {
            Image(a.icon, contentDescription = null, modifier = Modifier.size(40.dp).clip(RoundedCornerShape(10.dp)))
        } else {
            Box(Modifier.size(40.dp).clip(RoundedCornerShape(10.dp)).background(m.card), contentAlignment = Alignment.Center) {
                Text(a.label.take(1), fontWeight = FontWeight.Bold, color = m.muted)
            }
        }
        Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(2.dp)) {
            Text(a.label, fontSize = 16.sp, maxLines = 2)
            if (notified > 0) Text("최근 알림 ${notified}건", fontSize = 13.sp, color = m.faint)
        }
        CheckCircle(checked)
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
