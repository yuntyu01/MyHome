package com.yuntyu.myhome.ui

import android.Manifest
import android.app.TimePickerDialog
import android.content.Intent
import android.net.Uri
import android.os.Build
import android.provider.Settings
import android.widget.Toast
import androidx.activity.compose.BackHandler
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.animation.AnimatedContent
import androidx.compose.animation.fadeIn
import androidx.compose.animation.fadeOut
import androidx.compose.animation.slideInHorizontally
import androidx.compose.animation.slideOutHorizontally
import androidx.compose.animation.togetherWith
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ColumnScope
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.imePadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.selection.DisableSelection
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberUpdatedState
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.saveable.Saver
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import com.yuntyu.myhome.MyHomeApp
import com.yuntyu.myhome.auth.AppLink
import com.yuntyu.myhome.core.HealthPlan
import com.yuntyu.myhome.core.api.ApiResult
import com.yuntyu.myhome.core.api.GymPlan
import com.yuntyu.myhome.core.api.HealthSettings
import com.yuntyu.myhome.core.api.MealSlot
import com.yuntyu.myhome.core.api.MyHomeJson
import com.yuntyu.myhome.core.api.Nutrition
import com.yuntyu.myhome.core.api.Recommendation
import com.yuntyu.myhome.core.api.SleepPlan
import com.yuntyu.myhome.health.Reminders
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import kotlinx.serialization.builtins.ListSerializer
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive

/**
 * 처음 설정(온보딩): 시작 → 로그인 → 결제 알림 읽기 → 앱 알림·정시 알람 → 건강 목표 → 끼니·헬스·취침 → 끝.
 * 처음 켤 때 한 번 뜨고(이미 한 쪽은 건너뛴다), 설정의 "처음 설정 다시 보기"로 모든 쪽을 다시 볼 수 있다.
 * 위치(헬스장 자동 출석·공부 장소)는 여기서 묻지 않는다: Play 정책상 그 기능을 켤 때 그 화면에서 설명하고 묻는다.
 */
enum class OnboardPage { WELCOME, LOGIN, MONEY, NOTIFY, BODY, ROUTINE, DONE }

/** 쪽을 고르는 데 쓰는 지금 상태. healthPersonal이 null이면 건강 설정을 아직 못 받았다(로그인 전 등) */
data class OnboardFacts(
    val hasToken: Boolean,
    val listenerOn: Boolean,
    val appsChosen: Boolean,
    val notifyOk: Boolean,
    val exactOk: Boolean,
    val healthPersonal: Boolean?,
)

/** 보여 줄 쪽. all이면 다(설정에서 다시 보기), 아니면 아직 안 한 것만. 시작·끝 쪽은 늘 있다 */
fun onboardPages(f: OnboardFacts, all: Boolean): List<OnboardPage> =
    if (all) OnboardPage.entries.toList() else buildList {
        add(OnboardPage.WELCOME)
        if (!f.hasToken) add(OnboardPage.LOGIN)
        if (!f.listenerOn || !f.appsChosen) add(OnboardPage.MONEY)
        if (!f.notifyOk || !f.exactOk) add(OnboardPage.NOTIFY)
        if (f.healthPersonal != true) { add(OnboardPage.BODY); add(OnboardPage.ROUTINE) }
        add(OnboardPage.DONE)
    }

/** 성별·나이·활동까지 넣었으면 건강 목표를 직접 정한 것(서버 기본값은 이 셋이 비어 있다) */
fun HealthSettings.personal(): Boolean = sex != null && age != null && activity != null

/** 처음 설정이 어디까지 왔는지. 앱 고르기·예산 화면에 다녀와도 이어지게 AppRoot가 들고 있다 */
class OnboardNav {
    /** 이번에 보여 줄 쪽(시작할 때 정한다). 비었으면 아직 안 정했다 */
    var pages by mutableStateOf<List<OnboardPage>>(emptyList())
    var current by mutableStateOf(OnboardPage.WELCOME)
    /** 설정에서 "다시 보기"로 열었으면 이미 한 것도 다 보여 준다 */
    var all by mutableStateOf(false)

    fun start(all: Boolean) {
        this.all = all
        pages = emptyList()
        current = OnboardPage.WELCOME
    }

    fun next() {
        pages.getOrNull(pages.indexOf(current) + 1)?.let { current = it }
    }

    /** 앞 쪽으로. 첫 쪽이면 false */
    fun prev(): Boolean {
        val i = pages.indexOf(current)
        if (i <= 0) return false
        current = pages[i - 1]
        return true
    }

    /** 끝 쪽에서 안 한 것을 누르면 그 쪽으로(목록에 없으면 순서대로 끼워 넣는다) */
    fun jump(p: OnboardPage) {
        if (p !in pages) pages = (pages + p).sortedBy { it.ordinal }
        current = p
    }

    /** 건강을 이미 정해 둔 걸 늦게 알았으면(설정을 받은 뒤) 건강 쪽을 뺀다. 지금 그 쪽에 있으면 둔다 */
    fun dropHealth() {
        if (all || current == OnboardPage.BODY || current == OnboardPage.ROUTINE) return
        pages = pages - OnboardPage.BODY - OnboardPage.ROUTINE
    }

