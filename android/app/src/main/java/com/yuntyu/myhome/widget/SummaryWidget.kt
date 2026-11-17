package com.yuntyu.myhome.widget

import android.content.Context
import android.content.Intent
import android.net.Uri
import android.os.SystemClock
import android.widget.RemoteViews
import android.widget.Toast
import androidx.compose.runtime.Composable
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.remember
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.unit.DpSize
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.datastore.preferences.core.intPreferencesKey
import androidx.glance.GlanceId
import androidx.glance.GlanceModifier
import androidx.glance.GlanceTheme
import androidx.glance.Image
import androidx.glance.ImageProvider
import androidx.glance.LocalSize
import androidx.glance.action.ActionParameters
import androidx.glance.action.actionParametersOf
import androidx.glance.action.clickable
import androidx.glance.appwidget.AndroidRemoteViews
import androidx.glance.appwidget.GlanceAppWidget
import androidx.glance.appwidget.GlanceAppWidgetReceiver
import androidx.glance.appwidget.SizeMode
import androidx.glance.appwidget.action.ActionCallback
import androidx.glance.appwidget.action.actionRunCallback
import androidx.glance.appwidget.action.actionStartActivity
import androidx.glance.appwidget.cornerRadius
import androidx.glance.appwidget.provideContent
import androidx.glance.appwidget.state.updateAppWidgetState
import androidx.glance.appwidget.updateAll
import androidx.glance.background
import androidx.glance.color.ColorProvider
import androidx.glance.currentState
import androidx.glance.layout.Alignment
import androidx.glance.layout.Box
import androidx.glance.layout.Column
import androidx.glance.layout.Row
import androidx.glance.layout.Spacer
import androidx.glance.layout.fillMaxSize
import androidx.glance.layout.fillMaxWidth
import androidx.glance.layout.height
import androidx.glance.layout.padding
import androidx.glance.layout.size
import androidx.glance.layout.width
import androidx.glance.text.FontWeight
import androidx.glance.text.Text
import androidx.glance.text.TextStyle
import com.yuntyu.myhome.MyHomeApp
import com.yuntyu.myhome.R
import com.yuntyu.myhome.core.BudgetState
import com.yuntyu.myhome.core.FocusDay
import com.yuntyu.myhome.core.FocusSpan
import com.yuntyu.myhome.core.FocusStats
import com.yuntyu.myhome.core.FocusSubjects
import com.yuntyu.myhome.core.Money
import com.yuntyu.myhome.core.WidgetLayout
import com.yuntyu.myhome.core.WidgetModel
import com.yuntyu.myhome.core.api.ApiResult
import com.yuntyu.myhome.core.api.Pick
import com.yuntyu.myhome.core.api.Summary
import com.yuntyu.myhome.core.api.TransactionEdit
import com.yuntyu.myhome.core.formatHms
import com.yuntyu.myhome.data.FocusSession
import com.yuntyu.myhome.data.FocusSubject
import com.yuntyu.myhome.focus.FocusTimer
import com.yuntyu.myhome.ui.MainActivity
import com.yuntyu.myhome.ui.plain
import com.yuntyu.myhome.work.WorkScheduler
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext

/**
 * 홈 화면 위젯. 위의 탭으로 두 페이지를 오간다(위젯은 옆으로 미는 손짓을 받지 못해서 탭으로 바꾼다. 위젯마다 기억한다).
 * - 가계부: 총 남은 돈, 고를 거래(동그라미 한 번으로 분류, 다음 것으로 넘어감), 분류별 남은 돈(사용률 높은 순), 최근 결제.
 *   서버 캐시(SummaryRepository)만 읽는다
 * - 집중: 오늘 순공, 과목 줄(동그라미를 누르면 앱을 열지 않고 시작·멈춤). 폰 기록(Room)만 읽는다
 * 그릴 때 네트워크는 쓰지 않는다. 도는 타이머의 초는 시스템 Chronometer가 그려서 앱을 깨우지 않는다.
 */
