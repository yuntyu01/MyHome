package com.yuntyu.myhome.core

import com.yuntyu.myhome.core.api.DayCard
import com.yuntyu.myhome.core.api.HealthToday
import com.yuntyu.myhome.core.api.Nutrition
import com.yuntyu.myhome.core.api.Weight
import kotlinx.serialization.Serializable
import java.time.LocalDate
import java.time.OffsetDateTime
import java.time.ZoneId

/** 위젯 건강 표 한 줄: 먹은 끼니(번호, 먹은 시각 "08:10", 그 끼니 칼로리·탄단지) */
@Serializable
data class WidgetMeal(val index: Int, val time: String = "", val nutrition: Nutrition = Nutrition())

/** 위젯 몸무게: 최근 값(날짜)과 그 전 기록과의 차이 */
@Serializable
data class WidgetBody(val date: String, val weightKg: Double, val diffKg: Double? = null)

/**
 * 위젯 건강 칸에 그릴 것(위젯은 네트워크를 안 써서 앱이 받아 둔다): 오늘 합계·목표, 끼니별 표, 최근 몸무게.
 * 표는 사용자 요청(2026-11-17 "탄단지칼은 한 끼당 표 하고 상단에 탄단지칼 어느 정도 했는지").
 */
@Serializable
data class HealthWidgetData(
    val today: HealthToday? = null,
    val meals: List<WidgetMeal> = emptyList(),
    val body: WidgetBody? = null,
)

object HealthWidget {
    /** 하루 기록의 먹은 끼니를 먹은 시각 순으로(같으면 번호 순). 시각은 폰 시간대 "HH:mm", 못 읽으면 "" */
    fun mealsOf(card: DayCard?, zone: ZoneId = ZoneId.systemDefault()): List<WidgetMeal> =
        card?.meals.orEmpty()
            .sortedWith(compareBy({ it.loggedAt }, { it.index }))
            .map { m ->
                val time = runCatching { OffsetDateTime.parse(m.loggedAt).atZoneSameInstant(zone).toLocalTime().toString().take(5) }.getOrDefault("")
                WidgetMeal(m.index, time, m.nutrition)
            }

    /** 몸무게 기록(날짜 순)의 마지막 값과 그 전 값과의 차이(소수 한 자리). 기록이 없으면 null */
    fun bodyOf(list: List<Weight>): WidgetBody? {
        val last = list.lastOrNull() ?: return null
        val prev = list.dropLast(1).lastOrNull()
        return WidgetBody(last.date, last.weightKg, prev?.let { Math.round((last.weightKg - it.weightKg) * 10) / 10.0 })
    }

    /** 받아 둔 것이 지금 끼니 날(day)의 것이 아니면(날이 바뀜) 먹은 양·끼니 표를 비운다. 목표·몸무게는 그대로 */
    fun forDay(data: HealthWidgetData?, day: LocalDate): HealthWidgetData? {
        val t = data?.today ?: return data
        if (t.date == day.toString()) return data
        return data.copy(today = t.copy(date = day.toString(), total = Nutrition(), loggedCount = 0), meals = emptyList())
    }

    /** 표에 보일 끼니: 칸에 들어가는 만큼 최근 것(먹은 순서는 그대로) */
    fun shownMeals(meals: List<WidgetMeal>, max: Int): List<WidgetMeal> = if (max <= 0) emptyList() else meals.takeLast(max)

    /** 72.0 → "72", 72.35 → "72.4" */
    fun kg(v: Double): String {
        val r = Math.round(v * 10) / 10.0
        return if (r % 1.0 == 0.0) r.toLong().toString() else r.toString()
    }

    /** 몸무게 줄: "72.4kg · 지난번보다 −0.3kg", 좁으면(short) "72.4kg (−0.3)". 차이가 없으면 몸무게만 */
    fun bodyText(b: WidgetBody, short: Boolean = false): String {
        val d = b.diffKg ?: return "${kg(b.weightKg)}kg"
        val sign = if (d > 0) "+" else if (d < 0) "−" else "±"
        val diff = sign + kg(kotlin.math.abs(d))
        return if (short) "${kg(b.weightKg)}kg ($diff)" else "${kg(b.weightKg)}kg · 지난번보다 ${diff}kg"
    }
}
