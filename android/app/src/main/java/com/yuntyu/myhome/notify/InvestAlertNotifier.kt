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
 * 자동매매 알림. 서버의 매매 작업(systemd)이 실패하면(kind fail), 또는 그 달 매수가 모두 끝나면(kind done, 한 번)
 * invest_alerts에 한 줄이 생기고,
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
        val open = PendingIntent.getActivity(
            context,
            3,
            Intent(context, MainActivity::class.java)
                .putExtra(MainActivity.EXTRA_SCREEN, "invest")
                .addFlags(Intent.FLAG_ACTIVITY_NEW_TASK or Intent.FLAG_ACTIVITY_CLEAR_TOP),
            PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT,
        )
        var seen = last
        for (a in r.value.alerts) {
            val done = a.kind == "done"
            val channel = if (done) Notifications.CH_INVEST_DONE else Notifications.CH_INVEST
            if (!Notifications.canShow(context, channel)) {
                // 매매 실패는 놓치면 안 되는 알림이라, 못 띄우면(권한·앱 알림·채널이 꺼져 있으면) 본 것으로 치지 않는다 —
                // 켜면 그때 받는다(서버가 7일·20건까지만 준다). 완료 알림은 확인용이라 꺼 두었으면 그냥 넘긴다
                if (done) { seen = a.id; continue }
                break
            }
            val n = NotificationCompat.Builder(context, channel)
                .setSmallIcon(R.drawable.ic_stat_notify)
                .setContentTitle(a.title)
                .setContentText(a.body.lineSequence().firstOrNull { it.isNotBlank() } ?: "")
                .setStyle(NotificationCompat.BigTextStyle().bigText(a.body))
                .setContentIntent(open)
                .setAutoCancel(true)
                .setPriority(if (done) NotificationCompat.PRIORITY_LOW else NotificationCompat.PRIORITY_HIGH)
                .setSilent(done)
                .build()
            Notifications.notifyChecked(context, 3000 + (a.id % 1000).toInt(), n)
            seen = a.id
        }
        if (seen > last) prefs.edit { putLong(KEY_LAST_ID, seen) }
    }
}
