package com.yuntyu.myhome.core

import com.yuntyu.myhome.core.api.ApiResult
import com.yuntyu.myhome.core.api.Category
import com.yuntyu.myhome.core.api.EventPayload
import com.yuntyu.myhome.core.api.InvestAlerts
import com.yuntyu.myhome.core.api.InvestStatus
import com.yuntyu.myhome.core.api.InvestValue
import com.yuntyu.myhome.core.api.MyHomeApi
import com.yuntyu.myhome.core.api.UploadDecision
import com.yuntyu.myhome.core.api.toUploadDecision
import okhttp3.mockwebserver.MockResponse
import okhttp3.mockwebserver.MockWebServer
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test

class ApiTest {
    private lateinit var server: MockWebServer
    private var token: String? = "mhd_test"

    @Before fun setUp() {
        server = MockWebServer()
        server.start()
    }

    @After fun tearDown() = server.shutdown()

    private fun api() = MyHomeApi({ server.url("/").toString() }, { token })

    // 합성 데이터
    private val payload = EventPayload("test.card", "합성 제목", "1,000원 합성가맹점", "", 1793000000000)

    @Test fun postEventSendsBearerAndJson() {
        server.enqueue(MockResponse().setResponseCode(201).setBody("""{"id":7,"duplicate":false,"extra":"무시됨"}"""))
        val r = api().postEvent(payload)
        assertTrue(r is ApiResult.Ok)
        assertEquals(7L, (r as ApiResult.Ok).value.id)
        val req = server.takeRequest()
        assertEquals("POST", req.method)
        assertEquals("/v1/events", req.path)
        assertEquals("Bearer mhd_test", req.getHeader("Authorization"))
        val body = req.body.readUtf8()
        assertTrue(body.contains("\"source_package\":\"test.card\""))
        assertTrue(body.contains("\"posted_at\":1793000000000"))
        assertEquals(UploadDecision.SENT, r.toUploadDecision())
    }

    @Test fun duplicateIsSent() {
        server.enqueue(MockResponse().setResponseCode(200).setBody("""{"id":7,"duplicate":true}"""))
        val r = api().postEvent(payload)
        assertEquals(UploadDecision.SENT, r.toUploadDecision())
        assertTrue((r as ApiResult.Ok).value.duplicate)
    }

    @Test fun decisions() {
        server.enqueue(MockResponse().setResponseCode(401).setBody("""{"error":"unauthorized"}"""))
        server.enqueue(MockResponse().setResponseCode(503))
        server.enqueue(MockResponse().setResponseCode(429))
        server.enqueue(MockResponse().setResponseCode(400).setBody("""{"error":"bad"}"""))
        val a = api()
        val r401 = a.postEvent(payload)
        assertEquals(UploadDecision.AUTH_FAILED, r401.toUploadDecision())
        assertEquals("unauthorized", (r401 as ApiResult.HttpError).message)
        assertEquals(UploadDecision.RETRY, a.postEvent(payload).toUploadDecision())
        assertEquals(UploadDecision.RETRY, a.postEvent(payload).toUploadDecision())
        assertEquals(UploadDecision.REJECTED, a.postEvent(payload).toUploadDecision())
    }

    // 건강 API 오류 본문의 code(예: AI 크레딧 없음)를 앱이 구분할 수 있게 넘긴다. 헬스 출석은 도착 시각을 같이 보낸다
    @Test fun healthErrorCodeAndGymCheckInTime() {
        server.enqueue(MockResponse().setResponseCode(503).setBody("""{"error":"AI 크레딧이 없어요 - 직접 입력해 주세요","code":"AI_NO_CREDIT"}"""))
        server.enqueue(MockResponse().setResponseCode(200).setBody("""{"date":"2026-11-09","verifiedBy":"geofence"}"""))
        val a = api()
        val r = a.analyzeMeal(listOf(byteArrayOf(1))) as ApiResult.HttpError
        assertEquals("AI_NO_CREDIT", r.errorCode)
        assertEquals("AI 크레딧이 없어요 - 직접 입력해 주세요", r.message)
        server.takeRequest()
        assertTrue(a.gymCheckIn("2026-11-09", "geofence", "2026-11-09T10:00:00Z") is ApiResult.Ok)
        val body = server.takeRequest().body.readUtf8()
        assertTrue(body, body.contains("\"checkInAt\":\"2026-11-09T10:00:00Z\""))
    }

    @Test fun networkErrorRetries() {
        val dead = MyHomeApi({ "http://127.0.0.1:1" }, { "t" })
        assertEquals(UploadDecision.RETRY, dead.postEvent(payload).toUploadDecision())
    }