class SummaryWidget : GlanceAppWidget() {
    override val sizeMode = SizeMode.Responsive(setOf(SMALL, MEDIUM, LARGE))

    override suspend fun provideGlance(context: Context, id: GlanceId) {
        val app = context.applicationContext as MyHomeApp
        val repo = app.summaryRepo
        val focus = app.db.focus()
        provideContent {
            // updateAll은 실행 중인 provideGlance를 다시 시작하지 않으므로, 캐시·기록을 구독해서 바로 다시 그린다.
            val st by repo.state.collectAsState()
            val page = currentState(PAGE) ?: PAGE_BUDGET
            val running by FocusTimer.running(context).collectAsState()
            val day = FocusDay.dayOf(System.currentTimeMillis())
            val subjects by remember { focus.subjects() }.collectAsState(initial = null)
            val sessions by remember(day) { focus.sessionsBetween(FocusDay.startOf(day), FocusDay.endOf(day)) }.collectAsState(initial = emptyList())
            SummaryWidgetContent(context, page, st.summary, subjects, sessions, running, st.picking)
        }
    }

    companion object {
        val SMALL = DpSize(120.dp, 100.dp)
        val MEDIUM = DpSize(250.dp, 180.dp)
        val LARGE = DpSize(250.dp, 300.dp)
        val PAGE = intPreferencesKey("page")
        const val PAGE_BUDGET = 0
        const val PAGE_FOCUS = 1
    }
}

/** 위젯 한 장(가계부 · 집중 페이지). 화면 찍기 테스트도 이것을 그린다(ShotsTest) */
@Composable
internal fun SummaryWidgetContent(
    context: Context,
    page: Int,
    summary: Summary?,
    subjects: List<com.yuntyu.myhome.data.FocusSubject>?,
    sessions: List<com.yuntyu.myhome.data.FocusSession>,
    running: com.yuntyu.myhome.focus.FocusTimer.Running?,
    picking: Pair<Long, Long>? = null,
) {
    GlanceTheme {
        WidgetFrame(context, page) {
            if (page == SummaryWidget.PAGE_FOCUS) FocusPage(context, subjects, sessions, running)
            else BudgetPage(context, summary, picking)
        }
    }
}

class SummaryWidgetReceiver : GlanceAppWidgetReceiver() {
    override val glanceAppWidget: GlanceAppWidget = SummaryWidget()
}

private val PageParam = ActionParameters.Key<Int>("page")
private val SubjectParam = ActionParameters.Key<String>("subject")
private val PickTxParam = ActionParameters.Key<Long>("pick_tx")
private val PickCatParam = ActionParameters.Key<Long>("pick_cat")

/**
 * 고르기 동그라미(사용자 요청, 2026-11-16 "애매한 건 위젯에서 동그라미 한 번 체크하면 다음 걸로"): 앱을 열지 않고 그 분류로 확정한다.
 * 누른 동그라미를 바로 채워 보이고, 서버에 적은 뒤 요약을 다시 받으면 다음 거래가 온다. 규칙은 만들지 않는다(살 때마다 다른 가게라서).
 */
class PickCategoryAction : ActionCallback {
    override suspend fun onAction(context: Context, glanceId: GlanceId, parameters: ActionParameters) {
        val tx = parameters[PickTxParam] ?: return
        val cat = parameters[PickCatParam] ?: return
        val app = context.applicationContext as MyHomeApp
        if (app.summaryRepo.state.value.picking != null) return // 앞에 누른 것을 적는 중
        app.summaryRepo.setPicking(tx to cat)
        SummaryWidget().updateAll(context)
        val r = withContext(Dispatchers.IO) { app.api.editTransaction(tx, TransactionEdit(categoryId = cat)) }
        if (r !is ApiResult.Ok) {
            app.summaryRepo.setPicking(null)
            withContext(Dispatchers.Main) { Toast.makeText(context, "못 골랐어요. ${r.plain()}", Toast.LENGTH_LONG).show() }
        }
        if (!WidgetRefresher.refresh(context)) {
            app.summaryRepo.setPicking(null)
            SummaryWidget().updateAll(context)
        }
        // 고른 거래의 원래 결제 알림(분류 몰라 남겨 둔 것)을 치운다
        if (r is ApiResult.Ok) WorkScheduler.checkNotices(context)
    }
}

