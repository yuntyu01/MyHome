package com.yuntyu.myhome.work

import android.content.Context
import androidx.work.CoroutineWorker
import androidx.work.ForegroundInfo
import androidx.work.WorkerParameters
import com.yuntyu.myhome.MyHomeApp
import com.yuntyu.myhome.capture.CaptureService
import com.yuntyu.myhome.core.UploadQueuePolicy
import com.yuntyu.myhome.core.api.EventPayload
import com.yuntyu.myhome.core.api.UploadDecision
import com.yuntyu.myhome.core.api.describe
import com.yuntyu.myhome.core.api.toUploadDecision
import com.yuntyu.myhome.data.SendLogEntry
import com.yuntyu.myhome.notify.Notifications
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext

/**
 * 대기열의 알림 원문을 오래된 것부터 서버로 보낸다.
 * - 성공(새로/중복): 대기열에서 지우고, 설정이 켜져 있으면 원래 알림을 제거한다.
 * - 일시 오류: 남겨두고 재시도(지수 백오프). 같은 항목이 여러 번 실패하면 남겨 둔 채 뒤 항목부터 보낸다(UploadQueuePolicy).
 * - 인증 실패: 남겨두고 멈춘다. 홈 화면에 경고를 띄운다.
 * - 형식 거부: REJECTED로 보존한다(삭제하지 않음).
 */
class UploadWorker(context: Context, params: WorkerParameters) : CoroutineWorker(context, params) {

    override suspend fun doWork(): Result = withContext(Dispatchers.IO) {
        val app = applicationContext as MyHomeApp
        val dao = app.db.pendingEvents()
        var sentAny = false
        val skipped = mutableListOf<Long>() // 계속 실패해서 이번엔 건너뛴 항목(대기열에 남음)
        var consecutiveFailures = 0
        while (true) {
            val batch = dao.nextBatch(20, skipped)
            if (batch.isEmpty()) break
            for (e in batch) {
                val result = app.api.postEvent(EventPayload(e.packageName, e.title, e.text, e.bigText, e.postedAt))
                val msg = result.describe()
                when (result.toUploadDecision()) {
                    UploadDecision.SENT -> {
                        dao.delete(e.id)
                        app.db.sendLog().add(SendLogEntry(at = now(), packageName = e.packageName, success = true, message = msg))
                        if (app.settings.autoDelete) CaptureService.dismiss(e.notificationKey)
                        app.settings.authProblem = null
                        sentAny = true
                        consecutiveFailures = 0
                    }
                    UploadDecision.RETRY -> {
                        dao.markAttempt(e.id, msg)
                        app.db.sendLog().add(SendLogEntry(at = now(), packageName = e.packageName, success = false, message = msg))
                        consecutiveFailures++
                        if (UploadQueuePolicy.skipAfterRetry(e.attempts + 1, consecutiveFailures)) {
                            // 이 항목만 계속 실패한다: 남겨 두고 뒤 항목부터 보낸다. 다음 전송 때 다시 시도한다.
                            skipped += e.id
                            continue
                        }
                        afterSent(sentAny)
                        return@withContext Result.retry()
                    }
                    UploadDecision.AUTH_FAILED -> {
                        dao.markAttempt(e.id, msg)
                        app.db.sendLog().add(SendLogEntry(at = now(), packageName = e.packageName, success = false, message = msg))
                        app.settings.authProblem = msg
                        afterSent(sentAny)
                        // 재시도해도 같으므로 멈춘다. 토큰을 다시 넣으면 flushNow로 재개된다.
                        return@withContext Result.failure()
                    }
                    UploadDecision.REJECTED -> {
                        dao.markRejected(e.id, msg)
                        app.db.sendLog().add(SendLogEntry(at = now(), packageName = e.packageName, success = false, message = "거부됨: $msg"))
                        consecutiveFailures = 0
                    }
                }
            }
        }
        afterSent(sentAny)
        // 건너뛴 항목이 남았으면 나중에 다시 보낸다.
        if (skipped.isNotEmpty()) Result.retry() else Result.success()
    }

    /** 전송 성공 직후 훅. Phase 6에서 위젯 즉시 갱신을 붙인다. */
    private suspend fun afterSent(sentAny: Boolean) {
        if (sentAny) AfterUpload.onEventsSent(applicationContext)
    }

    override suspend fun getForegroundInfo(): ForegroundInfo {
        // Android 11 이하에서 긴급 작업이 포그라운드로 돌 때만 쓰인다.
        // Android 12+는 JobScheduler 긴급 작업으로 돌아서 이 경로를 타지 않는다.
        return ForegroundInfo(Notifications.ID_UPLOADING, Notifications.uploadingNotification(applicationContext))
    }

    private fun now() = System.currentTimeMillis()
}
