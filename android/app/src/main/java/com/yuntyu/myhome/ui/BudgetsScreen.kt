package com.yuntyu.myhome.ui

import android.widget.Toast
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.imePadding
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.ExperimentalMaterial3Api
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
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import com.yuntyu.myhome.MyHomeApp
import com.yuntyu.myhome.core.CategoryTree
import com.yuntyu.myhome.core.Money
import com.yuntyu.myhome.core.WidgetModel
import com.yuntyu.myhome.core.api.ApiResult
import com.yuntyu.myhome.core.api.BudgetItem
import com.yuntyu.myhome.core.api.BudgetList
import com.yuntyu.myhome.core.api.Category
import com.yuntyu.myhome.widget.WidgetRefresher
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext

/** 고칠 대상: 있는 분류, 또는 새 분류(category == null, parent 안에 kind로 만든다) */
private class EditTarget(val category: Category?, val amount: Long, val parent: Long? = null, val kind: String = "expense", val folder: Boolean = false)

/** 저장할 값. parent는 넣을 폴더(null = 맨 위) */
private class CategoryForm(val name: String, val amount: Long, val memo: String, val parent: Long?, val hide: Boolean)

/** 폴더 한 단계 들여쓰기 */
private val Indent = 22.dp

/** 줄 앞 들여쓰기(맨 위면 없음). FlatRow 칸 사이 14dp를 빼서 한 단계가 Indent만큼 들어가게 한다 */
fun indentLead(depth: Int): (@Composable () -> Unit)? =
    if (depth > 0) { { Spacer(Modifier.width(Indent * depth - 14.dp)) } } else null

/**
 * 분류와 예산: 분류를 폴더처럼 묶고(최대 3단계), 분류마다 한 달 예산과 설명을 정한다. 설명은 AI 분류 추천이 참고한다.
 * 폴더에 예산을 따로 안 정하면 안 분류 예산의 합이 폴더 예산이 된다. 예산 시작일(월급날)도 여기서.
 * 특수 분류(정산·내 계좌 이체·투자·저축)는 계산 규칙에 묶여 있어서 보여주지 않는다. docs/DECISIONS.md 「분류 폴더」
 */