/** 위의 탭: 그 위젯의 페이지를 바꾼다 */
class SetWidgetPageAction : ActionCallback {
    override suspend fun onAction(context: Context, glanceId: GlanceId, parameters: ActionParameters) {
        val page = parameters[PageParam] ?: return
        updateAppWidgetState(context, glanceId) { it[SummaryWidget.PAGE] = page }
        SummaryWidget().update(context, glanceId)
    }
}

/** 집중 페이지의 과목 동그라미: 앱을 열지 않고 시작·멈춤(다른 과목이 돌고 있으면 넘어간다) */
class ToggleFocusAction : ActionCallback {
    override suspend fun onAction(context: Context, glanceId: GlanceId, parameters: ActionParameters) {
        val id = parameters[SubjectParam] ?: return
        FocusTimer.toggle(context.applicationContext, id)
        SummaryWidget().updateAll(context)
    }
}

// 앱과 같은 색: 80% 이상·확인 필요 라벤더, 초과 살구, 카드 바탕, 본문·보조 글자. 평소 금액은 본문색(읽기 쉽게, 2026-11-17)
private val WarnColor = ColorProvider(day = Color(0xFF6F6698), night = Color(0xFFAFA5D4))
private val OverColor = ColorProvider(day = Color(0xFFA15A36), night = Color(0xFFD89777))
private val WidgetBg = ColorProvider(day = Color(0xFFFFFFFF), night = Color(0xFF222222))
private val TextColor = ColorProvider(day = Color(0xFF171717), night = Color(0xFFFFFFFF))
// 보조 글자(라벨·안 고른 탭·동그라미 이름): 카드·고르기 칸 바탕 위 대비 4.5:1 이상
private val SubColor = ColorProvider(day = Color(0xFF666666), night = Color(0xFFA0A0A0))
// 고르기 칸 바탕: 앱 Modo의 composer(누르고 쓰는 칸 바탕)
private val PickBg = ColorProvider(day = Color(0xFFEEEEED), night = Color(0xFF2A2A2A))

private fun colorFor(s: BudgetState) = when (s) {
    BudgetState.OVER -> OverColor
    BudgetState.WARN -> WarnColor
    else -> TextColor
}

/** 화면 열기 인텐트. 위젯 항목마다 PendingIntent가 섞이지 않게 data URI를 다르게 준다. */
private fun openIntent(context: Context, screen: String, txId: Long? = null): Intent =
    Intent(context, MainActivity::class.java)
        .setData(Uri.parse("myhome://open/$screen/${txId ?: 0}"))
        .putExtra(MainActivity.EXTRA_SCREEN, screen)
        .apply { if (txId != null) putExtra(MainActivity.EXTRA_TX_ID, txId) }
        .addFlags(Intent.FLAG_ACTIVITY_NEW_TASK or Intent.FLAG_ACTIVITY_CLEAR_TOP)

/** 바탕 카드와 위의 탭(가계부 · 집중). 카드를 누르면 그 페이지의 앱 화면이 열린다 */
@Composable
private fun WidgetFrame(context: Context, page: Int, content: @Composable () -> Unit) {
    Column(
        modifier = GlanceModifier.fillMaxSize().cornerRadius(22.dp)
            .background(WidgetBg).padding(horizontal = 14.dp, vertical = 12.dp)
            .clickable(actionStartActivity(openIntent(context, if (page == SummaryWidget.PAGE_FOCUS) "focus" else "home"))),
    ) {
        Row(modifier = GlanceModifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
            PageTab("가계부", page == SummaryWidget.PAGE_BUDGET, SummaryWidget.PAGE_BUDGET)
            Spacer(GlanceModifier.width(10.dp))
            PageTab("집중", page == SummaryWidget.PAGE_FOCUS, SummaryWidget.PAGE_FOCUS)
        }
        Spacer(GlanceModifier.height(6.dp))
        content()
    }
}

