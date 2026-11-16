package com.yuntyu.myhome.core

import com.yuntyu.myhome.core.api.TransactionDto
import java.time.Duration
import java.time.OffsetDateTime

/**
 * 직접 "중복으로 합치기"할 후보(사용자 요청, 2026-11-16): 자동으로 못 합친 같은 결제.
 * 같은 방향·같은 금액이고 가계부에 들어 있는(뺀 것·취소 아님) 다른 결제 중 WINDOW 안, 가까운 것부터.
 * 서버가 같은 조건(금액·방향·뺀 결제·취소)을 한 번 더 본다. docs/DECISIONS.md 「체크카드 승인과 계좌 출금 합치기」
 */
object TxDuplicates {
    val WINDOW: Duration = Duration.ofDays(1)
    const val MAX = 3

    fun isDuplicate(t: TransactionDto) = t.status == "excluded" && t.exclusionReason == "duplicate"

    fun candidates(t: TransactionDto, all: List<TransactionDto>, max: Int = MAX): List<TransactionDto> {
        if (t.status == "excluded" || t.cancelsId != null) return emptyList()
        val at = time(t.occurredAt) ?: return emptyList()
        return all.asSequence()
            .filter { it.id != t.id && it.flow == t.flow && it.amount == t.amount && it.status != "excluded" && it.cancelsId == null }
            .mapNotNull { c -> time(c.occurredAt)?.let { c to Duration.between(at, it).abs() } }
            .filter { (_, d) -> d <= WINDOW }
            .sortedBy { (_, d) -> d }
            .distinctBy { (c, _) -> c.id }
            .take(max)
            .map { it.first }
            .toList()
    }

    private fun time(iso: String): OffsetDateTime? = runCatching { OffsetDateTime.parse(iso) }.getOrNull()
}
