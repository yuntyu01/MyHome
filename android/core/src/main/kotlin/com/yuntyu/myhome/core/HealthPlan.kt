package com.yuntyu.myhome.core

import com.yuntyu.myhome.core.api.FoodItem
import com.yuntyu.myhome.core.api.FoodResult
import com.yuntyu.myhome.core.api.MealSlot
import java.time.DayOfWeek
import java.time.LocalDate
import java.time.LocalDateTime
import java.time.LocalTime
import kotlin.math.max
import kotlin.math.roundToInt

/*
 * 건강(핏두잉 이식)의 화면 없는 계산. 핏두잉 앱의 domain/meals.ts, notifications.ts, portion.ts와 같은 규칙이다.
 * 시각은 전부 폰의 지역 시각(LocalDateTime)으로 다룬다.
 */
object HealthPlan {
    /** 1~6은 계획한 끼니(마감 있음), 7~9는 "기타"(간식 등, 마감 없음). 서버와 같은 번호. */
    const val EXTRA_BASE = 7
    const val MAX_EXTRA = 3
    const val PRE_DEADLINE_MIN = 30
    /** 헬스는 도착 시간만 받는다. 알림은 도착 1시간 전. */
    const val GYM_LEAD_MIN = 60
    const val HORIZON_DAYS = 7

    /** 먹은 양 (끼니 전체). 1.5·2배는 AI가 큰 그릇을 자주 작게 봐서 둔다. */
    val PORTIONS = listOf(0.25, 0.5, 1.0, 1.5, 2.0)
    /** 음식별 먹은 양 */
    val ITEM_PORTIONS = listOf(0.25, 0.5, 0.75, 1.0, 1.5, 2.0)

    /** 끼니 수를 바꿀 때 새 끼니의 기본 마감 */
    val DEFAULT_DEADLINES = mapOf(
        3 to listOf("09:00", "14:00", "20:00"),
        4 to listOf("08:00", "14:00", "17:00", "19:00"),
        5 to listOf("08:00", "11:00", "14:00", "17:00", "20:00"),
        6 to listOf("08:00", "10:30", "13:00", "15:30", "18:00", "21:00"),
    )

    fun isExtra(index: Int) = index >= EXTRA_BASE

    /** "2끼", 기타는 "기타 1" */
    fun label(index: Int) = if (isExtra(index)) "기타 ${index - EXTRA_BASE + 1}" else "${index}끼"

    fun minutes(hhmm: String): Int {
        val (h, m) = hhmm.split(":").map { it.trim().toInt() }
        return h * 60 + m
    }

    fun hhmm(minutes: Int): String {
        val m = ((minutes % 1440) + 1440) % 1440
        return "%02d:%02d".format(m / 60, m % 60)
    }

    fun validHhmm(s: String): Boolean = Regex("^([01]\\d|2[0-3]):[0-5]\\d$").matches(s)

    data class Deadline(val index: Int, val day: LocalDate, val at: LocalDateTime)

    /** 하루치 마감을 실제 시각으로 펼친다. 앞 끼니보다 이르거나 같으면 자정을 넘긴 것으로 보고 다음 날로 민다. */
    fun deadlinesFor(day: LocalDate, meals: List<MealSlot>): List<Deadline> {
        var offset = 0
        var prev = -1
        return meals.sortedBy { it.index }.map { m ->
            var min = minutes(m.deadline) + offset
            if (min <= prev) {
                offset += 1440
                min += 1440
            }
            prev = min
            Deadline(m.index, day, day.atStartOfDay().plusMinutes(min.toLong()))
        }
    }

    enum class TileState { LOGGED, CURRENT, UPCOMING, MISSED }

    /** 기록한 끼니는 LOGGED, 마감 지난 미기록은 MISSED, 마감 전 미기록 중 가장 이른 하나만 CURRENT. */
    fun statuses(deadlines: List<Deadline>, logged: Set<Int>, now: LocalDateTime): List<Pair<Deadline, TileState>> {
        var currentTaken = false
        return deadlines.map { d ->
            val s = when {
                d.index in logged -> TileState.LOGGED
                !d.at.isAfter(now) -> TileState.MISSED
                !currentTaken -> { currentTaken = true; TileState.CURRENT }
                else -> TileState.UPCOMING
            }
            d to s
        }
    }

    /** 지금이 어느 날의 끼니 일정인지: 어제 마지막 마감이 아직 안 지났으면(자정 넘긴 끼니) 어제. */
    fun scheduleDay(meals: List<MealSlot>, now: LocalDateTime): LocalDate {
        val yesterday = now.toLocalDate().minusDays(1)
        val last = deadlinesFor(yesterday, meals).lastOrNull()
        return if (last != null && last.at.isAfter(now)) yesterday else now.toLocalDate()
    }

    /** 끼니 수를 바꾼다. 수가 같으면 마감 유지, 다르면 기본값. */
    fun resizeMeals(meals: List<MealSlot>, count: Int): List<MealSlot> {
        val defaults = DEFAULT_DEADLINES[count] ?: DEFAULT_DEADLINES.getValue(4)
        return (0 until count).map { i -> MealSlot(i + 1, if (meals.size == count) meals[i].deadline else defaults[i]) }
    }

    /** 그날 아직 안 쓴 기타 번호(7→8→9), 다 썼으면 null */
    fun nextExtraIndex(used: Collection<Int>): Int? = (EXTRA_BASE until EXTRA_BASE + MAX_EXTRA).firstOrNull { it !in used }

    fun mealKey(day: LocalDate, index: Int) = "$day#$index"

    enum class Kind { MEAL_PRE, MEAL_DUE, GYM, SLEEP }