@Composable
private fun PageTab(label: String, selected: Boolean, page: Int) {
    Text(
        label,
        style = TextStyle(color = if (selected) TextColor else SubColor, fontSize = 12.sp, fontWeight = if (selected) FontWeight.Bold else FontWeight.Normal),
        modifier = GlanceModifier.padding(vertical = 2.dp)
            .clickable(actionRunCallback<SetWidgetPageAction>(actionParametersOf(PageParam to page))),
    )
}

@Composable
private fun BudgetPage(context: Context, summary: Summary?, picking: Pair<Long, Long>?) {
    val size = LocalSize.current
    val layout = WidgetLayout.forSize(size.width.value, size.height.value)
    val text = TextColor
    val sub = SubColor

    if (summary == null) {
        Text("앱을 열어 로그인하면 남은 돈이 보여요", style = TextStyle(color = sub, fontSize = 13.sp))
        return
    }
    Row(modifier = GlanceModifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
        Column(modifier = GlanceModifier.defaultWeight()) {
            Text("${WidgetModel.monthName(summary.period.label)} 남은 돈", style = TextStyle(color = sub, fontSize = 12.sp))
            // 작은 위젯(120dp)은 "1,933,050원"이 두 줄로 깨져서 "193만원"처럼 줄여 쓴다
            Text(
                if (layout == WidgetLayout.SMALL) Money.compact(summary.totals.remaining) + "원" else Money.won(summary.totals.remaining),
                style = TextStyle(
                    color = if (summary.totals.remaining < 0) OverColor else text,
                    fontSize = if (layout == WidgetLayout.SMALL) 18.sp else 22.sp,
                    fontWeight = FontWeight.Bold,
                ),
                maxLines = 1,
            )
        }
        if (summary.needsReview > 0 && layout != WidgetLayout.SMALL) {
            Text(
                "확인 ${summary.needsReview}",
                style = TextStyle(color = WarnColor, fontSize = 13.sp, fontWeight = FontWeight.Bold),
                modifier = GlanceModifier.clickable(actionStartActivity(openIntent(context, "review"))),
            )
        }
        if (summary.failedEvents > 0) {
            // 서버가 읽지 못한 알림(거래로 안 잡혔을 수 있음)
            Text(
                "실패 ${summary.failedEvents}",
                style = TextStyle(color = OverColor, fontSize = 13.sp, fontWeight = FontWeight.Bold),
                modifier = GlanceModifier.padding(start = 6.dp).clickable(actionStartActivity(openIntent(context, "unread"))),
            )
        }
    }
    Spacer(GlanceModifier.height(6.dp))
    val pick = summary.pick?.takeIf { it.choices.isNotEmpty() }
    val (categoryRows, recentRows) = WidgetModel.rows(layout, pick != null)
    if (pick != null && WidgetModel.showPick(layout, true)) {
        PickCard(pick, picking)
        Spacer(GlanceModifier.height(6.dp))
    }
    // Glance 컨테이너는 직접 자식을 10개까지만 그리므로 분류·최근 결제를 각각 Column으로 묶는다.
    Column(modifier = GlanceModifier.fillMaxWidth()) {
        for (c in WidgetModel.topCategories(summary, categoryRows)) {
            Row(modifier = GlanceModifier.fillMaxWidth().padding(vertical = 1.dp)) {
                Text(c.name, style = TextStyle(color = text, fontSize = 15.sp), modifier = GlanceModifier.defaultWeight(), maxLines = 1)
                Text(
                    if (layout == WidgetLayout.SMALL) Money.compact(c.remaining) else Money.won(c.remaining),
                    style = TextStyle(color = colorFor(WidgetModel.stateOf(c)), fontSize = 15.sp, fontWeight = FontWeight.Medium),
                )
            }
        }
    }
    val recent = if (recentRows > 0) WidgetModel.recentPayments(summary, recentRows) else emptyList()
    if (recent.isNotEmpty()) {
        Spacer(GlanceModifier.height(6.dp))
        Column(modifier = GlanceModifier.fillMaxWidth()) {
            Text("최근 결제", style = TextStyle(color = sub, fontSize = 12.sp))
            for (t in recent) {
                val review = t.status == "needs_review"
                Row(
                    modifier = GlanceModifier.fillMaxWidth().padding(vertical = 1.dp).clickable(
                        actionStartActivity(if (review) openIntent(context, "review", t.id) else openIntent(context, "transactions")),
                    ),
                ) {
                    Text(
                        (if (review) "⚠ " else "") + t.merchant.ifBlank { "(이름 없음)" },
                        style = TextStyle(color = if (review) WarnColor else text, fontSize = 14.sp),
                        modifier = GlanceModifier.defaultWeight(),
                        maxLines = 1,
                    )
                    Text((if (t.flow == "in") "+" else "") + Money.won(t.amount), style = TextStyle(color = text, fontSize = 14.sp))
                }
            }
        }
    }
}

