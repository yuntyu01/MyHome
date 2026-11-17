package com.yuntyu.myhome.core

import com.yuntyu.myhome.core.api.DayCard
import com.yuntyu.myhome.core.api.HealthToday
import com.yuntyu.myhome.core.api.MealView
import com.yuntyu.myhome.core.api.Nutrition
import com.yuntyu.myhome.core.api.Weight
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test
import java.time.LocalDate
import java.time.ZoneId

/** 위젯 건강 표·몸무게 (합성 값) */
class HealthWidgetTest {
    private val kst = ZoneId.of("Asia/Seoul")

    @Test fun mealsInEatenOrderWithLocalTime() {
        val card = DayCard(
            date = "2026-11-17",
            meals = listOf(
                MealView(index = 2, nutrition = Nutrition(820, 100, 58, 19), loggedAt = "2026-11-17T12:40:00+09:00"),
                MealView(index = 1, nutrition = Nutrition(420, 52, 30, 12), loggedAt = "2026-11-17T08:10:00+09:00"),
                MealView(index = 3, nutrition = Nutrition(100, 10, 5, 2), loggedAt = "bad"),
            ),
        )
        val meals = HealthWidget.mealsOf(card, kst)
        // 시각을 못 읽은 것("bad")은 문자열 순으로 뒤에, 시각은 ""
        assertEquals(listOf(1, 2, 3), meals.map { it.index })
        assertEquals(listOf("08:10", "12:40", ""), meals.map { it.time })
        assertEquals(Nutrition(820, 100, 58, 19), meals[1].nutrition)
        assertEquals(emptyList<WidgetMeal>(), HealthWidget.mealsOf(null, kst))
        // 칸에 3줄이면 최근 3끼
        val four = (1..4).map { WidgetMeal(it) }
        assertEquals(listOf(2, 3, 4), HealthWidget.shownMeals(four, 3).map { it.index })
        assertEquals(emptyList<WidgetMeal>(), HealthWidget.shownMeals(four, 0))
    }

    @Test fun tableHasEveryPlannedMealThenExtras() {
        val n = Nutrition(420, 52, 30, 12)
        val meals = listOf(WidgetMeal(2, "12:40", n), WidgetMeal(101, "15:00", Nutrition(100)), WidgetMeal(1, "08:10", n))
        assertEquals(listOf(1 to n, 2 to n, 3 to null, 101 to Nutrition(100)), HealthWidget.tableRows(meals, 3))
        assertEquals(emptyList<Pair<Int, Nutrition?>>(), HealthWidget.tableRows(emptyList(), 0))
    }

    @Test fun bodyLastAndDiff() {
        assertNull(HealthWidget.bodyOf(emptyList()))
        assertEquals(WidgetBody("2026-11-16", 72.4, null), HealthWidget.bodyOf(listOf(Weight("2026-11-16", 72.4))))
        val b = HealthWidget.bodyOf(listOf(Weight("2026-11-10", 72.7), Weight("2026-11-16", 72.4)))!!
        assertEquals(-0.3, b.diffKg!!, 1e-9)
        assertEquals("72.4kg · 지난번보다 −0.3kg", HealthWidget.bodyText(b))
        assertEquals("72kg · 지난번보다 +1kg", HealthWidget.bodyText(WidgetBody("x", 72.0, 1.0)))
        assertEquals("72kg", HealthWidget.bodyText(WidgetBody("x", 72.0)))
        assertEquals("72.4kg (−0.3)", HealthWidget.bodyText(b, short = true))
    }

    @Test fun newDayClearsEatenButKeepsTargetsAndBody() {
        val data = HealthWidgetData(
            today = HealthToday(date = "2026-11-16", loggedCount = 2, total = Nutrition(1000), targets = Nutrition(2100)),
            meals = listOf(WidgetMeal(1)),
            body = WidgetBody("2026-11-16", 72.4),
        )
        assertEquals(data, HealthWidget.forDay(data, LocalDate.of(2026, 11, 16)))
        val next = HealthWidget.forDay(data, LocalDate.of(2026, 11, 17))!!
        assertEquals(Nutrition(), next.today!!.total)
        assertEquals(0, next.today!!.loggedCount)
        assertEquals(Nutrition(2100), next.today!!.targets)
        assertEquals(emptyList<WidgetMeal>(), next.meals)
        assertEquals(data.body, next.body)
        assertNull(HealthWidget.forDay(null, LocalDate.of(2026, 11, 17)))
    }
}