    companion object {
        val Saver: Saver<OnboardNav, List<String>> = Saver(
            save = { n -> listOf(n.current.name, n.all.toString()) + n.pages.map { it.name } },
            restore = { v ->
                OnboardNav().apply {
                    current = runCatching { OnboardPage.valueOf(v[0]) }.getOrDefault(OnboardPage.WELCOME)
                    all = v.getOrNull(1) == "true"
                    pages = v.drop(2).mapNotNull { runCatching { OnboardPage.valueOf(it) }.getOrNull() }
                }
            },
        )
    }
}

/** 처음 설정 화면이 그리는 데 쓰는 상태(권한·로그인·건강 설정). 테스트에서는 가짜로 넣는다 */
data class OnboardStatus(
    val hasToken: Boolean = false,
    val loginMessage: String? = null,
    val listenerOn: Boolean = false,
    val batteryOk: Boolean = false,
    val appsChosen: Int = 0,
    val budgetSet: Boolean = false,
    val notifyOk: Boolean = false,
    /** 알림 허용을 물었는데 거절했다(이제는 설정 화면에서 켜야 한다) */
    val notifyDenied: Boolean = false,
    /** 정시 알람 권한이 따로 있는 폰(Android 12+) */
    val exactNeeded: Boolean = true,
    val exactOk: Boolean = false,
    /** 서버 건강 설정. null이면 아직 못 받았다 */
    val health: HealthSettings? = null,
    val healthError: String? = null,
) {
    val facts get() = OnboardFacts(hasToken, listenerOn, appsChosen > 0, notifyOk, !exactNeeded || exactOk, health?.personal())
}

/** 처음 설정 화면이 바깥에 시키는 일. 테스트에서는 가짜로 바꾼다 */
interface OnboardHost {
    fun login()
    fun allowListener()
    fun chooseApps()
    fun allowBattery()
    fun budgets()
    fun allowNotifications()
    fun allowExact()
    fun retryHealth()
    fun pickTime(initial: String, onPick: (String) -> Unit)
    suspend fun recommend(goal: String, weightKg: Double, heightCm: Int, age: Int, sex: String, activity: String): Result<Recommendation>
    /** 바꾼 필드만 보낸다. 실패하면 사용자에게 보여 줄 문장 */
    suspend fun saveHealth(patch: JsonObject): String?
    fun finish()
}