/**
 * 고를 거래 한 칸: 가게 · 금액(남은 수), 그 아래 동그라미(분류) 줄. 앱 Modo의 CheckCircle과 같은 동그라미다
 * (안 고름: 테두리, 고름: 채운 동그라미에 체크). 동그라미를 누르면 [PickCategoryAction].
 */
@Composable
private fun PickCard(p: Pick, picking: Pair<Long, Long>?) {
    Column(modifier = GlanceModifier.fillMaxWidth().cornerRadius(14.dp).background(PickBg).padding(horizontal = 10.dp, vertical = 6.dp)) {
        Row(modifier = GlanceModifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
            Text(
                p.merchant.ifBlank { "(이름 없음)" },
                style = TextStyle(color = TextColor, fontSize = 14.sp, fontWeight = FontWeight.Bold),
                modifier = GlanceModifier.defaultWeight(),
                maxLines = 1,
            )
            Text(Money.won(p.amount), style = TextStyle(color = TextColor, fontSize = 14.sp, fontWeight = FontWeight.Medium))
            Text("  ${p.left}건 남음", style = TextStyle(color = SubColor, fontSize = 12.sp))
        }
        Spacer(GlanceModifier.height(4.dp))
        Row(modifier = GlanceModifier.fillMaxWidth()) {
            for (c in p.choices.take(4)) {
                val on = picking?.first == p.id && picking.second == c.id
                Column(
                    modifier = GlanceModifier.defaultWeight()
                        .clickable(actionRunCallback<PickCategoryAction>(actionParametersOf(PickTxParam to p.id, PickCatParam to c.id))),
                    horizontalAlignment = Alignment.CenterHorizontally,
                ) {
                    Image(
                        provider = ImageProvider(if (on) R.drawable.widget_circle_on else R.drawable.widget_circle_off),
                        contentDescription = "${c.name}로 고르기",
                        modifier = GlanceModifier.size(20.dp),
                    )
                    Spacer(GlanceModifier.height(2.dp))
                    Text(
                        c.name,
                        style = TextStyle(color = if (on) TextColor else SubColor, fontSize = 12.sp, fontWeight = if (on) FontWeight.Bold else FontWeight.Normal),
                        maxLines = 1,
                    )
                }
            }
        }
    }
}

/** 크기별 과목 줄 수: 작게는 합계만(누르면 앱), 중간 3줄, 크게 6줄 */
private fun focusRows(layout: WidgetLayout) = when (layout) {
    WidgetLayout.SMALL -> 0
    WidgetLayout.MEDIUM -> 2 // 180dp 높이에 셋째 줄이 반쯤 잘렸다
    WidgetLayout.LARGE -> 6
}

