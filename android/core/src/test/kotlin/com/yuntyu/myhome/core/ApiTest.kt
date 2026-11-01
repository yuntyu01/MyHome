package com.yuntyu.myhome.core

import com.yuntyu.myhome.core.api.ApiResult
import com.yuntyu.myhome.core.api.EventPayload
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

    @Test fun networkErrorRetries() {
        val dead = MyHomeApi({ "http://127.0.0.1:1" }, { "t" })
        assertEquals(UploadDecision.RETRY, dead.postEvent(payload).toUploadDecision())
    }

    @Test fun kakaoDeviceLoginWithoutDeviceToken() {
        token = null // 아직 기기 토큰이 없어도 호출할 수 있어야 한다
        server.enqueue(MockResponse().setResponseCode(201).setBody("""{"token":"mhd_new","device_id":3,"device_name":"galaxy"}"""))
        val r = api().kakaoDeviceLogin("kakao-at", "galaxy")
        assertTrue(r is ApiResult.Ok)
        assertEquals("mhd_new", (r as ApiResult.Ok).value.token)
        val req = server.takeRequest()
        assertEquals("/v1/auth/kakao/device", req.path)
        assertEquals(null, req.getHeader("Authorization"))
        assertTrue(req.body.readUtf8().contains("\"access_token\":\"kakao-at\""))
    }

    @Test fun kakaoNotAllowed() {
        server.enqueue(MockResponse().setResponseCode(403).setBody("""{"error":"kakao account not allowed","kakao_id":9}"""))
        val r = api().kakaoDeviceLogin("x", "y")
        assertEquals(403, (r as ApiResult.HttpError).code)
    }

    @Test fun notConfigured() {
        token = null
        val r = api().postEvent(payload)
        assertTrue(r is ApiResult.NotConfigured)
        assertEquals(0, server.requestCount)
    }
}