/** 실제 앱: 권한 상태를 읽고, 시스템 화면·서버를 부른다 */
@Composable
fun OnboardingScreen(modifier: Modifier, resumeTick: Int, nav: OnboardNav, onNavigate: (Screen) -> Unit, onFinish: () -> Unit) {
    val navigate by rememberUpdatedState(onNavigate)
    val finishNow by rememberUpdatedState(onFinish)
    val ctx = LocalContext.current
    val app = MyHomeApp.instance
    val settings by app.settings.state.collectAsState()
    val hasToken by app.tokenStore.hasToken.collectAsState()
    val summary by app.summaryRepo.state.collectAsState()
    val loginMessage by AppLink.message.collectAsState()
    var tick by remember { mutableIntStateOf(0) }
    val listenerOn = remember(resumeTick) { PermissionStatus.listenerEnabled(ctx) }
    val batteryOk = remember(resumeTick) { PermissionStatus.batteryUnrestricted(ctx) }
    val notifyOk = remember(resumeTick, tick) { PermissionStatus.notificationsOn(ctx) }
    val exactOk = remember(resumeTick) { Reminders.canExact(ctx) }
    var notifyDenied by rememberSaveable { mutableStateOf(false) }
    var health by remember { mutableStateOf<HealthSettings?>(null) }
    var healthError by remember { mutableStateOf<String?>(null) }
    var healthTick by remember { mutableIntStateOf(0) }
    LaunchedEffect(hasToken, healthTick) {
        if (!hasToken) return@LaunchedEffect
        healthError = null
        when (val r = withContext(Dispatchers.IO) { app.api.healthSettings() }) {
            is ApiResult.Ok -> health = MyHomeJson.decodeFromJsonElement(HealthSettings.serializer(), r.value.settings)
            else -> healthError = r.plain()
        }
    }
    // 정시 알람을 허용하고 돌아오면 다음 알림을 제시각 알람으로 다시 건다
    LaunchedEffect(exactOk) { Reminders.reschedule(ctx) }
    // 알림 읽기를 허용하고 돌아오면 배터리 절전 예외를 이어서 묻는다(홈의 처음 설정과 같은 흐름)
    var askBattery by rememberSaveable { mutableStateOf(false) }
    LaunchedEffect(resumeTick, listenerOn) {
        if (askBattery && listenerOn) {
            askBattery = false
            if (!batteryOk) PermissionStatus.requestBatteryExemption(ctx)
        }
    }
    val notifLauncher = rememberLauncherForActivityResult(ActivityResultContracts.RequestPermission()) { granted ->
        if (!granted) notifyDenied = true
        tick++
    }
    val status = OnboardStatus(
        hasToken = hasToken,
        loginMessage = loginMessage,
        listenerOn = listenerOn,
        batteryOk = batteryOk,
        appsChosen = settings.selectedPackages.size,
        budgetSet = (summary.summary?.totals?.budget ?: 0L) > 0,
        notifyOk = notifyOk,
        notifyDenied = notifyDenied,
        exactNeeded = Build.VERSION.SDK_INT >= Build.VERSION_CODES.S,
        exactOk = exactOk,
        health = health,
        healthError = healthError,
    )
    val host = remember(ctx) {
        object : OnboardHost {
            override fun login() {
                if (!AppLink.start(ctx)) {
                    Toast.makeText(ctx, "서버 주소가 없어요. 문제 해결에서 넣어 주세요", Toast.LENGTH_LONG).show()
                    navigate(Screen.STATUS)
                }
            }
            override fun allowListener() { askBattery = true; PermissionStatus.openListenerSettings(ctx) }
            override fun chooseApps() = navigate(Screen.APPS)
            override fun allowBattery() = PermissionStatus.requestBatteryExemption(ctx)
            override fun budgets() = navigate(Screen.BUDGETS)
            override fun allowNotifications() {
                // Android 13+는 한 번 묻고, 거절했으면 설정 화면에서 켜야 한다. 그 아래는 앱 알림을 꺼 둔 경우라 설정 화면으로
                if (Build.VERSION.SDK_INT >= 33 && !PermissionStatus.canPostNotifications(ctx) && !notifyDenied) {
                    app.settings.notifAsked = true
                    notifLauncher.launch(Manifest.permission.POST_NOTIFICATIONS)
                } else {
                    PermissionStatus.openNotificationSettings(ctx)
                }
            }
            override fun allowExact() {
                runCatching {
                    ctx.startActivity(Intent(Settings.ACTION_REQUEST_SCHEDULE_EXACT_ALARM, Uri.parse("package:${ctx.packageName}")))
                }.onFailure { Toast.makeText(ctx, "설정 화면을 열지 못했어요", Toast.LENGTH_SHORT).show() }
            }
            override fun retryHealth() { healthTick++ }
            override fun pickTime(initial: String, onPick: (String) -> Unit) {
                val mins = runCatching { HealthPlan.minutes(initial) }.getOrDefault(12 * 60)
                TimePickerDialog(ctx, { _, h, mi -> onPick(HealthPlan.hhmm(h * 60 + mi)) }, mins / 60, mins % 60, true).show()
            }
            override suspend fun recommend(goal: String, weightKg: Double, heightCm: Int, age: Int, sex: String, activity: String): Result<Recommendation> =
                withContext(Dispatchers.IO) {
                    val mode = if (goal == "keep" || goal == "lose") "balanced" else "training"
                    when (val r = app.api.recommendTargets(goal, weightKg, heightCm, age, sex, activity, mode)) {
                        is ApiResult.Ok -> Result.success(r.value)
                        else -> Result.failure(IllegalStateException(r.plain()))
                    }
                }
            override suspend fun saveHealth(patch: JsonObject): String? {
                val r = withContext(Dispatchers.IO) { app.api.putHealthSettings(patch) }
                if (r !is ApiResult.Ok) return r.plain()
                val saved = MyHomeJson.decodeFromJsonElement(HealthSettings.serializer(), r.value.settings)
                health = saved
                app.healthLocal.updateFrom(saved)
                Reminders.reschedule(ctx)
                return null
            }
            override fun finish() = finishNow()
        }
    }
    OnboardingContent(modifier, nav, status, host)
}

/** 쪽을 고르고 넘긴다. 바깥 일은 host에 시킨다(테스트가 이것을 그린다) */
@Composable
fun OnboardingContent(modifier: Modifier, nav: OnboardNav, status: OnboardStatus, host: OnboardHost) {
    LaunchedEffect(nav.pages.isEmpty()) {
        if (nav.pages.isEmpty()) nav.pages = onboardPages(status.facts, nav.all)
    }
    // 건강을 이미 정해 둔 걸 설정을 받고서야 알면 건강 쪽을 뺀다
    LaunchedEffect(status.health?.personal()) {
        if (status.health?.personal() == true && nav.pages.isNotEmpty()) nav.dropHealth()
    }
    BackHandler(enabled = nav.pages.indexOf(nav.current) > 0) { nav.prev() }
    AnimatedContent(
        targetState = nav.current,
        transitionSpec = {
            val dir = if (targetState.ordinal >= initialState.ordinal) 1 else -1
            (slideInHorizontally { it * dir / 5 } + fadeIn()) togetherWith (slideOutHorizontally { -it * dir / 5 } + fadeOut())
        },
        modifier = modifier.fillMaxSize(),
        label = "onboard",
    ) { page ->
        val middle = nav.pages.filter { it != OnboardPage.WELCOME && it != OnboardPage.DONE }
        val progress = middle.indexOf(page).takeIf { it >= 0 }?.let { it to middle.size }
        val back: (() -> Unit)? = if (nav.pages.indexOf(page) > 0) ({ nav.prev() }) else null
        when (page) {
            OnboardPage.WELCOME -> WelcomePage(onStart = { nav.next() }, onSkip = { host.finish() })
            OnboardPage.LOGIN -> LoginPage(status, host, progress, back) { nav.next() }
            OnboardPage.MONEY -> MoneyPage(status, host, progress, back) { nav.next() }
            OnboardPage.NOTIFY -> NotifyPage(status, host, progress, back) { nav.next() }
            OnboardPage.BODY -> BodyPage(status, host, progress, back, onLogin = { nav.jump(OnboardPage.LOGIN) }) { nav.next() }
            OnboardPage.ROUTINE -> RoutinePage(status, host, progress, back, onLogin = { nav.jump(OnboardPage.LOGIN) }) { nav.next() }
            OnboardPage.DONE -> DonePage(status, back, onJump = { nav.jump(it) }, onFinish = { host.finish() })
        }
    }
}