/** 시스템이 초를 그리는 시계. shownMs는 지금 보여야 할 값이고 거기서부터 간다 */
@Composable
private fun LiveClock(context: Context, layoutId: Int, shownMs: Long) {
    val rv = RemoteViews(context.packageName, layoutId)
    rv.setChronometer(R.id.chrono, SystemClock.elapsedRealtime() - shownMs, null, true)
    AndroidRemoteViews(rv)
}

@Composable
private fun FocusPage(context: Context, subjects: List<FocusSubject>?, sessions: List<FocusSession>, running: FocusTimer.Running?) {
    val size = LocalSize.current
    val layout = WidgetLayout.forSize(size.width.value, size.height.value)
    val now = System.currentTimeMillis()
    val day = FocusDay.dayOf(now)
    val spans = sessions.map { FocusSpan(it.subjectId, it.startedAt, it.endedAt) } +
        listOfNotNull(running?.let { FocusSpan(it.subjectId, it.startedAt, now) })
    val total = FocusStats.focusTotal(spans, day)
    val bySubject = FocusStats.bySubject(spans, day)
    val list = subjects.orEmpty()
    val runningName = running?.let { r -> list.firstOrNull { it.id == r.subjectId }?.name }

    Text(if (runningName != null) "$runningName 하는 중" else "오늘 순공", style = TextStyle(color = SubColor, fontSize = 12.sp), maxLines = 1)
    if (running != null) {
        LiveClock(context, R.layout.widget_chrono_big, total)
    } else {
        Text(formatHms(total), style = TextStyle(color = TextColor, fontSize = if (layout == WidgetLayout.SMALL) 20.sp else 22.sp, fontWeight = FontWeight.Bold))
    }
    val n = focusRows(layout)
    if (n == 0) return
    Spacer(GlanceModifier.height(6.dp))
    if (subjects != null && list.isEmpty()) {
        Text("앱의 집중 칸에서 과목을 추가해 주세요", style = TextStyle(color = SubColor, fontSize = 13.sp))
        return
    }
    val byId = list.associateBy { it.id }
    // Glance 컨테이너는 직접 자식을 10개까지만 그려서 줄을 Column으로 묶는다
    Column(modifier = GlanceModifier.fillMaxWidth()) {
        for (id in FocusSubjects.widgetRows(list.map { it.id }, running?.subjectId, n)) {
            val s = byId[id] ?: continue
            val isRunning = running?.subjectId == id
            val ms = bySubject[id] ?: 0L
            val color = runCatching { Color(android.graphics.Color.parseColor(s.color)) }.getOrDefault(Color.Gray)
            Row(
                modifier = GlanceModifier.fillMaxWidth().padding(vertical = 3.dp)
                    .clickable(actionRunCallback<ToggleFocusAction>(actionParametersOf(SubjectParam to id))),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                Box(
                    modifier = GlanceModifier.size(24.dp).cornerRadius(12.dp).background(ColorProvider(day = color, night = color)),
                    contentAlignment = Alignment.Center,
                ) {
                    Image(
                        provider = ImageProvider(if (isRunning) R.drawable.ic_widget_pause else R.drawable.ic_widget_play),
                        contentDescription = if (isRunning) "${s.name} 멈추기" else "${s.name} 시작",
                        modifier = GlanceModifier.size(14.dp),
                    )
                }
                Spacer(GlanceModifier.width(10.dp))
                Text(
                    s.name,
                    style = TextStyle(color = TextColor, fontSize = 15.sp, fontWeight = if (isRunning) FontWeight.Bold else FontWeight.Normal),
                    modifier = GlanceModifier.defaultWeight(),
                    maxLines = 1,
                )
                if (isRunning) {
                    LiveClock(context, R.layout.widget_chrono_row, ms)
                } else {
                    Text(formatHms(ms), style = TextStyle(color = if (ms > 0) TextColor else SubColor, fontSize = 15.sp))
                }
            }
        }
    }
}
