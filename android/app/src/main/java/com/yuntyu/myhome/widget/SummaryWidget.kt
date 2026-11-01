package com.yuntyu.myhome.widget

import android.content.Context
import android.content.Intent
import android.net.Uri
import androidx.compose.runtime.Composable
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.unit.DpSize
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.glance.GlanceId
import androidx.glance.GlanceModifier
import androidx.glance.GlanceTheme
import androidx.glance.LocalSize
import androidx.glance.action.clickable
import androidx.glance.appwidget.GlanceAppWidget
import androidx.glance.appwidget.GlanceAppWidgetReceiver
import androidx.glance.appwidget.SizeMode
import androidx.glance.appwidget.action.actionStartActivity
import androidx.glance.appwidget.cornerRadius
import androidx.glance.appwidget.provideContent
import androidx.glance.background
import androidx.glance.color.ColorProvider
import androidx.glance.layout.Alignment
import androidx.glance.layout.Column
import androidx.glance.layout.Row
import androidx.glance.layout.Spacer
import androidx.glance.layout.fillMaxSize
import androidx.glance.layout.fillMaxWidth
import androidx.glance.layout.height
import androidx.glance.layout.padding
import androidx.glance.text.FontWeight
import androidx.glance.text.Text
import androidx.glance.text.TextStyle
import com.yuntyu.myhome.MyHomeApp
import com.yuntyu.myhome.core.BudgetState
import com.yuntyu.myhome.core.Money
import com.yuntyu.myhome.core.WidgetLayout
import com.yuntyu.myhome.core.WidgetModel
import com.yuntyu.myhome.core.api.Summary
import com.yuntyu.myhome.ui.MainActivity

/**
 * 홈 화면 위젯: 총 남은 돈, 분류별 남은 돈(사용률 높은 순), 최근 결제.
 * 그릴 때는 네트워크를 쓰지 않고 SummaryRepository의 캐시만 읽는다. 갱신은 WidgetRefresher가 한다.
 */
class SummaryWidget : GlanceAppWidget() {
    override val sizeMode = SizeMode.Responsive(setOf(SMALL, MEDIUM, LARGE))

    override suspend fun provideGlance(context: Context, id: GlanceId) {
        val repo = (context.applicationContext as MyHomeApp).summaryRepo
        provideContent {
            // updateAll은 실행 중인 provideGlance를 다시 시작하지 않으므로, 캐시 상태를 구독해서 바로 다시 그린다.
            val st by repo.state.collectAsState()
            GlanceTheme { WidgetContent(context, st.summary) }
        }
    }

    companion object {
        val SMALL = DpSize(120.dp, 100.dp)
        val MEDIUM = DpSize(250.dp, 180.dp)
        val LARGE = DpSize(250.dp, 300.dp)
    }
}

class SummaryWidgetReceiver : GlanceAppWidgetReceiver() {
    override val glanceAppWidget: GlanceAppWidget = SummaryWidget()
}

private val OkColor = ColorProvider(day = Color(0xFF2E7D5B), night = Color(0xFF6CC79C))
private val WarnColor = ColorProvider(day = Color(0xFFB07800), night = Color(0xFFF0B93A))
private val OverColor = ColorProvider(day = Color(0xFFC62F2F), night = Color(0xFFFF7A70))

private fun colorFor(s: BudgetState) = when (s) {
    BudgetState.OVER -> OverColor
    BudgetState.WARN -> WarnColor
    else -> OkColor
}

/** 화면 열기 인텐트. 위젯 항목마다 PendingIntent가 섞이지 않게 data URI를 다르게 준다. */
private fun openIntent(context: Context, screen: String, txId: Long? = null): Intent =
    Intent(context, MainActivity::class.java)
        .setData(Uri.parse("myhome://open/$screen/${txId ?: 0}"))
        .putExtra(MainActivity.EXTRA_SCREEN, screen)
        .apply { if (txId != null) putExtra(MainActivity.EXTRA_TX_ID, txId) }
        .addFlags(Intent.FLAG_ACTIVITY_NEW_TASK or Intent.FLAG_ACTIVITY_CLEAR_TOP)

