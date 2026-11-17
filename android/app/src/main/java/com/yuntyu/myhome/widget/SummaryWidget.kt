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
import androidx.glance.action.Action
import androidx.glance.appwidget.lazy.LazyColumn
import androidx.glance.appwidget.lazy.items
import androidx.glance.layout.ColumnScope
import androidx.glance.layout.RowScope
import androidx.glance.semantics.contentDescription
import androidx.glance.semantics.semantics
import com.yuntyu.myhome.core.formatHm
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
import com.yuntyu.myhome.core.HealthWidget
import com.yuntyu.myhome.core.HealthWidgetData
import com.yuntyu.myhome.core.WidgetBody
import com.yuntyu.myhome.core.api.Nutrition
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
import java.time.LocalDate

/**
 * 홈 화면 위젯. 아래 쪽 점으로 세 페이지를 오간다(위젯은 옆으로 미는 손짓을 받지 못해서 점을 누른다. 위젯마다 기억한다).
 * 모양은 사용자 시안(2026-11-18): 페이지마다 굵은 제목 줄, 큰 숫자 칸, 선 아래 목록, 오른쪽 끝 동그라미.
 * - 가계부: 남은 날과 하루 쓸 돈 / 남은 돈(누르면 쓴 돈 / 예산), 고를 거래(동그라미 한 번으로 분류, 다음 것으로 넘어감),
 *   분류별 남은 돈(사용률 높은 순), 최근 결제. 서버 캐시(SummaryRepository)만 읽는다
 * - 집중: 오늘 순공, 과목 줄(동그라미를 누르면 앱을 열지 않고 시작·멈춤). 폰 기록(Room)만 읽는다
 * - 건강: 오늘 칼로리·탄단지(먹은 양 / 목표), 끼니 표, 몸무게, 끼니 찍기·몸 기록 단추. 받아 둔 것(HealthWidgetCache)만 읽는다
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
            val health by app.healthWidget.data.collectAsState()
            val running by FocusTimer.running(context).collectAsState()
            val day = FocusDay.dayOf(System.currentTimeMillis())
            val subjects by remember { focus.subjects() }.collectAsState(initial = null)
            val sessions by remember(day) { focus.sessionsBetween(FocusDay.startOf(day), FocusDay.endOf(day)) }.collectAsState(initial = emptyList())
            val healthDay = HealthPlan.scheduleDay(app.healthLocal.schedule.meals, java.time.LocalDateTime.now())
            SummaryWidgetContent(
                context, page, st.summary, subjects, sessions, running, st.picking,
                head = head, health = HealthWidget.forDay(health, healthDay), today = LocalDate.now(),
            )
        }
    }

    companion object {
        val SMALL = DpSize(120.dp, 100.dp)
        val MEDIUM = DpSize(250.dp, 180.dp)
        // 4칸×3줄(폰마다 높이 260~340dp)에서도 크게가 나오게 260(2026-11-18, 300이면 290dp짜리 4×3은 중간으로 그려졌다)
        val LARGE = DpSize(250.dp, 260.dp)
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
    health: HealthWidgetData? = null,
    today: LocalDate = LocalDate.now(),
) {
    GlanceTheme {
        // 점 줄 양 끝 단추: 건강은 몸 기록 · 끼니 찍기(작은 위젯은 찍기 단추가 내용 안에 있다)
        val size = LocalSize.current
        val roomy = WidgetLayout.forSize(size.width.value, size.height.value) != WidgetLayout.SMALL
        val healthPage = page == SummaryWidget.PAGE_HEALTH && roomy
        WidgetFrame(
            context, page,
            bottomStart = if (healthPage) ({ HealthBodyButton(context) }) else null,
            bottomEnd = if (healthPage) ({ HealthCameraButton(context) }) else null,
        ) {
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

/** 아래 쪽 점: 그 위젯의 페이지를 바꾼다 */
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
// 아래 쪽 점(지금 페이지가 아닌 것)
private val DotOffColor = ColorProvider(day = Color(0xFFC9C9C7), night = Color(0xFF5C5C5C))

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
 * 바탕 카드: 위에 내용, 아래 가운데 쪽 점(누르면 그 페이지). 사용자 시안(2026-11-18 "위젯 이런 식으로"): 위 굵은 제목 줄, 가운데 내용,
 * 아래 쪽 점. 위젯은 옆으로 미는 손짓을 받지 못해서 점을 눌러 넘긴다(위젯마다 기억). 점 줄 양 끝에 그 페이지 단추([bottomStart]·[bottomEnd]).
 * 카드 빈 곳을 누르면 그 페이지의 앱 화면이 열린다.
 */
