package com.yuntyu.myhome.core

import com.yuntyu.myhome.core.api.Summary

/** 예산 알림 한 건. level은 80(80% 도달) 또는 100(초과). */
data class BudgetAlert(
    val key: String,
    val categoryId: Long,
    val categoryName: String,
    val level: Int,
    val remaining: Long,
    val budget: Long,
) {
    val title: String
        get() = if (level == 100) "$categoryName 예산 초과" else "$categoryName 예산 80% 사용"

    val text: String
        get() = if (level == 100) "${Money.won(-remaining)} 넘었어요 (예산 ${Money.won(budget)})"
        else "남은 돈 ${Money.won(remaining)} (예산 ${Money.won(budget)})"
}

/**
 * 예산 알림 판단. 같은 기간·분류·단계는 한 번만 알린다(보낸 키를 기억).
 * 초과를 처음 감지하면 80% 알림은 건너뛰고 초과만 알린다.
 */
object BudgetAlerts {
    fun key(periodLabel: String, categoryId: Long, level: Int) = "$periodLabel:$categoryId:$level"

    fun pending(s: Summary, sent: Set<String>): List<BudgetAlert> {
        val out = mutableListOf<BudgetAlert>()
        for (c in s.categories) {
            // 직접 예산을 정한 분류만 알린다. 예산을 안 정한 폴더(아래 합계)까지 알리면 같은 초과를 두 번 알린다.
            if ((c.ownBudget ?: c.budget) <= 0 || c.budget <= 0) continue
            val k100 = key(s.period.label, c.id, 100)
            val k80 = key(s.period.label, c.id, 80)
            when {
                c.remaining < 0 && k100 !in sent -> out += BudgetAlert(k100, c.id, c.name, 100, c.remaining, c.budget)
                c.remaining >= 0 && c.ratio >= 0.8 && k80 !in sent && k100 !in sent ->
                    out += BudgetAlert(k80, c.id, c.name, 80, c.remaining, c.budget)
            }
        }
        return out
    }

    /** 알림을 보낸 뒤 기억할 키. 초과 알림을 보냈으면 80% 키도 함께 기억한다. */
    fun keysToRemember(alerts: List<BudgetAlert>, periodLabel: String): Set<String> =
        alerts.flatMap { a -> if (a.level == 100) listOf(a.key, key(periodLabel, a.categoryId, 80)) else listOf(a.key) }.toSet()

    /** 지난 기간 키는 버린다(저장 공간이 계속 늘지 않게). */
    fun prune(sent: Set<String>, periodLabel: String): Set<String> = sent.filter { it.startsWith("$periodLabel:") }.toSet()
}