    @Test fun exchangeAppLinkWithoutDeviceToken() {
        token = null // 아직 기기 토큰이 없어도 호출할 수 있어야 한다
        server.enqueue(MockResponse().setResponseCode(201).setBody("""{"token":"mhd_new","device_id":3,"device_name":"galaxy"}"""))
        val r = api().exchangeAppLink("code1", "v".repeat(43), "galaxy")
        assertTrue(r is ApiResult.Ok)
        assertEquals("mhd_new", (r as ApiResult.Ok).value.token)
        val req = server.takeRequest()
        assertEquals("/v1/auth/exchange", req.path)
        assertEquals(null, req.getHeader("Authorization"))
        val body = req.body.readUtf8()
        assertTrue(body.contains("\"code\":\"code1\"") && body.contains("\"device_name\":\"galaxy\""))
    }

    @Test fun exchangeRejected() {
        server.enqueue(MockResponse().setResponseCode(401).setBody("""{"error":"invalid code"}"""))
        val r = api().exchangeAppLink("x", "y", "z")
        assertEquals(401, (r as ApiResult.HttpError).code)
    }

    // RFC 7636 부록 B의 예시 값
    @Test fun pkceChallengeMatchesRfc() {
        assertEquals("E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM", AppLinkPkce.challengeOf("dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"))
        val v = AppLinkPkce.newVerifier()
        assertEquals(43, v.length)
        assertTrue(v.all { it.isLetterOrDigit() || it == '-' || it == '_' })
        assertEquals("https://x.test/app/link?challenge=abc&state=s1", AppLinkPkce.linkUrl("https://x.test/", "abc", "s1"))
    }

    @Test fun notConfigured() {
        token = null
        val r = api().postEvent(payload)
        assertTrue(r is ApiResult.NotConfigured)
        assertEquals(0, server.requestCount)
    }

    // 서버 investStatus/investValue의 JSON 이름과 맞는지 (합성 데이터)
    @Test fun investStatusAndValue() {
        server.enqueue(MockResponse().setBody(
            """{"migrated":true,"symbol":"TQQQ","shares":10,"price_stale":false,"last_price_day":"2026-11-05","last_price_label":"11월 5일",
            "month":{"title":"12월 매수","meta":"적용월 12월","value":"예정 12/1","bad":false},
            "schedule":[{"title":"다음 판정일","meta":"월 마지막 거래일","value":"11/28 · D-21","bad":false}],
            "checks":[{"title":"원금 대조","meta":"원금 내역 1건","value":"✗ 1,000원 어긋남","bad":true}],"problems":1}""",
        ))
        server.enqueue(MockResponse().setBody(
            """{"migrated":true,"symbol":"TQQQ","shares":10,"can_value":true,"why":"","net_krw":721000,"basis":"합성 기준",
            "contributed_krw":600000,"gain_krw":121000,"gain_pct":"+20.2%","tax_krw":0,"cash_usd":"$12.50",
            "ledger":[{"on":"2026-01-02","ym":"2026-01","source":"자동매수","shares":"TQQQ +10주","note":"합성 매수","amount":600000}],
            "ledger_sum":600000,"ledger_gap":0,"ledger_bad":false}""",
        ))
        val s = (api().invest() as ApiResult.Ok<InvestStatus>).value
        assertEquals("/v1/invest", server.takeRequest().path)
        assertTrue(s.migrated)
        assertEquals("TQQQ", s.symbol)
        assertEquals(10, s.shares)
        assertEquals("예정 12/1", s.month?.value)
        assertEquals(1, s.schedule?.size)
        assertTrue(s.checks!!.single().bad)
        assertEquals(1, s.problems)

        val v = (api().investValue() as ApiResult.Ok<InvestValue>).value
        assertEquals("/v1/invest/value", server.takeRequest().path)
        assertEquals(721_000L, v.netKrw)
        assertEquals(121_000L, v.gainKrw)
        assertEquals("$12.50", v.cashUsd)
        assertEquals("자동매수", v.ledger!!.single().source)
    }

    // 끌어서 옮기기: 맨 위는 parent_id 0, 기준이 없으면 before·after를 빼고 보낸다
    @Test fun moveCategory() {
        server.enqueue(MockResponse().setBody("""{"id":4,"name":"교통","kind":"expense","sort":10,"parent_id":1}"""))
        server.enqueue(MockResponse().setBody("""{"id":4,"name":"교통","kind":"expense","sort":30}"""))
        val c = (api().moveCategory(4, DropPlace(1L, before = 2L)) as ApiResult.Ok<Category>).value
        assertEquals(1L, c.parentId)
        var req = server.takeRequest()
        assertEquals("POST /v1/categories/4/move", "${req.method} ${req.path}")
        assertEquals("""{"parent_id":1,"before":2}""", req.body.readUtf8())
        api().moveCategory(4, DropPlace(null))
        req = server.takeRequest()
        assertEquals("""{"parent_id":0}""", req.body.readUtf8())
    }