@Composable
private fun WidgetFrame(
    context: Context,
    page: Int,
    bottomStart: (@Composable () -> Unit)?,
    bottomEnd: (@Composable () -> Unit)?,
    content: @Composable ColumnScope.() -> Unit,
) {
    val size = LocalSize.current
    val small = WidgetLayout.forSize(size.width.value, size.height.value) == WidgetLayout.SMALL
    val screen = when (page) {
        SummaryWidget.PAGE_FOCUS -> "focus"
        SummaryWidget.PAGE_HEALTH -> "health"
        else -> "home"
    }
    val side = if (small) 12.dp else 16.dp
    Column(
        modifier = GlanceModifier.fillMaxSize().cornerRadius(R.dimen.widget_radius)
            .background(ImageProvider(R.drawable.widget_bg))
            .padding(start = side, end = side, top = if (small) 10.dp else 14.dp, bottom = if (small) 2.dp else 6.dp)
            .clickable(actionStartActivity(openIntent(context, screen))),
    ) {
        // Glance 컨테이너는 직접 자식을 10개까지만 그려서 페이지 내용을 한 Column으로 묶는다(넘으면 아래가 조용히 빠진다)
        Column(modifier = GlanceModifier.fillMaxWidth().defaultWeight()) { content() }
        PageDots(page, bottomStart, bottomEnd)
    }
}

private val PageNames = listOf("가계부", "집중", "건강")

/** 아래 가운데 쪽 점(지금 페이지는 본문색), 양 끝에 단추 자리. 왼쪽·오른쪽 칸 너비가 같아서 점은 늘 가운데 */
@Composable
private fun PageDots(page: Int, start: (@Composable () -> Unit)?, end: (@Composable () -> Unit)?) {
    Row(modifier = GlanceModifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
        Box(modifier = GlanceModifier.defaultWeight(), contentAlignment = Alignment.CenterStart) { start?.invoke() }
        for (p in PageNames.indices) {
            Box(
                modifier = GlanceModifier.padding(horizontal = 5.dp, vertical = 7.dp)
                    .semantics { contentDescription = PageNames[p] + " 페이지" }
                    .clickable(actionRunCallback<SetWidgetPageAction>(actionParametersOf(PageParam to p))),
                contentAlignment = Alignment.Center,
            ) {
                Box(
                    modifier = GlanceModifier.size(7.dp)
                        .background(ImageProvider(R.drawable.widget_dot), colorFilter = ColorFilter.tint(if (p == page) TextColor else DotOffColor)),
                ) {}
            }
        }
        Box(modifier = GlanceModifier.defaultWeight(), contentAlignment = Alignment.CenterEnd) { end?.invoke() }
    }
}

/** 굵은 제목과 오른쪽 덧말(시안의 "오늘의 식사 … 2/3끼 기록") */
@Composable
private fun TitleRow(title: String, onTitle: Action? = null, right: @Composable () -> Unit = {}) {
    Row(modifier = GlanceModifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
        val m = GlanceModifier.defaultWeight()
        Text(
            title,
            style = TextStyle(color = TextColor, fontSize = 17.sp, fontWeight = FontWeight.Bold),
            modifier = if (onTitle != null) m.clickable(onTitle) else m,
            maxLines = 1,
        )
        right()
    }
}

/** 얇은 구분선 */
@Composable
private fun Hairline() {
    Box(modifier = GlanceModifier.fillMaxWidth().height(1.dp).background(LineColor)) {}
}

/** 동그라미 단추(옅은 바탕 또는 진한 바탕) 안에 아이콘. 누르면 앱의 [screen] */
@Composable
private fun RoundButton(context: Context, bg: Int, icon: Int, tint: GlanceColor, label: String, screen: String, size: Dp, action: Action? = null) {
    Box(
        modifier = GlanceModifier.size(size).background(ImageProvider(bg))
            .clickable(action ?: actionStartActivity(openIntent(context, screen))),
        contentAlignment = Alignment.Center,
    ) {
        Image(provider = ImageProvider(icon), contentDescription = label, colorFilter = ColorFilter.tint(tint), modifier = GlanceModifier.size(size * 0.5f))
    }
}

