package com.yuntyu.myhome.ui

import android.widget.Toast
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.ui.draw.clip
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Checkbox
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.FilledTonalButton
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
import androidx.compose.ui.unit.dp
import com.yuntyu.myhome.MyHomeApp
import com.yuntyu.myhome.core.Money
import com.yuntyu.myhome.core.WidgetModel
import com.yuntyu.myhome.core.api.ApiResult
import com.yuntyu.myhome.core.api.Category
import com.yuntyu.myhome.core.api.TransactionDto
import com.yuntyu.myhome.core.api.TransactionEdit
import com.yuntyu.myhome.core.api.describe
import com.yuntyu.myhome.widget.WidgetRefresher
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext

private val reviewReasonLabels = mapOf(
    "no_merchant" to "가맹점 이름 없음", "no_rule" to "처음 보는 가맹점", "payment_gateway" to "결제대행사로 찍힘",
    "transfer" to "송금", "deposit" to "어떤 입금인지 확인", "unknown_kind" to "어떤 거래인지 모름",
    "cancel_unmatched" to "원래 결제를 못 찾은 취소", "cancel_partial" to "부분 취소",
)

/** 확인 필요 처리: 항목을 탭해서 분류를 고른다. 위젯에서 특정 항목을 탭하면 openTxId로 바로 연다. */
@Composable
fun ReviewScreen(modifier: Modifier, openTxId: Long?, onOpened: () -> Unit) {
    val ctx = LocalContext.current
    val app = MyHomeApp.instance
    val scope = rememberCoroutineScope()
    var items by remember { mutableStateOf<List<TransactionDto>>(emptyList()) }
    var cats by remember { mutableStateOf<List<Category>>(emptyList()) }
    var error by remember { mutableStateOf<String?>(null) }
    var loading by remember { mutableStateOf(true) }
    var selected by remember { mutableStateOf<TransactionDto?>(null) }

    suspend fun load() {
        loading = true
        val (q, c) = withContext(Dispatchers.IO) { app.api.reviewQueue() to app.api.categories() }
        if (q is ApiResult.Ok) items = q.value.transactions else error = q.describe()
        if (c is ApiResult.Ok) cats = c.value.categories.filter { !it.archived }
        loading = false
    }

    LaunchedEffect(openTxId) {
        load()
        if (openTxId != null) {
            selected = items.firstOrNull { it.id == openTxId }
            onOpened()
        }
    }

    LazyColumn(modifier.fillMaxSize()) {
        item {
            Text(
                when {
                    loading -> "불러오는 중…"
                    error != null -> "불러오지 못했어요: $error"
                    items.isEmpty() -> "확인할 거래가 없어요. 🎉"
                    else -> "${items.size}건. 탭해서 분류를 고르세요."
                },
                modifier = Modifier.padding(16.dp),
            )
        }
        items(items, key = { it.id }) { t ->
            val m = LocalModo.current
            Column(
                Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 4.dp).clip(RoundedCornerShape(16.dp))
                    .background(m.card).clickable { selected = t }.padding(horizontal = 18.dp, vertical = 14.dp),
            ) {
                Row(verticalAlignment = Alignment.CenterVertically) {
                    Text(t.merchant.ifBlank { "(이름 없음)" }, Modifier.weight(1f), style = MaterialTheme.typography.bodyLarge)
                    Text((if (t.flow == "in") "+" else "") + Money.won(t.amount), style = MaterialTheme.typography.bodyLarge)
                }
                Text(
                    WidgetModel.shortTime(t.occurredAt) + " · " + (reviewReasonLabels[t.reviewReason] ?: t.reviewReason.orEmpty()) +
                        (if (t.category.isNotBlank()) " · 제안: ${t.category}" else ""),
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
        }
    }

    selected?.let { t ->
        ClassifyDialog(
            t = t,
            cats = cats,
            onDismiss = { selected = null },
            onSubmit = { edit ->
                scope.launch {
                    val r = withContext(Dispatchers.IO) { app.api.editTransaction(t.id, edit) }
                    if (r is ApiResult.Ok) {
                        val extra = if (r.value.alsoUpdated > 0) " 같은 가맹점 ${r.value.alsoUpdated}건도 함께 분류했어요." else ""
                        Toast.makeText(ctx, "저장했어요.$extra", Toast.LENGTH_SHORT).show()
                        selected = null
                        load()
                        WidgetRefresher.refresh(ctx)
                    } else {
                        Toast.makeText(ctx, "실패: ${r.describe()}", Toast.LENGTH_LONG).show()
                    }
                }
            },
        )
    }
}

@Composable
private fun ClassifyDialog(t: TransactionDto, cats: List<Category>, onDismiss: () -> Unit, onSubmit: (TransactionEdit) -> Unit) {
    var remember by remember { mutableStateOf(t.merchant.isNotBlank()) }
    // 들어온 돈이면 수입·정산·내 계좌 같은 특수 분류를 먼저 보여준다
    val ordered = if (t.flow == "in") cats.sortedBy { if (it.kind == "expense") 1 else 0 } else cats.sortedBy { if (it.kind == "expense") 0 else 1 }
    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text("${t.merchant.ifBlank { "(이름 없음)" }} · ${Money.won(t.amount)}") },
        text = {
            Column(Modifier.heightIn(max = 420.dp).verticalScroll(rememberScrollState()), verticalArrangement = Arrangement.spacedBy(4.dp)) {
                Row(verticalAlignment = Alignment.CenterVertically, modifier = Modifier.clickable(enabled = t.merchant.isNotBlank()) { remember = !remember }) {
                    Checkbox(checked = remember, onCheckedChange = { remember = it }, enabled = t.merchant.isNotBlank())
                    Text("이 가맹점은 항상 이 분류")
                }
                for (c in ordered) {
                    FilledTonalButton(onClick = { onSubmit(TransactionEdit(categoryId = c.id, rememberRule = remember)) }, modifier = Modifier.fillMaxWidth()) {
                        Text(c.name)
                    }
                }
            }
        },
        confirmButton = { TextButton(onClick = { onSubmit(TransactionEdit(status = "excluded")) }) { Text("제외") } },
        dismissButton = { TextButton(onClick = onDismiss) { Text("닫기") } },
    )
}
