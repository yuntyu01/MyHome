package com.yuntyu.myhome.ui

import android.widget.Toast
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.imePadding
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.rememberModalBottomSheetState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import com.yuntyu.myhome.MyHomeApp
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

/** 고칠 대상: 있는 분류 또는 새 분류(category == null) */
private class EditTarget(val category: Category?, val amount: Long)

/**
 * 분류와 예산: 쓰는 돈 분류마다 한 달 예산을 정하고, 분류를 더하거나 이름을 바꾸거나 숨긴다. 예산 시작일(월급날)도 여기서.
 * 특수 분류(수입·정산·내 계좌 이체·투자·저축)는 계산 규칙에 묶여 있어서 보여주지 않는다.
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

    LaunchedEffect(tick) {
        val c = withContext(Dispatchers.IO) { app.api.categories() }
        val b = withContext(Dispatchers.IO) { app.api.budgets() }
        val d = withContext(Dispatchers.IO) { app.api.budgetStartDay() }
        if (c is ApiResult.Ok) cats = c.value.categories.filter { it.kind == "expense" || it.kind == "income" } else error = c.plain()
        if (b is ApiResult.Ok) budgets = b.value else error = b.plain()
        if (d is ApiResult.Ok) startDay = d.value
    }
    fun changed(msg: String) {
        Toast.makeText(ctx, msg, Toast.LENGTH_SHORT).show()
        editing = null
        tick++
        scope.launch { WidgetRefresher.refresh(ctx) }
    }

    val amounts = budgets?.items.orEmpty().associate { it.categoryId to it.amount }
    val list = cats
    Column(modifier.fillMaxSize().verticalScroll(rememberScrollState()).padding(bottom = 32.dp)) {
        if (list == null || budgets == null) {
            EmptyNote(if (error != null) "불러오지 못했어요" else "불러오는 중이에요", error)
            return@Column
        }
        val month = budgets?.period?.label?.let(WidgetModel::monthName) ?: "이번 달"
        val active = list.filter { !it.archived }
        val total = active.filter { it.kind == "expense" }.sumOf { amounts[it.id] ?: 0L }
        Column(Modifier.padding(start = Gutter, end = Gutter, top = 8.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
            Text("$month 예산", fontSize = 16.sp, color = m.muted)
            Text(if (total > 0) Money.won(total) else "아직 없어요", fontSize = 30.sp, fontWeight = FontWeight.Bold)
            Text("분류를 누르면 한 달에 쓸 돈과 이름을 바꿀 수 있어요.", fontSize = 14.sp, color = m.muted, lineHeight = 20.sp)
        }

        SectionLabel("쓰는 돈")
        for (c in active.filter { it.kind == "expense" }) {
            val a = amounts[c.id] ?: 0L
            FlatRow(
                c.name,
                value = if (a > 0) Money.won(a) else "예산 없음",
                valueColor = if (a > 0) MaterialTheme.colorScheme.onBackground else m.faint,
                onClick = { editing = EditTarget(c, a) },
            )
        }
        AddRow("분류 추가") { editing = EditTarget(null, 0) }

        val income = active.filter { it.kind == "income" }
        if (income.isNotEmpty()) {
            SectionLabel("들어오는 돈")
            for (c in income) FlatRow(c.name, onClick = { editing = EditTarget(c, 0) })
        }

        val hidden = list.filter { it.archived }
        if (hidden.isNotEmpty()) {
            Hairline(Modifier.padding(top = 12.dp))
            FlatRow(if (showHidden) "숨긴 분류 접기" else "숨긴 분류", value = "${hidden.size}개", titleColor = m.muted, chevron = false, onClick = { showHidden = !showHidden })
            if (showHidden) for (c in hidden) {
                FlatRow(c.name, value = "다시 보이기", titleColor = m.muted, chevron = false, onClick = {
                    scope.launch {
                        val r = withContext(Dispatchers.IO) { app.api.updateCategory(c.id, archived = false) }
                        if (r is ApiResult.Ok) changed("'${c.name}'을 다시 보여요") else Toast.makeText(ctx, r.plain(), Toast.LENGTH_LONG).show()
                    }
                })
            }
        }

        SectionLabel("기간")
        FlatRow("예산 시작일", value = startDay?.let { "매달 ${it}일" } ?: "", sub = "월급날로 바꾸면 그날부터 한 달로 쳐요", onClick = { dayDialog = true })
    }

    editing?.let { target ->
        val periodMonth = budgets?.period?.label
        CategorySheet(target, onDismiss = { editing = null }) { name, amount, hide ->
            scope.launch {
                val c = target.category
                val err: String? = withContext(Dispatchers.IO) {
                    when {
                        hide && c != null -> (app.api.updateCategory(c.id, archived = true) as? ApiResult.Ok)?.let { null } ?: "숨기지 못했어요"
                        c == null -> when (val r = app.api.createCategory(name)) {
                            is ApiResult.Ok -> if (amount > 0 && periodMonth != null) {
                                app.api.putBudgets(periodMonth, listOf(BudgetItem(r.value.id, amount = amount))).let { if (it is ApiResult.Ok) null else it.plain() }
                            } else null
                            else -> if ((r as? ApiResult.HttpError)?.code == 409) "같은 이름의 분류가 있어요" else r.plain()
                        }
                        else -> {
                            val renamed = if (name != c.name) app.api.updateCategory(c.id, name = name) else null
                            val saved = if (c.kind == "expense" && amount != target.amount && periodMonth != null) {
                                app.api.putBudgets(periodMonth, listOf(BudgetItem(c.id, amount = amount)))
                            } else null
                            listOfNotNull(renamed, saved).firstOrNull { it !is ApiResult.Ok }?.plain()
                        }
                    }
                }
                if (err == null) changed(if (hide) "숨겼어요. 지난 거래는 그대로 남아요" else "저장했어요") else Toast.makeText(ctx, err, Toast.LENGTH_LONG).show()
            }
        }
    }
    if (dayDialog) {
        var text by remember { mutableStateOf(startDay?.toString() ?: "1") }
        val n = text.toIntOrNull()
        AlertDialog(
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

/** 분류 고치기·추가: 이름, 한 달 예산(쓰는 돈만), 숨기기 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun CategorySheet(target: EditTarget, onDismiss: () -> Unit, onSave: (name: String, amount: Long, hide: Boolean) -> Unit) {
    val m = LocalModo.current
    val c = target.category
    var name by remember { mutableStateOf(c?.name.orEmpty()) }
    var amount by remember { mutableStateOf(if (target.amount > 0) target.amount.toString() else "") }
    val won = amount.toLongOrNull() ?: 0L
    val expense = c == null || c.kind == "expense"
    ModalBottomSheet(onDismissRequest = onDismiss, sheetState = rememberModalBottomSheetState(skipPartiallyExpanded = true), containerColor = m.bg) {
        Column(Modifier.fillMaxWidth().imePadding().navigationBarsPadding()) {
            Text(if (c == null) "분류 추가" else c.name, Modifier.padding(horizontal = Gutter), fontSize = 20.sp, fontWeight = FontWeight.Bold)
            Column(Modifier.padding(horizontal = Gutter, vertical = 8.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
                LineField(name, { name = it.take(20) }, "이름")
                if (expense) {
                    LineField(amount, { amount = it.filter(Char::isDigit).take(10) }, if (won > 0) "한 달 예산 · ${Money.won(won)}" else "한 달 예산(원, 없으면 비워 두기)", keyboardType = KeyboardType.Number)
                }
            }
            PrimaryButton("저장", Modifier.padding(horizontal = Gutter, vertical = 12.dp), enabled = name.isNotBlank()) { onSave(name.trim(), won, false) }
            if (c != null) {
                FlatRow("이 분류 숨기기", sub = "지난 거래는 그대로 남고, 새 결제에는 안 보여요", titleColor = m.muted, chevron = false, onClick = { onSave(c.name, target.amount, true) })
            }
            Spacer(Modifier.height(12.dp))
        }
    }
}
