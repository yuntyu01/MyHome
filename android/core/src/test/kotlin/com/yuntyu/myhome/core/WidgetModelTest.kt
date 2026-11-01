package com.yuntyu.myhome.core

import com.yuntyu.myhome.core.api.MyHomeJson
import com.yuntyu.myhome.core.api.Summary
import org.junit.Assert.assertEquals
import org.junit.Test

class WidgetModelTest {
    // 서버 /v1/summary 응답 형식(합성 값). 모르는 필드(settle_category_id 등)는 무시돼야 한다.
    private val json = """
        {"period":{"label":"2026-11","start":"2026-11-01","end":"2026-11-30"},
         "totals":{"budget":850000,"spent":108600,"settled":0,"remaining":741400,"income":3200000,"investment":300000,"own_transfer":0},
         "categories":[
           {"id":1,"name":"식비","budget":300000,"spent":250000,"settled":0,"remaining":50000,"ratio":0.8333},
           {"id":2,"name":"카페·간식","budget":60000,"spent":70000,"settled":0,"remaining":-10000,"ratio":1.1667},
           {"id":3,"name":"교통","budget":70000,"spent":7000,"settled":0,"remaining":63000,"ratio":0.1},
           {"id":4,"name":"기타","budget":0,"spent":5000,"settled":0,"remaining":-5000,"ratio":0}
         ],
         "recent":[
           {"id":32,"occurred_at":"2026-11-02T02:20:06+09:00","amount":1000,"flow":"out","merchant":"합성가게","memo":"","direction":"expense","category_id":5,"category":"생활","settle_category_id":null,"status":"confirmed","exclusion_reason":null,"review_reason":null,"merged_into":null,"cancels_id":null,"source":"kb","kind":"approval","installment_months":0,"manually_edited":true,"raw_event_id":32},
           {"id":31,"occurred_at":"2026-11-01T23:29:00+09:00","amount":12500,"flow":"in","merchant":"합성이름","direction":"income","status":"needs_review","review_reason":"deposit","source":"toss"},
           {"id":30,"occurred_at":"2026-11-01T23:25:00+09:00","amount":25000,"flow":"out","merchant":"합성송금","direction":"expense","status":"needs_review","review_reason":"transfer","source":"toss"},
           {"id":29,"occurred_at":"2026-11-01T09:00:00+09:00","amount":3200000,"flow":"in","merchant":"급여","direction":"income","status":"confirmed","source":"toss"}
         ],
         "needs_review":2,"generated_at":"2026-11-02T02:30:00+09:00"}
    """.trimIndent()

    private val summary = MyHomeJson.decodeFromString(Summary.serializer(), json)

    @Test fun parsesServerSummary() {
        assertEquals("2026-11", summary.period.label)
        assertEquals(741400L, summary.totals.remaining)
        assertEquals(4, summary.categories.size)
        assertEquals(2L, summary.needsReview)
        assertEquals("생활", summary.recent[0].category)
    }

    @Test fun topCategoriesByUsage() {
        val top = WidgetModel.topCategories(summary, 2)
        assertEquals(listOf("카페·간식", "식비"), top.map { it.name }) // 예산 없는 '기타'는 제외
    }

    @Test fun states() {
        val byName = summary.categories.associateBy { it.name }
        assertEquals(BudgetState.OVER, WidgetModel.stateOf(byName.getValue("카페·간식")))
        assertEquals(BudgetState.WARN, WidgetModel.stateOf(byName.getValue("식비")))
        assertEquals(BudgetState.OK, WidgetModel.stateOf(byName.getValue("교통")))
        assertEquals(BudgetState.NO_BUDGET, WidgetModel.stateOf(byName.getValue("기타")))
    }

    @Test fun recentPaymentsSkipsConfirmedIncome() {
        val r = WidgetModel.recentPayments(summary, 4)
        assertEquals(listOf(32L, 31L, 30L), r.map { it.id }) // 확정 급여(들어온 돈)는 빠지고, 확인 필요 입금은 보인다
    }

    @Test fun layoutBySize() {
        assertEquals(WidgetLayout.SMALL, WidgetLayout.forSize(110f, 110f))
        assertEquals(WidgetLayout.SMALL, WidgetLayout.forSize(250f, 100f))
        assertEquals(WidgetLayout.MEDIUM, WidgetLayout.forSize(250f, 180f))
        assertEquals(WidgetLayout.LARGE, WidgetLayout.forSize(250f, 300f))
    }

    @Test fun shortTime() {
        assertEquals("11/02 02:20", WidgetModel.shortTime("2026-11-02T02:20:06+09:00"))
        assertEquals("", WidgetModel.shortTime("bad"))
    }
}
