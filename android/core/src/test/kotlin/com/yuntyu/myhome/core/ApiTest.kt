package com.yuntyu.myhome.core

import com.yuntyu.myhome.core.api.ApiResult
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
}