@Composable
fun BudgetsScreen(modifier: Modifier) {
    val ctx = LocalContext.current
    val app = MyHomeApp.instance
    val scope = rememberCoroutineScope()
    val m = LocalModo.current
    var cats by remember { mutableStateOf<List<Category>?>(null) }
    var budgets by remember { mutableStateOf<BudgetList?>(null) }
    var startDay by remember { mutableStateOf<Int?>(null) }
    var error by remember { mutableStateOf<String?>(null) }
    var tick by remember { mutableIntStateOf(0) }
    var editing by remember { mutableStateOf<EditTarget?>(null) }
    var showHidden by remember { mutableStateOf(false) }
    var dayDialog by remember { mutableStateOf(false) }
    // AI 분류 공통 메모(분류마다 다는 설명과 따로, 모든 가게에 먼저 적용). null = 아직 못 받음
    var aiMemo by remember { mutableStateOf<String?>(null) }
    var memoDialog by remember { mutableStateOf(false) }

    LaunchedEffect(tick) {
        val c = withContext(Dispatchers.IO) { app.api.categories() }
        val b = withContext(Dispatchers.IO) { app.api.budgets() }
        val d = withContext(Dispatchers.IO) { app.api.budgetStartDay() }
        if (c is ApiResult.Ok) cats = c.value.categories.filter { it.kind == "expense" || it.kind == "income" } else error = c.plain()
        if (b is ApiResult.Ok) budgets = b.value else error = b.plain()
        if (d is ApiResult.Ok) startDay = d.value
        val memo = withContext(Dispatchers.IO) { app.api.aiMemo() }
        if (memo is ApiResult.Ok) aiMemo = memo.value
    }
    fun changed(msg: String) {
        Toast.makeText(ctx, msg, Toast.LENGTH_SHORT).show()
        editing = null
        tick++
        scope.launch { WidgetRefresher.refresh(ctx) }
    }

    val own = budgets?.items.orEmpty().associate { it.categoryId to it.amount }
    val list = cats
    val tree = remember(list) { CategoryTree(list.orEmpty()) }
    Column(modifier.fillMaxSize().verticalScroll(rememberScrollState()).padding(bottom = 32.dp)) {
        if (list == null || budgets == null) {
            EmptyNote(if (error != null) "불러오지 못했어요" else "불러오는 중이에요", error)
            return@Column
        }
        val month = budgets?.period?.label?.let(WidgetModel::monthName) ?: "이번 달"
        val total = tree.totalBudget(own)
        Column(Modifier.padding(start = Gutter, end = Gutter, top = 8.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
            Text("$month 예산", fontSize = 16.sp, color = m.muted)
            Row(verticalAlignment = Alignment.CenterVertically) {
                Text(if (total > 0) Money.won(total) else "아직 없어요", Modifier.weight(1f, fill = false), fontSize = 30.sp, fontWeight = FontWeight.Bold)
                HelpDot("분류를 누르면 예산, 이름, 설명, 들어갈 폴더를 바꿀 수 있어요. 폴더에 예산을 안 정하면 안에 있는 분류 예산을 더해요.", title = "예산")
            }
        }

        @Composable
        fun TreeRows(kind: String) {
            for ((c, depth) in tree.preorder { !it.archived }.filter { it.first.kind == kind }) {
                val folder = tree.children(c.id).any { !it.archived }
                val a = own[c.id] ?: 0L
                val eff = if (kind == "expense") tree.effectiveBudget(own, c.id) else 0L
                val value = when {
                    kind != "expense" -> null
                    a > 0 -> Money.won(a)
                    eff > 0 -> "안 합계 ${Money.won(eff)}"
                    else -> "예산 없음"
                }
                FlatRow(
                    c.name,
                    sub = c.memo.ifBlank { null },
                    value = value,
                    bold = folder,
                    valueColor = if (a > 0) MaterialTheme.colorScheme.onBackground else m.faint,
                    leading = indentLead(depth),
                    onClick = { editing = EditTarget(c, a) },
                )
            }
        }

        FlatRow(
            "AI 분류 메모",
            value = aiMemo?.let { t -> t.lineSequence().firstOrNull { it.isNotBlank() }?.trim()?.let { if (it.length > 14) it.take(14) + "…" else it } ?: "없음" } ?: "",
            valueColor = if (aiMemo.isNullOrBlank()) m.faint else MaterialTheme.colorScheme.onBackground,
            help = "AI가 결제를 분류하기 전에 모든 가게에 먼저 적용하는 메모예요. 분류마다 다는 설명과 따로예요.\n\n예: 회사 근처 식당은 식비, 편의점 도시락은 식비, 다이소는 생활\n\n가게 이름과 분류 이름·설명, 이 메모만 AI에게 보내요.",
            onClick = { memoDialog = true },
        )
        Hairline(Modifier.padding(horizontal = Gutter))
        SectionLabel(
            "쓰는 돈",
            help = "분류 안에 분류를 넣으면 폴더가 돼요(3단계까지). 폴더를 만든 뒤 분류를 눌러 '들어갈 폴더'에서 고르거나, 폴더를 눌러 '이 안에 분류 추가'를 누르세요. 폴더 예산을 안 정하면 안에 있는 분류 예산을 더해요.",
        )
        TreeRows("expense")
        AddRow("분류 추가") { editing = EditTarget(null, 0) }
        AddRow("폴더 만들기") { editing = EditTarget(null, 0, folder = true) }

        if (list.any { it.kind == "income" && !it.archived }) {
            SectionLabel("들어오는 돈")
            TreeRows("income")
        }

        // 숨긴 분류: 위 폴더까지 숨긴 것은 그 폴더 하나로만 보인다(되살리면 안도 같이 돌아온다)
        val hidden = list.filter { it.archived && tree.parent(it.id)?.let { p -> tree.byId[p]?.archived } != true }
        if (hidden.isNotEmpty()) {
            Hairline(Modifier.padding(top = 12.dp))
            FlatRow(if (showHidden) "지운 분류 접기" else "지운 분류", value = "${hidden.size}개", titleColor = m.muted, chevron = false, onClick = { showHidden = !showHidden })
            if (showHidden) for (c in hidden) {
                FlatRow(tree.label(c.id), value = "되살리기", titleColor = m.muted, chevron = false, onClick = {
                    scope.launch {
                        val r = withContext(Dispatchers.IO) { app.api.updateCategory(c.id, archived = false) }
                        if (r is ApiResult.Ok) changed("'${c.name}'을 되살렸어요") else Toast.makeText(ctx, r.plain(), Toast.LENGTH_LONG).show()
                    }
                })
            }
        }

        SectionLabel("기간")
        FlatRow("예산 시작일", value = startDay?.let { "매달 ${it}일" } ?: "", help = "월급날로 바꾸면 그날부터 한 달로 쳐요.", onClick = { dayDialog = true })
    }

    editing?.let { target ->
        val periodMonth = budgets?.period?.label
        CategorySheet(target, tree, own, onDismiss = { editing = null }, onAddInside = { parent -> editing = EditTarget(null, 0, parent.id, parent.kind) }) { f ->
            scope.launch {
                val c = target.category
                val err: String? = withContext(Dispatchers.IO) {
                    when {
                        f.hide && c != null -> app.api.updateCategory(c.id, archived = true).let { if (it is ApiResult.Ok) null else it.plain() }
                        c == null -> when (val r = app.api.createCategory(f.name, target.kind, f.parent, f.memo.ifBlank { null })) {
                            is ApiResult.Ok -> if (f.amount > 0 && periodMonth != null && r.value.kind == "expense") {
                                app.api.putBudgets(periodMonth, listOf(BudgetItem(r.value.id, amount = f.amount))).let { if (it is ApiResult.Ok) null else it.plain() }
                            } else null
                            else -> r.plain()
                        }
                        else -> {
                            val moved = f.parent != tree.parent(c.id)
                            val needPatch = f.name != c.name || f.memo != c.memo || moved
                            val patched = if (needPatch) {
                                app.api.updateCategory(
                                    c.id,
                                    name = f.name.takeIf { it != c.name },
                                    parentId = if (moved) (f.parent ?: 0L) else null,
                                    memo = f.memo.takeIf { it != c.memo },
                                )
                            } else null
                            val saved = if (patched?.let { it is ApiResult.Ok } != false && c.kind == "expense" && f.amount != target.amount && periodMonth != null) {
                                app.api.putBudgets(periodMonth, listOf(BudgetItem(c.id, amount = f.amount)))
                            } else null
                            listOfNotNull(patched, saved).firstOrNull { it !is ApiResult.Ok }?.plain()
                        }
                    }
                }
                if (err == null) changed(
                    when {
                        f.hide -> "지웠어요. 지난 결제는 그대로 남아요"
                        target.folder -> "폴더를 만들었어요. 폴더를 눌러 안에 분류를 넣어요"
                        else -> "저장했어요"
                    },
                ) else Toast.makeText(ctx, err, Toast.LENGTH_LONG).show()
            }
        }
    }
    if (memoDialog) {
        var text by remember { mutableStateOf(aiMemo.orEmpty()) }
        ModoAlertDialog(
            onDismissRequest = { memoDialog = false },
            title = { Text("AI 분류 메모") },
            text = {
                LineField(text, { text = it.take(AI_MEMO_MAX) }, "모든 가게에 먼저 적용해요", maxLines = 8)
            },
            confirmButton = {
                TextButton(onClick = {
                    scope.launch {
                        val r = withContext(Dispatchers.IO) { app.api.putAiMemo(text.trim()) }
                        memoDialog = false
                        if (r is ApiResult.Ok) { aiMemo = text.trim(); Toast.makeText(ctx, "저장했어요. 새 결제부터 참고해요", Toast.LENGTH_SHORT).show() }
                        else Toast.makeText(ctx, r.plain(), Toast.LENGTH_LONG).show()
                    }
                }) { Text("저장") }
            },
            dismissButton = { TextButton(onClick = { memoDialog = false }) { Text("닫기") } },
        )
    }
    if (dayDialog) {
        var text by remember { mutableStateOf(startDay?.toString() ?: "1") }
        val n = text.toIntOrNull()
        ModoAlertDialog(
            onDismissRequest = { dayDialog = false },
            title = { Text("예산 시작일") },
            text = {
                Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                    Text("매달 며칠부터 한 달로 칠까요? 1~28일 중에서 골라 주세요.", fontSize = 14.sp, color = m.muted)
                    LineField(text, { text = it.filter(Char::isDigit).take(2) }, "날짜", keyboardType = KeyboardType.Number)
                }
            },
            confirmButton = {
                TextButton(enabled = n != null && n in 1..28, onClick = {
                    val day = n ?: return@TextButton
                    scope.launch {
                        val r = withContext(Dispatchers.IO) { app.api.putBudgetStartDay(day) }
                        dayDialog = false
                        if (r is ApiResult.Ok) changed("매달 ${day}일부터 한 달로 쳐요") else Toast.makeText(ctx, r.plain(), Toast.LENGTH_LONG).show()
                    }
                }) { Text("저장") }
            },
            dismissButton = { TextButton(onClick = { dayDialog = false }) { Text("닫기") } },
        )
    }
}

