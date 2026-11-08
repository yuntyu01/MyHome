package com.yuntyu.myhome.notify

import android.app.PendingIntent
import android.content.Context
import android.content.Intent
import androidx.core.app.NotificationCompat
import androidx.core.content.edit
import com.yuntyu.myhome.MyHomeApp
import com.yuntyu.myhome.R
import com.yuntyu.myhome.core.api.ApiResult
import com.yuntyu.myhome.ui.MainActivity
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext

/**
 * 자동매매 실패 알림. 서버의 매매 작업(systemd)이 실패하면 invest_alerts에 한 줄이 생기고,
 * 위젯 갱신 주기(30분·결제 전송 직후)마다 마지막으로 본 id 다음을 받아 한 건씩 띄운다.
 * 서버는 7일 안의 것만 주므로, 처음 연결한 기기에는 최근 7일 실패만 온다.
 */
object InvestAlertNotifier {
    private const val PREFS = "invest_alerts"
    private const val KEY_LAST_ID = "last_id"

    suspend fun check(context: Context) = withContext(Dispatchers.IO) {
        val prefs = context.getSharedPreferences(PREFS, Context.MODE_PRIVATE)
        val last = prefs.getLong(KEY_LAST_ID, 0L)
        val r = MyHomeApp.instance.api.investAlerts(last)
        if (r !is ApiResult.Ok || r.value.alerts.isEmpty()) return@withContext
        // 알림 권한이 없어도 본 것으로 친다(권한을 켠 뒤 옛 알림이 한꺼번에 쏟아지지 않게)
        if (Notifications.canPost(context)) {
            val open = PendingIntent.getActivity(
                context,
                3,
                Intent(context, MainActivity::class.java)
                    .putExtra(MainActivity.EXTRA_SCREEN, "invest")
                    .addFlags(Intent.FLAG_ACTIVITY_NEW_TASK or Intent.FLAG_ACTIVITY_CLEAR_TOP),
                PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT,
            )
            for (a in r.value.alerts) {
                val n = NotificationCompat.Builder(context, Notifications.CH_INVEST)
                    .setSmallIcon(R.drawable.ic_stat_notify)
                    .setContentTitle(a.title)
                    .setContentText(a.body.lineSequence().firstOrNull { it.isNotBlank() } ?: "")
                    .setStyle(NotificationCompat.BigTextStyle().bigText(a.body))
                    .setContentIntent(open)
                    .setAutoCancel(true)
                    .setPriority(NotificationCompat.PRIORITY_HIGH)
                    .build()
                Notifications.notifyChecked(context, 3000 + (a.id % 1000).toInt(), n)
            }
        }
        prefs.edit { putLong(KEY_LAST_ID, r.value.alerts.maxOf { it.id }) }
    }
}
