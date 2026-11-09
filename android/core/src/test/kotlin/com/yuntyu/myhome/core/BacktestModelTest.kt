package com.yuntyu.myhome.core

import com.yuntyu.myhome.core.api.Backtest
import com.yuntyu.myhome.core.api.MyHomeJson
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
}
