package com.yuntyu.myhome.core

import com.yuntyu.myhome.core.api.CategoryBudget
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
         "needs_review":2,"failed_events":3,"generated_at":"2026-11-02T02:30:00+09:00"}
    """.trimIndent()

    private val summary = MyHomeJson.decodeFromString(Summary.serializer(), json)

    @Test fun parsesServerSummary() {
        assertEquals("2026-11", summary.period.label)
        assertEquals(741400L, summary.totals.remaining)
        assertEquals(4, summary.categories.size)
        assertEquals(2L, summary.needsReview)
        assertEquals(3L, summary.failedEvents)
        assertEquals("생활", summary.recent[0].category)
    }

    @Test fun failedEventsDefaultsToZeroForOldServer() {
        val old = MyHomeJson.decodeFromString(Summary.serializer(), json.replace("\"failed_events\":3,", ""))
        assertEquals(0L, old.failedEvents)
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
        assertEquals(2, WidgetLayout.MEDIUM.recentCount)
    }

    @Test fun shortTime() {
        assertEquals("11/02 02:20", WidgetModel.shortTime("2026-11-02T02:20:06+09:00"))
        assertEquals("", WidgetModel.shortTime("bad"))
    }

    @Test fun homeCategoriesPutOverBudgetFirstAndFoldUnused() {
        fun c(name: String, budget: Long, spent: Long) = CategoryBudget(
            id = 0, name = name, budget = budget, spent = spent, settled = 0, remaining = budget - spent,
            ratio = if (budget > 0) spent.toDouble() / budget else 0.0,
        )
        val s = summary.copy(
            categories = listOf(
                c("식비", 100, 50), c("카페", 100, 130), c("교통", 100, 90), c("쇼핑", 100, 10), c("생활", 100, 200),
                c("구독", 100, 20), c("문화", 100, 30), c("의료", 100, 0), c("기타", 0, 0), c("경조사", 0, 40),
            ),
        )
        val h = WidgetModel.homeCategories(s)
        assertEquals(listOf("생활", "경조사", "카페", "교통", "식비"), h.top.map { it.name })
        assertEquals(listOf("문화", "구독", "쇼핑", "의료"), h.rest.map { it.name })
    }

    @Test fun homeCategoriesKeepEveryOverBudget() {
        val over = CategoryBudget(id = 0, name = "초과", budget = 100, spent = 150, settled = 0, remaining = -50, ratio = 1.5)
        val h = WidgetModel.homeCategories(summary.copy(categories = List(7) { over }))
        assertEquals(7, h.top.size)
        assertEquals(0, h.rest.size)
    }

    @Test fun monthNameIsReadable() {
        assertEquals("10월", WidgetModel.monthName("2026-10", 2026))
        assertEquals("2025년 12월", WidgetModel.monthName("2025-12", 2026))
        assertEquals("이상한값", WidgetModel.monthName("이상한값", 2026))
    }
}