    data class Planned(val id: String, val at: LocalDateTime, val kind: Kind, val meal: Int = 0, val time: String = "") {
        val title: String
            get() = when (kind) {
                Kind.MEAL_PRE -> "${meal}끼 마감 30분 전이에요"
                Kind.MEAL_DUE -> "${meal}끼 마감 시간이에요"
                Kind.GYM -> "헬스 갈 시간이에요"
                Kind.SLEEP -> "잘 시간이에요"
            }
        val body: String
            get() = when (kind) {
                Kind.MEAL_PRE -> "${time}까지 사진을 찍어 주세요"
                Kind.MEAL_DUE -> "아직 사진이 없어요. 지금 찍어 주세요"
                Kind.GYM -> "${time}까지 헬스장에 도착해 주세요"
                Kind.SLEEP -> "오늘 취침 계획은 ${time}이에요"
            }
    }

    data class ReminderInput(
        val meals: List<MealSlot>,
        val gymEnabled: Boolean,
        val gymDays: List<Int>,
        val arriveBy: String,
        val sleepEnabled: Boolean,
        val bedTime: String,
    )

    /**
     * 앞으로 며칠치 알림을 날짜별 일회성으로 펼친다(이미 찍은 끼니는 뺀다). 지난 것은 버리고 시각 순.
     * logged는 mealKey(날짜, 끼니) 모음.
     */
    fun planReminders(input: ReminderInput, now: LocalDateTime, logged: Set<String>, days: Int = HORIZON_DAYS): List<Planned> {
        val out = mutableListOf<Planned>()
        val first = scheduleDay(input.meals, now)
        for (i in 0 until days) {
            val day = first.plusDays(i.toLong())
            for (d in deadlinesFor(day, input.meals)) {
                if (mealKey(d.day, d.index) in logged) continue
                val time = hhmm(d.at.hour * 60 + d.at.minute)
                out += Planned("meal-pre-${d.day}-${d.index}", d.at.minusMinutes(PRE_DEADLINE_MIN.toLong()), Kind.MEAL_PRE, d.index, time)
                out += Planned("meal-due-${d.day}-${d.index}", d.at, Kind.MEAL_DUE, d.index, time)
            }
            if (input.gymEnabled && weekdayMon0(day) in input.gymDays) {
                val at = day.atStartOfDay().plusMinutes((minutes(input.arriveBy) - GYM_LEAD_MIN).toLong())
                out += Planned("gym-$day", at, Kind.GYM, time = input.arriveBy)
            }
            if (input.sleepEnabled) {
                out += Planned("sleep-$day", day.atStartOfDay().plusMinutes(minutes(input.bedTime).toLong()), Kind.SLEEP, time = input.bedTime)
            }
        }
        return out.filter { it.at.isAfter(now) }.sortedBy { it.at }
    }

    /** 0=월 … 6=일 */
    fun weekdayMon0(d: LocalDate): Int = d.dayOfWeek.value - DayOfWeek.MONDAY.value

    /** 오늘 도착 시각까지 남은 분(지났으면 음수) */
    fun minutesUntil(now: LocalDateTime, hhmm: String): Long =
        java.time.Duration.between(now, now.toLocalDate().atTime(LocalTime.of(minutes(hhmm) / 60, minutes(hhmm) % 60))).toMinutes()

    // ── 먹은 양 (portion.ts) ──────────────────────────────

    fun itemPortion(it: FoodItem): Double = it.portion?.takeIf { p -> p > 0 } ?: 1.0

    fun hasNumbers(it: FoodItem) = it.kcal != null && it.carbs != null && it.protein != null && it.fat != null

    /** 음식이 하나 이상이고 모두 숫자가 있으면 음식별 먹은 양으로 합계를 낸다. */
    fun canPortionPerFood(items: List<FoodItem>) = items.isNotEmpty() && items.all(::hasNumbers)

    /** 먹은 음식들의 합(음식 숫자 × 먹은 비율), 0 이상 정수로 반올림. */
    fun sumEaten(items: List<FoodItem>): FoodResult {
        var k = 0.0; var c = 0.0; var p = 0.0; var f = 0.0
        for (it in items) {
            val r = itemPortion(it)
            k += (it.kcal ?: 0) * r; c += (it.carbs ?: 0) * r; p += (it.protein ?: 0) * r; f += (it.fat ?: 0) * r
        }
        fun n(x: Double) = max(0, jsRound(x))
        return FoodResult(items, n(k), n(c), n(p), n(f))
    }

    /**
     * 기록 확인 화면에서 저장할 값. 음식별 숫자가 다 있고 합계를 손으로 고치지 않았으면 합계 = 먹은 음식 합, 끼니 먹은 양 1.
     * 아니면 지금 합계와 끼니 먹은 양 그대로, 음식별 비율은 지운다.
     */
    fun mealToSave(ai: FoodResult, portion: Double, manualTotals: Boolean): Pair<FoodResult, Double> =
        if (!manualTotals && canPortionPerFood(ai.items)) {
            sumEaten(ai.items).copy(confidence = ai.confidence) to 1.0
        } else {
            ai.copy(items = ai.items.map { it.copy(portion = null) }) to portion
        }

    /** 끼니 먹은 양과 나눠 먹음을 곱한 실제 비율(나눠 먹으면 절반). */
    fun eatenFactor(portion: Double, shared: Boolean) = portion * (if (shared) 0.5 else 1.0)

    /** JS Math.round와 같은 반올림(.5는 위로). */
    fun jsRound(x: Double): Int = kotlin.math.floor(x + 0.5).toInt()

    /** 칼로리 막대 비율(0~1로 자르지 않음) */
    fun ratio(value: Int, target: Int): Double = if (target <= 0) 0.0 else value.toDouble() / target

    fun percent(value: Int, target: Int): Int = (ratio(value, target) * 100).roundToInt()
}
