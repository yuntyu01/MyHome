package com.yuntyu.myhome.work

import android.content.Context
import androidx.work.CoroutineWorker
import androidx.work.WorkerParameters
import com.yuntyu.myhome.MyHomeApp
import com.yuntyu.myhome.capture.CaptureService
import com.yuntyu.myhome.core.NoticePolicy
import com.yuntyu.myhome.core.api.ApiResult
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext

/**
 * 붙든 결제 알림(held_notices)을 지워도 되는지 서버에 다시 묻고, dismiss면 지운다(사용자 요청, 2026-11-15: 알아본 것만 지우기).
 * - 사용자가 이미 치웠거나 같은 자리에 새 알림이 뜬 것, 오래된 것은 잊는다.
 * - AI 판정을 기다리는 것(wait)이 막 보낸 것이면 재시도(선형 백오프)로 곧 다시 묻는다. 그 밖엔 업로드 주기 작업·분류 뒤에 다시 부른다.
 */
class NoticeCheckWorker(context: Context, params: WorkerParameters) : CoroutineWorker(context, params) {

    override suspend fun doWork(): Result = withContext(Dispatchers.IO) {
        val app = applicationContext as MyHomeApp
        val dao = app.db.heldNotices()
        if (!app.settings.autoDelete) {
            dao.clear()
            return@withContext Result.success()
        }
        val now = System.currentTimeMillis()
        dao.deleteSentBefore(now - NoticePolicy.FORGET_AFTER_MS)
        // 수집기가 안 붙어 있으면 지울 수도, 떠 있는지 볼 수도 없다. 다음 전송·주기 작업 때 다시 본다
        val active = CaptureService.activePostTimes() ?: return@withContext Result.success()
        val held = dao.oldest(NoticePolicy.MAX_IDS)
        val gone = held.filter { active[it.notificationKey] != it.postedAt }
        if (gone.isNotEmpty()) dao.delete(gone.map { it.rawEventId })
        val ask = held - gone.toSet()
        if (ask.isEmpty()) return@withContext Result.success()

        when (val r = app.api.eventNotices(ask.map { it.rawEventId })) {
            is ApiResult.Ok -> {
                val byId = ask.associateBy { it.rawEventId }
                val done = mutableListOf<Long>()
                for (n in r.value.notices) {
                    val h = byId[n.id] ?: continue
                    if (n.notice == NoticePolicy.DISMISS) {
                        if (CaptureService.dismiss(h.notificationKey, h.postedAt)) done += h.rawEventId
                    } else if (n.notice != h.state) {
                        dao.setState(h.rawEventId, n.notice)
                    }
                }
                if (done.isNotEmpty()) dao.delete(done)
            }
            // 네트워크·서버 오류: AI를 기다리는 중이면 아래 재시도로, 아니면 다음 주기 작업 때 다시 묻는다
            else -> Unit
        }
        val left = dao.all().map { NoticePolicy.Held(it.sentAt, it.state) }
        if (NoticePolicy.recheckSoon(left, System.currentTimeMillis())) Result.retry() else Result.success()
    }

    companion object {
        /** 전송 뒤 처음 묻기까지(서버 AI 작업은 수집 직후 깨어나서 보통 몇 초~몇십 초 안에 답한다) */
        const val FIRST_DELAY_SECONDS = 20L

        /** 아직 판정 전이면 이만큼씩 늘려 가며 다시 묻는다(선형: 30초, 60초, 90초…) */
        const val RETRY_SECONDS = 30L
    }
}
