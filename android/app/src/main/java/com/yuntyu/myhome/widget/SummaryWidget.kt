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
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.DpSize
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.datastore.preferences.core.intPreferencesKey
import androidx.glance.ColorFilter
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
import androidx.glance.unit.ColorProvider as GlanceColor
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
import androidx.glance.text.TextAlign
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
import com.yuntyu.myhome.core.HealthPlan
import com.yuntyu.myhome.core.api.HealthToday
import com.yuntyu.myhome.core.api.Pick
import com.yuntyu.myhome.core.api.Summary
import com.yuntyu.myhome.core.api.TransactionEdit
import com.yuntyu.myhome.core.formatHms
import com.yuntyu.myhome.data.FocusSession
import com.yuntyu.myhome.data.HealthWidgetCache
import com.yuntyu.myhome.data.FocusSubject
import com.yuntyu.myhome.focus.FocusTimer
import com.yuntyu.myhome.ui.MainActivity
import com.yuntyu.myhome.ui.plain
import com.yuntyu.myhome.work.WorkScheduler
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import java.time.LocalDate

/**
 * 홈 화면 위젯. 위의 탭으로 세 페이지를 오간다(위젯은 옆으로 미는 손짓을 받지 못해서 탭으로 바꾼다. 위젯마다 기억한다).
 * - 가계부: 남은 날과 하루 쓸 돈 / 남은 돈(누르면 쓴 돈 / 예산), 고를 거래(동그라미 한 번으로 분류, 다음 것으로 넘어감),
 *   분류별 남은 돈(사용률 높은 순), 최근 결제. 서버 캐시(SummaryRepository)만 읽는다
 * - 집중: 오늘 순공, 과목 줄(동그라미를 누르면 앱을 열지 않고 시작·멈춤). 폰 기록(Room)만 읽는다
 * - 건강: 오늘 칼로리·탄단지(먹은 양 / 목표)와 찍기 단추. 받아 둔 것(HealthWidgetCache)만 읽는다
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
            val head = currentState(HEAD) ?: HEAD_PER_DAY
            val health by app.healthWidget.today.collectAsState()
            val running by FocusTimer.running(context).collectAsState()
            val day = FocusDay.dayOf(System.currentTimeMillis())
            val subjects by remember { focus.subjects() }.collectAsState(initial = null)
            val sessions by remember(day) { focus.sessionsBetween(FocusDay.startOf(day), FocusDay.endOf(day)) }.collectAsState(initial = emptyList())
            val healthDay = HealthPlan.scheduleDay(app.healthLocal.schedule.meals, java.time.LocalDateTime.now())
            SummaryWidgetContent(
                context, page, st.summary, subjects, sessions, running, st.picking,
                head = head, health = HealthWidgetCache.forDay(health, healthDay), today = LocalDate.now(),
            )
        }
    }

    companion object {
        val SMALL = DpSize(120.dp, 100.dp)
        val MEDIUM = DpSize(250.dp, 180.dp)
        val LARGE = DpSize(250.dp, 300.dp)
        val PAGE = intPreferencesKey("page")
        const val PAGE_BUDGET = 0
        const val PAGE_FOCUS = 1
        const val PAGE_HEALTH = 2
        /** 가계부 머리 숫자: 하루 쓸 돈 / 남은 돈 ↔ 쓴 돈 / 예산(숫자를 누르면 바뀐다, 위젯마다 기억) */
        val HEAD = intPreferencesKey("head")
        const val HEAD_PER_DAY = 0
        const val HEAD_USED = 1
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
    head: Int = SummaryWidget.HEAD_PER_DAY,
    health: HealthToday? = null,
    today: LocalDate = LocalDate.now(),
) {
    GlanceTheme {
        val badges: (@Composable () -> Unit)? =
            if (page == SummaryWidget.PAGE_BUDGET && summary != null) ({ BudgetBadges(context, summary) }) else null
        WidgetFrame(context, page, badges) {
            when (page) {
                SummaryWidget.PAGE_FOCUS -> FocusPage(context, subjects, sessions, running)
                SummaryWidget.PAGE_HEALTH -> HealthPage(context, health)
                else -> BudgetPage(context, summary, picking, head, today)
            }
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

/** 가계부 머리 숫자를 누르면: 하루 쓸 돈 / 남은 돈 ↔ 쓴 돈 / 예산 */
class ToggleHeadAction : ActionCallback {
    override suspend fun onAction(context: Context, glanceId: GlanceId, parameters: ActionParameters) {
        updateAppWidgetState(context, glanceId) {
            it[SummaryWidget.HEAD] = if ((it[SummaryWidget.HEAD] ?: SummaryWidget.HEAD_PER_DAY) == SummaryWidget.HEAD_USED) SummaryWidget.HEAD_PER_DAY else SummaryWidget.HEAD_USED
        }
        SummaryWidget().update(context, glanceId)
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
private val TextColor = ColorProvider(day = Color(0xFF171717), night = Color(0xFFFFFFFF))
// 보조 글자(라벨·안 고른 탭·동그라미 이름): 카드·고르기 칸 바탕 위 대비 4.5:1 이상
private val SubColor = ColorProvider(day = Color(0xFF666666), night = Color(0xFFA0A0A0))
// 구분선(앱 line), 진한 단추 위 아이콘(앱 onCta). 바탕·탭·막대·단추 모양은 드로어블(res/drawable/widget_*)이 둥글게 그린다
private val LineColor = ColorProvider(day = Color(0xFFE4E4E2), night = Color(0xFF303030))
private val OnCtaColor = ColorProvider(day = Color(0xFFFFFFFF), night = Color(0xFF151515))

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

/**
 * 바탕 카드와 위의 탭(가계부 · 집중 · 건강), 탭 줄 오른쪽에 [trailing](가계부의 확인·실패 배지). 카드를 누르면 그 페이지의 앱 화면이 열린다.
 * 모양(2026-11-17 "위젯 디자인이 구리다"): 바탕은 둥근 카드(Android 12부터 런처 모서리), 고른 탭은 알약 바탕.
 */
@Composable
private fun WidgetFrame(context: Context, page: Int, trailing: (@Composable () -> Unit)?, content: @Composable () -> Unit) {
    val size = LocalSize.current
    // 작은 위젯(120dp)은 탭 셋이 한 줄에 들어가게 옆 여백을 줄이고 알약 없이 굵기만
    val small = WidgetLayout.forSize(size.width.value, size.height.value) == WidgetLayout.SMALL
    val screen = when (page) {
        SummaryWidget.PAGE_FOCUS -> "focus"
        SummaryWidget.PAGE_HEALTH -> "health"
        else -> "home"
    }
    Column(
        modifier = GlanceModifier.fillMaxSize().cornerRadius(R.dimen.widget_radius)
            .background(ImageProvider(R.drawable.widget_bg))
            .padding(horizontal = if (small) 11.dp else 14.dp, vertical = if (small) 8.dp else 10.dp)
            .clickable(actionStartActivity(openIntent(context, screen))),
    ) {
        Row(modifier = GlanceModifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
            val gap = if (small) 6.dp else 14.dp
            PageTab("가계부", page == SummaryWidget.PAGE_BUDGET, SummaryWidget.PAGE_BUDGET, small)
            Spacer(GlanceModifier.width(gap))
            PageTab("집중", page == SummaryWidget.PAGE_FOCUS, SummaryWidget.PAGE_FOCUS, small)
            Spacer(GlanceModifier.width(gap))
            PageTab("건강", page == SummaryWidget.PAGE_HEALTH, SummaryWidget.PAGE_HEALTH, small)
            if (trailing != null && !small) {
                Spacer(GlanceModifier.defaultWeight())
                trailing()
            }
        }
        Spacer(GlanceModifier.height(if (small) 4.dp else 6.dp))
        // Glance 컨테이너는 직접 자식을 10개까지만 그려서 페이지 내용을 한 Column으로 묶는다(넘으면 아래가 조용히 빠진다)
        Column(modifier = GlanceModifier.fillMaxWidth()) { content() }
    }
}

/** 위의 탭: 고른 것은 본문색 굵게, 나머지는 보조색. 바탕(알약)은 깔지 않는다(사용자 의견, 2026-11-17 "박스 안에 박스가 별로") */
@Composable
private fun PageTab(label: String, selected: Boolean, page: Int, small: Boolean) {
    Text(
        label,
        style = TextStyle(color = if (selected) TextColor else SubColor, fontSize = if (small) 12.sp else 13.sp, fontWeight = if (selected) FontWeight.Bold else FontWeight.Normal),
        modifier = GlanceModifier.padding(vertical = if (small) 1.dp else 2.dp)
            .clickable(actionRunCallback<SetWidgetPageAction>(actionParametersOf(PageParam to page))),
    )
}

/** 가계부 탭 줄 오른쪽 글자 배지(바탕 없이 색 글자): 확인 필요 N(라벤더), 서버가 읽지 못한 알림 N(살구). 누르면 그 목록 */
@Composable
private fun BudgetBadges(context: Context, summary: Summary) {
    if (summary.needsReview > 0) Badge("확인 ${summary.needsReview}", WarnColor, openIntent(context, "review"))
    if (summary.needsReview > 0 && summary.failedEvents > 0) Spacer(GlanceModifier.width(8.dp))
    // 서버가 읽지 못한 알림(거래로 안 잡혔을 수 있음)
    if (summary.failedEvents > 0) Badge("실패 ${summary.failedEvents}", OverColor, openIntent(context, "unread"))
}

@Composable
private fun Badge(text: String, color: GlanceColor, intent: Intent) {
    Text(
        text,
        style = TextStyle(color = color, fontSize = 12.sp, fontWeight = FontWeight.Bold),
        modifier = GlanceModifier.padding(vertical = 2.dp).clickable(actionStartActivity(intent)),
        maxLines = 1,
    )
}

/**
 * 둥근 진행 막대(앱 ModoBar처럼 5dp 트랙 위 채움, 색은 평소 무채색·80% 이상 라벤더·넘으면 살구). 놓인 자리 너비를 채운다.
 * Glance 막대(LinearProgressIndicator)는 Android 12 아래에서 색을 못 바꾸고 끝이 각져서 RemoteViews ProgressBar로 그린다.
 * 채움이 조금이라도 있으면 보이게 2% 이상.
 */
@Composable
private fun Bar(context: Context, ratio: Double, state: BudgetState, modifier: GlanceModifier = GlanceModifier.fillMaxWidth()) {
    val layout = when (state) {
        BudgetState.OVER -> R.layout.widget_bar_over
        BudgetState.WARN -> R.layout.widget_bar_warn
        else -> R.layout.widget_bar_ok
    }
    val rv = RemoteViews(context.packageName, layout)
    val p = (ratio.coerceIn(0.0, 1.0) * 1000).toInt().let { if (ratio > 0 && it < 20) 20 else it }
    rv.setProgressBar(R.id.bar, 1000, p, false)
    // 높이를 주지 않으면 Glance 틀이 세로를 꽉 채워서(match_parent) 아래 줄이 밀려 사라진다
    AndroidRemoteViews(rv, modifier.height(5.dp))
}

/**
 * 이름 · 막대 · 값 한 줄(분류 남은 돈, 탄단지). 이름·값 칸 너비를 고정해서 막대가 줄마다 같은 자리에 선다.
 * 좁은 위젯(2칸)은 막대가 점으로 줄어서 [bar] = false로 이름 · 값만.
 */
@Composable
private fun MeterRow(
    context: Context,
    name: String,
    ratio: Double,
    state: BudgetState,
    value: String,
    valueColor: GlanceColor,
    nameWidth: Dp = 68.dp,
    valueWidth: Dp = 86.dp,
    pad: Dp = 0.dp,
    bar: Boolean = true,
) {
    if (!bar) {
        Row(modifier = GlanceModifier.fillMaxWidth().padding(vertical = pad), verticalAlignment = Alignment.CenterVertically) {
            Text(name, style = TextStyle(color = TextColor, fontSize = 15.sp), modifier = GlanceModifier.defaultWeight(), maxLines = 1)
            Text(value, style = TextStyle(color = valueColor, fontSize = 15.sp, fontWeight = FontWeight.Medium), maxLines = 1)
        }
        return
    }
    Row(modifier = GlanceModifier.fillMaxWidth().padding(vertical = pad), verticalAlignment = Alignment.CenterVertically) {
        Text(name, style = TextStyle(color = TextColor, fontSize = 15.sp), modifier = GlanceModifier.width(nameWidth), maxLines = 1)
        Bar(context, ratio, state, GlanceModifier.defaultWeight())
        Text(
            value,
            style = TextStyle(color = valueColor, fontSize = 15.sp, fontWeight = FontWeight.Medium, textAlign = TextAlign.End),
            modifier = GlanceModifier.width(valueWidth),
            maxLines = 1,
        )
    }
}

@Composable
private fun BudgetPage(context: Context, summary: Summary?, picking: Pair<Long, Long>?, head: Int, today: LocalDate) {
    val size = LocalSize.current
    val layout = WidgetLayout.forSize(size.width.value, size.height.value)
    val text = TextColor
    val sub = SubColor

    if (summary == null) {
        Text("앱을 열어 로그인하면 남은 돈이 보여요", style = TextStyle(color = sub, fontSize = 13.sp))
        return
    }
    val pick = summary.pick?.takeIf { it.choices.isNotEmpty() }
    val showPick = pick != null && WidgetModel.showPick(layout, true)
    if (!showPick || WidgetModel.headWhilePicking(layout)) {
        BudgetHead(context, summary, layout, head, today)
        Spacer(GlanceModifier.height(6.dp))
    }
    if (showPick && pick != null) {
        // 고르는 동안은 아래(분류·최근 결제)를 가린다(사용자 요청, 2026-11-17). 머리가 위에 있으면(크게) 머리 막대 아래를 조금 띄운다
        if (WidgetModel.headWhilePicking(layout)) Spacer(GlanceModifier.height(6.dp))
        PickCard(context, pick, picking, summary, layout, today)
        return
    }
    val (categoryRows, recentRows) = WidgetModel.rows(layout, false)
    // 분류 줄: 이름 · 쓴 비율 막대 · 남은 돈. Glance 컨테이너는 직접 자식을 10개까지만 그려서 Column으로 묶는다
    Column(modifier = GlanceModifier.fillMaxWidth()) {
        for (c in WidgetModel.topCategories(summary, categoryRows)) {
            val st = WidgetModel.stateOf(c)
            val small = layout == WidgetLayout.SMALL
            MeterRow(
                context, c.name, c.ratio.coerceIn(0.0, 1.0), st,
                if (small) Money.compact(c.remaining) else WidgetModel.rowMoney(c.remaining), colorFor(st),
                pad = if (layout == WidgetLayout.LARGE) 2.dp else 0.dp, bar = !small,
            )
        }
    }
    val recent = if (recentRows > 0) WidgetModel.recentPayments(summary, recentRows) else emptyList()
    if (recent.isNotEmpty()) {
        // 최근 결제: 얇은 선 아래 "시각 · 가게 · 금액"(분류보다 작고 옅게)
        Column(modifier = GlanceModifier.fillMaxWidth()) {
            Spacer(GlanceModifier.height(7.dp))
            Box(modifier = GlanceModifier.fillMaxWidth().height(1.dp).background(LineColor)) {}
            Spacer(GlanceModifier.height(5.dp))
            for (t in recent) {
                val review = t.status == "needs_review"
                Row(
                    modifier = GlanceModifier.fillMaxWidth().padding(vertical = 1.dp).clickable(
                        actionStartActivity(if (review) openIntent(context, "review", t.id) else openIntent(context, "transactions")),
                    ),
                    verticalAlignment = Alignment.CenterVertically,
                ) {
                    Text(WidgetModel.recentTime(t.occurredAt, today), style = TextStyle(color = SubColor, fontSize = 13.sp), modifier = GlanceModifier.width(44.dp), maxLines = 1)
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
 * 가계부 머리(사용자 요청, 2026-11-17): 위 줄 왼쪽 "하루 / 남은 돈", 오른쪽 "N일 남음"(누르면 앱의 예산 시작일 고르기), 아래 큰 숫자,
 * 그 아래 기간 전체 막대(쓴 돈 / 예산). 숫자는 "하루 쓸 돈 / 남은 돈"(남은 돈 ÷ 남은 날)이고, 누르면 "쓴 돈 / 예산"으로 바뀐다(위젯마다 기억).
 * 작은 위젯은 뒤 숫자를 만 단위로 줄여 위 줄에.
 */
@Composable
private fun BudgetHead(context: Context, summary: Summary, layout: WidgetLayout, head: Int, today: LocalDate) {
    val small = layout == WidgetLayout.SMALL
    val t = summary.totals
    val days = WidgetModel.daysLeft(summary.period.end, today)
    // 예산이 없으면 하루 쓸 돈을 셀 수 없어서 쓴 돈만
    val used = head == SummaryWidget.HEAD_USED || t.budget <= 0
    val spent = WidgetModel.used(summary)
    val main: Long
    val rest: Long?
    val mainColor: GlanceColor
    val restColor: GlanceColor
    if (used) {
        main = spent
        rest = t.budget.takeIf { it > 0 }
        mainColor = if (t.budget > 0 && spent > t.budget) OverColor else TextColor
        restColor = SubColor
    } else {
        main = WidgetModel.perDay(t.remaining, days)
        rest = t.remaining
        mainColor = if (t.remaining < 0) OverColor else TextColor
        restColor = if (t.remaining < 0) OverColor else SubColor
    }
    val toggle = actionRunCallback<ToggleHeadAction>()
    val pickDay = actionStartActivity(openIntent(context, "budget_day"))
    Column(modifier = GlanceModifier.fillMaxWidth()) {
    Row(modifier = GlanceModifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
        // 작은 위젯은 둘째 숫자가 들어갈 줄이 없어서 위 줄에 만 단위로 쓴다
        val label = when {
            small && rest == null -> "${days}일 남음"
            small && used -> "${days}일 · 예산 ${Money.compact(rest!!)}"
            small -> "${days}일 · ${Money.compact(rest!!)} 남음"
            used && rest != null -> "쓴 돈 / 예산"
            used -> "쓴 돈"
            else -> "하루 / 남은 돈"
        }
        Text(
            label,
            style = TextStyle(color = SubColor, fontSize = 12.sp),
            modifier = GlanceModifier.defaultWeight().clickable(if (small) pickDay else toggle),
            maxLines = 1,
        )
        if (!small) Text("${days}일 남음", style = TextStyle(color = SubColor, fontSize = 12.sp), modifier = GlanceModifier.clickable(pickDay), maxLines = 1)
    }
    // 작은 위젯(120dp)은 "1,933,050원"이 두 줄로 깨져서 "193만원"처럼 줄여 쓴다
    fun money(v: Long) = if (small) Money.compact(v) + "원" else Money.won(v)
    if (small) {
        Text(money(main), style = TextStyle(color = mainColor, fontSize = 18.sp, fontWeight = FontWeight.Bold), modifier = GlanceModifier.clickable(toggle), maxLines = 1)
    } else {
        Row(modifier = GlanceModifier.fillMaxWidth().clickable(toggle), verticalAlignment = Alignment.Bottom) {
            Text(money(main), style = TextStyle(color = mainColor, fontSize = if (layout == WidgetLayout.LARGE) 24.sp else 22.sp, fontWeight = FontWeight.Bold), maxLines = 1)
            if (rest != null) {
                Text(
                    " / " + money(rest),
                    style = TextStyle(color = restColor, fontSize = 14.sp),
                    modifier = GlanceModifier.defaultWeight().padding(bottom = 3.dp),
                    maxLines = 1,
                )
            }
        }
    }
    // 기간 전체 막대: 쓴 돈 / 예산(고정으로 빠지는 돈 포함)
    if (t.budget > 0) {
        Spacer(GlanceModifier.height(if (small) 3.dp else 4.dp))
        Bar(context, WidgetModel.barRatio(spent, t.budget), WidgetModel.barState(spent, t.budget))
    }
    }
}

/**
 * 고를 거래 칸(사용자 요청, 2026-11-17 "가로로 말고 밑에 내용 가리고 세로로, 우측에는 사용량/총량", "없음 항목도, 몇 시에 결제한
 * 알림인지도"): 위에 가게 · 금액, 그 아래 "오늘 19:56 · N건 남음", 분류 줄을 세로로(동그라미 · 이름 · 오른쪽에 그 분류의
 * 쓴 돈 / 예산), 맨 밑 "없음"(누르면 앱에서 이 결제의 분류 고르기 판 — 다른 분류·가계부에서 빼기). 동그라미는 앱 Modo의
 * CheckCircle과 같다(안 고름: 테두리, 고름: 채운 동그라미에 체크). 줄 어디를 눌러도 [PickCategoryAction].
 */
@Composable
private fun PickCard(context: Context, p: Pick, picking: Pair<Long, Long>?, summary: Summary, layout: WidgetLayout, today: LocalDate) {
    val byId = summary.categories.associateBy { it.id }
    // 바탕 칸 없이 위젯 카드 위에 바로(사용자 의견, 2026-11-17 "박스 안에 박스가 별로")
    Column(modifier = GlanceModifier.fillMaxWidth()) {
        Row(modifier = GlanceModifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
            Text(
                p.merchant.ifBlank { "(이름 없음)" },
                style = TextStyle(color = TextColor, fontSize = 15.sp, fontWeight = FontWeight.Bold),
                modifier = GlanceModifier.defaultWeight(),
                maxLines = 1,
            )
            Text(Money.won(p.amount), style = TextStyle(color = TextColor, fontSize = 15.sp, fontWeight = FontWeight.Bold))
        }
        Text(
            listOf(WidgetModel.whenText(p.occurredAt, today), "${p.left}건 남음").filter { it.isNotEmpty() }.joinToString(" · "),
            style = TextStyle(color = SubColor, fontSize = 12.sp),
            maxLines = 1,
        )
        // Glance 컨테이너는 직접 자식을 10개까지만 그려서 줄을 Column으로 묶는다. 중간 위젯(세로 2칸)에 들어가게 줄 사이를 좁힌다
        Column(modifier = GlanceModifier.fillMaxWidth()) {
            for (c in p.choices.take(WidgetModel.pickChoiceCount(layout))) {
                val on = picking?.first == p.id && picking.second == c.id
                val cat = byId[c.id]
                Row(
                    modifier = GlanceModifier.fillMaxWidth().padding(vertical = 1.dp)
                        .clickable(actionRunCallback<PickCategoryAction>(actionParametersOf(PickTxParam to p.id, PickCatParam to c.id))),
                    verticalAlignment = Alignment.CenterVertically,
                ) {
                    Image(
                        provider = ImageProvider(if (on) R.drawable.widget_circle_on else R.drawable.widget_circle_off),
                        contentDescription = "${c.name}로 고르기",
                        modifier = GlanceModifier.size(18.dp),
                    )
                    Spacer(GlanceModifier.width(8.dp))
                    Text(
                        c.name,
                        style = TextStyle(color = TextColor, fontSize = 14.sp, fontWeight = if (on) FontWeight.Bold else FontWeight.Normal),
                        modifier = GlanceModifier.defaultWeight(),
                        maxLines = 1,
                    )
                    Text(
                        WidgetModel.usageText(cat),
                        style = TextStyle(color = cat?.let { colorFor(WidgetModel.stateOf(it)) } ?: TextColor, fontSize = 13.sp),
                        maxLines = 1,
                    )
                }
            }
            // 없음: 위 분류가 다 아니면 앱에서 이 결제의 분류 고르기 판(다른 분류·가계부에서 빼기)
            Row(
                modifier = GlanceModifier.fillMaxWidth().padding(vertical = 1.dp)
                    .clickable(actionStartActivity(openIntent(context, "review", p.id))),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                Image(provider = ImageProvider(R.drawable.widget_circle_off), contentDescription = "없음, 앱에서 고르기", modifier = GlanceModifier.size(18.dp))
                Spacer(GlanceModifier.width(8.dp))
                Text("없음", style = TextStyle(color = TextColor, fontSize = 14.sp), modifier = GlanceModifier.defaultWeight(), maxLines = 1)
                Text("앱에서 고르기", style = TextStyle(color = SubColor, fontSize = 13.sp), maxLines = 1)
            }
        }
    }
}

/**
 * 건강 페이지(사용자 요청, 2026-11-17 "건강 항목으로 사진 찍는 버튼이랑 탄단지칼로리"): 위 "오늘 · N/M끼", 칼로리 먹은 양 / 목표,
 * 오른쪽 진한 동그라미 찍기 단추(누르면 앱의 끼니 찍기), 칼로리 막대, 아래 탄수화물·단백질·지방 막대(먹은 양 / 목표).
 * 중간은 한 줄에 이름 · 막대 · 값, 크게는 이름 · 값 아래 긴 막대. 작은 위젯은 칼로리·막대와 단추만.
 */
@Composable
private fun HealthPage(context: Context, health: HealthToday?) {
    val size = LocalSize.current
    val layout = WidgetLayout.forSize(size.width.value, size.height.value)
    val small = layout == WidgetLayout.SMALL
    val large = layout == WidgetLayout.LARGE
    val total = health?.total
    val goal = health?.targets
    Row(modifier = GlanceModifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
        Column(modifier = GlanceModifier.defaultWeight()) {
            val meals = health?.takeIf { it.mealCount > 0 }?.let { " · ${it.loggedCount}/${it.mealCount}끼" }.orEmpty()
            // 작은 위젯은 목표 줄이 들어가지 않아서 먹은 칼로리만
            Text(if (small) "오늘 kcal" else "오늘$meals", style = TextStyle(color = SubColor, fontSize = 12.sp), maxLines = 1)
            val kcal = total?.let { Money.grouped(it.kcal.toLong()) } ?: "-"
            val of = goal?.kcal?.takeIf { it > 0 }?.let { "/ ${Money.grouped(it.toLong())}kcal" } ?: "kcal"
            if (small) {
                Text(kcal, style = TextStyle(color = TextColor, fontSize = 18.sp, fontWeight = FontWeight.Bold), maxLines = 1)
            } else {
                Row(verticalAlignment = Alignment.Bottom) {
                    Text(kcal, style = TextStyle(color = TextColor, fontSize = if (large) 24.sp else 22.sp, fontWeight = FontWeight.Bold), maxLines = 1)
                    Text(" $of", style = TextStyle(color = SubColor, fontSize = 14.sp), modifier = GlanceModifier.padding(bottom = 3.dp), maxLines = 1)
                }
            }
        }
        // 찍기 단추: 앱의 끼니 찍기(다음 끼니)로 바로. 진한 동그라미(앱의 주 단추 색)라 눌러야 할 곳이 바로 보인다
        Box(
            modifier = GlanceModifier.size(if (small) 36.dp else 44.dp).background(ImageProvider(R.drawable.widget_cta))
                .clickable(actionStartActivity(openIntent(context, "health_capture"))),
            contentAlignment = Alignment.Center,
        ) {
            Image(
                provider = ImageProvider(R.drawable.ic_widget_camera),
                contentDescription = "끼니 찍기",
                colorFilter = ColorFilter.tint(OnCtaColor),
                modifier = GlanceModifier.size(if (small) 18.dp else 22.dp),
            )
        }
    }
    val kcalGoal = goal?.kcal ?: 0
    if (health != null && kcalGoal > 0) {
        Spacer(GlanceModifier.height(if (small) 3.dp else 4.dp))
        val eaten = total?.kcal ?: 0
        Bar(context, eaten.toDouble() / kcalGoal, if (eaten > kcalGoal) BudgetState.OVER else BudgetState.OK)
    }
    // 좁아도 세로가 넉넉하면(2칸×2줄) 탄단지를 보인다
    if (small && size.height.value < 150f) return
    Spacer(GlanceModifier.height(6.dp))
    if (health == null) {
        Text("앱의 건강 칸을 한 번 열어 주세요", style = TextStyle(color = SubColor, fontSize = 13.sp))
        return
    }
    Column(modifier = GlanceModifier.fillMaxWidth()) {
        MacroRow(context, "탄수화물", total?.carbs ?: 0, goal?.carbs ?: 0, stacked = large, overIsBad = true, bar = !small)
        MacroRow(context, "단백질", total?.protein ?: 0, goal?.protein ?: 0, stacked = large, overIsBad = false, bar = !small)
        MacroRow(context, "지방", total?.fat ?: 0, goal?.fat ?: 0, stacked = large, overIsBad = true, bar = !small)
    }
}

/**
 * 탄단지 한 줄: 이름, 막대(먹은 양 / 목표), "먹은 g / 목표 g"(목표가 없으면 먹은 g만, 막대 없음).
 * 목표를 넘으면 살구색(단백질은 넘어도 괜찮아서 그대로). stacked면 이름 · 값 아래 긴 막대(크게).
 */
@Composable
private fun MacroRow(context: Context, name: String, eaten: Int, goal: Int, stacked: Boolean, overIsBad: Boolean, bar: Boolean = true) {
    val value = if (goal > 0) "$eaten / ${goal}g" else "${eaten}g"
    val over = overIsBad && goal > 0 && eaten > goal
    val state = if (over) BudgetState.OVER else BudgetState.OK
    val ratio = if (goal > 0) eaten.toDouble() / goal else 0.0
    val valueColor = if (over) OverColor else TextColor
    if (!stacked) {
        MeterRow(context, name, ratio, state, value, valueColor, bar = bar)
        return
    }
    Column(modifier = GlanceModifier.fillMaxWidth().padding(vertical = 4.dp)) {
        Row(modifier = GlanceModifier.fillMaxWidth()) {
            Text(name, style = TextStyle(color = TextColor, fontSize = 15.sp), modifier = GlanceModifier.defaultWeight(), maxLines = 1)
            Text(value, style = TextStyle(color = valueColor, fontSize = 15.sp, fontWeight = FontWeight.Medium), maxLines = 1)
        }
        Spacer(GlanceModifier.height(5.dp))
        Bar(context, ratio, state)
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
                    modifier = GlanceModifier.size(26.dp)
                        .background(ImageProvider(R.drawable.widget_dot), colorFilter = ColorFilter.tint(ColorProvider(day = color, night = color))),
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