/** 제목 줄 오른쪽 글자 배지(바탕 없이 색 글자): 확인 필요 N(라벤더), 서버가 읽지 못한 알림 N(살구). 누르면 그 목록 */
@Composable
private fun BudgetBadges(context: Context, summary: Summary) {
    if (summary.needsReview > 0) Badge("확인 ${summary.needsReview}", WarnColor, openIntent(context, "review"))
    // 서버가 읽지 못한 알림(거래로 안 잡혔을 수 있음)
    if (summary.failedEvents > 0) Badge("실패 ${summary.failedEvents}", OverColor, openIntent(context, "unread"))
}

@Composable
private fun Badge(text: String, color: GlanceColor, intent: Intent) {
    Text(
        text,
        style = TextStyle(color = color, fontSize = 13.sp, fontWeight = FontWeight.Bold),
        modifier = GlanceModifier.padding(end = 10.dp).clickable(actionStartActivity(intent)),
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

/**
 * 가계부 페이지: 고를 결제가 있으면 [PickPage]가 위젯을 다 쓴다(사용자 요청, 2026-11-17 "화면을 전부 채우는").
 * 아니면 제목 줄("오늘 쓸 돈" … "N일 남음"), 큰 숫자와 기간 막대, 선 아래 분류 줄(이름 · 막대 · 남은 돈), 크게는 최근 결제.
 */
@Composable
private fun ColumnScope.BudgetPage(context: Context, summary: Summary?, picking: Pair<Long, Long>?, head: Int, today: LocalDate) {
    val size = LocalSize.current
    val layout = WidgetLayout.forSize(size.width.value, size.height.value)
    val small = layout == WidgetLayout.SMALL
    if (summary == null) {
        Text("앱을 열어 로그인하면 남은 돈이 보여요", style = TextStyle(color = SubColor, fontSize = 13.sp))
        return
    }
    val pick = summary.pick?.takeIf { it.choices.isNotEmpty() }
    if (pick != null && WidgetModel.showPick(layout, true)) {
        PickPage(context, pick, picking, summary, today)
        return
    }
    BudgetHead(context, summary, layout, head, today)
    // 작은 위젯은 세로가 넉넉할 때(2칸×2줄)만 분류 줄
    if (small && size.height.value < 150f) return
    val (categoryRows, recentRows) = WidgetModel.rows(layout, false)
    // 크게는 선으로 나누고, 중간(세로 2칸)은 줄이 들어가게 띄우기만
    if (layout == WidgetLayout.LARGE) {
        Spacer(GlanceModifier.height(10.dp))
        Hairline()
        Spacer(GlanceModifier.height(4.dp))
    } else {
        Spacer(GlanceModifier.height(if (small) 6.dp else 8.dp))
    }
    // 분류 줄: 이름 · 쓴 비율 막대 · 남은 돈. 2칸 너비는 막대 없이
    Column(modifier = GlanceModifier.fillMaxWidth()) {
        for (c in WidgetModel.topCategories(summary, categoryRows)) {
            val st = WidgetModel.stateOf(c)
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
            Spacer(GlanceModifier.height(6.dp))
            Hairline()
            Spacer(GlanceModifier.height(4.dp))
            for (t in recent) {
                val review = t.status == "needs_review"
                Row(
                    modifier = GlanceModifier.fillMaxWidth().padding(vertical = 2.dp).clickable(
                        actionStartActivity(if (review) openIntent(context, "review", t.id) else openIntent(context, "transactions")),
                    ),
                    verticalAlignment = Alignment.CenterVertically,
                ) {
                    Text(WidgetModel.recentTime(t.occurredAt, today), style = TextStyle(color = SubColor, fontSize = 13.sp), modifier = GlanceModifier.width(46.dp), maxLines = 1)
                    Text(
                        (if (review) "⚠ " else "") + t.merchant.ifBlank { "(이름 없음)" },
                        style = TextStyle(color = if (review) WarnColor else TextColor, fontSize = 14.sp),
                        modifier = GlanceModifier.defaultWeight(),
                        maxLines = 1,
                    )
                    Text((if (t.flow == "in") "+" else "") + Money.won(t.amount), style = TextStyle(color = TextColor, fontSize = 14.sp))
                }
            }
        }
    }
}

/**
 * 가계부 머리(사용자 요청, 2026-11-17, 모양은 2026-11-18 시안): 제목 "오늘 쓸 돈"(누르면 "쓴 돈"으로), 오른쪽 확인·실패 배지와 "N일 남음"
 * (누르면 앱의 예산 시작일 고르기), 큰 숫자 "39,650원 / 713,700원 남음"(하루 쓸 돈 = 남은 돈 ÷ 남은 날) ↔ "486,300원 / 1,200,000원 예산",
 * 그 아래 기간 막대(쓴 돈 / 예산). 숫자·제목을 누르면 바뀐다(위젯마다 기억). 작은 위젯은 제목 없이 "18일 · 71만 남음"과 숫자.
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
        if (small) {
            // 작은 위젯(120dp)은 제목 대신 한 줄에 남은 날과 만 단위 둘째 숫자, "1,933,050원"은 깨져서 "193만원"처럼
            val label = when {
                rest == null -> "${days}일 남음"
                used -> "${days}일 · 예산 ${Money.compact(rest)}"
                else -> "${days}일 · ${Money.compact(rest)} 남음"
            }
            Text(label, style = TextStyle(color = SubColor, fontSize = 12.sp), modifier = GlanceModifier.clickable(pickDay), maxLines = 1)
            Text(Money.compact(main) + "원", style = TextStyle(color = mainColor, fontSize = 18.sp, fontWeight = FontWeight.Bold), modifier = GlanceModifier.clickable(toggle), maxLines = 1)
        } else {
            TitleRow(if (used) "쓴 돈" else "오늘 쓸 돈", onTitle = toggle) {
                BudgetBadges(context, summary)
                Text("${days}일 남음", style = TextStyle(color = SubColor, fontSize = 13.sp), modifier = GlanceModifier.clickable(pickDay), maxLines = 1)
            }
            Spacer(GlanceModifier.height(4.dp))
            Row(modifier = GlanceModifier.fillMaxWidth().clickable(toggle), verticalAlignment = Alignment.Bottom) {
                Text(Money.won(main), style = TextStyle(color = mainColor, fontSize = if (layout == WidgetLayout.LARGE) 26.sp else 24.sp, fontWeight = FontWeight.Bold), maxLines = 1)
                if (rest != null) {
                    Text(
                        // 100만 위는 "120만원"처럼 줄여서 좁은 위젯에서도 "예산·남음"까지 보이게
                        " / " + WidgetModel.rowMoney(rest) + if (used) " 예산" else " 남음",
                        style = TextStyle(color = restColor, fontSize = 14.sp),
                        modifier = GlanceModifier.defaultWeight().padding(bottom = 4.dp),
                        maxLines = 1,
                    )
                }
            }
        }
        // 기간 전체 막대: 쓴 돈 / 예산(고정으로 빠지는 돈 포함)
        if (t.budget > 0) {
            Spacer(GlanceModifier.height(if (small) 3.dp else 6.dp))
            Bar(context, WidgetModel.barRatio(spent, t.budget), WidgetModel.barState(spent, t.budget))
        }
    }
}

/**
 * 분류할 결제(사용자 시안, 2026-11-18): 제목 "분류할 결제" … "N건 남음", 가게 · 금액(크게), 결제 시각, 선 아래 분류 목록(밀어서 넘김).
 * 줄은 이름 · 그 분류의 쓴 돈 / 예산(2026-11-17 요청) · 오른쪽 끝 동그라미(2026-11-18 "확인 원은 우측에"). 맨 밑 "없음"은 앱에서
 * 이 결제의 분류 고르기 판(다른 분류·가계부에서 빼기). 동그라미는 앱 Modo의 CheckCircle과 같다. 줄 어디를 눌러도 [PickCategoryAction].
 */
@Composable
private fun ColumnScope.PickPage(context: Context, p: Pick, picking: Pair<Long, Long>?, summary: Summary, today: LocalDate) {
    val byId = summary.categories.associateBy { it.id }
    TitleRow("분류할 결제") {
        Text("${p.left}건 남음", style = TextStyle(color = SubColor, fontSize = 13.sp), maxLines = 1)
    }
    Spacer(GlanceModifier.height(10.dp))
    Row(modifier = GlanceModifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
        Text(
            p.merchant.ifBlank { "(이름 없음)" },
            style = TextStyle(color = TextColor, fontSize = 17.sp, fontWeight = FontWeight.Bold),
            modifier = GlanceModifier.defaultWeight(),
            maxLines = 1,
        )
        Text(Money.won(p.amount), style = TextStyle(color = TextColor, fontSize = 17.sp, fontWeight = FontWeight.Bold))
    }
    val whenText = WidgetModel.whenText(p.occurredAt, today)
    if (whenText.isNotEmpty()) Text(whenText, style = TextStyle(color = SubColor, fontSize = 13.sp), maxLines = 1)
    Spacer(GlanceModifier.height(8.dp))
    Hairline()
    // 서버가 주는 분류(최대 6) + 맨 밑 없음(null)을 밀어서 넘기는 목록으로
    ScrollList(p.choices + listOf(null), itemId = { it?.id ?: NONE_ITEM }) { c ->
        if (c == null) {
            ChoiceRow("없음", "앱에서 고르기", SubColor, false, actionStartActivity(openIntent(context, "review", p.id)), "없음, 앱에서 고르기")
        } else {
            val on = picking?.first == p.id && picking.second == c.id
            val cat = byId[c.id]
            ChoiceRow(
                c.name, WidgetModel.usageText(cat), cat?.let { colorFor(WidgetModel.stateOf(it)) } ?: SubColor, on,
                actionRunCallback<PickCategoryAction>(actionParametersOf(PickTxParam to p.id, PickCatParam to c.id)),
                "${c.name}로 고르기",
            )
        }
    }
}

private const val NONE_ITEM = -1L

/** 화면 찍기 테스트용: Robolectric은 Glance 목록(ListView 어댑터)을 그리지 못해서 같은 줄을 Column으로 그린다(폰에서는 늘 밀어서 넘기는 목록) */
@androidx.annotation.VisibleForTesting
internal var widgetListsAsColumns = false

/** 남은 높이를 채우는 목록: 폰에서는 밀어서 넘기는 LazyColumn */
@Composable
private fun <T> ColumnScope.ScrollList(rows: List<T>, itemId: (T) -> Long, row: @Composable (T) -> Unit) {
    if (widgetListsAsColumns) {
        Column(modifier = GlanceModifier.fillMaxWidth().defaultWeight()) { for (t in rows.take(9)) row(t) }
        return
    }
    LazyColumn(modifier = GlanceModifier.fillMaxWidth().defaultWeight()) {
        items(rows, itemId = itemId) { row(it) }
    }
}

/** 고르기·집중 목록 한 줄: 이름 · 오른쪽 덧말 · 오른쪽 끝 동그라미(고름: 채운 동그라미에 체크) */
@Composable
private fun ChoiceRow(name: String, side: String, sideColor: GlanceColor, on: Boolean, action: Action, label: String) {
    Row(
        modifier = GlanceModifier.fillMaxWidth().padding(vertical = 7.dp).clickable(action),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Text(
            name,
            style = TextStyle(color = TextColor, fontSize = 16.sp, fontWeight = if (on) FontWeight.Bold else FontWeight.Normal),
            modifier = GlanceModifier.defaultWeight(),
            maxLines = 1,
        )
        if (side.isNotEmpty()) Text(side, style = TextStyle(color = sideColor, fontSize = 14.sp), maxLines = 1)
        Spacer(GlanceModifier.width(14.dp))
        Image(
            provider = ImageProvider(if (on) R.drawable.widget_circle_on else R.drawable.widget_circle_off),
            contentDescription = label,
            modifier = GlanceModifier.size(22.dp),
        )
    }
}

/**
 * 건강 페이지(사용자 시안, 2026-11-18 "오늘의 식사"): 제목 … "2/3끼 기록", 탄수화물 · 단백질 · 지방 · 칼로리 네 칸(먹은 양 크게, 목표 작게),
 * 크게는 선 아래 끼니 표(1끼 · 탄수 · 단백 · 지방, 아직이면 "기록하기 — — —", 누르면 그 끼니 찍기).
 * 점 줄 왼쪽에 몸 기록, 오른쪽에 끼니 찍기 단추([HealthBottom]). 작은 위젯은 칼로리와 찍기 단추만, 2칸×2줄이면 탄단지 세 줄과 몸무게 줄도.
 */
@Composable
private fun HealthPage(context: Context, health: HealthWidgetData?) {
    val size = LocalSize.current
    val layout = WidgetLayout.forSize(size.width.value, size.height.value)
    val today = health?.today
    val total = today?.total ?: Nutrition()
    val goal = today?.targets ?: Nutrition()
    fun over(eaten: Int, target: Int) = target > 0 && eaten > target
    if (layout == WidgetLayout.SMALL) {
        Row(modifier = GlanceModifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
            Column(modifier = GlanceModifier.defaultWeight()) {
                Text("오늘 kcal", style = TextStyle(color = SubColor, fontSize = 12.sp), maxLines = 1)
                Text(
                    if (today != null) Money.grouped(total.kcal.toLong()) else "-",
                    style = TextStyle(color = if (over(total.kcal, goal.kcal)) OverColor else TextColor, fontSize = 18.sp, fontWeight = FontWeight.Bold),
                    maxLines = 1,
                )
            }
            RoundButton(context, R.drawable.widget_cta, R.drawable.ic_widget_camera, OnCtaColor, "끼니 찍기", "health_capture", 36.dp)
        }
        // 좁아도 세로가 넉넉하면(2칸×2줄) 탄단지와 몸무게
        if (size.height.value < 150f || today == null) return
        Spacer(GlanceModifier.height(6.dp))
        Column(modifier = GlanceModifier.fillMaxWidth()) {
            SmallMacro("탄수화물", total.carbs, goal.carbs, overIsBad = true)
            SmallMacro("단백질", total.protein, goal.protein, overIsBad = false)
            SmallMacro("지방", total.fat, goal.fat, overIsBad = true)
            Spacer(GlanceModifier.height(4.dp))
            BodyLine(context, health.body, fontSize = 13, short = true)
        }
        return
    }
    TitleRow("오늘의 식사") {
        if (today != null && today.mealCount > 0) {
            Text("${today.loggedCount}/${today.mealCount}끼 기록", style = TextStyle(color = SubColor, fontSize = 13.sp), maxLines = 1)
        }
    }
    if (today == null) {
        Spacer(GlanceModifier.height(10.dp))
        Text("앱의 건강 칸을 한 번 열어 주세요", style = TextStyle(color = SubColor, fontSize = 13.sp))
        return
    }
    Spacer(GlanceModifier.height(6.dp))
    // 네 칸: 먹은 양(목표를 넘으면 살구, 단백질은 넘어도 그대로) / 목표
    Row(modifier = GlanceModifier.fillMaxWidth()) {
        Stat("탄수화물", "${total.carbs}g", "${goal.carbs}g", over(total.carbs, goal.carbs))
        Stat("단백질", "${total.protein}g", "${goal.protein}g", false)
        Stat("지방", "${total.fat}g", "${goal.fat}g", over(total.fat, goal.fat))
        Stat("칼로리", Money.grouped(total.kcal.toLong()), Money.grouped(goal.kcal.toLong()) + "kcal", over(total.kcal, goal.kcal))
    }
    if (layout != WidgetLayout.LARGE) return
    Spacer(GlanceModifier.height(8.dp))
    Hairline()
    Spacer(GlanceModifier.height(4.dp))
    // 끼니 표. Glance 컨테이너는 직접 자식을 10개까지만 그려서 Column으로 묶는다
    Column(modifier = GlanceModifier.fillMaxWidth()) {
        Row(modifier = GlanceModifier.fillMaxWidth().padding(vertical = 1.dp)) {
            Spacer(GlanceModifier.defaultWeight())
            for (h in listOf("탄수", "단백", "지방")) {
                Text(h, style = TextStyle(color = SubColor, fontSize = 13.sp, textAlign = TextAlign.Center), modifier = GlanceModifier.defaultWeight(), maxLines = 1)
            }
        }
        for ((index, n) in HealthWidget.tableRows(health.meals, today.mealCount).take(4)) {
            Row(
                modifier = GlanceModifier.fillMaxWidth().padding(vertical = 1.dp).clickable(
                    // 아직 안 먹은 끼니는 그 끼니 찍기로, 먹은 끼니는 앱 건강 화면으로
                    actionStartActivity(if (n == null) openIntent(context, "health_capture", index.toLong()) else openIntent(context, "health")),
                ),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                Row(modifier = GlanceModifier.defaultWeight(), verticalAlignment = Alignment.CenterVertically) {
                    Text(HealthPlan.label(index), style = TextStyle(color = TextColor, fontSize = 14.sp, fontWeight = FontWeight.Bold), maxLines = 1)
                    if (n == null) Text("  기록하기", style = TextStyle(color = SubColor, fontSize = 12.sp), maxLines = 1)
                }
                for (v in listOf(n?.carbs, n?.protein, n?.fat)) {
                    Text(
                        v?.let { "${it}g" } ?: "—",
                        style = TextStyle(color = if (v == null) SubColor else TextColor, fontSize = 14.sp, textAlign = TextAlign.Center),
                        modifier = GlanceModifier.defaultWeight(),
                        maxLines = 1,
                    )
                }
            }
        }
    }
}

/** 건강 네 칸 중 하나: 이름, 먹은 양(크게), 목표(작게) */
@Composable
private fun RowScope.Stat(label: String, value: String, goal: String, over: Boolean) {
    Column(modifier = GlanceModifier.defaultWeight(), horizontalAlignment = Alignment.CenterHorizontally) {
        Text(label, style = TextStyle(color = TextColor, fontSize = 12.sp, textAlign = TextAlign.Center), maxLines = 1)
        Text(value, style = TextStyle(color = if (over) OverColor else TextColor, fontSize = 19.sp, fontWeight = FontWeight.Bold, textAlign = TextAlign.Center), maxLines = 1)
        Text(goal, style = TextStyle(color = SubColor, fontSize = 12.sp, textAlign = TextAlign.Center), maxLines = 1)
    }
}

/** 건강 점 줄 양 끝 단추: 왼쪽 몸 기록(옅은 동그라미에 체중계, 몸무게·몸 사진), 오른쪽 끼니 찍기(진한 동그라미) */
@Composable
private fun HealthBodyButton(context: Context) =
    RoundButton(context, R.drawable.widget_soft, R.drawable.ic_widget_body, TextColor, "몸 기록", "body", 36.dp)

@Composable
private fun HealthCameraButton(context: Context) =
    RoundButton(context, R.drawable.widget_cta, R.drawable.ic_widget_camera, OnCtaColor, "끼니 찍기", "health_capture", 40.dp)

/** 작은 위젯(2칸×2줄)의 탄단지 한 줄: 이름 · "152/250g" */
@Composable
private fun SmallMacro(name: String, eaten: Int, goal: Int, overIsBad: Boolean) {
    val over = overIsBad && goal > 0 && eaten > goal
    Row(modifier = GlanceModifier.fillMaxWidth()) {
        Text(name, style = TextStyle(color = TextColor, fontSize = 13.sp), modifier = GlanceModifier.defaultWeight(), maxLines = 1)
        Text(if (goal > 0) "$eaten/${goal}g" else "${eaten}g", style = TextStyle(color = if (over) OverColor else TextColor, fontSize = 13.sp, fontWeight = FontWeight.Medium), maxLines = 1)
    }
}

/** 몸무게 줄: "몸무게 72.4kg · 지난번보다 −0.3kg"(좁으면 "몸무게 72.4kg (−0.3)", 없으면 "몸무게 기록하기"). 누르면 앱의 몸 기록 */
@Composable
private fun BodyLine(context: Context, body: WidgetBody?, fontSize: Int, short: Boolean = false) {
    Row(
        modifier = GlanceModifier.fillMaxWidth().clickable(actionStartActivity(openIntent(context, "body"))),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Text("몸무게 ", style = TextStyle(color = SubColor, fontSize = fontSize.sp), maxLines = 1)
        Text(
            body?.let { HealthWidget.bodyText(it, short) } ?: "기록하기",
            style = TextStyle(color = if (body != null) TextColor else SubColor, fontSize = fontSize.sp),
            modifier = GlanceModifier.defaultWeight(),
            maxLines = 1,
        )
    }
}

/** 시스템이 초를 그리는 시계. shownMs는 지금 보여야 할 값이고 거기서부터 간다 */
@Composable
private fun LiveClock(context: Context, layoutId: Int, shownMs: Long) {
    val rv = RemoteViews(context.packageName, layoutId)
    rv.setChronometer(R.id.chrono, SystemClock.elapsedRealtime() - shownMs, null, true)
    AndroidRemoteViews(rv)
}

/**
 * 집중 페이지(사용자 시안, 2026-11-18 "오늘의 집중"): 제목 … "오늘 누적 2시간 15분", 지금 과목 "수학 · 진행 중"과 이번 시간(시스템이 초를 그림),
 * 오른쪽 큰 멈춤·시작 단추, 크게는 선 아래 "집중 항목 … + 추가"와 과목 목록(이름 · 오늘 시간 · 동그라미, 도는 과목은 채운 동그라미).
 * 과목 줄·단추를 누르면 앱을 열지 않고 시작·멈춤([ToggleFocusAction]). 아무것도 안 돌면 마지막에 한 과목을 "대기"로 둔다.
 */
@Composable
private fun ColumnScope.FocusPage(context: Context, subjects: List<FocusSubject>?, sessions: List<FocusSession>, running: FocusTimer.Running?) {
    val size = LocalSize.current
    val layout = WidgetLayout.forSize(size.width.value, size.height.value)
    val now = System.currentTimeMillis()
    val day = FocusDay.dayOf(now)
    val spans = sessions.map { FocusSpan(it.subjectId, it.startedAt, it.endedAt) } +
        listOfNotNull(running?.let { FocusSpan(it.subjectId, it.startedAt, now) })
    val total = FocusStats.focusTotal(spans, day)
    val bySubject = FocusStats.bySubject(spans, day)
    val list = subjects.orEmpty()
    if (layout == WidgetLayout.SMALL) {
        Text("오늘 누적", style = TextStyle(color = SubColor, fontSize = 12.sp), maxLines = 1)
        Text(formatHm(total), style = TextStyle(color = TextColor, fontSize = 18.sp, fontWeight = FontWeight.Bold), maxLines = 1)
        return
    }
    TitleRow("오늘의 집중") {
        Column(horizontalAlignment = Alignment.End) {
            Text("오늘 누적", style = TextStyle(color = SubColor, fontSize = 12.sp), maxLines = 1)
            Text(formatHm(total), style = TextStyle(color = TextColor, fontSize = 15.sp, fontWeight = FontWeight.Medium), maxLines = 1)
        }
    }
    if (subjects != null && list.isEmpty()) {
        Spacer(GlanceModifier.height(10.dp))
        Text("앱의 집중 칸에서 과목을 추가해 주세요", style = TextStyle(color = SubColor, fontSize = 13.sp))
        return
    }
    // 지금 과목: 도는 것, 아니면 오늘 마지막에 한 것, 아니면 첫 과목
    val currentId = running?.subjectId ?: sessions.maxByOrNull { it.endedAt }?.subjectId ?: list.firstOrNull()?.id
    val current = list.firstOrNull { it.id == currentId }
    Spacer(GlanceModifier.height(4.dp))
    Row(modifier = GlanceModifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
        Column(modifier = GlanceModifier.defaultWeight()) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Text(current?.name.orEmpty(), style = TextStyle(color = TextColor, fontSize = 14.sp, fontWeight = FontWeight.Medium), maxLines = 1)
                Text(if (running != null) " · 진행 중" else " · 대기", style = TextStyle(color = SubColor, fontSize = 14.sp), maxLines = 1)
            }
            if (running != null) {
                LiveClock(context, R.layout.widget_chrono_hero, now - running.startedAt)
            } else {
                Text("0:00", style = TextStyle(color = SubColor, fontSize = 32.sp, fontWeight = FontWeight.Bold), maxLines = 1)
            }
        }
        if (current != null) {
            val toggle = actionRunCallback<ToggleFocusAction>(actionParametersOf(SubjectParam to current.id))
            if (running != null) {
                RoundButton(context, R.drawable.widget_soft, R.drawable.ic_widget_pause, TextColor, "${current.name} 멈추기", "focus", 52.dp, toggle)
            } else {
                RoundButton(context, R.drawable.widget_cta, R.drawable.ic_widget_play, OnCtaColor, "${current.name} 시작", "focus", 52.dp, toggle)
            }
        }
    }
    if (layout != WidgetLayout.LARGE) return
    Spacer(GlanceModifier.height(12.dp))
    Hairline()
    Spacer(GlanceModifier.height(8.dp))
    Row(modifier = GlanceModifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
        Text("집중 항목", style = TextStyle(color = TextColor, fontSize = 14.sp, fontWeight = FontWeight.Medium), modifier = GlanceModifier.defaultWeight(), maxLines = 1)
        Text("+ 추가", style = TextStyle(color = TextColor, fontSize = 14.sp), modifier = GlanceModifier.clickable(actionStartActivity(openIntent(context, "focus"))), maxLines = 1)
    }
    // 과목이 많으면 밀어서 넘긴다
    ScrollList(list, itemId = { it.id.hashCode().toLong() }) { s ->
        val isRunning = running?.subjectId == s.id
        ChoiceRow(
            s.name, formatHm(bySubject[s.id] ?: 0L), TextColor, isRunning,
            actionRunCallback<ToggleFocusAction>(actionParametersOf(SubjectParam to s.id)),
            if (isRunning) "${s.name} 멈추기" else "${s.name} 시작",
        )
    }
}
