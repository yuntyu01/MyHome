package com.yuntyu.myhome.health

import android.app.AlarmManager
import android.app.PendingIntent
import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
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
 * 다음 알림 하나만 AlarmManager에 걸고, 울리면 띄운 뒤 그다음 것을 건다. 이미 찍은 끼니는 빠진다.
 * 정확한 알람 권한 없이 setAndAllowWhileIdle을 쓰므로 절전 중에는 몇 분 늦을 수 있다.
 */
object Reminders {
    private const val REQUEST = 7301

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
        val next = HealthPlan.planReminders(s.reminderInput(), LocalDateTime.now(), local.logged).firstOrNull() ?: return
        val at = next.at.atZone(ZoneId.systemDefault()).toInstant().toEpochMilli()
        local.scheduledAt = at
        am.setAndAllowWhileIdle(AlarmManager.RTC_WAKEUP, at, pi)
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
            HealthPlan.planReminders(s.reminderInput(), fromTime.minusSeconds(1), local.logged)
                .filter { !it.at.isAfter(now) && now.minusHours(1).isBefore(it.at) }
                .forEach { post(context, it) }
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

/** 재부팅·앱 업데이트·시각 변경 뒤 알림과 헬스장 지오펜스를 다시 건다(지오펜스는 재부팅 때 지워진다). */
class ReminderBootReceiver : BroadcastReceiver() {
    override fun onReceive(context: Context, intent: Intent) {
        Reminders.reschedule(context)
        GymGeofence.register(context)
    }
}
