package com.yuntyu.myhome.widget

import android.content.Context
import androidx.glance.appwidget.updateAll
import androidx.work.Constraints
import androidx.work.CoroutineWorker
import androidx.work.ExistingPeriodicWorkPolicy
import androidx.work.NetworkType
import androidx.work.PeriodicWorkRequestBuilder
import androidx.work.WorkManager
import androidx.work.WorkerParameters
import com.yuntyu.myhome.MyHomeApp
import com.yuntyu.myhome.core.api.ApiResult
import com.yuntyu.myhome.notify.BudgetAlertNotifier
import com.yuntyu.myhome.notify.InvestAlertNotifier
import com.yuntyu.myhome.health.GymGeofence
import com.yuntyu.myhome.health.MealOutbox
import com.yuntyu.myhome.health.MealPhotos
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import java.util.concurrent.TimeUnit

/** 위젯 갱신: 요약을 다시 받아 캐시에 넣고 모든 위젯을 다시 그린다. */
object WidgetRefresher {
    private const val PERIODIC = "widget-refresh"

    /** @return 요약을 새로 받았으면 true */
    suspend fun refresh(context: Context): Boolean {
        val app = context.applicationContext as MyHomeApp
        val r = app.summaryRepo.refresh()
        // 실패해도 캐시로 다시 그린다(로그인 직후 등 상태 변화 반영)
        SummaryWidget().updateAll(context)
        if (r is ApiResult.Ok) BudgetAlertNotifier.check(context, r.value)
        // 매매 실패 알림도 같은 주기(30분·결제 전송 직후)에 확인한다. 서버가 푸시를 보내지 않으므로 여기서 가져온다
        if (r is ApiResult.Ok) InvestAlertNotifier.check(context)
        // 못 보낸 끼니 기록 보내기(작업으로 건다), 올리지 못한 끼니 사진 다시 올리기
        MealOutbox.flush(context)
        if (r is ApiResult.Ok) withContext(Dispatchers.IO) { MealPhotos.retryPending(context) }
        // 헬스장 지오펜스 다시 걸기: 위치를 껐다 켜면 시스템이 지운 채로 두므로(실패 이유는 남겨서 헬스 화면에 보인다)
        GymGeofence.register(context)
        return r is ApiResult.Ok
    }

    /** 30분마다 갱신(결제가 없어도 다른 기기·웹에서 바꾼 내용을 반영). */
    fun ensurePeriodic(context: Context) {
        val req = PeriodicWorkRequestBuilder<WidgetRefreshWorker>(30, TimeUnit.MINUTES)
            .setConstraints(Constraints.Builder().setRequiredNetworkType(NetworkType.CONNECTED).build())
            .build()
        WorkManager.getInstance(context).enqueueUniquePeriodicWork(PERIODIC, ExistingPeriodicWorkPolicy.KEEP, req)
    }
}

class WidgetRefreshWorker(context: Context, params: WorkerParameters) : CoroutineWorker(context, params) {
    override suspend fun doWork(): Result {
        WidgetRefresher.refresh(applicationContext)
        // 실패는 다음 주기에 다시 시도한다(재시도 폭주 방지)
        return Result.success()
    }
}
