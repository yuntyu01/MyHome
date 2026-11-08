package com.yuntyu.myhome.health

import android.content.Context
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
import com.yuntyu.myhome.core.api.MealRequest
import com.yuntyu.myhome.core.api.MyHomeJson
import com.yuntyu.myhome.core.api.UploadDecision
import com.yuntyu.myhome.core.api.describe
import com.yuntyu.myhome.core.api.toUploadDecision
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import kotlinx.serialization.Serializable
import kotlinx.serialization.builtins.ListSerializer
import java.util.concurrent.TimeUnit

/**
 * 서버에 아직 못 보낸 끼니 기록(지하 식당처럼 신호가 없거나 서버가 잠깐 안 될 때). 저장을 누른 기록을 잃지 않으려고
 * 폰에 남겨 두고 WorkManager가 연결되면 보낸다(MealOutboxWorker). 같은 날 같은 끼니는 마지막 것만 남긴다(서버도 덮어쓴다).
 * 사진은 저장할 때 이미 끼니 사진(MealPhotos.keep)으로 옮겨 두고 "올릴 것"으로 표시해서, 기록을 보낸 뒤에 올린다.
 * 서버가 받지 않는 기록(400 등)은 다시 보내지 않고 error를 붙여 남긴다(화면에 "보내지 못함", 다시 저장하면 바뀐다).
 */
object MealOutbox {
    private const val PREFS = "meal_outbox"
    private const val KEY = "entries"
    private const val WORK = "meal-outbox"

    @Serializable
    data class Entry(val req: MealRequest, val hasPhotos: Boolean = false, val queuedAt: Long = 0, val error: String? = null) {
        val key: String get() = MealPhotos.key(req.date, req.index)
    }

    private fun prefs(ctx: Context) = ctx.applicationContext.getSharedPreferences(PREFS, Context.MODE_PRIVATE)

    @Synchronized
    fun entries(ctx: Context): List<Entry> = prefs(ctx).getString(KEY, null)
        ?.let { runCatching { MyHomeJson.decodeFromString(ListSerializer(Entry.serializer()), it) }.getOrNull() }
        .orEmpty()

    @Synchronized
    private fun save(ctx: Context, list: List<Entry>) {
        prefs(ctx).edit(commit = true) { putString(KEY, MyHomeJson.encodeToString(ListSerializer(Entry.serializer()), list)) }
    }

    /** 아직 서버에 없는 끼니("날짜|끼니") */
    fun keys(ctx: Context): Set<String> = entries(ctx).map { it.key }.toSet()

    /** 그날 보내는 중인(또는 보내지 못한) 끼니 번호 → 항목 */
    fun forDay(ctx: Context, date: String): Map<Int, Entry> = entries(ctx).filter { it.req.date == date }.associateBy { it.req.index }

    /** 기록을 남기고 보내기를 건다. 같은 끼니가 있으면 바꾼다. 사진이 있으면 기록을 보낸 뒤 올리게 표시한다. */
    @Synchronized
    fun add(ctx: Context, req: MealRequest, hasPhotos: Boolean) {
        val e = Entry(req, hasPhotos, System.currentTimeMillis())
        save(ctx, entries(ctx).filter { it.key != e.key } + e)
        if (hasPhotos) MealPhotos.markPending(ctx, req.date, req.index)
        flush(ctx)
    }

    /** 그 끼니를 화면에서 바로 저장했거나 지웠을 때: 남아 있던 옛 기록이 나중에 덮어쓰지 않게 뺀다. */
    @Synchronized
    fun remove(ctx: Context, date: String, index: Int) {
        val k = MealPhotos.key(date, index)
        val cur = entries(ctx)
        if (cur.any { it.key == k }) save(ctx, cur.filter { it.key != k })
    }

    /** 보낸 그 항목만 뺀다(보내는 사이에 다시 저장한 새 기록은 남긴다). */
    @Synchronized
    internal fun removeSent(ctx: Context, sent: Entry) = save(ctx, entries(ctx).filter { it != sent })

    @Synchronized
    internal fun markRejected(ctx: Context, sent: Entry, why: String) =
        save(ctx, entries(ctx).map { if (it == sent) it.copy(error = why) else it })

    /** 보낼 것이 있으면 보내기를 건다(앱 시작·30분 주기에서도 부른다). */
    fun flush(ctx: Context) {
        if (entries(ctx).none { it.error == null }) return
        val req = OneTimeWorkRequestBuilder<MealOutboxWorker>()
            .setConstraints(Constraints.Builder().setRequiredNetworkType(NetworkType.CONNECTED).build())
            .setBackoffCriteria(BackoffPolicy.EXPONENTIAL, 30, TimeUnit.SECONDS)
            .build()
        // 끼니 저장은 덮어쓰기라 다시 보내도 안전하다. 새 항목도 보내도록 돌고 있던 것을 바꾼다
        WorkManager.getInstance(ctx).enqueueUniqueWork(WORK, ExistingWorkPolicy.REPLACE, req)
    }
}

class MealOutboxWorker(context: Context, params: WorkerParameters) : CoroutineWorker(context, params) {
    override suspend fun doWork(): Result = withContext(Dispatchers.IO) {
        val ctx = applicationContext
        val app = MyHomeApp.instance
        var sent = false
        for (e in MealOutbox.entries(ctx).filter { it.error == null }) {
            val r = app.api.saveMeal(e.req)
            when (r.toUploadDecision()) {
                UploadDecision.SENT -> {
                    MealOutbox.removeSent(ctx, e)
                    app.healthLocal.markLogged(e.req.date, e.req.index, true)
                    if (e.hasPhotos) MealPhotos.upload(ctx, e.req.date, e.req.index)
                    sent = true
                }
                UploadDecision.REJECTED -> MealOutbox.markRejected(ctx, e, r.describe())
                // 연결·서버 문제, 토큰 문제: 기다렸다가 다시(토큰을 고치면 30분 주기가 다시 건다)
                UploadDecision.RETRY, UploadDecision.AUTH_FAILED -> {
                    if (sent) Reminders.reschedule(ctx)
                    return@withContext Result.retry()
                }
            }
        }
        if (sent) Reminders.reschedule(ctx)
        Result.success()
    }
}
