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

    /** 이번 기간 요약(위젯·홈). recent는 최근 거래 개수. */
    fun summary(recent: Int = 4): ApiResult<Summary> = call("GET", "/v1/summary?recent=$recent", null, Summary.serializer())

    /** 투자 탭 첫 화면(자동매매 상태, 읽기 전용) */
    fun invest(): ApiResult<InvestStatus> = call("GET", "/v1/invest", null, InvestStatus.serializer())

    /** 투자 평가 화면(평가액·원금·원금 내역, 읽기 전용) */
    fun investValue(): ApiResult<InvestValue> = call("GET", "/v1/invest/value", null, InvestValue.serializer())

    /** 확인 필요 거래 목록 */
    fun reviewQueue(): ApiResult<TransactionList> = call("GET", "/v1/review", null, TransactionList.serializer())

    /** 이번 기간 거래 목록 */
    fun transactions(limit: Int = 50): ApiResult<TransactionList> =
        call("GET", "/v1/transactions?limit=$limit", null, TransactionList.serializer())

    fun categories(): ApiResult<CategoryList> = call("GET", "/v1/categories", null, CategoryList.serializer())

    /** 거래 분류 변경. rememberRule이면 "이 가맹점은 항상" 규칙을 저장한다. */
    fun editTransaction(id: Long, edit: TransactionEdit): ApiResult<EditResponse> =
        call("PATCH", "/v1/transactions/$id", MyHomeJson.encodeToString(TransactionEdit.serializer(), edit), EditResponse.serializer())

    /** 카카오 로그인으로 기기 토큰을 받는다. 기기 토큰이 아직 없으므로 인증 없이 호출한다. */
    fun kakaoDeviceLogin(kakaoAccessToken: String, deviceName: String): ApiResult<DeviceLoginResponse> =
        call(
            "POST",
            "/v1/auth/kakao/device",
            MyHomeJson.encodeToString(DeviceLoginRequest.serializer(), DeviceLoginRequest(kakaoAccessToken, deviceName)),
            DeviceLoginResponse.serializer(),
            auth = false,
        )

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
