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
        assertEquals(1, WidgetLayout.MEDIUM.recentCount)
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
        // 넘은 것 → 한도(쓴 비율 순) → 자유(예산 0인 경조사는 "넘음"이 아니다, 2026-11-10) → 안 쓴 것
        assertEquals(listOf("생활", "카페", "교통", "식비", "문화"), h.top.map { it.name })
        assertEquals(listOf("구독", "쇼핑", "경조사", "의료"), h.rest.map { it.name })
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

    // 고를 거래가 있으면 고르기 칸이 아래(분류·최근 결제)를 가리고, 작은 위젯은 고르기를 안 보인다
    @Test fun pickCoversRowsExceptSmall() {
        assertEquals(WidgetLayout.MEDIUM.categoryCount to WidgetLayout.MEDIUM.recentCount, WidgetModel.rows(WidgetLayout.MEDIUM, false))
        assertEquals(0 to 0, WidgetModel.rows(WidgetLayout.MEDIUM, true))
        assertEquals(0 to 0, WidgetModel.rows(WidgetLayout.LARGE, true))
        assertEquals(WidgetLayout.SMALL.categoryCount to 0, WidgetModel.rows(WidgetLayout.SMALL, true))
        assertEquals(false, WidgetModel.showPick(WidgetLayout.SMALL, true))
        assertEquals(false, WidgetModel.headWhilePicking(WidgetLayout.MEDIUM))
        assertEquals(true, WidgetModel.headWhilePicking(WidgetLayout.LARGE))
    }

    // 남은 날은 오늘과 마지막 날을 모두 센다. 5일 시작이면 11/17 → 12/04까지 18일
    @Test fun daysLeftCountsTodayAndLastDay() {
        val d = java.time.LocalDate.of(2026, 11, 17)
        assertEquals(18, WidgetModel.daysLeft("2026-12-04", d))
        assertEquals(1, WidgetModel.daysLeft("2026-11-17", d))
        assertEquals(1, WidgetModel.daysLeft("2026-11-10", d)) // 지난 기간이어도 0으로 나누지 않게
        assertEquals(1, WidgetModel.daysLeft("이상한값", d))
    }

    @Test fun pickShowsWhenPaid() {
        val d = java.time.LocalDate.of(2026, 11, 17)
        assertEquals("오늘 09:05", WidgetModel.whenText("2026-11-17T09:05:00+09:00", d))
        assertEquals("어제 19:56", WidgetModel.whenText("2026-11-16T19:56:00+09:00", d))
        assertEquals("11/3 23:10", WidgetModel.whenText("2026-11-03T23:10:00+09:00", d))
        assertEquals("", WidgetModel.whenText("", d))
        assertEquals(3, WidgetModel.pickChoiceCount(WidgetLayout.MEDIUM))
        assertEquals(4, WidgetModel.pickChoiceCount(WidgetLayout.LARGE))
    }

    @Test fun perDayAndUsed() {
        assertEquals(39_650L, WidgetModel.perDay(713_700, 18))
        assertEquals(0L, WidgetModel.perDay(-11_900, 18))
        assertEquals(850_000L - 741_400L, WidgetModel.used(summary))
    }

    @Test fun usageTextShortensToManwon() {
        val c = summary.categories.first { it.name == "식비" }
        assertEquals("25만 / 30만", WidgetModel.usageText(c))
        assertEquals("7,000 / 7만", WidgetModel.usageText(summary.categories.first { it.name == "교통" }))
        assertEquals("7만 / 6만", WidgetModel.usageText(summary.categories.first { it.name == "카페·간식" }))
        assertEquals("18.2만 / 29.9만", WidgetModel.usageText(c.copy(spent = 182_000, budget = 299_999)))
        assertEquals("5,000", WidgetModel.usageText(summary.categories.first { it.name == "기타" })) // 예산 없으면 쓴 돈만
        assertEquals("", WidgetModel.usageText(null))
    }

    @Test fun summaryWithPickDecodes() {
        val s = MyHomeJson.decodeFromString(
            Summary.serializer(),
            """{"period":{"label":"2026-11","start":"2026-11-01","end":"2026-12-01"},"totals":{"budget":0,"spent":0,"settled":0,"remaining":0},
               "pick":{"id":7,"merchant":"합성편의점","amount":3300,"occurred_at":"2026-11-16T12:00:00+09:00","reason":"ambiguous",
               "choices":[{"id":1,"name":"식비"},{"id":2,"name":"카페·간식"}],"left":2}}""",
        )
        assertEquals(7L, s.pick?.id)
        assertEquals(listOf("식비", "카페·간식"), s.pick?.choices?.map { it.name })
        assertEquals(null, MyHomeJson.decodeFromString(Summary.serializer(), """{"period":{"label":"2026-11","start":"2026-11-01","end":"2026-12-01"},"totals":{"budget":0,"spent":0,"settled":0,"remaining":0}}""").pick)
    }
}