    // 매매 실패 알림: 마지막으로 본 id를 after로 보낸다 (합성 데이터)
    @Test fun investAlerts() {
        server.enqueue(MockResponse().setBody(
            """{"alerts":[{"id":7,"created_at":"2026-11-08T09:17:00+09:00","title":"자동매매 주문 실패 (exit 1)","body":"✗ 합성 거부"}]}""",
        ))
        val r = (api().investAlerts(6) as ApiResult.Ok<InvestAlerts>).value
        assertEquals("/v1/invest/alerts?after=6", server.takeRequest().path)
        assertEquals(7L, r.alerts.single().id)
        assertEquals("✗ 합성 거부", r.alerts.single().body)
    }

    // 옮기기 전에는 migrated만 false로 온다(목록은 null)
    @Test fun investNotMigrated() {
        server.enqueue(MockResponse().setBody("""{"migrated":false,"symbol":"","shares":0,"month":{"title":"","meta":"","value":"","bad":false},"schedule":null,"checks":null,"problems":0}"""))
        val s = (api().invest() as ApiResult.Ok<InvestStatus>).value
        assertTrue(!s.migrated && s.schedule == null)
    }

    // 확인 목록의 AI 추천 (합성 데이터). 추천이 없으면 null
    @Test fun reviewItemsCarrySuggestion() {
        server.enqueue(MockResponse().setBody(
            """{"transactions":[
              {"id":1,"occurred_at":"2026-11-08T03:00:00Z","amount":8900,"flow":"out","merchant":"합성식당","status":"needs_review","review_reason":"no_rule",
               "suggestion":{"category_id":3,"category":"식비","confidence":0.82}},
              {"id":2,"occurred_at":"2026-11-08T04:00:00Z","amount":12000,"flow":"out","merchant":"쿠팡","status":"needs_review","review_reason":"payment_gateway","suggestion":null}
            ]}""",
        ))
        val items = (api().reviewQueue() as ApiResult.Ok).value.transactions
        assertEquals(3L, items[0].suggestion?.categoryId)
        assertEquals("식비", items[0].suggestion?.category)
        assertEquals(null, items[1].suggestion)
    }

    // 거래 목록 거르기·현금 입력·예산 저장·분류 숨기기가 서버 형식대로 나가는지 (합성 데이터)
    @Test fun ledgerEditsSendServerShapes() {
        server.enqueue(MockResponse().setBody("""{"transactions":[]}"""))
        api().transactions(limit = 200, month = "2026-10", status = "needs_review")
        assertEquals("/v1/transactions?limit=200&month=2026-10&status=needs_review", server.takeRequest().path)

        server.enqueue(MockResponse().setResponseCode(201).setBody("""{"transaction":{"id":9,"occurred_at":"2026-11-08T03:00:00Z","amount":5000,"flow":"out","merchant":"합성분식"}}"""))
        val made = api().createTransaction(com.yuntyu.myhome.core.api.ManualTransaction(amount = 5000, merchant = "합성분식", categoryId = 3))
        assertEquals(9L, (made as ApiResult.Ok).value.transaction.id)
        val manual = server.takeRequest().body.readUtf8()
        assertTrue(manual.contains("\"category_id\":3") && manual.contains("\"flow\":\"out\"") && !manual.contains("occurred_at"))

        server.enqueue(MockResponse().setBody("""{"period":{"label":"2026-11","start":"2026-11-01","end":"2026-11-30"},"items":[{"category_id":3,"category":"식비","amount":300000}]}"""))
        val saved = api().putBudgets("2026-11", listOf(com.yuntyu.myhome.core.api.BudgetItem(categoryId = 3, amount = 300000)))
        assertEquals(300000L, (saved as ApiResult.Ok).value.items.single().amount)
        val req = server.takeRequest()
        assertEquals("PUT", req.method)
        assertTrue(req.body.readUtf8().contains("\"month\":\"2026-11\""))

        server.enqueue(MockResponse().setBody("""{"id":4,"name":"합성분류","kind":"expense","sort":500,"archived":true}"""))
        api().updateCategory(4, archived = true)
        val patch = server.takeRequest()
        assertEquals("/v1/categories/4", patch.path)
        val body = patch.body.readUtf8()
        // 서버는 kind가 오면 거부한다. 이름도 안 바꿀 때는 빼고 보낸다
        assertTrue(body.contains("\"archived\":true") && !body.contains("kind") && !body.contains("name"))

        server.enqueue(MockResponse().setBody("""{"settings":{"budget_start_day":25,"own_accounts":[]}}"""))
        assertEquals(25, (api().budgetStartDay() as ApiResult.Ok).value)
    }