/**
 * 한 쪽의 틀: 위에 뒤로·진행 칸·나중에, 가운데 제목·설명·내용(스크롤), 아래 큰 버튼.
 * progress는 (지금 순서, 전체) — 시작·끝 쪽은 null
 */
@Composable
private fun PageFrame(
    title: String,
    sub: String?,
    button: String,
    onButton: () -> Unit,
    progress: Pair<Int, Int>? = null,
    onBack: (() -> Unit)? = null,
    onSkip: (() -> Unit)? = null,
    skipLabel: String = "나중에",
    buttonEnabled: Boolean = true,
    content: @Composable ColumnScope.() -> Unit = {},
) {
    val m = LocalModo.current
    Column(Modifier.fillMaxSize().imePadding()) {
        Row(Modifier.fillMaxWidth().height(56.dp).padding(start = 4.dp, end = 8.dp), verticalAlignment = Alignment.CenterVertically) {
            if (onBack != null) {
                IconButton(onClick = onBack) { Icon(ChevronLeftIcon, contentDescription = "앞으로", modifier = Modifier.size(24.dp)) }
            } else {
                Spacer(Modifier.width(48.dp))
            }
            Box(Modifier.weight(1f).padding(horizontal = 8.dp)) {
                if (progress != null) ProgressSegments(progress.first, progress.second)
            }
            if (onSkip != null) {
                DisableSelection {
                    Text(
                        skipLabel, fontSize = 15.sp, color = m.muted,
                        modifier = Modifier.clip(RoundedCornerShape(10.dp)).tap(role = Role.Button, onClick = onSkip).padding(horizontal = 12.dp, vertical = 10.dp),
                    )
                }
            } else {
                Spacer(Modifier.width(48.dp))
            }
        }
        Column(Modifier.weight(1f).verticalScroll(rememberScrollState())) {
            Column(Modifier.padding(start = Gutter, end = Gutter, top = 20.dp), verticalArrangement = Arrangement.spacedBy(10.dp)) {
                Text(title, fontSize = 28.sp, fontWeight = FontWeight.Bold, lineHeight = 38.sp)
                if (sub != null) Text(sub, fontSize = 16.sp, color = m.muted, lineHeight = 24.sp)
            }
            Spacer(Modifier.height(24.dp))
            content()
            Spacer(Modifier.height(16.dp))
        }
        PrimaryButton(button, Modifier.padding(horizontal = Gutter, vertical = 12.dp), enabled = buttonEnabled, onClick = onButton)
    }
}

@Composable
private fun ProgressSegments(index: Int, total: Int) {
    val m = LocalModo.current
    Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.spacedBy(4.dp)) {
        for (i in 0 until total) {
            Box(Modifier.weight(1f).height(4.dp).clip(RoundedCornerShape(2.dp)).background(if (i <= index) m.cta else m.line))
        }
    }
}

/** 할 일 한 줄: 왼쪽 체크(했으면)나 번호, 제목·설명. 안 했으면 눌러서 바로 한다 */
@Composable
private fun TaskRow(n: Int, title: String, sub: String, done: Boolean, current: Boolean, optional: Boolean = false, onClick: (() -> Unit)?) {
    val m = LocalModo.current
    Row(
        Modifier.fillMaxWidth()
            .then(if (onClick != null && (!done || optional)) Modifier.tap(onClick = onClick) else Modifier)
            .padding(horizontal = Gutter, vertical = 16.dp),
        horizontalArrangement = Arrangement.spacedBy(16.dp),
    ) {
        Box(Modifier.width(24.dp).padding(top = 1.dp), contentAlignment = Alignment.TopCenter) {
            if (done) {
                Icon(CheckIcon, contentDescription = "했어요", tint = m.cta, modifier = Modifier.size(22.dp))
            } else {
                Text("$n", fontSize = 17.sp, fontWeight = if (current) FontWeight.Bold else FontWeight.Normal, color = if (current) MaterialTheme.colorScheme.onBackground else m.faint)
            }
        }
        Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(3.dp)) {
            Text(
                title + if (optional && !done) " (나중에 해도 돼요)" else "",
                fontSize = 17.sp,
                fontWeight = if (current) FontWeight.Bold else FontWeight.Normal,
                color = if (current || !done) MaterialTheme.colorScheme.onBackground else m.muted,
            )
            Text(sub, fontSize = 14.sp, color = m.muted, lineHeight = 21.sp)
        }
    }
}

@Composable
private fun FeatureRow(icon: ImageVector, title: String, sub: String) {
    val m = LocalModo.current
    Row(Modifier.fillMaxWidth().padding(horizontal = Gutter, vertical = 14.dp), horizontalArrangement = Arrangement.spacedBy(16.dp)) {
        Box(Modifier.size(40.dp).clip(CircleShape).background(m.pill), contentAlignment = Alignment.Center) {
            Icon(icon, contentDescription = null, tint = MaterialTheme.colorScheme.onBackground, modifier = Modifier.size(20.dp))
        }
        Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(3.dp)) {
            Text(title, fontSize = 17.sp, fontWeight = FontWeight.SemiBold)
            Text(sub, fontSize = 14.sp, color = m.muted, lineHeight = 21.sp)
        }
    }
}

