package com.yuntyu.myhome.core

import com.yuntyu.myhome.core.HealthPlan.Kind
import com.yuntyu.myhome.core.HealthPlan.TileState
import com.yuntyu.myhome.core.api.FoodItem
import com.yuntyu.myhome.core.api.FoodResult
import com.yuntyu.myhome.core.api.MealSlot
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import java.time.LocalDate

// 핏두잉 앱 domain 테스트와 같은 규칙 (합성 값)
class HealthPlanTest {
    private val day = LocalDate.of(2026, 11, 9) // 월요일
    private val four = listOf(MealSlot(1, "08:00"), MealSlot(2, "14:00"), MealSlot(3, "17:00"), MealSlot(4, "19:00"))

    @Test fun deadlinePastMidnightMovesToNextDay() {
        val d = HealthPlan.deadlinesFor(day, listOf(MealSlot(1, "20:00"), MealSlot(2, "00:30")))
        assertEquals(day.atTime(20, 0), d[0].at)
        assertEquals(day.plusDays(1).atTime(0, 30), d[1].at)
        assertEquals(day, d[1].day) // 끼니는 1끼 기준 날짜에 속한다
    }

    @Test fun statusesPickOneCurrent() {
        val d = HealthPlan.deadlinesFor(day, four)
        val s = HealthPlan.statuses(d, setOf(1), day.atTime(15, 0)).map { it.second }
        assertEquals(listOf(TileState.LOGGED, TileState.MISSED, TileState.CURRENT, TileState.UPCOMING), s)
    }

    @Test fun scheduleDayStaysOnYesterdayBeforeLateDeadline() {
        val meals = listOf(MealSlot(1, "12:00"), MealSlot(2, "00:30"))
        assertEquals(day.minusDays(1), HealthPlan.scheduleDay(meals, day.atTime(0, 10)))
        assertEquals(day, HealthPlan.scheduleDay(meals, day.atTime(0, 40)))
    }

    @Test fun resizeKeepsOrUsesDefaults() {
        assertEquals(listOf("09:00", "14:00", "20:00"), HealthPlan.resizeMeals(four, 3).map { it.deadline })
        assertEquals(four, HealthPlan.resizeMeals(four, 4))
        assertEquals(7, HealthPlan.nextExtraIndex(listOf(1, 2)))
        assertNull(HealthPlan.nextExtraIndex(listOf(7, 8, 9)))
        assertEquals("기타 2", HealthPlan.label(8))
    }

    @Test fun remindersSkipLoggedAndPast() {
        val input = HealthPlan.ReminderInput(four, gymEnabled = true, gymDays = listOf(0), arriveBy = "19:00", sleepEnabled = true, bedTime = "23:30")
        val now = day.atTime(13, 0)
        val plan = HealthPlan.planReminders(input, now, setOf(HealthPlan.mealKey(day, 3)), days = 2)
        val today = plan.filter { it.at.toLocalDate() == day }
        // 2끼 13:30(30분 전)·14:00, 3끼는 기록함, 4끼 18:30·19:00, 헬스(월) 18:00, 취침 23:30
        assertEquals(
            listOf("13:30 MEAL_PRE 2", "14:00 MEAL_DUE 2", "18:00 GYM 0", "18:30 MEAL_PRE 4", "19:00 MEAL_DUE 4", "23:30 SLEEP 0"),
            today.map { "%02d:%02d %s %d".format(it.at.hour, it.at.minute, it.kind, it.meal) },
        )
        // 화요일은 헬스 요일이 아니다
        assertTrue(plan.none { it.kind == Kind.GYM && it.at.toLocalDate() == day.plusDays(1) })
        assertTrue(plan.all { it.at.isAfter(now) })
        assertEquals("2끼 마감 30분 전이에요", plan.first().title)
        assertEquals("14:00까지 사진을 찍어 주세요", plan.first().body)
    }

    @Test fun perFoodPortionSum() {
        val items = listOf(
            FoodItem("밥", "1공기", kcal = 300, carbs = 65, protein = 5, fat = 1, portion = 0.5),
            FoodItem("닭가슴살", "100g", kcal = 110, carbs = 0, protein = 23, fat = 1),
        )
        val (ai, portion) = HealthPlan.mealToSave(FoodResult(items, 0, 0, 0, 0, 0.8), 1.5, manualTotals = false)
        assertEquals(260, ai.kcal) // 150 + 110
        assertEquals(33, ai.carbs) // 32.5 → 33
        assertEquals(1.0, portion, 0.0)
        assertEquals(0.8, ai.confidence, 0.0)
    }

    @Test fun manualTotalsKeepMealPortionAndClearFoodPortions() {
        val items = listOf(FoodItem("직접 입력", "", portion = 0.5))
        val (ai, portion) = HealthPlan.mealToSave(FoodResult(items, 500, 50, 20, 10, 0.0), 0.5, manualTotals = false)
        assertEquals(500, ai.kcal)
        assertEquals(0.5, portion, 0.0)
        assertNull(ai.items.single().portion)
    }
}
