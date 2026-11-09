package com.yuntyu.myhome

import android.app.Application
import com.yuntyu.myhome.core.api.MyHomeApi
import com.yuntyu.myhome.data.AppDatabase
import com.yuntyu.myhome.data.SummaryRepository
import com.yuntyu.myhome.health.HealthLocal
import com.yuntyu.myhome.health.MealPhotos
import com.yuntyu.myhome.health.Reminders
import com.yuntyu.myhome.notify.Notifications
import com.yuntyu.myhome.security.TokenStore
import com.yuntyu.myhome.settings.AppSettings
import com.yuntyu.myhome.widget.WidgetRefresher
import com.yuntyu.myhome.work.WorkScheduler

/** 앱 전체에서 공유하는 객체. DI 프레임워크 없이 수동으로 조립한다. */
class MyHomeApp : Application() {
    lateinit var db: AppDatabase
        private set
    lateinit var settings: AppSettings
        private set
    lateinit var tokenStore: TokenStore
        private set
    lateinit var api: MyHomeApi
        private set
    lateinit var summaryRepo: SummaryRepository
        private set

    lateinit var healthLocal: HealthLocal
        private set

    /** 화면이 닫혀도 끝까지 돌아야 하는 짧은 작업(사진 올리기 등) */
    val ioScope = kotlinx.coroutines.CoroutineScope(kotlinx.coroutines.SupervisorJob() + kotlinx.coroutines.Dispatchers.IO)

    override fun onCreate() {
        super.onCreate()
        instance = this
        db = AppDatabase.create(this)
        settings = AppSettings(this)
        tokenStore = TokenStore(this)
        api = MyHomeApi(baseUrl = { settings.serverUrl }, token = { tokenStore.read() })
        summaryRepo = SummaryRepository(this)
        healthLocal = HealthLocal(this)
        Notifications.createChannels(this)
        // 앱이 켜질 때마다 남은 대기열을 비우고 주기 작업을 보장한다.
        WorkScheduler.ensurePeriodic(this)
        WorkScheduler.flushNow(this)
        WidgetRefresher.ensurePeriodic(this)
        // 건강: 알림을 다시 걸고, 보관 기간이 지난 끼니 사진을 지우고, 못 보낸 끼니 기록을 보낸다
        Reminders.reschedule(this)
        MealPhotos.purge(this, healthLocal.photoKeepDays)
        com.yuntyu.myhome.health.MealOutbox.flush(this)
        com.yuntyu.myhome.health.GymGeofence.register(this)
        // 집중 타이머: 돌던 타이머와 알림을 되살리고, 공부 장소 지오펜스를 다시 걸고, 못 보낸 기록을 보낸다
        com.yuntyu.myhome.focus.FocusTimer.init(this)
        com.yuntyu.myhome.focus.StudyPlaces.register(this)
        com.yuntyu.myhome.focus.FocusSync.enqueue(this)
    }

    companion object {
        lateinit var instance: MyHomeApp
            private set
    }
}
