package com.yuntyu.myhome

import android.app.Application
import com.kakao.sdk.common.KakaoSdk
import com.yuntyu.myhome.core.api.MyHomeApi
import com.yuntyu.myhome.data.AppDatabase
import com.yuntyu.myhome.notify.Notifications
import com.yuntyu.myhome.security.TokenStore
import com.yuntyu.myhome.settings.AppSettings
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

    override fun onCreate() {
        super.onCreate()
        instance = this
        db = AppDatabase.create(this)
        settings = AppSettings(this)
        tokenStore = TokenStore(this)
        api = MyHomeApi(baseUrl = { settings.serverUrl }, token = { tokenStore.read() })
        Notifications.createChannels(this)
        if (BuildConfig.KAKAO_NATIVE_APP_KEY.isNotBlank()) {
            KakaoSdk.init(this, BuildConfig.KAKAO_NATIVE_APP_KEY)
        }
        // 앱이 켜질 때마다 남은 대기열을 비우고 주기 작업을 보장한다.
        WorkScheduler.ensurePeriodic(this)
        WorkScheduler.flushNow(this)
    }

    companion object {
        lateinit var instance: MyHomeApp
            private set
    }
}
