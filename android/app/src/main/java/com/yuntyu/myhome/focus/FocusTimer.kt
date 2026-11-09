package com.yuntyu.myhome.focus

import android.app.PendingIntent
import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import androidx.core.app.NotificationCompat
import androidx.core.app.NotificationManagerCompat
import androidx.core.content.edit
import androidx.glance.appwidget.updateAll
import com.yuntyu.myhome.MyHomeApp
import com.yuntyu.myhome.R
import com.yuntyu.myhome.core.FocusSubjects
import com.yuntyu.myhome.data.FocusSession
import com.yuntyu.myhome.data.FocusSubject
import com.yuntyu.myhome.notify.Notifications
import com.yuntyu.myhome.ui.MainActivity
import com.yuntyu.myhome.widget.SummaryWidget
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.launch
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import java.util.UUID

/**
 * 집중 타이머(열품타처럼 과목을 누르면 재고, 다시 누르면 멈춘다). 한 번에 하나만 돈다.
 * 도는 중인 것(과목, 시작 시각)은 설정에 남겨서 앱이 꺼지거나 폰을 껐다 켜도 이어서 잰다(시작 시각에서 지금까지로 센다).
 * 멈추면 기록 한 줄(FocusSession)이 되고 서버로 보낸다(FocusSync). 도는 동안은 시계가 가는 알림을 띄운다(멈추기 버튼).
 */
object FocusTimer {
    data class Running(val subjectId: String, val startedAt: Long)

    private const val PREFS = "focus_timer"
    const val NOTIFY_ID = 4101
    /** 서버가 하루(24시간)보다 긴 기록을 받지 않는다. 깜빡 켜 둔 것은 이만큼만 남긴다 */
    private const val MAX_SESSION_MS = 24 * 3_600_000L - 60_000L
    /** 실수로 두 번 누른 것은 남기지 않는다 */
    private const val MIN_SESSION_MS = 1_000L

    private val running = MutableStateFlow<Running?>(null)
    private var loaded = false
    /** 빠르게 여러 번 눌러도 시작·멈춤이 섞이지 않게 하나씩 */
    private val lock = Mutex()

    private fun prefs(ctx: Context) = ctx.getSharedPreferences(PREFS, Context.MODE_PRIVATE)
    private fun dao() = MyHomeApp.instance.db.focus()

    /** 앱 시작 때 부른다: 도는 중이던 타이머를 되살리고 알림을 다시 띄운다(재부팅 뒤에도) */
    fun init(ctx: Context) {
        load(ctx)
        val r = running.value ?: return
        MyHomeApp.instance.ioScope.launch {
            val s = dao().subject(r.subjectId)
            if (s == null || s.archived) stop(ctx) else showRunning(ctx, r, s)
        }
    }

    private fun load(ctx: Context) {
        if (loaded) return
        loaded = true
        val p = prefs(ctx)
        val id = p.getString("subject", null)
        val at = p.getLong("started", 0L)
        running.value = if (id != null && at > 0) Running(id, at) else null
    }

    fun running(ctx: Context): StateFlow<Running?> {
        load(ctx)
        return running.asStateFlow()
    }

    /** 누른 과목이 돌고 있으면 멈추고, 아니면(다른 과목이 돌고 있으면 그것을 멈추고) 시작한다 */
    suspend fun toggle(ctx: Context, subjectId: String) = lock.withLock {
        load(ctx)
        val cur = running.value
        stopLocked(ctx)
        if (cur?.subjectId == subjectId) return@withLock
        val s = dao().subject(subjectId) ?: return@withLock
        val r = Running(subjectId, System.currentTimeMillis())
        prefs(ctx).edit { putString("subject", r.subjectId); putLong("started", r.startedAt) }
        running.value = r
        showRunning(ctx, r, s)
        refreshWidget(ctx)
    }

    /** 멈추고 기록을 남긴다(1초 미만은 버린다). 돌고 있지 않으면 아무것도 안 한다 */
    suspend fun stop(ctx: Context): FocusSession? = lock.withLock { stopLocked(ctx) }

    private suspend fun stopLocked(ctx: Context): FocusSession? {
        load(ctx)
        val r = running.value ?: return null
        prefs(ctx).edit { remove("subject"); remove("started") }
        running.value = null
        NotificationManagerCompat.from(ctx).cancel(NOTIFY_ID)
        refreshWidget(ctx)
        val now = System.currentTimeMillis()
        val end = minOf(now, r.startedAt + MAX_SESSION_MS)
        if (end - r.startedAt < MIN_SESSION_MS) return null
        val session = FocusSession(
            id = UUID.randomUUID().toString(),
            subjectId = r.subjectId,
            startedAt = r.startedAt,
            endedAt = end,
            updatedAt = now,
        )
        dao().upsertSession(session)
        FocusSync.enqueue(ctx)
        refreshWidget(ctx)
        return session
    }

