package com.yuntyu.myhome.core

import com.yuntyu.myhome.core.api.CategoryBudget
import com.yuntyu.myhome.core.api.PeriodInfo
import com.yuntyu.myhome.core.api.Summary
import com.yuntyu.myhome.core.api.Totals
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class BudgetAlertsTest {
    private fun summary(vararg cats: CategoryBudget) =
        Summary(PeriodInfo("2026-11", "2026-11-01", "2026-11-30"), Totals(), cats.toList())

    private fun cat(id: Long, budget: Long, spent: Long, settled: Long = 0) =
        CategoryBudget(id = id, name = "분류$id", budget = budget, spent = spent, settled = settled, remaining = budget - spent + settled, ratio = if (budget > 0) (spent - settled).toDouble() / budget else 0.0)

    @Test fun eightyPercentOnce() {
        val s = summary(cat(1, 100_000, 85_000), cat(2, 100_000, 50_000))
        val first = BudgetAlerts.pending(s, emptySet())
        assertEquals(listOf(80), first.map { it.level })
        assertEquals("분류1 예산 80% 사용", first[0].title)
        val sent = BudgetAlerts.keysToRemember(first, "2026-11")
        assertTrue(BudgetAlerts.pending(s, sent).isEmpty())
    }

    @Test fun overSkipsEighty() {
        val s = summary(cat(1, 100_000, 120_000))
        val alerts = BudgetAlerts.pending(s, emptySet())
        assertEquals(listOf(100), alerts.map { it.level })
        assertEquals("20,000원 넘었어요 (예산 100,000원)", alerts[0].text)
        val sent = BudgetAlerts.keysToRemember(alerts, "2026-11")
        assertEquals(setOf("2026-11:1:100", "2026-11:1:80"), sent)
        assertTrue(BudgetAlerts.pending(s, sent).isEmpty())
    }

    @Test fun eightyThenOver() {
        val sent = BudgetAlerts.keysToRemember(BudgetAlerts.pending(summary(cat(1, 100_000, 90_000)), emptySet()), "2026-11")
        val later = BudgetAlerts.pending(summary(cat(1, 100_000, 101_000)), sent)
        assertEquals(listOf(100), later.map { it.level })
    }

    @Test fun settlementCountsAndNoBudgetIgnored() {
        // 정산으로 되돌린 금액은 빼고 판단(85,000 지출 - 10,000 정산 = 75%)
        assertTrue(BudgetAlerts.pending(summary(cat(1, 100_000, 85_000, settled = 10_000)), emptySet()).isEmpty())
        assertTrue(BudgetAlerts.pending(summary(cat(2, 0, 50_000)), emptySet()).isEmpty())
    }

    @Test fun pruneOldPeriods() {
        assertEquals(setOf("2026-11:1:80"), BudgetAlerts.prune(setOf("2026-10:1:80", "2026-11:1:80"), "2026-11"))
    }
}
