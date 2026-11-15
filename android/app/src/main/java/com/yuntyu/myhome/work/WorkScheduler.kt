package com.yuntyu.myhome.work

import android.content.Context
import androidx.work.BackoffPolicy
import androidx.work.Constraints
import androidx.work.ExistingPeriodicWorkPolicy
import androidx.work.ExistingWorkPolicy
import androidx.work.NetworkType
import androidx.work.OneTimeWorkRequestBuilder
import androidx.work.OutOfQuotaPolicy
import androidx.work.PeriodicWorkRequestBuilder
import androidx.work.WorkManager
import java.util.concurrent.TimeUnit

/** WorkManager 예약은 전부 여기서 한다. */
object WorkScheduler {
    private const val UPLOAD_NOW = "upload-now"
    private const val UPLOAD_PERIODIC = "upload-periodic"
    private const val NOTICE_CHECK = "notice-check"

    private val network = Constraints.Builder().setRequiredNetworkType(NetworkType.CONNECTED).build()

    /**
     * 대기열을 바로 비운다. 실행 중인 작업이 있으면 교체(REPLACE)해서 새로 시작한다.
     * 교체 때문에 전송 직후 취소돼도 행이 남아 다시 보내지는데, 서버가 content_hash로 중복 처리하므로 안전하다.
     */
    fun flushNow(context: Context) {
        val req = OneTimeWorkRequestBuilder<UploadWorker>()
            .setConstraints(network)
            .setExpedited(OutOfQuotaPolicy.RUN_AS_NON_EXPEDITED_WORK_REQUEST)
            .setBackoffCriteria(BackoffPolicy.EXPONENTIAL, 30, TimeUnit.SECONDS)
            .build()
        WorkManager.getInstance(context).enqueueUniqueWork(UPLOAD_NOW, ExistingWorkPolicy.REPLACE, req)
    }

    /** 안전망: 15분마다 대기열 확인. 재부팅 후에도 WorkManager가 다시 예약한다. */
    fun ensurePeriodic(context: Context) {
        val req = PeriodicWorkRequestBuilder<UploadWorker>(15, TimeUnit.MINUTES)
            .setConstraints(network)
            .build()
        WorkManager.getInstance(context).enqueueUniquePeriodicWork(UPLOAD_PERIODIC, ExistingPeriodicWorkPolicy.KEEP, req)
    }

    /**
     * 붙든 결제 알림을 지워도 되는지 서버에 다시 묻는다(NoticeCheckWorker). AI 판정을 기다리는 동안은 작업이 스스로 재시도한다.
     * 새로 부르면 기다리던 것을 바꿔서 바로(또는 delaySeconds 뒤) 묻는다.
     */
    fun checkNotices(context: Context, delaySeconds: Long = 0) {
        val req = OneTimeWorkRequestBuilder<NoticeCheckWorker>()
            .setConstraints(network)
            .setInitialDelay(delaySeconds, TimeUnit.SECONDS)
            .setBackoffCriteria(BackoffPolicy.LINEAR, NoticeCheckWorker.RETRY_SECONDS, TimeUnit.SECONDS)
            .build()
        WorkManager.getInstance(context).enqueueUniqueWork(NOTICE_CHECK, ExistingWorkPolicy.REPLACE, req)
    }
}
