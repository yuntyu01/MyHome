package com.yuntyu.myhome.health

import android.content.Context
import com.yuntyu.myhome.core.HealthPlan
import com.yuntyu.myhome.core.api.HealthSettings
import com.yuntyu.myhome.core.api.MealSlot
import com.yuntyu.myhome.core.api.MyHomeJson
import kotlinx.serialization.Serializable
import kotlinx.serialization.builtins.ListSerializer
import kotlinx.serialization.builtins.serializer
import java.time.LocalDate

/**
 * 건강 알림을 서버 없이 다시 예약할 수 있게 폰에 남겨 두는 값: 끼니·헬스·취침 일정, 기록한 끼니, 사진 보관 기간.
 * 서버가 진짜 값이고, 이 사본은 설정·오늘 화면을 받을 때마다 갱신한다(알림 수신기는 네트워크를 쓰지 않는다).
 */
class HealthLocal(context: Context) {
    private val prefs = context.applicationContext.getSharedPreferences("health", Context.MODE_PRIVATE)

    @Serializable
    data class Schedule(
        val meals: List<MealSlot> = emptyList(),
        val gymEnabled: Boolean = false,
        val gymDays: List<Int> = emptyList(),
        val arriveBy: String = "19:00",
        val sleepEnabled: Boolean = false,
        val bedTime: String = "23:30",
        val notifications: Boolean = true,
    ) {
        fun reminderInput() = HealthPlan.ReminderInput(meals, gymEnabled, gymDays, arriveBy, sleepEnabled, bedTime)
    }

    var schedule: Schedule
        get() = prefs.getString(KEY_SCHEDULE, null)?.let { runCatching { MyHomeJson.decodeFromString(Schedule.serializer(), it) }.getOrNull() } ?: Schedule()
        set(v) = prefs.edit().putString(KEY_SCHEDULE, MyHomeJson.encodeToString(Schedule.serializer(), v)).apply()

    fun updateFrom(s: HealthSettings) {
        schedule = Schedule(
            meals = s.meals,
            gymEnabled = s.gym?.enabled == true,
            gymDays = s.gym?.days.orEmpty(),
            arriveBy = s.gym?.arriveBy ?: "19:00",
            sleepEnabled = s.sleep?.enabled == true,
            bedTime = s.sleep?.bedTime ?: "23:30",
            notifications = s.notifications != false,
        )
    }

    /** 기록한 끼니("날짜#번호"). 최근 2주만 남긴다. */
    val logged: Set<String>
        get() = prefs.getString(KEY_LOGGED, null)
            ?.let { runCatching { MyHomeJson.decodeFromString(ListSerializer(String.serializer()), it) }.getOrNull() }
            ?.toSet() ?: emptySet()

    private fun saveLogged(keys: Set<String>) {
        val cutoff = LocalDate.now().minusDays(14).toString()
        val kept = keys.filter { it.substringBefore('#') >= cutoff }
        prefs.edit().putString(KEY_LOGGED, MyHomeJson.encodeToString(ListSerializer(String.serializer()), kept)).apply()
    }

    /** 그날 기록한 끼니 번호를 서버 값으로 맞춘다. */
    fun setLoggedForDay(day: String, indexes: Collection<Int>) {
        saveLogged(logged.filterNot { it.startsWith("$day#") }.toSet() + indexes.map { "$day#$it" })
    }

    fun markLogged(day: String, index: Int, on: Boolean) {
        val k = "$day#$index"
        saveLogged(if (on) logged + k else logged - k)
    }

    /** 폰에 끼니 사진을 둘 기간(일). 0이면 계속. */
    var photoKeepDays: Int
        get() = prefs.getInt(KEY_PHOTO_KEEP, 0)
        set(v) = prefs.edit().putInt(KEY_PHOTO_KEEP, v).apply()

    /** 다음 알림으로 예약해 둔 시각(밀리초). 알림이 울릴 때 이 시각부터 지금까지 것을 띄운다. */
    var scheduledAt: Long
        get() = prefs.getLong(KEY_SCHEDULED_AT, 0)
        set(v) = prefs.edit().putLong(KEY_SCHEDULED_AT, v).apply()

    private companion object {
        const val KEY_SCHEDULE = "schedule"
        const val KEY_LOGGED = "logged"
        const val KEY_PHOTO_KEEP = "photo_keep_days"
        const val KEY_SCHEDULED_AT = "scheduled_at"
    }
}
