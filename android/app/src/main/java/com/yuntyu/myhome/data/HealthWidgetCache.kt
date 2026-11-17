package com.yuntyu.myhome.data

import android.content.Context
import androidx.core.content.edit
import com.yuntyu.myhome.MyHomeApp
import com.yuntyu.myhome.core.HealthPlan
import com.yuntyu.myhome.core.HealthWidget
import com.yuntyu.myhome.core.HealthWidgetData
import com.yuntyu.myhome.core.WidgetBody
import com.yuntyu.myhome.core.api.ApiResult
import com.yuntyu.myhome.core.api.DayCard
import com.yuntyu.myhome.core.api.HealthToday
import com.yuntyu.myhome.core.api.MyHomeJson
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.withContext
import java.time.LocalDate
import java.time.LocalDateTime
import java.time.format.DateTimeFormatter

/**
 * 위젯 건강 칸이 그리는 것: 오늘 식단 합계(칼로리·탄단지와 목표), 끼니 표(끼니마다 시각·칼로리·탄단지), 최근 몸무게.
 * 위젯은 네트워크를 쓰지 않으므로 앱 건강 화면·몸 기록을 열 때, 끼니를 보낸 뒤, 위젯 갱신 주기(30분·결제 전송 직후)에 받아 둔다.
 * 숫자만 남긴다(음식 이름·사진 없음, 앱 백업 제외).
 */
class HealthWidgetCache(context: Context) {
    private val prefs = context.getSharedPreferences("health_widget", Context.MODE_PRIVATE)
    private val _data = MutableStateFlow(load())
    val data: StateFlow<HealthWidgetData?> = _data.asStateFlow()

    private fun load(): HealthWidgetData? {
        prefs.getString(KEY, null)?.let { s ->
            runCatching { return MyHomeJson.decodeFromString(HealthWidgetData.serializer(), s) }
        }
        // 끼니 표를 넣기 전(2026-11-17)에 남긴 오늘 합계만 있는 경우
        return prefs.getString(OLD_KEY, null)?.let { s ->
            runCatching { HealthWidgetData(today = MyHomeJson.decodeFromString(HealthToday.serializer(), s)) }.getOrNull()
        }
    }

    private fun save(d: HealthWidgetData) {
        prefs.edit { putString(KEY, MyHomeJson.encodeToString(HealthWidgetData.serializer(), d)); remove(OLD_KEY) }
        _data.value = d
    }

    /** 오늘 합계와(있으면) 그날 끼니 기록. 기록을 못 받았으면 같은 날의 끼니 표는 그대로, 날이 바뀌었으면 비운다 */
    fun set(today: HealthToday, card: DayCard?) {
        val cur = _data.value
        val meals = when {
            card != null -> HealthWidget.mealsOf(card)
            cur?.today?.date == today.date -> cur.meals
            else -> emptyList()
        }
        // 식단 표(rows)는 위젯에 안 쓰므로 빼고 남긴다
        save(HealthWidgetData(today.copy(rows = null), meals, cur?.body))
    }

    fun setBody(body: WidgetBody?) = save((_data.value ?: HealthWidgetData()).copy(body = body))

    /** 끼니 일정 기준 날(자정 넘긴 끼니면 어제)의 오늘 요약·끼니 기록·몸무게를 받는다. 오늘 요약을 못 받으면 캐시 그대로 두고 false */
    suspend fun refresh(app: MyHomeApp): Boolean = withContext(Dispatchers.IO) {
        val now = LocalDateTime.now()
        val day = HealthPlan.scheduleDay(app.healthLocal.schedule.meals, now)
        val nowText = if (day == now.toLocalDate()) now.format(HHMM) else "23:59"
        val r = app.api.healthToday(day.toString(), nowText)
        if (r is ApiResult.Ok) set(r.value, (app.api.healthDay(day.toString()) as? ApiResult.Ok)?.value)
        val w = app.api.bodyWeights(from = LocalDate.now().minusDays(BODY_DAYS).toString())
        if (w is ApiResult.Ok) setBody(HealthWidget.bodyOf(w.value.weights))
        r is ApiResult.Ok
    }

    companion object {
        private const val KEY = "data"
        private const val OLD_KEY = "today"
        /** 몸무게는 이만큼 지난 기록까지만 본다(앱 건강 첫 화면의 몸무게 줄과 같다) */
        const val BODY_DAYS = 120L
        private val HHMM = DateTimeFormatter.ofPattern("HH:mm")
    }
}
