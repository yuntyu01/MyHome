package com.yuntyu.myhome.data

import android.content.Context
import androidx.core.content.edit
import com.yuntyu.myhome.MyHomeApp
import com.yuntyu.myhome.core.HealthPlan
import com.yuntyu.myhome.core.api.ApiResult
import com.yuntyu.myhome.core.api.HealthToday
import com.yuntyu.myhome.core.api.MyHomeJson
import com.yuntyu.myhome.core.api.Nutrition
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.withContext
import java.time.LocalDate
import java.time.LocalDateTime
import java.time.format.DateTimeFormatter

/**
 * 위젯 건강 칸이 그리는 오늘 식단 합계(칼로리·탄단지와 목표). 위젯은 네트워크를 쓰지 않으므로 앱 건강 화면을 열 때,
 * 끼니를 보낸 뒤, 위젯 갱신 주기(30분·결제 전송 직후)에 받아 둔다. 숫자만 남긴다(앱 백업 제외).
 */
class HealthWidgetCache(context: Context) {
    private val prefs = context.getSharedPreferences("health_widget", Context.MODE_PRIVATE)
    private val _today = MutableStateFlow(load())
    val today: StateFlow<HealthToday?> = _today.asStateFlow()

    private fun load(): HealthToday? = prefs.getString(KEY, null)?.let {
        runCatching { MyHomeJson.decodeFromString(HealthToday.serializer(), it) }.getOrNull()
    }

    fun set(t: HealthToday) {
        // 식단 표(rows)는 위젯에 안 쓰므로 빼고 남긴다
        val slim = t.copy(rows = null)
        prefs.edit { putString(KEY, MyHomeJson.encodeToString(HealthToday.serializer(), slim)) }
        _today.value = slim
    }

    /** 끼니 일정 기준 날(자정 넘긴 끼니면 어제)의 오늘 요약을 받는다. 실패하면 캐시 그대로 두고 false */
    suspend fun refresh(app: MyHomeApp): Boolean = withContext(Dispatchers.IO) {
        val now = LocalDateTime.now()
        val day = HealthPlan.scheduleDay(app.healthLocal.schedule.meals, now)
        val nowText = if (day == now.toLocalDate()) now.format(HHMM) else "23:59"
        val r = app.api.healthToday(day.toString(), nowText)
        if (r is ApiResult.Ok) set(r.value)
        r is ApiResult.Ok
    }

    companion object {
        private const val KEY = "today"
        private val HHMM = DateTimeFormatter.ofPattern("HH:mm")

        /** 받아 둔 것이 지금 끼니 날의 것이 아니면(날이 바뀜) 먹은 양만 0으로 보인다. 목표는 그대로 */
        fun forDay(cached: HealthToday?, day: LocalDate): HealthToday? = when {
            cached == null -> null
            cached.date == day.toString() -> cached
            else -> cached.copy(date = day.toString(), total = Nutrition(), loggedCount = 0)
        }
    }
}