@Composable
private fun WelcomePage(onStart: () -> Unit, onSkip: () -> Unit) {
    PageFrame(
        title = "가계부·건강·집중을\n한 앱에서",
        sub = "처음 한 번만 설정해 두면 결제는 저절로 적히고, 끼니·헬스·취침 알림이 제때 와요. 2분이면 끝나요.",
        button = "시작하기",
        onButton = onStart,
        onSkip = onSkip,
        skipLabel = "건너뛰기",
    ) {
        FeatureRow(ListLineIcon, "가계부", "카드·은행 알림을 읽어서 쓴 돈을 바로 적고, 분류별 남은 돈을 보여 줘요")
        FeatureRow(HealthIcon, "건강", "끼니를 찍으면 영양을 계산하고, 끼니 마감·헬스·취침 전에 알려 줘요")
        FeatureRow(TimerIcon, "집중", "공부 시간을 재서 하루 순공과 과목별 시간을 모아 봐요")
    }
}

@Composable
private fun LoginPage(s: OnboardStatus, host: OnboardHost, progress: Pair<Int, Int>?, back: (() -> Unit)?, next: () -> Unit) {
    PageFrame(
        title = "먼저 로그인해요",
        sub = "가계부와 건강 기록은 내 서버에 저장돼요. 브라우저에서 이메일로 받은 코드를 넣으면 이 폰이 연결돼요.",
        button = if (s.hasToken) "다음" else "이메일로 로그인하기",
        onButton = { if (s.hasToken) next() else host.login() },
        progress = progress, onBack = back, onSkip = if (s.hasToken) null else next,
    ) {
        TaskRow(1, "로그인", if (s.hasToken) "연결됐어요" else s.loginMessage ?: "로그인하면 앱으로 돌아와요", s.hasToken, current = !s.hasToken, onClick = host::login)
    }
}

@Composable
private fun MoneyPage(s: OnboardStatus, host: OnboardHost, progress: Pair<Int, Int>?, back: (() -> Unit)?, next: () -> Unit) {
    // 해야 할 것 순서: 알림 읽기 → 앱 고르기 → 절전 예외. 예산은 나중에 해도 된다
    val tasks = listOf(
        Triple(s.listenerOn, "알림 읽기 허용하기", host::allowListener),
        Triple(s.appsChosen > 0, "카드·은행 앱 고르기", host::chooseApps),
        Triple(s.batteryOk, "절전 예외 허용하기", host::allowBattery),
    )
    val current = tasks.indexOfFirst { !it.first }
    PageFrame(
        title = "쓴 돈을 자동으로\n적어 드릴게요",
        sub = "카드·은행 앱 알림을 읽어서 가계부에 바로 적어요. 알림 글은 내 서버로만 가요.",
        button = tasks.getOrNull(current)?.second ?: "다음",
        onButton = { tasks.getOrNull(current)?.third?.invoke() ?: next() },
        progress = progress, onBack = back, onSkip = if (current >= 0) next else null,
    ) {
        TaskRow(
            1, "결제 알림 읽기 허용",
            if (s.listenerOn) "허용했어요" else "설정 화면에서 MyHome을 켜 주세요. 돌아오면 절전 예외도 이어서 물어봐요",
            s.listenerOn, current = current == 0, onClick = host::allowListener,
        )
        Hairline(Modifier.padding(horizontal = Gutter))
        TaskRow(
            2, "카드·은행 앱 고르기",
            if (s.appsChosen > 0) "${s.appsChosen}개 골랐어요 · 눌러서 바꾸기" else "KB Pay, 토스처럼 결제 알림이 오는 앱",
            s.appsChosen > 0, current = current == 1, optional = s.appsChosen > 0, onClick = host::chooseApps,
        )
        Hairline(Modifier.padding(horizontal = Gutter))
        TaskRow(
            3, "배터리 절전 예외",
            if (s.batteryOk) "허용했어요" else "폰이 MyHome을 재우면 결제 알림을 놓칠 수 있어요",
            s.batteryOk, current = current == 2, onClick = host::allowBattery,
        )
        Hairline(Modifier.padding(horizontal = Gutter))
        TaskRow(
            4, "한 달 예산", if (s.budgetSet) "정했어요 · 눌러서 바꾸기" else "분류별로 정하면 남은 돈을 보여 줘요",
            s.budgetSet, current = false, optional = true, onClick = host::budgets,
        )
    }
}

