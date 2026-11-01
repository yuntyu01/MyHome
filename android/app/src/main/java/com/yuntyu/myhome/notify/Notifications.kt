package com.yuntyu.myhome.notify

import android.Manifest
import android.annotation.SuppressLint
import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.content.Context
import android.content.pm.PackageManager
import androidx.core.app.NotificationCompat
import androidx.core.app.NotificationManagerCompat
import androidx.core.content.ContextCompat
import com.yuntyu.myhome.R
import com.yuntyu.myhome.core.CaptureRules

/** 앱 자체 알림 채널과 알림. 기본은 전부 무음. */
object Notifications {
    const val CH_SYNC = "sync"
    const val CH_TEST = "test"
    const val CH_BUDGET = "budget"

    const val ID_UPLOADING = 1001
    const val ID_TEST = 1002

    fun createChannels(context: Context) {
        val nm = context.getSystemService(NotificationManager::class.java)
        nm.createNotificationChannels(
            listOf(
                NotificationChannel(CH_SYNC, "전송 중", NotificationManager.IMPORTANCE_MIN),
                NotificationChannel(CH_TEST, "테스트 알림", NotificationManager.IMPORTANCE_LOW),
                NotificationChannel(CH_BUDGET, "예산 알림", NotificationManager.IMPORTANCE_LOW).apply {
                    description = "분류별 예산 80% 도달·초과 알림 (기본 무음)"
                    setSound(null, null)
                    enableVibration(false)
                },
            ),
        )
    }

    fun uploadingNotification(context: Context): Notification =
        NotificationCompat.Builder(context, CH_SYNC)
            .setSmallIcon(R.drawable.ic_stat_notify)
            .setContentTitle("결제 알림 전송 중")
            .setPriority(NotificationCompat.PRIORITY_MIN)
            .setOngoing(true)
            .build()

    fun canPost(context: Context): Boolean =
        android.os.Build.VERSION.SDK_INT < 33 ||
            ContextCompat.checkSelfPermission(context, Manifest.permission.POST_NOTIFICATIONS) == PackageManager.PERMISSION_GRANTED

    /**
     * 수집 → 전송 → 삭제 흐름을 확인하는 테스트 알림. 제목에 표시가 있어서 서버는 거래로 만들지 않는다.
     * @return 알림 권한이 없으면 false
     */
    fun postTest(context: Context): Boolean {
        if (!canPost(context)) return false
        val n = NotificationCompat.Builder(context, CH_TEST)
            .setSmallIcon(R.drawable.ic_stat_notify)
            .setContentTitle("${CaptureRules.TEST_TITLE_MARKER} 수집 확인")
            .setContentText("테스트 1,000원 (합성 알림, 거래로 기록되지 않음)")
            .setAutoCancel(true)
            .build()
        notifyChecked(context, ID_TEST, n)
        return true
    }

    /** 권한 확인 후 알림을 띄운다. */
    @SuppressLint("MissingPermission")
    fun notifyChecked(context: Context, id: Int, n: Notification) {
        if (!canPost(context)) return
        NotificationManagerCompat.from(context).notify(id, n)
    }
}
