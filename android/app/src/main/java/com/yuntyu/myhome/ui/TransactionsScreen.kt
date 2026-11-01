package com.yuntyu.myhome.ui

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.ui.draw.clip
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp
import com.yuntyu.myhome.MyHomeApp
import com.yuntyu.myhome.core.Money
import com.yuntyu.myhome.core.WidgetModel
import com.yuntyu.myhome.core.api.ApiResult
import com.yuntyu.myhome.core.api.TransactionDto
import com.yuntyu.myhome.core.api.describe
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext

/** 이번 달 최근 거래(최대 100건). 자세한 수정은 PC 웹에서. */
@Composable
fun TransactionsScreen(modifier: Modifier) {
    var items by remember { mutableStateOf<List<TransactionDto>?>(null) }
    var error by remember { mutableStateOf<String?>(null) }
    LaunchedEffect(Unit) {
        when (val r = withContext(Dispatchers.IO) { MyHomeApp.instance.api.transactions(100) }) {
            is ApiResult.Ok -> items = r.value.transactions
            else -> error = r.describe()
        }
    }
    LazyColumn(modifier.fillMaxSize()) {
        if (items == null) item { Text(error?.let { "불러오지 못했어요: $it" } ?: "불러오는 중…", Modifier.padding(16.dp)) }
        items(items.orEmpty(), key = { it.id }) { t ->
            val excluded = t.status == "excluded"
            val m = LocalModo.current
            Row(
                Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 4.dp).clip(RoundedCornerShape(16.dp))
                    .background(m.card).padding(horizontal = 18.dp, vertical = 14.dp),
            ) {
                Column(Modifier.weight(1f)) {
                    Text(t.merchant.ifBlank { "(이름 없음)" }, maxLines = 1, color = if (excluded) MaterialTheme.colorScheme.onSurfaceVariant else MaterialTheme.colorScheme.onSurface)
                    Text(
                        WidgetModel.shortTime(t.occurredAt) + " · " + when (t.status) {
                            "needs_review" -> "확인 필요"
                            "excluded" -> "제외"
                            else -> t.category
                        },
                        style = MaterialTheme.typography.bodySmall,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                }
                Text(
                    (if (t.flow == "in") "+" else "") + Money.won(t.amount),
                    color = when {
                        excluded -> MaterialTheme.colorScheme.onSurfaceVariant
                        t.flow == "in" -> MaterialTheme.colorScheme.primary
                        else -> MaterialTheme.colorScheme.onSurface
                    },
                )
            }
        }
    }
}
