package com.yuntyu.myhome.focus

import android.content.Context
import android.util.Log
import androidx.core.content.edit
import androidx.work.BackoffPolicy
import androidx.work.Constraints
import androidx.work.CoroutineWorker
import androidx.work.ExistingWorkPolicy
import androidx.work.NetworkType
import androidx.work.OneTimeWorkRequestBuilder
import androidx.work.WorkManager
import androidx.work.WorkerParameters
import com.yuntyu.myhome.MyHomeApp
import com.yuntyu.myhome.core.api.ApiResult
import com.yuntyu.myhome.core.api.FocusSessionBody
import com.yuntyu.myhome.core.api.FocusStayBody
import com.yuntyu.myhome.core.api.FocusSubjectBody
import com.yuntyu.myhome.data.FocusSession
import com.yuntyu.myhome.data.FocusStay
import com.yuntyu.myhome.data.FocusSubject
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.delay
import kotlinx.coroutines.withContext
import java.time.Instant
import java.time.OffsetDateTime
import java.util.concurrent.TimeUnit

/**
 * 집중 기록을 서버(/v1/focus)에 올리고, 새로 깐 앱이면 한 번 내려받는다.
 * 폰이 원본이라 실패해도 폰 기록은 그대로다. 과목 → 기록 → 머묾 순서로 보낸다(기록이 과목을 가리키므로).
 *
 * 서버 응답별로:
 * - 저장됨 → 보냄 표시
 * - 400(서버가 내용을 거절) → 보냄 표시하고 넘어간다(같은 것을 계속 보내 봐야 같은 답이라서). 폰 기록은 남는다
 * - 404·405(서버에 아직 이 API가 없음 = 서버를 업데이트하기 전) → 지우지 않고 나중에 다시
 * - 연결·서버 문제(429 포함)·로그인 문제 → 나중에 다시. 이미 보낸 것은 보냄 표시가 남아서 다음에 이어서 보낸다
 */
object FocusSync {
    private const val TAG = "FocusSync"
    private const val WORK = "focus-sync"
    private const val PREFS = "focus_sync"

    fun enqueue(ctx: Context) {
        val req = OneTimeWorkRequestBuilder<FocusSyncWorker>()
            .setConstraints(Constraints.Builder().setRequiredNetworkType(NetworkType.CONNECTED).build())
            .setBackoffCriteria(BackoffPolicy.EXPONENTIAL, 30, TimeUnit.SECONDS)
            .build()
        // 이미 걸려 있으면 그대로 둔다: 보내는 동안 생긴 것도 push가 다시 읽어 보내고, 남은 것은 다음 신호(멈춤·30분 주기)에 간다.
        // 뒤에 붙이면(APPEND) 서버를 아직 안 올렸을 때 다시 하기를 기다리는 사이 30분마다 작업이 쌓인다
        WorkManager.getInstance(ctx).enqueueUniqueWork(WORK, ExistingWorkPolicy.KEEP, req)
    }

    private enum class Outcome { SAVED, DROP, LATER }

    /** 서버의 IP당 요청 한도(초당 20)를 넘지 않게 보낼 때마다 조금 쉰다. 넘으면 429라 나중에 이어서 보낸다 */
    private const val PACE_MS = 70L

    private fun outcome(r: ApiResult<*>): Outcome = when (r) {
        is ApiResult.Ok -> Outcome.SAVED
        is ApiResult.HttpError -> when (r.code) {
            400, 422 -> Outcome.DROP
            else -> Outcome.LATER
        }
        else -> Outcome.LATER
    }

    fun iso(ms: Long): String = Instant.ofEpochMilli(ms).toString()
    fun ms(iso: String): Long = OffsetDateTime.parse(iso).toInstant().toEpochMilli()

