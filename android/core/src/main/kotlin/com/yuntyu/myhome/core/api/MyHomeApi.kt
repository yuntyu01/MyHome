package com.yuntyu.myhome.core.api

import kotlinx.serialization.KSerializer
import kotlinx.serialization.serializer
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.RequestBody
import okhttp3.RequestBody.Companion.toRequestBody
import java.io.IOException
import java.util.concurrent.TimeUnit

/**
 * 가계부 서버 API 클라이언트. 호출은 블로킹이므로 백그라운드 스레드(Dispatchers.IO, Worker)에서 부른다.
 *
 * @param baseUrl 서버 주소를 돌려주는 함수(설정이 바뀌어도 새 인스턴스가 필요 없게)
 * @param token 기기 토큰을 돌려주는 함수
 */
class MyHomeApi(
    private val baseUrl: () -> String?,
    private val token: () -> String?,
    private val client: OkHttpClient = defaultClient(),
) {
    fun postEvent(payload: EventPayload): ApiResult<EventResponse> =
        call("POST", "/v1/events", MyHomeJson.encodeToString(EventPayload.serializer(), payload), EventResponse.serializer())

    /** 공통 호출. 인증이 필요 없는 경로는 auth=false. */
    fun <T> call(
        method: String,
        path: String,
        jsonBody: String?,
        responseSerializer: KSerializer<T>,
        auth: Boolean = true,
    ): ApiResult<T> {
        val base = baseUrl()?.takeIf { it.isNotBlank() } ?: return ApiResult.NotConfigured("서버 주소")
        val builder = Request.Builder().url(base.trimEnd('/') + path)
        if (auth) {
            val tok = token()?.takeIf { it.isNotBlank() } ?: return ApiResult.NotConfigured("기기 토큰")
            builder.header("Authorization", "Bearer $tok")
        }
        val body: RequestBody? = jsonBody?.toRequestBody(JSON)
        builder.method(method, body ?: if (method == "POST" || method == "PUT" || method == "PATCH") ByteArray(0).toRequestBody(JSON) else null)
        return try {
            client.newCall(builder.build()).execute().use { resp ->
                val text = resp.body?.string().orEmpty()
                if (resp.isSuccessful) {
                    try {
                        ApiResult.Ok(MyHomeJson.decodeFromString(responseSerializer, text.ifEmpty { "{}" }), resp.code)
                    } catch (e: Exception) {
                        ApiResult.HttpError(resp.code, "응답 해석 실패: ${e.message}")
                    }
                } else {
                    val msg = runCatching { MyHomeJson.decodeFromString(ErrorResponse.serializer(), text).error }.getOrDefault(text)
                    ApiResult.HttpError(resp.code, msg)
                }
            }
        } catch (e: IOException) {
            ApiResult.NetworkError(e)
        } catch (e: IllegalArgumentException) {
            ApiResult.NotConfigured("서버 주소 형식")
        }
    }

    inline fun <reified T> get(path: String): ApiResult<T> = call("GET", path, null, serializer<T>())

    companion object {
        val JSON = "application/json; charset=utf-8".toMediaType()

        fun defaultClient(): OkHttpClient = OkHttpClient.Builder()
            .connectTimeout(15, TimeUnit.SECONDS)
            .readTimeout(20, TimeUnit.SECONDS)
            .writeTimeout(20, TimeUnit.SECONDS)
            .build()
    }
}
