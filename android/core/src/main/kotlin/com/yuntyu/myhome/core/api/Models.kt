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
)

@Serializable
data class ErrorResponse(val error: String = "")

/** POST /v1/auth/kakao/device 요청: 카카오 SDK가 준 액세스 토큰을 서버가 검증한다. */
@Serializable
data class DeviceLoginRequest(
    @SerialName("access_token") val accessToken: String,
    @SerialName("device_name") val deviceName: String,
)

@Serializable
data class DeviceLoginResponse(
    val token: String,
    @SerialName("device_id") val deviceId: Long,
    @SerialName("device_name") val deviceName: String = "",
)