@Composable
private fun NotifyPage(s: OnboardStatus, host: OnboardHost, progress: Pair<Int, Int>?, back: (() -> Unit)?, next: () -> Unit) {
    val needExact = s.exactNeeded && !s.exactOk
    val action: (() -> Unit)? = when {
        !s.notifyOk -> host::allowNotifications
        needExact -> host::allowExact
        else -> null
    }
    PageFrame(
        title = "알림을 제때\n받으려면",
        sub = "끼니 마감 30분 전, 헬스 도착 1시간 전, 취침 시각, 예산 80%·초과, 자동매매 실패를 알려 드려요. 무엇을 받을지는 설정에서 하나씩 끌 수 있어요.",
        button = when {
            !s.notifyOk -> if (s.notifyDenied) "설정에서 알림 켜기" else "알림 허용하기"
            needExact -> "알람 및 리마인더 허용하기"
            else -> "다음"
        },
        onButton = { action?.invoke() ?: next() },
        progress = progress, onBack = back, onSkip = if (action != null) next else null,
    ) {
        TaskRow(
            1, "앱 알림 허용",
            when {
                s.notifyOk -> "허용했어요"
                s.notifyDenied -> "거절해서 이제는 설정 화면에서 MyHome 알림을 켜야 해요"
                else -> "누르면 허용할지 물어봐요"
            },
            s.notifyOk, current = !s.notifyOk, onClick = host::allowNotifications,
        )
        if (s.exactNeeded) {
            Hairline(Modifier.padding(horizontal = Gutter))
            TaskRow(
                2, "알람 및 리마인더 허용",
                if (s.exactOk) "허용했어요" else "허용하지 않으면 폰이 쉬는 동안 알림이 늦어져서, 마감 30분 전 알림이 마감 뒤에 올 수 있어요",
                s.exactOk, current = s.notifyOk && !s.exactOk, onClick = host::allowExact,
            )
        }
    }
}

/** 건강 쪽이 서버 설정을 못 쓸 때(로그인 전, 받는 중, 실패) 보여 줄 틀. 쓸 수 있으면 null */
@Composable
private fun healthBlocked(s: OnboardStatus, title: String, progress: Pair<Int, Int>?, back: (() -> Unit)?, onLogin: () -> Unit, next: () -> Unit): Boolean {
    val (sub, button, action) = when {
        !s.hasToken -> Triple("건강 기록은 서버에 저장돼서 로그인해야 정할 수 있어요.", "로그인하러 가기", onLogin)
        s.health == null && s.healthError != null -> Triple("설정을 받지 못했어요: ${s.healthError}", "다음", next)
        s.health == null -> Triple("설정을 받는 중이에요…", "다음", next)
        else -> return false
    }
    PageFrame(title, sub, button, action, progress, back, onSkip = next)
    return true
}

@Composable
private fun BodyPage(s: OnboardStatus, host: OnboardHost, progress: Pair<Int, Int>?, back: (() -> Unit)?, onLogin: () -> Unit, next: () -> Unit) {
    val title = "건강 목표를 정해요"
    if (healthBlocked(s, title, progress, back, onLogin, next)) return
    val st = s.health!!
    val scope = rememberCoroutineScope()
    val ctx = LocalContext.current
    val m = LocalModo.current
    // 직접 정해 둔 적이 있으면 그 값으로 채운다(서버 기본 키·몸무게는 내 값이 아니라 비운다)
    val mine = st.personal()
    var goal by rememberSaveable { mutableStateOf(st.goal) }
    var sex by rememberSaveable { mutableStateOf(st.sex) }
    var height by rememberSaveable { mutableStateOf(if (mine && st.heightCm > 0) "${st.heightCm}" else "") }
    var weight by rememberSaveable { mutableStateOf(if (mine && st.weightKg > 0) st.weightKg.let { if (it % 1.0 == 0.0) "${it.toInt()}" else "$it" } else "") }
    var age by rememberSaveable { mutableStateOf(st.age?.toString() ?: "") }
    var activity by rememberSaveable { mutableStateOf(st.activity) }
    var rec by remember { mutableStateOf<Recommendation?>(null) }
    var busy by remember { mutableStateOf(false) }
    var error by remember { mutableStateOf<String?>(null) }
    val h = height.toIntOrNull()?.takeIf { it in 100..250 }
    val w = weight.toDoubleOrNull()?.takeIf { it in 20.0..400.0 }
    val a = age.toIntOrNull()?.takeIf { it in 10..100 }
    val ready = h != null && w != null && a != null && sex != null && activity != null
    fun changed() { rec = null; error = null }

    PageFrame(
        title = title,
        sub = "몸 정보와 평소 활동으로 하루 칼로리와 탄단지 목표를 계산해요. 나중에 건강 설정에서 바꿀 수 있어요.",
        button = when {
            busy -> "잠시만요…"
            rec != null -> "이 목표로 저장"
            else -> "목표 계산하기"
        },
        buttonEnabled = !busy && ready,
        onButton = {
            val r = rec
            busy = true
            scope.launch {
                if (r == null) {
                    host.recommend(goal, w!!, h!!, a!!, sex!!, activity!!)
                        .onSuccess { rec = it; error = null }
                        .onFailure { error = it.message }
                } else if (r.kcal < 1200) {
                    error = "목표가 1,200 kcal보다 낮게 나와요. 건강 설정에서 직접 정해 주세요"
                } else {
                    val patch = JsonObject(
                        mapOf(
                            "goal" to JsonPrimitive(goal), "heightCm" to JsonPrimitive(h!!), "weightKg" to JsonPrimitive(w!!),
                            "age" to JsonPrimitive(a!!), "sex" to JsonPrimitive(sex!!), "activity" to JsonPrimitive(activity!!),
                            "targets" to MyHomeJson.encodeToJsonElement(Nutrition.serializer(), Nutrition(r.kcal, r.carbs, r.protein, r.fat)),
                        ),
                    )
                    val err = host.saveHealth(patch)
                    if (err == null) {
                        Toast.makeText(ctx, "목표를 저장했어요", Toast.LENGTH_SHORT).show()
                        next()
                    } else {
                        error = err
                    }
                }
                busy = false
            }
        },
        progress = progress, onBack = back, onSkip = next,
    ) {
        Column(Modifier.padding(horizontal = Gutter), verticalArrangement = Arrangement.spacedBy(10.dp)) {
            Label("목표")
            Row(horizontalArrangement = Arrangement.spacedBy(6.dp)) {
                for ((k, l) in GOALS) Chip(l, goal == k, Modifier.weight(1f)) { goal = k; changed() }
            }
            Label("성별")
            Row(horizontalArrangement = Arrangement.spacedBy(6.dp)) {
                Chip("남성", sex == "male", Modifier.weight(1f)) { sex = "male"; changed() }
                Chip("여성", sex == "female", Modifier.weight(1f)) { sex = "female"; changed() }
            }
            Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                LineField(height, { v -> height = v.filter(Char::isDigit).take(3); changed() }, "키(cm)", Modifier.weight(1f), keyboardType = KeyboardType.Number)
                LineField(weight, { v -> weight = v.filter { it.isDigit() || it == '.' }.take(5); changed() }, "몸무게(kg)", Modifier.weight(1f), keyboardType = KeyboardType.Decimal)
                LineField(age, { v -> age = v.filter(Char::isDigit).take(3); changed() }, "나이", Modifier.weight(1f), keyboardType = KeyboardType.Number)
            }
            Label("평소 활동")
            for ((k, l) in ACTIVITIES) Chip(l, activity == k, Modifier.fillMaxWidth()) { activity = k; changed() }
            if (!ready && (height.isNotEmpty() || weight.isNotEmpty() || age.isNotEmpty())) {
                Text("키 100~250cm, 몸무게 20~400kg, 나이 10~100세로 넣고 성별·활동을 골라 주세요", fontSize = 13.sp, color = m.muted)
            }
            rec?.let { r ->
                Spacer(Modifier.height(4.dp))
                Column(
                    Modifier.fillMaxWidth().clip(RoundedCornerShape(14.dp)).background(m.pill).padding(16.dp),
                    verticalArrangement = Arrangement.spacedBy(4.dp),
                ) {
                    Text("하루 ${num(r.kcal)} kcal", fontSize = 22.sp, fontWeight = FontWeight.Bold)
                    Text("탄 ${num(r.carbs)}g · 단 ${num(r.protein)}g · 지 ${num(r.fat)}g", fontSize = 15.sp)
                    Text("유지 칼로리 약 ${num(r.maintenanceKcal.toInt())} kcal 기준", fontSize = 13.sp, color = m.muted)
                }
            }
            error?.let { Text(it, fontSize = 14.sp, color = m.over) }
        }
    }
}

