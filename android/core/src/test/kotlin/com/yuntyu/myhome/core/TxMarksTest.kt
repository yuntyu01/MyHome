package com.yuntyu.myhome.core

import com.yuntyu.myhome.core.api.TransactionDto
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

/** 거래 줄 앞 표시 고르기 (합성 가게 이름) */
class TxMarksTest {
    private fun tx(merchant: String, category: String = "", status: String = "confirmed", direction: String = "expense", flow: String = "out") =
        TransactionDto(id = 1, occurredAt = "2026-11-10T12:00:00+09:00", amount = 1000, flow = flow, merchant = merchant, direction = direction, category = category, status = status)

    private fun label(m: String) = (TxMarks.of(tx(m)) as? TxMark.Brand)?.label

    @Test fun knownShopsGetBrandColorAndLetters() {
        assertEquals("7", label("세븐일레븐 역삼언주로점"))
        assertEquals("MGC", label("메가엠지씨커피 역삼GFC점"))
        assertEquals("M", label("매머드익스프레스역삼언주로점"))
        assertEquals("이츠", label("쿠팡이츠")) // 쿠팡보다 먼저
        assertEquals("C", label("쿠팡(쿠페이)"))
        assertEquals("24", label("이마트24 선릉점")) // 이마트보다 먼저
        assertEquals("CU", label("CU 역삼점"))
        assertEquals("GS", label("GS25 역삼점"))
        assertNull(label("CUP CAFE")) // 영문 안의 CU는 아님
        assertNull(label("토스트하우스")) // 토스와 헷갈리지 않게
    }

    @Test fun transitIncomeTransferReviewAndCategory() {
        assertEquals(TxMark.Kind(MarkKind.TRANSIT), TxMarks.of(tx("10월교통카드이용")))
        assertEquals(TxMark.Kind(MarkKind.TRANSIT), TxMarks.of(tx("ezl 지하철 5건 이용")))
        assertEquals(TxMark.Kind(MarkKind.INCOME), TxMarks.of(tx("카드 캐시백", flow = "in", direction = "income")))
        assertEquals(TxMark.Kind(MarkKind.TRANSFER), TxMarks.of(tx("토스증권", direction = "transfer")))
        assertEquals(TxMark.Unknown, TxMarks.of(tx("합성상회", status = "needs_review")))
        assertEquals(TxMark.Kind(MarkKind.CAFE), TxMarks.of(tx("동네카페 역삼점", "카페·간식")))
        assertEquals(TxMark.Kind(MarkKind.FOOD), TxMarks.of(tx("김밥천국 선릉점", "식비")))
        val custom = TxMarks.of(tx("합성학원", "자기계발")) as TxMark.Letter
        assertEquals("자", custom.letter)
    }
}

/** 홈 분류 박스 미리보기: 폴더면 그 아래 전부에서 새것부터 n개, 뺀 것은 빼고 (합성) */
class RecentInTest {
    @Test fun folderIncludesDescendantsNewestFirst() {
        val s = com.yuntyu.myhome.core.api.Summary(
            period = com.yuntyu.myhome.core.api.PeriodInfo("2026-11", "2026-11-01", "2026-12-01"),
            totals = com.yuntyu.myhome.core.api.Totals(),
            categories = listOf(
                com.yuntyu.myhome.core.api.CategoryBudget(1, "생활"),
                com.yuntyu.myhome.core.api.CategoryBudget(2, "식비", parentId = 1),
                com.yuntyu.myhome.core.api.CategoryBudget(3, "배달", parentId = 2),
                com.yuntyu.myhome.core.api.CategoryBudget(4, "쇼핑"),
            ),
        )
        fun tx(id: Long, cat: Long, at: String, status: String = "confirmed") =
            TransactionDto(id = id, occurredAt = "2026-11-${at}+09:00", amount = 1, flow = "out", categoryId = cat, status = status)
        val txs = listOf(tx(1, 2, "09T10:00:00"), tx(2, 3, "10T09:00:00"), tx(3, 4, "10T11:00:00"), tx(4, 1, "08T10:00:00"), tx(5, 2, "10T12:00:00", "excluded"))
        assertEquals(listOf(2L, 1L, 4L), WidgetModel.recentIn(s, txs, 1, 3).map { it.id })
        assertEquals(listOf(2L), WidgetModel.recentIn(s, txs, 3, 3).map { it.id })
    }
}

/** 홈 분류 순서: 넘은 것 → 한도 → 자유 → 예산 없음. 예산 0인 분류를 "넘음"으로 보지 않는다 (합성) */
class HomeOrderTest {
    @Test fun noBudgetIsNotOver() {
        fun cb(id: Long, budget: Long, spent: Long, inBudget: Boolean = true) =
            com.yuntyu.myhome.core.api.CategoryBudget(id, "c$id", budget = budget, spent = spent, remaining = budget - spent, ratio = if (budget > 0) spent.toDouble() / budget else 0.0, inBudget = inBudget)
        val s = com.yuntyu.myhome.core.api.Summary(
            period = com.yuntyu.myhome.core.api.PeriodInfo("2026-11", "2026-11-01", "2026-12-01"),
            totals = com.yuntyu.myhome.core.api.Totals(),
            categories = listOf(cb(1, 0, 50_000, inBudget = false), cb(2, 0, 9_000), cb(3, 100_000, 20_000), cb(4, 10_000, 15_000), cb(5, 100_000, 60_000)),
        )
        val h = WidgetModel.homeCategories(s, topCount = 3)
        assertEquals(listOf(4L, 5L, 3L, 2L, 1L), (h.top + h.rest).map { it.id })
    }
}

/** 금액 칸 위 한국식 읽기 */
class MoneyKoreanTest {
    @Test fun readsLikeKorean() {
        assertEquals("50만원", Money.korean(500_000))
        assertEquals("1,234만 5,000원", Money.korean(12_345_000))
        assertEquals("1억 2,000만원", Money.korean(120_000_000))
        assertEquals("211만원", Money.korean(2_110_000))
        assertEquals("3,000원", Money.korean(3_000))
        assertEquals("0원", Money.korean(0))
    }
}
