package com.yuntyu.myhome.capture

import android.app.Notification
import android.content.pm.PackageManager
import android.os.Build
import android.service.notification.NotificationListenerService
import android.service.notification.StatusBarNotification
import com.yuntyu.myhome.MyHomeApp
import com.yuntyu.myhome.core.CaptureRules
import com.yuntyu.myhome.core.DedupeKey
import com.yuntyu.myhome.data.PendingEvent
import com.yuntyu.myhome.work.WorkScheduler
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancel
import kotlinx.coroutines.launch

/**
 * 알림 수집기. 선택한 앱의 알림 원문을 Room 대기열에 넣고 전송 작업을 깨운다.
 * 해석은 하지 않는다. 원문 그대로 서버로 보낸다.
 */
class CaptureService : NotificationListenerService() {
    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.IO)

    override fun onListenerConnected() {
        current = this
    }

    override fun onListenerDisconnected() {
        if (current === this) current = null
    }

    override fun onDestroy() {
        if (current === this) current = null
        scope.cancel()
        super.onDestroy()
    }

    override fun onNotificationPosted(sbn: StatusBarNotification) {
        val app = applicationContext as MyHomeApp
        val n = sbn.notification ?: return
        val extras = n.extras
        val title = extras.getCharSequence(Notification.EXTRA_TITLE)?.toString().orEmpty()
        val text = extras.getCharSequence(Notification.EXTRA_TEXT)?.toString().orEmpty()
        val bigText = extras.getCharSequence(Notification.EXTRA_BIG_TEXT)?.toString()
            ?: extras.getCharSequenceArray(Notification.EXTRA_TEXT_LINES)?.joinToString("\n")
            ?: ""
        val isSummary = (n.flags and Notification.FLAG_GROUP_SUMMARY) != 0
        val pkg = sbn.packageName
        val now = System.currentTimeMillis()

        scope.launch {
            // 최근 알림 목록: 패키지 선택 화면용 메타데이터(앱 이름, 제목 앞부분)만 기기에 남긴다.
            if (pkg != packageName && !sbn.isOngoing) {
                app.db.recentApps().record(pkg, appLabel(pkg), title, now)
            }

            val candidate = CaptureRules.Candidate(pkg, sbn.isOngoing, isSummary, title, text, bigText)
            if (!CaptureRules.shouldCapture(candidate, app.settings.selectedPackages, packageName)) return@launch

            val inserted = app.db.pendingEvents().insert(
                PendingEvent(
                    packageName = pkg,
                    title = title,
                    text = text,
                    bigText = bigText,
                    postedAt = sbn.postTime,
                    notificationKey = sbn.key,
                    dedupeKey = DedupeKey.of(pkg, sbn.postTime, title, text, bigText),
                    createdAt = now,
                ),
            )
            if (inserted != -1L) WorkScheduler.flushNow(applicationContext)
        }
    }

    private fun appLabel(pkg: String): String = try {
        val pm = packageManager
        val info = if (Build.VERSION.SDK_INT >= 33) {
            pm.getApplicationInfo(pkg, PackageManager.ApplicationInfoFlags.of(0))
        } else {
            @Suppress("DEPRECATION")
            pm.getApplicationInfo(pkg, 0)
        }
        pm.getApplicationLabel(info).toString()
    } catch (e: PackageManager.NameNotFoundException) {
        pkg
    }

    companion object {
        /** 연결된 수집기. 전송 성공 후 알림을 지울 때 쓴다. 연결이 끊겼으면 null. */
        @Volatile
        var current: CaptureService? = null
            private set

        /** 알림을 지운다. 수집기가 연결돼 있지 않거나 이미 사라진 알림이면 조용히 넘어간다. */
        fun dismiss(key: String): Boolean {
            val svc = current ?: return false
            return try {
                svc.cancelNotification(key)
                true
            } catch (e: SecurityException) {
                false
            } catch (e: RuntimeException) {
                false
            }
        }
    }
}