    // 거래 검색: 날짜 사이를 주면 month는 보내지 않고, 검색어는 q로 인코딩한다 (합성 데이터)
    @Test fun searchSendsRangeAndQuery() {
        server.enqueue(MockResponse().setBody("""{"transactions":[]}"""))
        api().transactions(limit = 200, month = "2026-10", from = "2000-01-01", to = "2026-11-10", search = " 합성 분식 ")
        assertEquals(
            "/v1/transactions?limit=200&from=2000-01-01&to=2026-11-10&q=%ED%95%A9%EC%84%B1+%EB%B6%84%EC%8B%9D",
            server.takeRequest().path,
        )
        // 빈 검색어는 보내지 않는다
        server.enqueue(MockResponse().setBody("""{"transactions":[]}"""))
        api().transactions(limit = 10, month = "2026-10", search = "  ")
        assertEquals("/v1/transactions?limit=10&month=2026-10", server.takeRequest().path)
    }

    // 메모·이름·금액 고치기: 바꾸는 것만 보낸다(null은 빼서 서버가 그대로 둔다)
    @Test fun editSendsOnlyChangedFields() {
        server.enqueue(MockResponse().setBody("""{"transaction":{"id":5,"occurred_at":"2026-11-08T03:00:00Z","amount":4500,"flow":"out","memo":"합성 메모"},"also_updated":0}"""))
        val r = api().editTransaction(5, com.yuntyu.myhome.core.api.TransactionEdit(memo = "합성 메모"))
        assertEquals("합성 메모", (r as ApiResult.Ok).value.transaction.memo)
        val body = server.takeRequest().body.readUtf8()
        assertTrue(body.contains("\"memo\":\"합성 메모\""))
        assertTrue(!body.contains("merchant") && !body.contains("amount") && !body.contains("category_id") && !body.contains("status"))

        server.enqueue(MockResponse().setBody("""{"transaction":{"id":5,"occurred_at":"2026-11-08T03:00:00Z","amount":7000,"flow":"out","merchant":"합성식당"},"also_updated":0}"""))
        api().editTransaction(5, com.yuntyu.myhome.core.api.TransactionEdit(merchant = "합성식당", amount = 7000))
        val b2 = server.takeRequest().body.readUtf8()
        assertTrue(b2.contains("\"merchant\":\"합성식당\"") && b2.contains("\"amount\":7000") && !b2.contains("memo"))
    }

    // AI 공통 메모: 설정 목록에서 읽고(없으면 ""), JSON 문자열로 보낸다
    @Test fun aiMemoReadsAndWritesAJsonString() {
        server.enqueue(MockResponse().setBody("""{"settings":{"budget_start_day":1,"ai_classify_memo":"회사 근처 \"식당\"은 식비"}}"""))
        server.enqueue(MockResponse().setBody("""{"settings":{}}"""))
        server.enqueue(MockResponse().setBody("""{"key":"ai_classify_memo","value":"x"}"""))
        assertEquals("회사 근처 \"식당\"은 식비", (api().aiMemo() as ApiResult.Ok).value)
        assertEquals("", (api().aiMemo() as ApiResult.Ok).value)
        server.takeRequest(); server.takeRequest()
        api().putAiMemo("편의점은 \"생활\"")
        val put = server.takeRequest()
        assertEquals("PUT", put.method)
        assertEquals("/v1/settings/ai_classify_memo", put.path)
        assertEquals("\"편의점은 \\\"생활\\\"\"", put.body.readUtf8())
    }

    // 몸무게: 목록·저장·저울 읽기(사진 한 장 multipart)
    @Test fun bodyWeightsAndScaleScan() {
        server.enqueue(MockResponse().setBody("""{"weights":[{"date":"2026-11-09","weightKg":72.4}]}"""))
        server.enqueue(MockResponse().setBody("""{"date":"2026-11-10","weightKg":72.1}"""))
        server.enqueue(MockResponse().setBody("""{"weightKg":71.8,"confidence":0.9}"""))
        assertEquals(72.4, (api().bodyWeights(from = "2026-11-01") as ApiResult.Ok).value.weights.single().weightKg, 0.0)
        assertEquals("/v1/health/body?from=2026-11-01", server.takeRequest().path)
        api().saveWeight("2026-11-10", 72.1)
        val put = server.takeRequest()
        assertEquals("PUT", put.method)
        assertTrue(put.body.readUtf8().contains("\"weightKg\":72.1"))
        val r = api().scanScale(byteArrayOf(1, 2, 3)) as ApiResult.Ok
        assertEquals(71.8, r.value.weightKg, 0.0)
        val scan = server.takeRequest()
        assertEquals("/v1/health/body/scan", scan.path)
        assertTrue(scan.getHeader("Content-Type")!!.startsWith("multipart/form-data"))
    }
}
