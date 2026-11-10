package com.yuntyu.myhome.core

import com.yuntyu.myhome.core.api.Backtest
import com.yuntyu.myhome.core.api.MyHomeJson
import com.yuntyu.myhome.core.api.sortedByColumn
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class BacktestModelTest {
    // 서버 /v1/invest/backtest 응답 모양(합성 값 두 줄)
    private val json = """
        {"period":"1998-08 ~ 2026-07 · 336개월","fee_rate":0.001,"unit":"억원",
         "columns":[{"key":"worst","label":"최악","percent":false,"higher_is_better":true},
                    {"key":"loss_pct","label":"손실%","percent":true,"higher_is_better":false},
                    {"key":"mdd_worst_pct","label":"MDD최악","percent":true,"higher_is_better":true}],
         "key":[{"name":"★ SPX185+변동성30","star":true,"principal":1.26,"worst":0.6601,"loss_pct":11.51,"mdd_worst_pct":-59.56,"tops":["loss_pct"],"extra":1},
                {"name":"TQQQ 보유","worst":0.2285,"loss_pct":21.43,"mdd_worst_pct":-97.93,"tops":[]}],
         "horizons":[{"label":"7년","ex_dotcom":[],"all":[]}],
         "notes":["최악·하위10%를 먼저 보라."]}
    """.trimIndent()

    @Test
    fun parsesTheServerShape() {
        val b = MyHomeJson.decodeFromString(Backtest.serializer(), json)
        assertEquals(3, b.columns.size)
        assertEquals(false, b.columns[1].higherIsBetter)
        assertTrue(b.key[0].star)
        assertEquals(listOf("loss_pct"), b.key[0].tops)
        assertEquals("7년", b.horizons[0].label)
    }

    @Test
    fun cellTextUsesARealMinusAndFixedPlaces() {
        val b = MyHomeJson.decodeFromString(Backtest.serializer(), json)
        val star = b.key[0]
        assertEquals("0.66", star.text("worst"))
        assertEquals("11.5%", star.text("loss_pct"))
        assertEquals("−60%", star.text("mdd_worst_pct"))
        assertEquals("−98%", b.key[1].text("mdd_worst_pct"))
        assertEquals("★ SPX185+vol30", star.shortName)
    }

    // 머리 칸을 누르면 그 열의 1등부터: 손실%는 낮은 쪽, 최악·MDD는 높은(0에 가까운) 쪽이 위
    @Test
    fun sortsBestFirstByEachColumnsDirection() {
        val b = MyHomeJson.decodeFromString(Backtest.serializer(), json)
        val (worst, loss, mdd) = b.columns
        val tq = b.key[1]
        val star = b.key[0]
        assertEquals(listOf(star, tq), listOf(tq, star).sortedByColumn(worst))
        assertEquals(listOf(tq, star), listOf(star, tq).sortedByColumn(worst, bestFirst = false))
        assertEquals(listOf(star, tq), listOf(tq, star).sortedByColumn(loss))
        assertEquals(listOf(star, tq), listOf(tq, star).sortedByColumn(mdd))
    }

    // 줄을 누르면 ★와 견준다: 차이 글자와 나은지
    @Test
    fun comparesARowWithTheStar() {
        val b = MyHomeJson.decodeFromString(Backtest.serializer(), json)
        val (worst, loss, mdd) = b.columns
        val star = b.key[0]
        val tq = b.key[1]
        assertEquals("−0.43억", tq.diffText("worst", star))
        assertEquals("+9.9%p", tq.diffText("loss_pct", star))
        assertEquals("−38%p", tq.diffText("mdd_worst_pct", star))
        assertEquals("같음", star.diffText("worst", star))
        assertEquals(false, tq.betterThan(worst, star))
        assertEquals(false, tq.betterThan(loss, star)) // 손실%는 높으면 못하다
        assertEquals(false, tq.betterThan(mdd, star))
        assertEquals(true, star.betterThan(loss, tq))
        assertEquals(null, star.betterThan(worst, star))
    }
}