    suspend fun addSubject(ctx: Context, name: String, color: String): FocusSubject? {
        val clean = FocusSubjects.cleanName(name) ?: return null
        val s = FocusSubject(
            id = UUID.randomUUID().toString(),
            name = clean,
            color = if (FocusSubjects.validColor(color)) color else FocusSubjects.PALETTE[0],
            sort = dao().maxSort() + 10,
            updatedAt = System.currentTimeMillis(),
        )
        dao().upsertSubject(s)
        FocusSync.enqueue(ctx)
        refreshWidget(ctx)
        return s
    }

    suspend fun editSubject(ctx: Context, s: FocusSubject, name: String, color: String): Boolean {
        val clean = FocusSubjects.cleanName(name) ?: return false
        dao().upsertSubject(
            s.copy(name = clean, color = if (FocusSubjects.validColor(color)) color else s.color, updatedAt = System.currentTimeMillis(), synced = false),
        )
        FocusSync.enqueue(ctx)
        refreshWidget(ctx)
        return true
    }

    /** 보이는 순서(list)에서 한 칸 위·아래로. 순서는 10 간격으로 다시 매긴다 */
    suspend fun move(ctx: Context, list: List<FocusSubject>, id: String, delta: Int) {
        val i = list.indexOfFirst { it.id == id }
        val j = i + delta
        if (i < 0 || j !in list.indices) return
        val order = list.toMutableList().apply { add(j, removeAt(i)) }
        val now = System.currentTimeMillis()
        val changed = order.mapIndexedNotNull { k, s -> if (s.sort != (k + 1) * 10) s.copy(sort = (k + 1) * 10, updatedAt = now, synced = false) else null }
        dao().upsertSubjects(changed)
        FocusSync.enqueue(ctx)
        refreshWidget(ctx)
    }

    /** 숨긴다(지난 기록은 통계에 남는다). 돌고 있던 과목이면 먼저 멈춘다 */
    suspend fun archive(ctx: Context, s: FocusSubject) {
        if (running.value?.subjectId == s.id) stop(ctx)
        dao().upsertSubject(s.copy(archived = true, updatedAt = System.currentTimeMillis(), synced = false))
        FocusSync.enqueue(ctx)
        refreshWidget(ctx)
    }

    suspend fun deleteSession(ctx: Context, s: FocusSession) {
        dao().upsertSession(s.copy(deleted = true, updatedAt = System.currentTimeMillis(), synced = false))
        FocusSync.enqueue(ctx)
        refreshWidget(ctx)
    }

    /** 위젯 집중 페이지를 다시 그린다(앱·알림·위젯 어디서 바꿨든). 그리는 중이면 Room·상태 구독이 먼저 반영한다 */
    private fun refreshWidget(ctx: Context) {
        MyHomeApp.instance.ioScope.launch { runCatching { SummaryWidget().updateAll(ctx) } }
    }

    private fun showRunning(ctx: Context, r: Running, s: FocusSubject) {
        if (!Notifications.canPost(ctx)) return
        val open = PendingIntent.getActivity(
            ctx,
            5,
            Intent(ctx, MainActivity::class.java).putExtra(MainActivity.EXTRA_SCREEN, "focus")
                .addFlags(Intent.FLAG_ACTIVITY_NEW_TASK or Intent.FLAG_ACTIVITY_CLEAR_TOP),
            PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT,
        )
        val stop = PendingIntent.getBroadcast(
            ctx,
            6,
            Intent(ctx, FocusStopReceiver::class.java),
            PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT,
        )
        val color = runCatching { android.graphics.Color.parseColor(s.color) }.getOrDefault(0)
        val n = NotificationCompat.Builder(ctx, Notifications.CH_FOCUS)
            .setSmallIcon(R.drawable.ic_stat_notify)
            .setContentTitle("${s.name} 하는 중")
            .setContentText("집중 타이머")
            .setWhen(r.startedAt)
            .setShowWhen(true)
            .setUsesChronometer(true)
            .setColor(color)
            .setOngoing(true)
            .setSilent(true)
            .setOnlyAlertOnce(true)
            .setCategory(NotificationCompat.CATEGORY_STOPWATCH)
            .setContentIntent(open)
            .addAction(0, "멈추기", stop)
            .build()
        Notifications.notifyChecked(ctx, NOTIFY_ID, n)
    }
}

/** 알림의 "멈추기" */
class FocusStopReceiver : BroadcastReceiver() {
    override fun onReceive(context: Context, intent: Intent) {
        val pending = goAsync()
        MyHomeApp.instance.ioScope.launch {
            try {
                FocusTimer.stop(context.applicationContext)
            } finally {
                pending.finish()
            }
        }
    }
}
