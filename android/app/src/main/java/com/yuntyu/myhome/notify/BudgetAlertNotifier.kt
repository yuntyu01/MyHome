package com.yuntyu.myhome.notify

import android.app.PendingIntent
import android.content.Context
import android.content.Intent
import androidx.core.app.NotificationCompat
import androidx.core.content.edit
import com.yuntyu.myhome.MyHomeApp
import com.yuntyu.myhome.R
import com.yuntyu.myhome.core.BudgetAlerts
import com.yuntyu.myhome.core.api.Summary
import com.yuntyu.myhome.ui.MainActivity

/**
 * 예산 80% 도달·초과 알림. 기본 무음 채널을 쓰고 설정에서 끌 수 있다.
 * 같은 기간·분류·단계는 한 번만 알린다.
 */
object BudgetAlertNotifier {
    private const val PREFS = "budget_alerts"
    private const val KEY_SENT = "sent"

    fun check(context: Context, summary: Summary) {
        val app = context.applicationContext as MyHomeApp
        if (!app.settings.budgetAlerts || !Notifications.canPost(context)) return
        val prefs = context.getSharedPreferences(PREFS, Context.MODE_PRIVATE)
        val sent = BudgetAlerts.prune(prefs.getStringSet(KEY_SENT, emptySet())?.toSet() ?: emptySet(), summary.period.label)
        val alerts = BudgetAlerts.pending(summary, sent)
        if (alerts.isEmpty()) {
            prefs.edit { putStringSet(KEY_SENT, sent) }
            return
        }
        val open = PendingIntent.getActivity(
            context,
            0,
            Intent(context, MainActivity::class.java).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK or Intent.FLAG_ACTIVITY_CLEAR_TOP),
            PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT,
        )
        for (a in alerts) {
            val n = NotificationCompat.Builder(context, Notifications.CH_BUDGET)
                .setSmallIcon(R.drawable.ic_stat_notify)
                .setContentTitle(a.title)
                .setContentText(a.text)
                .setContentIntent(open)
                .setAutoCancel(true)
                .setSilent(true)
                .setPriority(NotificationCompat.PRIORITY_LOW)
                .build()
            Notifications.notifyChecked(context, 2000 + (a.categoryId * 2 + if (a.level == 100) 1 else 0).toInt(), n)
        }
        prefs.edit { putStringSet(KEY_SENT, sent + BudgetAlerts.keysToRemember(alerts, summary.period.label)) }
    }
}
