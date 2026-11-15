package com.yuntyu.myhome.core.api

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonElement

/** 서버와 주고받는 JSON 설정. 서버가 필드를 추가해도 앱이 깨지지 않게 모르는 키는 무시한다. */
val MyHomeJson = Json {
    ignoreUnknownKeys = true
    explicitNulls = false
    encodeDefaults = true
}

/** POST /v1/events 본문. 알림 원문 그대로. */
@Serializable
data class EventPayload(
    @SerialName("source_package") val sourcePackage: String,
    val title: String,
    val text: String,
    @SerialName("big_text") val bigText: String,
    /** Unix 밀리초 (StatusBarNotification.postTime) */
    @SerialName("posted_at") val postedAt: Long,
)

@Serializable
data class EventResponse(
    val id: Long,
    val duplicate: Boolean = false,
    val result: JsonElement? = null,
    /** 원래 알림을 지울지(dismiss|wait|keep, NoticePolicy). 예전 서버는 보내지 않는다(null이면 바로 지운다). */
    val notice: String? = null,
)

/** 읽지 못한 알림 원문(GET /v1/raw-events?failed=true). postedAt은 ISO-8601 */
@Serializable
data class RawEventDto(
    val id: Long,
    @SerialName("source_package") val sourcePackage: String = "",
    val title: String = "",
    val text: String = "",
    @SerialName("big_text") val bigText: String = "",
    @SerialName("posted_at") val postedAt: String = "",
    @SerialName("parse_error") val parseError: String? = null,
)

@Serializable
data class RawEventList(@SerialName("raw_events") val rawEvents: List<RawEventDto> = emptyList())

/** POST /v1/client-errors: 앱이 죽은 스택이나 화면에 띄운 오류 메시지(알림 원문·금액·사진은 넣지 않는다) */
@Serializable
data class ClientError(
    val kind: String,
    val message: String,
    @SerialName("app_version") val appVersion: String = "",
    val at: String = "",
)

/** GET /v1/events/notice 응답의 한 줄. */
@Serializable
data class EventNotice(val id: Long, val notice: String)

@Serializable
data class EventNotices(val notices: List<EventNotice> = emptyList())

/** 오류 본문 {"error": 메시지, "code": 코드}. code는 건강 API처럼 붙이는 경로만 있다. */
@Serializable
data class ErrorResponse(val error: String = "", val code: String? = null)

/** POST /v1/auth/exchange 요청: 브라우저 로그인 뒤 받은 1회용 코드와 PKCE verifier로 기기 토큰을 받는다. */
@Serializable
data class AppLinkExchange(
    val code: String,
    val verifier: String,
    @SerialName("device_name") val deviceName: String,
)

@Serializable
data class DeviceLoginResponse(
    val token: String,
    @SerialName("device_id") val deviceId: Long,
    @SerialName("device_name") val deviceName: String = "",
)
