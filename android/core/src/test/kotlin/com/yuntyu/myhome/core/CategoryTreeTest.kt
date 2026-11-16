package com.yuntyu.myhome.core

import com.yuntyu.myhome.core.api.Category
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertNull
import org.junit.Test

class CategoryTreeTest {
    private val tree = CategoryTree(
        listOf(
            Category(1, "생활비", "expense", sort = 10),
            Category(2, "식비", "expense", sort = 10, parentId = 1),
            Category(3, "배달", "expense", sort = 10, parentId = 2),
            Category(4, "교통", "expense", sort = 5, parentId = 1),
            Category(5, "고정비", "expense", sort = 20),
            Category(6, "수입", "income", code = "income", sort = 200),
            Category(7, "숨김", "expense", sort = 30, archived = true),
        ),
    )

    @Test
    fun pathsDepthAndOrder() {
        assertEquals("생활비 › 식비 › 배달", tree.label(3))
        assertEquals(2, tree.depth(3))
        assertEquals(2, tree.height(1))
        assertEquals(listOf(1L, 4L, 2L, 3L, 5L, 7L, 6L), tree.preorder().map { it.first.id })
        assertEquals(listOf(0, 1, 1, 2, 0, 0, 0), tree.preorder().map { it.second })
        // 숨긴 분류는 그 아래까지 뺀다
        assertEquals(listOf(1L, 4L, 2L, 3L, 5L, 6L), tree.preorder { !it.archived }.map { it.first.id })
    }

    @Test
    fun placeRulesMatchServer() {
        assertNull(tree.placeProblem(null, "expense", 2)) // 새 분류 3단계
        assertNotNull(tree.placeProblem(null, "expense", 3)) // 4단계
        assertNotNull(tree.placeProblem(1, "expense", 5)) // 폴더째 옮겨 4단계
        assertNull(tree.placeProblem(2, "expense", 5))
        assertNotNull(tree.placeProblem(1, "expense", 3)) // 자기 안으로
        assertNotNull(tree.placeProblem(null, "income", 6)) // 특수 분류 안
        assertNotNull(tree.placeProblem(6, "income", 1)) // 특수 분류를 폴더로
        assertNotNull(tree.placeProblem(null, "expense", 7)) // 숨긴 폴더
        assertNull(tree.placeProblem(3, "expense", null)) // 맨 위로
        // 식비(안에 배달이 있음)를 옮길 수 있는 곳: 생활비·고정비. 교통 안은 배달이 4단계라 안 되고, 자기·배달·특수·숨김도 빠진다
        assertEquals(listOf(1L, 5L), tree.parentChoices(2, "expense").map { it.id })
    }

    @Test
    fun budgetRollUp() {
        // 생활비는 안 정함 → 식비(200,000, 배달 30,000은 식비 한도 안) + 교통 50,000
        val own = mapOf(2L to 200_000L, 3L to 30_000L, 4L to 50_000L, 5L to 100_000L, 7L to 999L)
        assertEquals(250_000L, tree.effectiveBudget(own, 1))
        assertEquals(200_000L, tree.effectiveBudget(own, 2))
        assertEquals(350_000L, tree.totalBudget(own)) // 숨긴 분류는 빼고 맨 위만
        // 폴더에 직접 정하면 그 값이 폴더 한도
        assertEquals(300_000L, tree.effectiveBudget(own + (1L to 300_000L), 1))
    }

    @Test
    fun brokenParentsAreTopLevel() {
        val t = CategoryTree(
            listOf(
                Category(1, "a", "expense", parentId = 2),
                Category(2, "b", "expense", parentId = 1),
                Category(3, "c", "expense", parentId = 99),
            ),
        )
        assertEquals(3, t.preorder().size)
        assertNull(t.parent(3))
    }

    // 거래 목록 동그라미: 지금 분류 → 같은 폴더(폴더 자신과 안) → 남으면 다른 분류. 숨긴 분류·다른 종류는 빼고
    @Test fun relatedChoices() {
        val t = CategoryTree(listOf(
            Category(5, "생활", "expense", sort = 50),
            Category(1, "식비", "expense", sort = 10, parentId = 5),
            Category(2, "카페·간식", "expense", sort = 20, parentId = 5),
            Category(4, "쇼핑", "expense", sort = 40, parentId = 5),
            Category(6, "주거·통신", "expense", sort = 60, archived = true),
            Category(11, "기타", "expense", sort = 110),
            Category(12, "월급", "income", sort = 200),
            Category(13, "정산", "special", sort = 210),
        ))
        val out = setOf("expense")
        assertEquals(listOf(2L, 5L, 1L, 4L), t.related(2, out, 4).map { it.id })
        assertEquals(listOf(5L, 1L, 2L, 4L, 11L), t.related(5, out, 5).map { it.id })
        assertEquals(listOf(11L, 5L, 1L, 2L), t.related(11, out, 4).map { it.id })
        assertEquals(listOf(5L, 1L, 2L, 4L), t.related(null, out, 4).map { it.id })
        assertEquals(listOf(5L, 1L, 2L), t.related(6, out, 3).map { it.id })
        assertEquals(listOf(13L, 12L), t.related(13, setOf("income", "special"), 4).map { it.id })
    }
}