    /** 다 보냈으면 true, 나중에 다시 해야 하면 false */
    suspend fun push(): Boolean = withContext(Dispatchers.IO) {
        val app = MyHomeApp.instance
        val dao = app.db.focus()
        val api = app.api
        while (true) {
            val list = dao.unsyncedSubjects()
            if (list.isEmpty()) break
            for (s in list) {
                delay(PACE_MS)
                val r = api.putFocusSubject(s.id, FocusSubjectBody(s.name, s.color, s.sort, s.archived, iso(s.updatedAt)))
                when (outcome(r)) {
                    Outcome.SAVED -> dao.markSubjectSynced(s.id, s.updatedAt)
                    Outcome.DROP -> { Log.w(TAG, "subject rejected: $r"); dao.markSubjectSynced(s.id, s.updatedAt) }
                    Outcome.LATER -> return@withContext false
                }
            }
        }
        while (true) {
            val list = dao.unsyncedSessions()
            if (list.isEmpty()) break
            for (s in list) {
                delay(PACE_MS)
                val r = api.putFocusSession(s.id, FocusSessionBody(s.subjectId, iso(s.startedAt), iso(s.endedAt), s.deleted, iso(s.updatedAt)))
                when (outcome(r)) {
                    Outcome.SAVED -> dao.markSessionSynced(s.id, s.updatedAt)
                    Outcome.DROP -> { Log.w(TAG, "session rejected: $r"); dao.markSessionSynced(s.id, s.updatedAt) }
                    Outcome.LATER -> return@withContext false
                }
            }
        }
        while (true) {
            val list = dao.unsyncedStays()
            if (list.isEmpty()) break
            for (s in list) {
                delay(PACE_MS)
                val r = api.putFocusStay(s.id, FocusStayBody(s.place, iso(s.enteredAt), iso(s.exitedAt), s.deleted, iso(s.updatedAt)))
                when (outcome(r)) {
                    Outcome.SAVED -> dao.markStaySynced(s.id, s.updatedAt)
                    Outcome.DROP -> { Log.w(TAG, "stay rejected: $r"); dao.markStaySynced(s.id, s.updatedAt) }
                    Outcome.LATER -> return@withContext false
                }
            }
        }
        true
    }

    /**
     * 서버에 있는데 이 폰에 없는 것을 한 번 내려받는다(앱을 새로 깔았을 때). 폰에 있는 것은 덮지 않는다.
     * 한 번 성공하면 다시 하지 않는다.
     */
    suspend fun restoreOnce(ctx: Context) = withContext(Dispatchers.IO) {
        val p = ctx.getSharedPreferences(PREFS, Context.MODE_PRIVATE)
        if (p.getBoolean("restored", false)) return@withContext
        val r = MyHomeApp.instance.api.focusAll()
        if (r !is ApiResult.Ok) return@withContext
        val dao = MyHomeApp.instance.db.focus()
        runCatching {
            for (s in r.value.subjects) {
                if (dao.subject(s.id) == null) {
                    dao.upsertSubject(FocusSubject(s.id, s.name, s.color, s.sort, s.archived, ms(s.updatedAt), synced = true))
                }
            }
            for (s in r.value.sessions) {
                if (dao.session(s.id) == null) {
                    dao.upsertSession(FocusSession(s.id, s.subjectId, ms(s.startedAt), ms(s.endedAt), s.deleted, ms(s.updatedAt), synced = true))
                }
            }
            for (s in r.value.stays) {
                if (dao.stay(s.id) == null) {
                    dao.upsertStay(FocusStay(s.id, s.place, ms(s.enteredAt), ms(s.exitedAt), s.deleted, ms(s.updatedAt), synced = true))
                }
            }
        }.onSuccess { p.edit { putBoolean("restored", true) } }
            .onFailure { Log.w(TAG, "restore failed", it) }
    }
}

class FocusSyncWorker(context: Context, params: WorkerParameters) : CoroutineWorker(context, params) {
    override suspend fun doWork(): Result {
        FocusSync.restoreOnce(applicationContext)
        return if (FocusSync.push()) Result.success() else Result.retry()
    }
}
