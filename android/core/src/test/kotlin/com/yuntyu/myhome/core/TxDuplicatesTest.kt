package com.yuntyu.myhome.core

import com.yuntyu.myhome.core.api.TransactionDto
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class TxDuplicatesTest {
    private fun tx(id: Long, at: String, amount: Long = 9900, flow: String = "out", status: String = "confirmed", reason: String? = null, cancels: Long? = null) =
        TransactionDto(id = id, occurredAt = at, amount = amount, flow = flow, status = status, exclusionReason = reason, cancelsId = cancels)

    @Test
    fun sameAmountNearbyClosestFirst() {
        val t = tx(1, "2026-11-16T19:00:00+09:00")
        val all = listOf(
            t,
            tx(2, "2026-11-16T21:00:00+09:00"),
            tx(3, "2026-11-16T19:01:00+09:00"),
            tx(4, "2026-11-16T19:00:30+09:00", amount = 5000), // 금액 다름
            tx(5, "2026-11-16T19:00:10+09:00", flow = "in"), // 방향 다름
            tx(6, "2026-11-16T19:00:20+09:00", status = "excluded", reason = "duplicate"), // 이미 뺀 것
            tx(7, "2026-11-16T19:00:40+09:00", cancels = 99), // 취소
            tx(8, "2026-11-18T19:00:00+09:00"), // 하루 넘음
        )
        assertEquals(listOf(3L, 2L), TxDuplicates.candidates(t, all).map { it.id })
    }

    @Test
    fun noneForExcludedOrCancel() {
        val all = listOf(tx(2, "2026-11-16T19:00:05+09:00"))
        assertTrue(TxDuplicates.candidates(tx(1, "2026-11-16T19:00:00+09:00", status = "excluded", reason = "duplicate"), all).isEmpty())
        assertTrue(TxDuplicates.candidates(tx(1, "2026-11-16T19:00:00+09:00", cancels = 5), all).isEmpty())
        assertTrue(TxDuplicates.isDuplicate(tx(1, "x", status = "excluded", reason = "duplicate")))
        assertTrue(!TxDuplicates.isDuplicate(tx(1, "x", status = "excluded", reason = "user")))
    }
}
