package com.yuntyu.myhome.core

import com.yuntyu.myhome.core.CategoryDrag.Row
import com.yuntyu.myhome.core.api.Category
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

/** 꾹 눌러 끌어 옮기기: 놓을 자리 계산과 순서 (합성 분류) */
class CategoryDragTest {
    // 생활비(1) › [식비(2) › 배달(3), 교통(4)], 고정비(5), 쇼핑(6)
    private val cats = listOf(
        Category(1, "생활비", "expense", sort = 10),
        Category(2, "식비", "expense", sort = 10, parentId = 1),
        Category(3, "배달", "expense", sort = 10, parentId = 2),
        Category(4, "교통", "expense", sort = 20, parentId = 1),
        Category(5, "고정비", "expense", sort = 20),
        Category(6, "쇼핑", "expense", sort = 30),
    )
    private val tree = CategoryTree(cats)

    /** 보이는 줄(높이 100씩, 위에서부터), skip은 끄는 줄 */
    private fun rows(vararg skip: Long): List<Row> =
        tree.preorder().map { it.first.id }.withIndex().filter { it.value !in skip }.map { (i, id) -> Row(id, i * 100f, i * 100f + 100f) }

    @Test fun middleOfRowGoesInsideAtTheEnd() {
        // 쇼핑(6)을 고정비(5) 가운데에 → 고정비 안 맨 끝(고정비가 폴더가 된다)
        val d = CategoryDrag.target(tree, 6, rows(6), hole = 500f..600f, y = 450f)!!
        assertEquals(DropPlace(5), d.place)
        assertEquals(5L, d.into)
    }

    @Test fun topHalfIsBeforeBottomHalfIsAfter() {
        // 쇼핑(6)을 교통(4) 위쪽 → 생활비 안, 교통 앞(선은 교통 위, 1단계)
        val up = CategoryDrag.target(tree, 6, rows(6), 500f..600f, y = 310f)!!
        assertEquals(DropPlace(1, before = 4), up.place)
        assertEquals(300f, up.lineY)
        assertEquals(1, up.lineDepth)
        // 교통(4) 아래쪽 → 생활비 안, 교통 뒤
        val down = CategoryDrag.target(tree, 6, rows(6), 500f..600f, y = 390f)!!
        assertEquals(DropPlace(1, after = 4), down.place)
        // 식비(2, 폴더) 아래쪽 → 식비 안 맨 앞(배달 앞)
        val first = CategoryDrag.target(tree, 6, rows(6), 500f..600f, y = 195f)!!
        assertEquals(DropPlace(2, before = 3), first.place)
        assertEquals(2, first.lineDepth)
    }

    @Test fun belowEverythingIsTopLevelEnd_holeIsNothing() {
        val d = CategoryDrag.target(tree, 2, rows(2, 3), hole = 100f..300f, y = 900f)!!
        assertEquals(DropPlace(null), d.place)
        assertEquals(0, d.lineDepth)
        assertNull(CategoryDrag.target(tree, 2, rows(2, 3), hole = 100f..300f, y = 150f)) // 원래 자리
    }

    @Test fun depthRuleBlocksTooDeep() {
        // 식비(2, 아래 1단계 있음)를 교통(4, 1단계) 가운데 → 3단계 넘음: 안에 못 넣고 앞/뒤 자리로
        val d = CategoryDrag.target(tree, 2, rows(2, 3), hole = 100f..300f, y = 350f)!!
        assertEquals(DropPlace(1, after = 4), d.place)
        assertNull(d.into)
        // 생활비(1, 아래 2단계)는 다른 분류 안에 못 들어간다: 고정비 가운데여도 앞/뒤
        val top = CategoryDrag.target(tree, 1, rows(1, 2, 3, 4), hole = 0f..400f, y = 440f)!!
        assertEquals(DropPlace(null, before = 5), top.place)
    }

    @Test fun orderSameAndMoved() {
        assertEquals(listOf(4L, 2L), CategoryDrag.order(tree, 4, DropPlace(1, before = 2)))
        assertEquals(listOf(2L, 4L, 6L), CategoryDrag.order(tree, 6, DropPlace(1)))
        assertTrue(CategoryDrag.same(tree, 4, DropPlace(1, after = 2)))
        assertTrue(CategoryDrag.same(tree, 6, DropPlace(null)))
        assertFalse(CategoryDrag.same(tree, 5, DropPlace(null)))
        val after = CategoryTree(CategoryDrag.moved(cats, tree, 6, DropPlace(1, before = 4)))
        assertEquals(listOf("식비", "쇼핑", "교통"), after.children(1).map { it.name })
        assertEquals("생활비 › 쇼핑", after.label(6))
    }
}