/** AI 분류 공통 메모 길이 상한(서버 pipeline.AIMemoMax와 같다) */
private const val AI_MEMO_MAX = 1000

/** 분류 고치기·추가: 이름, 한 달 예산(쓰는 돈만), 설명(AI 참고), 들어갈 폴더, 지우기, 이 안에 분류 추가 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun CategorySheet(
    target: EditTarget,
    tree: CategoryTree,
    own: Map<Long, Long>,
    onDismiss: () -> Unit,
    onAddInside: (Category) -> Unit,
    onSave: (CategoryForm) -> Unit,
) {
    val m = LocalModo.current
    val c = target.category
    val kind = c?.kind ?: target.parent?.let { tree.byId[it]?.kind } ?: target.kind
    var name by remember { mutableStateOf(c?.name.orEmpty()) }
    var amount by remember { mutableStateOf(if (target.amount > 0) target.amount.toString() else "") }
    var memo by remember { mutableStateOf(c?.memo.orEmpty()) }
    var parent by remember { mutableStateOf(if (c != null) tree.parent(c.id) else target.parent) }
    var picking by remember { mutableStateOf(false) }
    val won = amount.toLongOrNull() ?: 0L
    val special = c?.code != null
    val folder = c != null && tree.children(c.id).any { !it.archived }
    ModoBottomSheet(onDismissRequest = onDismiss) {
        Column(Modifier.fillMaxWidth().imePadding().navigationBarsPadding().verticalScroll(rememberScrollState())) {
            Text(
                when {
                    c != null -> c.name
                    target.folder -> "폴더 만들기"
                    else -> "분류 추가"
                },
                Modifier.padding(horizontal = Gutter), fontSize = 20.sp, fontWeight = FontWeight.Bold,
            )
            Column(Modifier.padding(horizontal = Gutter, vertical = 8.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
                LineField(name, { name = it.take(20) }, "이름")
                if (kind == "expense") {
                    val sum = if (c != null) tree.children(c.id).filter { !it.archived }.sumOf { tree.effectiveBudget(own, it.id) } else 0L
                    LineField(
                        amount, { amount = it.filter(Char::isDigit).take(10) },
                        when {
                            won > 0 -> "한 달 예산 · ${Money.won(won)}"
                            folder && sum > 0 -> "한 달 예산 · 비우면 ${Money.won(sum)}"
                            else -> "한 달 예산"
                        },
                        keyboardType = KeyboardType.Number,
                    )
                }
                Row(verticalAlignment = Alignment.CenterVertically) {
                    LineField(memo, { memo = it.take(CategoryTree.MAX_MEMO) }, "설명", Modifier.weight(1f), maxLines = 4)
                    HelpDot("AI가 분류를 고를 때 참고해요. 예: 배달 앱, 편의점 도시락, 회사 근처 식당. 가게 이름과 이 설명만 AI에게 보내요.", title = "설명")
                }
            }
            if (!special) {
                FlatRow(
                    "들어갈 폴더",
                    value = parent?.let { tree.label(it) } ?: "맨 위",
                    chevron = false,
                    onClick = { picking = !picking },
                )
                if (picking) {
                    val choices = tree.parentChoices(c?.id, kind)
                    FlatRow("맨 위", chevron = false, trailing = { CheckCircle(parent == null) }, onClick = { parent = null; picking = false })
                    for (p in choices) {
                        FlatRow(
                            p.name,
                            chevron = false,
                            leading = indentLead(tree.depth(p.id) + 1),
                            trailing = { CheckCircle(parent == p.id) },
                            onClick = { parent = p.id; picking = false },
                        )
                    }
                    if (c != null && tree.height(c.id) > 0) {
                        Text("3단계를 넘는 곳으로는 못 옮겨요", Modifier.padding(horizontal = Gutter, vertical = 6.dp), fontSize = 13.sp, color = m.faint)
                    }
                }
            }
            PrimaryButton("저장", Modifier.padding(horizontal = Gutter, vertical = 12.dp), enabled = name.isNotBlank()) {
                onSave(CategoryForm(name.trim(), won, memo.trim(), parent, false))
            }
            if (c != null && !special && tree.depth(c.id) + 1 < CategoryTree.MAX_DEPTH) {
                AddRow("이 안에 분류 추가") { onAddInside(c) }
            }
            if (c != null && !special) {
                FlatRow(
                    "이 분류 지우기",
                    help = if (folder) "안에 있는 분류도 같이 지워요. 지난 결제는 그대로 남고, 지운 분류에서 되살릴 수 있어요."
                    else "지난 결제는 그대로 남고, 새 결제는 이 분류로 자동으로 들어가지 않아요.",
                    titleColor = m.over,
                    chevron = false,
                    onClick = { onSave(CategoryForm(c.name, target.amount, c.memo, tree.parent(c.id), true)) },
                )
            }
            Spacer(Modifier.height(12.dp))
        }
    }
}
