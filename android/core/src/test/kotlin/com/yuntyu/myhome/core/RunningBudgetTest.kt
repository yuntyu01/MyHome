package com.yuntyu.myhome.core

import com.yuntyu.myhome.core.api.TransactionDto
import org.junit.Test
import org.junit.Assert.assertEquals

/** 결제 뒤 남은 돈 (합성 거래) */
class RunningBudgetTest {
    private fun tx(id: Long, at: String, amount: Long, flow: String = "out", direction: String = "expense", status: String = "confirmed", cat: Long? = 1) =
        TransactionDto(id = id, occurredAt = "2026-11-${at}+09:00", amount = amount, flow = flow, direction = direction, status = status, categoryId = cat)

    @Test fun countsLikeTheServerSummary() {
        val settle = setOf(9L)
        val list = listOf(
            tx(3, "02T12:00:00", 5_000),                                   // 커피
            tx(1, "01T09:00:00", 10_000),                                  // 점심
            tx(2, "01T20:00:00", 3_000, status = "needs_review"),          // 분류 전: 셈에 안 들어감
            tx(4, "03T10:00:00", 2_000, flow = "in"),                       // 환불
            tx(5, "03T11:00:00", 4_000, flow = "in", direction = "income", cat = 9), // 정산(더치페이 회수)
            tx(6, "03T12:00:00", 50_000, flow = "in", direction = "income", cat = 7), // 월급: 셈에 안 들어감
            tx(7, "04T08:00:00", 1_000, status = "excluded"),               // 뺀 거래
        )
        val r = RunningBudget.remainingAfter(list, 100_000, settle)
        assertEquals(mapOf(1L to 90_000L, 3L to 85_000L, 4L to 87_000L, 5L to 91_000L), r)
    }

    @Test fun sameTimeUsesIdOrderAndOverBudgetGoesNegative() {
        val r = RunningBudget.remainingAfter(listOf(tx(2, "01T09:00:00", 7_000), tx(1, "01T09:00:00", 5_000)), 10_000, emptySet())
        assertEquals(mapOf(1L to 5_000L, 2L to -2_000L), r)
    }
}
