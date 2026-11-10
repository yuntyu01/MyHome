package com.yuntyu.myhome.core

import com.yuntyu.myhome.core.api.TransactionDto

/**
 * 거래마다 "이 결제 뒤 이번 달 남은 돈"(사용자 요청, 2026-11-10: 결제 밑에 작은 글씨로).
 * 서버 요약과 같은 셈이다: 남은 돈 = 예산 − 쓴 돈 + 정산.
 * - 쓴 돈: 확정(confirmed)된 지출(direction=expense). 나간 돈은 더하고 들어온 돈(환불)은 뺀다.
 * - 정산(더치페이 회수): 확정된 정산 분류(settlementIds) 거래는 남은 돈을 늘린다.
 * - 확인 필요·가계부에서 뺀 거래는 남은 돈을 바꾸지 않는다(그래서 그 줄에는 값이 없다).
 * 기간 전체 거래가 있어야 맞는다(일부만 받았으면 부르지 않는다). 같은 시각이면 id 순서로 본다.
 */
object RunningBudget {
    /** 남은 돈을 줄인 만큼(늘렸으면 음수). 남은 돈에 영향이 없으면 0 */
    fun delta(t: TransactionDto, settlementIds: Set<Long>): Long = when {
        t.status != "confirmed" -> 0L
        t.categoryId != null && t.categoryId in settlementIds -> -t.amount
        t.direction == "expense" -> if (t.flow == "out") t.amount else -t.amount
        else -> 0L
    }

    /** 거래 id → 그 거래 뒤 남은 돈. 남은 돈을 바꾸지 않은 거래는 빠진다 */
    fun remainingAfter(period: List<TransactionDto>, budget: Long, settlementIds: Set<Long>): Map<Long, Long> {
        var left = budget
        val out = HashMap<Long, Long>()
        for (t in period.sortedWith(compareBy<TransactionDto> { it.occurredAt }.thenBy { it.id })) {
            val d = delta(t, settlementIds)
            if (d == 0L) continue
            left -= d
            out[t.id] = left
        }
        return out
    }
}