@Composable
private fun RoutinePage(s: OnboardStatus, host: OnboardHost, progress: Pair<Int, Int>?, back: (() -> Unit)?, onLogin: () -> Unit, next: () -> Unit) {
    val title = "끼니·헬스·취침\n시간을 정해요"
    if (healthBlocked(s, title, progress, back, onLogin, next)) return
    val st = s.health!!
    val scope = rememberCoroutineScope()
    val ctx = LocalContext.current
    val m = LocalModo.current
    var meals by remember { mutableStateOf(st.meals.ifEmpty { HealthPlan.resizeMeals(emptyList(), 3) }) }
    var gym by remember { mutableStateOf(st.gym ?: GymPlan()) }
    var sleep by remember { mutableStateOf(st.sleep ?: SleepPlan()) }
    var busy by remember { mutableStateOf(false) }
    var error by remember { mutableStateOf<String?>(null) }
    val gymOk = !gym.enabled || gym.days.isNotEmpty()

    PageFrame(
        title = title,
        sub = "정한 시각에 맞춰 알려 드려요. 끼니는 마감 30분 전과 마감 때, 헬스는 도착 1시간 전, 취침은 그 시각에요.",
        button = if (busy) "잠시만요…" else "저장하고 다음",
        buttonEnabled = !busy && gymOk,
        onButton = {
            busy = true
            scope.launch {
                val j = MyHomeJson
                val g = gym.copy(weeklyTarget = gym.weeklyTarget.takeIf { it in 1..gym.days.size } ?: gym.days.size)
                val patch = JsonObject(
                    mapOf<String, JsonElement>(
                        "meals" to j.encodeToJsonElement(ListSerializer(MealSlot.serializer()), meals),
                        "gym" to j.encodeToJsonElement(GymPlan.serializer(), g),
                        "sleep" to j.encodeToJsonElement(SleepPlan.serializer(), sleep),
                    ),
                )
                val err = host.saveHealth(patch)
                busy = false
                if (err == null) {
                    Toast.makeText(ctx, "시간을 저장했어요", Toast.LENGTH_SHORT).show()
                    next()
                } else {
                    error = err
                }
            }
        },
        progress = progress, onBack = back, onSkip = next,
    ) {
        Column(Modifier.padding(horizontal = Gutter), verticalArrangement = Arrangement.spacedBy(10.dp)) {
            Label("하루 끼니")
            Row(horizontalArrangement = Arrangement.spacedBy(6.dp)) {
                for (n in 3..6) Chip("${n}끼", meals.size == n, Modifier.weight(1f)) { meals = HealthPlan.resizeMeals(meals, n) }
            }
            for (slot in meals) {
                TimeRow("${slot.index}끼 마감", slot.deadline) {
                    host.pickTime(slot.deadline) { t -> meals = meals.map { if (it.index == slot.index) MealSlot(it.index, t) else it } }
                }
            }
        }
        Spacer(Modifier.height(8.dp))
        SwitchRow("헬스 일정", gym.enabled, sub = "가는 요일과 도착 시간") { gym = gym.copy(enabled = it) }
        if (gym.enabled) {
            Column(Modifier.padding(horizontal = Gutter), verticalArrangement = Arrangement.spacedBy(10.dp)) {
                Row(horizontalArrangement = Arrangement.spacedBy(4.dp)) {
                    WEEKDAYS.forEachIndexed { i, wd ->
                        Chip(wd, i in gym.days, Modifier.weight(1f)) {
                            gym = gym.copy(days = if (i in gym.days) gym.days - i else (gym.days + i).sorted())
                        }
                    }
                }
                if (!gymOk) Text("가는 요일을 하나 이상 골라 주세요", fontSize = 13.sp, color = m.over)
                TimeRow("도착 시간", gym.arriveBy) { host.pickTime(gym.arriveBy) { t -> gym = gym.copy(arriveBy = t) } }
            }
        }
        SwitchRow("취침 알림", sleep.enabled, sub = "잘 시간에 알려 줘요") { sleep = sleep.copy(enabled = it) }
        if (sleep.enabled) {
            Column(Modifier.padding(horizontal = Gutter), verticalArrangement = Arrangement.spacedBy(10.dp)) {
                TimeRow("취침 시각", sleep.bedTime) { host.pickTime(sleep.bedTime) { t -> sleep = sleep.copy(bedTime = t) } }
                TimeRow("기상 시각", sleep.wakeTime) { host.pickTime(sleep.wakeTime) { t -> sleep = sleep.copy(wakeTime = t) } }
            }
        }
        error?.let { Text(it, Modifier.padding(horizontal = Gutter, vertical = 8.dp), fontSize = 14.sp, color = m.over) }
    }
}