@Composable
private fun WidgetContent(context: Context, summary: Summary?) {
    val size = LocalSize.current
    val layout = WidgetLayout.forSize(size.width.value, size.height.value)
    val text = GlanceTheme.colors.onSurface
    val sub = GlanceTheme.colors.onSurfaceVariant

    Column(
        modifier = GlanceModifier.fillMaxSize().cornerRadius(16.dp)
            .background(GlanceTheme.colors.widgetBackground).padding(12.dp)
            .clickable(actionStartActivity(openIntent(context, "home"))),
    ) {
        if (summary == null) {
            Text("가계부", style = TextStyle(color = text, fontWeight = FontWeight.Bold))
            Text("앱을 열어 로그인하면 남은 돈이 보여요", style = TextStyle(color = sub, fontSize = 12.sp))
            return@Column
        }
        Row(modifier = GlanceModifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
            Column(modifier = GlanceModifier.defaultWeight()) {
                Text("${summary.period.label} 남은 돈", style = TextStyle(color = sub, fontSize = 11.sp))
                Text(
                    Money.won(summary.totals.remaining),
                    style = TextStyle(
                        color = if (summary.totals.remaining < 0) OverColor else text,
                        fontSize = if (layout == WidgetLayout.SMALL) 18.sp else 22.sp,
                        fontWeight = FontWeight.Bold,
                    ),
                )
            }
            if (summary.needsReview > 0) {
                Text(
                    "확인 ${summary.needsReview}",
                    style = TextStyle(color = WarnColor, fontSize = 12.sp, fontWeight = FontWeight.Bold),
                    modifier = GlanceModifier.clickable(actionStartActivity(openIntent(context, "review"))),
                )
            }
        }
        Spacer(GlanceModifier.height(6.dp))
        // Glance 컨테이너는 직접 자식을 10개까지만 그리므로 분류·최근 결제를 각각 Column으로 묶는다.
        Column(modifier = GlanceModifier.fillMaxWidth()) {
            for (c in WidgetModel.topCategories(summary, layout.categoryCount)) {
                Row(modifier = GlanceModifier.fillMaxWidth().padding(vertical = 1.dp)) {
                    Text(c.name, style = TextStyle(color = text, fontSize = 13.sp), modifier = GlanceModifier.defaultWeight(), maxLines = 1)
                    Text(
                        if (layout == WidgetLayout.SMALL) Money.compact(c.remaining) else Money.won(c.remaining),
                        style = TextStyle(color = colorFor(WidgetModel.stateOf(c)), fontSize = 13.sp, fontWeight = FontWeight.Medium),
                    )
                }
            }
        }
        val recent = if (layout.recentCount > 0) WidgetModel.recentPayments(summary, layout.recentCount) else emptyList()
        if (recent.isNotEmpty()) {
            Spacer(GlanceModifier.height(6.dp))
            Column(modifier = GlanceModifier.fillMaxWidth()) {
                Text("최근 결제", style = TextStyle(color = sub, fontSize = 11.sp))
                for (t in recent) {
                    val review = t.status == "needs_review"
                    Row(
                        modifier = GlanceModifier.fillMaxWidth().padding(vertical = 1.dp).clickable(
                            actionStartActivity(if (review) openIntent(context, "review", t.id) else openIntent(context, "transactions")),
                        ),
                    ) {
                        Text(
                            (if (review) "⚠ " else "") + t.merchant.ifBlank { "(이름 없음)" },
                            style = TextStyle(color = if (review) WarnColor else text, fontSize = 12.sp),
                            modifier = GlanceModifier.defaultWeight(),
                            maxLines = 1,
                        )
                        Text((if (t.flow == "in") "+" else "") + Money.won(t.amount), style = TextStyle(color = sub, fontSize = 12.sp))
                    }
                }
            }
        }
    }
}
