package com.yuntyu.myhome.health

import android.app.AlarmManager
import android.app.PendingIntent
import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import android.os.Build
import androidx.core.app.NotificationCompat
import com.yuntyu.myhome.MyHomeApp
import com.yuntyu.myhome.R
import com.yuntyu.myhome.core.HealthPlan
import com.yuntyu.myhome.notify.Notifications
import com.yuntyu.myhome.ui.MainActivity
import java.time.Instant
import java.time.LocalDateTime
import java.time.ZoneId

/**
 * 끼니 마감 30분 전·마감, 헬스 도착 1시간 전, 취침 알림(핏두잉과 같은 규칙).
 * 다음 알림 하나만 AlarmManager에 걸고, 울리면 띄운 뒤 그다음 것을 건다. 이미 찍은 끼니, 헬스를 이미 한(쉰) 날은 빠진다.
 * "알람 및 리마인더"(SCHEDULE_EXACT_ALARM)를 허용하면 setExactAndAllowWhileIdle로 제시각에 울린다. 허용하지 않으면
 * setAndAllowWhileIdle이라 절전 중에 늦을 수 있고(최대 1시간쯤), 늦게 울려 뜻이 없어진 알림은 띄우지 않는다(HealthPlan.dueReminders).
 * USE_EXACT_ALARM은 쓰지 않는다(Play가 알람·캘린더 앱에만 허용).
 */
object Reminders {
    private const val REQUEST = 7301

    /** 제시각 알람을 걸 수 있는지. Android 12 미만은 늘 된다. */
    fun canExact(context: Context): Boolean =
        Build.VERSION.SDK_INT < Build.VERSION_CODES.S || context.getSystemService(AlarmManager::class.java).canScheduleExactAlarms()

    fun reschedule(context: Context) {
        val app = context.applicationContext
        val local = MyHomeApp.instance.healthLocal
        val am = app.getSystemService(AlarmManager::class.java)
        val pi = pending(app)
        am.cancel(pi)
        val s = local.schedule
        if (!s.notifications || (s.meals.isEmpty() && !s.gymEnabled && !s.sleepEnabled)) {
            local.scheduledAt = 0
            return
        }
        val next = HealthPlan.planReminders(s.reminderInput(), LocalDateTime.now(), local.logged, gymDone = local.gymDone).firstOrNull() ?: return
        val at = next.at.atZone(ZoneId.systemDefault()).toInstant().toEpochMilli()
        local.scheduledAt = at
        // 권한을 확인한 뒤에 걸어도 그사이 회수되면 SecurityException이라 그때는 부정확한 알람으로
        val exact = canExact(app) && runCatching { am.setExactAndAllowWhileIdle(AlarmManager.RTC_WAKEUP, at, pi) }.isSuccess
        if (!exact) am.setAndAllowWhileIdle(AlarmManager.RTC_WAKEUP, at, pi)
    }

    /** 예약한 시각부터 지금까지 울려야 할 알림을 띄운다(같은 분에 겹친 알림도 함께). */
    internal fun fire(context: Context) {
        val local = MyHomeApp.instance.healthLocal
        val s = local.schedule
        val from = local.scheduledAt.takeIf { it > 0 } ?: return
        val zone = ZoneId.systemDefault()
        val fromTime = LocalDateTime.ofInstant(Instant.ofEpochMilli(from), zone)
        val now = LocalDateTime.now()
        if (s.notifications) {
            val planned = HealthPlan.planReminders(s.reminderInput(), fromTime.minusSeconds(1), local.logged, gymDone = local.gymDone)
            HealthPlan.dueReminders(planned, now).forEach { post(context, it) }
        }
        reschedule(context)
    }

    private fun post(context: Context, p: HealthPlan.Planned) {
        val screen = when (p.kind) {
            HealthPlan.Kind.MEAL_PRE, HealthPlan.Kind.MEAL_DUE -> "health_capture"
            HealthPlan.Kind.GYM -> "health_gym"
            HealthPlan.Kind.SLEEP -> "health"
        }
        val open = Intent(context, MainActivity::class.java)
            .setData(android.net.Uri.parse("myhome://health/${p.id}"))
            .putExtra(MainActivity.EXTRA_SCREEN, screen)
            .putExtra(MainActivity.EXTRA_MEAL_INDEX, p.meal)
            .addFlags(Intent.FLAG_ACTIVITY_NEW_TASK or Intent.FLAG_ACTIVITY_CLEAR_TOP)
        val n = NotificationCompat.Builder(context, Notifications.CH_HEALTH)
            .setSmallIcon(R.drawable.ic_stat_notify)
            .setContentTitle(p.title)
            .setContentText(p.body)
            .setAutoCancel(true)
            .setContentIntent(PendingIntent.getActivity(context, p.id.hashCode(), open, PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT))
            .build()
        Notifications.notifyChecked(context, idFor(p), n)
    }

    /** 같은 종류의 알림은 하나만 남긴다(끼니는 끼니별). */
    private fun idFor(p: HealthPlan.Planned): Int = when (p.kind) {
        HealthPlan.Kind.MEAL_PRE, HealthPlan.Kind.MEAL_DUE -> 2100 + p.meal
        HealthPlan.Kind.GYM -> 2200
        HealthPlan.Kind.SLEEP -> 2300
    }

    private fun pending(context: Context): PendingIntent =
        PendingIntent.getBroadcast(
            context,
            REQUEST,
            Intent(context, ReminderReceiver::class.java),
            PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT,
        )
}

class ReminderReceiver : BroadcastReceiver() {
    override fun onReceive(context: Context, intent: Intent) = Reminders.fire(context)
}

/**
 * 재부팅·앱 업데이트·시각 변경·"알람 및 리마인더" 허용이 바뀐 뒤 알림과 헬스장·공부 장소 지오펜스를 다시 건다
 * (지오펜스는 재부팅 때 지워진다).
 */
class ReminderBootReceiver : BroadcastReceiver() {
    override fun onReceive(context: Context, intent: Intent) {
        Reminders.reschedule(context)
        GymGeofence.register(context)
        // 공부 장소 지오펜스도 재부팅 때 지워진다. 돌던 타이머 알림도 다시 띄운다(MyHomeApp.onCreate의 FocusTimer.init)
        com.yuntyu.myhome.focus.StudyPlaces.register(context)
    }
}