@Composable
private fun DonePage(s: OnboardStatus, back: (() -> Unit)?, onJump: (OnboardPage) -> Unit, onFinish: () -> Unit) {
    val m = LocalModo.current
    val f = s.facts
    val left = buildList {
        if (!f.hasToken) add(Triple("로그인", "가계부와 건강 기록을 저장하려면 필요해요", OnboardPage.LOGIN))
        if (!f.listenerOn || !f.appsChosen) add(Triple("결제 알림 읽기", "켜야 쓴 돈이 자동으로 적혀요", OnboardPage.MONEY))
        if (!f.notifyOk || !f.exactOk) add(Triple("알림·알람", "끼니·헬스·취침 알림이 제때 오게 해요", OnboardPage.NOTIFY))
        if (f.healthPersonal != true) add(Triple("건강 목표", "하루 칼로리와 탄단지 목표", OnboardPage.BODY))
    }
    PageFrame(
        title = if (left.isEmpty()) "준비됐어요" else "거의 다 됐어요",
        sub = if (left.isEmpty()) "이제 결제는 저절로 적히고, 알림은 정한 시각에 와요." else "남은 건 지금 눌러서 하거나, 나중에 설정 > 처음 설정 다시 보기에서 할 수 있어요.",
        button = "MyHome 시작하기",
        onButton = onFinish,
        onBack = back,
    ) {
        left.forEachIndexed { i, (title, sub, page) ->
            FlatRow(title, sub = sub, value = "아직", valueColor = m.over, onClick = { onJump(page) })
            if (i < left.lastIndex) Hairline(Modifier.padding(horizontal = Gutter))
        }
        if (left.isNotEmpty()) Spacer(Modifier.height(16.dp))
        Text(
            "헬스장에 도착하면 저절로 출석하는 기능과 공부 장소에 머문 시간은 위치가 필요해서, 쓰고 싶을 때 헬스·집중 화면에서 따로 켜요.",
            Modifier.padding(horizontal = Gutter), fontSize = 14.sp, color = m.muted, lineHeight = 21.sp,
        )
    }
}

@Composable
private fun Label(text: String) {
    Text(text, fontSize = 14.sp, color = LocalModo.current.muted, modifier = Modifier.padding(top = 6.dp))
}

/** 시각 한 줄: 왼쪽 이름, 오른쪽 시각, 누르면 시각 고르기 */
@Composable
private fun TimeRow(title: String, value: String, onClick: () -> Unit) {
    val m = LocalModo.current
    Row(
        Modifier.fillMaxWidth().clip(RoundedCornerShape(12.dp)).tap(onClick = onClick).padding(vertical = 10.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Text(title, Modifier.weight(1f), fontSize = 16.sp)
        Text(value, fontSize = 16.sp, fontWeight = FontWeight.SemiBold)
        Icon(ChevronRightIcon, contentDescription = null, tint = m.faint, modifier = Modifier.padding(start = 6.dp).size(18.dp))
    }
}
