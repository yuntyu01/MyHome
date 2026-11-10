package com.yuntyu.myhome.core.api

import kotlinx.serialization.KSerializer
import kotlinx.serialization.serializer
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonObject
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.MultipartBody
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

    /** 투자 → 백테스트 결과(고정 표, 읽기 전용) */
    fun investBacktest(): ApiResult<Backtest> = call("GET", "/v1/invest/backtest", null, Backtest.serializer())

    /** 매매 실패 알림 중 [after] 다음 것(오래된 것부터, 7일 안). 앱 주기 작업이 폰 알림으로 띄운다. */
    fun investAlerts(after: Long): ApiResult<InvestAlerts> =
        call("GET", "/v1/invest/alerts?after=$after", null, InvestAlerts.serializer())

    /** 확인 필요 거래 목록 */
    fun reviewQueue(): ApiResult<TransactionList> = call("GET", "/v1/review", null, TransactionList.serializer())

    /**
     * 기간 거래 목록. month는 "2026-11"처럼 기간 이름(없으면 이번 기간), status는 "needs_review" 같은 상태,
     * direction은 "income" 같은 방향으로 거른다. from·to("2026-11-01", 둘 다 줄 때만)를 주면 month 대신 그 날짜 사이,
     * search를 주면 가게 이름·메모에 그 글자가 든 것만(검색).
     */
    fun transactions(
        limit: Int = 50,
        month: String? = null,
        status: String? = null,
        direction: String? = null,
        from: String? = null,
        to: String? = null,
        search: String? = null,
    ): ApiResult<TransactionList> {
        val qs = buildString {
            append("limit=").append(limit)
            if (from != null && to != null) {
                append("&from=").append(q(from)).append("&to=").append(q(to))
            } else {
                month?.let { append("&month=").append(q(it)) }
            }
            status?.let { append("&status=").append(q(it)) }
            direction?.let { append("&direction=").append(q(it)) }
            search?.trim()?.takeIf { it.isNotEmpty() }?.let { append("&q=").append(q(it)) }
        }
        return call("GET", "/v1/transactions?$qs", null, TransactionList.serializer())
    }

    /** 현금 등 직접 적는 거래 */
    fun createTransaction(m: ManualTransaction): ApiResult<CreatedTransaction> =
        call("POST", "/v1/transactions", MyHomeJson.encodeToString(ManualTransaction.serializer(), m), CreatedTransaction.serializer())

    /** 보관한 분류도 함께 온다(archived) */
    fun categories(): ApiResult<CategoryList> = call("GET", "/v1/categories", null, CategoryList.serializer())

    /** 분류 만들기. parentId(폴더) 안에 만들면 종류는 폴더를 따른다(kind는 맨 위일 때만 쓴다). 3단계를 넘으면 400 */
    fun createCategory(name: String, kind: String = "expense", parentId: Long? = null, memo: String? = null): ApiResult<Category> =
        call(
            "POST", "/v1/categories",
            MyHomeJson.encodeToString(CategoryInput.serializer(), CategoryInput(name = name, kind = kind.takeIf { parentId == null }, parentId = parentId, memo = memo)),
            Category.serializer(),
        )

    /** 이름·설명 바꾸기, 폴더 옮기기(parentId 0 = 맨 위), 숨기기(archived). kind는 못 바꾼다. null인 값은 그대로 둔다. */
    fun updateCategory(id: Long, name: String? = null, archived: Boolean? = null, parentId: Long? = null, memo: String? = null): ApiResult<Category> =
        call(
            "PATCH", "/v1/categories/$id",
            MyHomeJson.encodeToString(CategoryInput.serializer(), CategoryInput(name = name, archived = archived, parentId = parentId, memo = memo)),
            Category.serializer(),
        )

    /** 기간 예산(지출 분류마다). month가 없으면 이번 기간 */
    fun budgets(month: String? = null): ApiResult<BudgetList> =
        call("GET", "/v1/budgets" + (month?.let { "?month=${q(it)}" } ?: ""), null, BudgetList.serializer())

    fun putBudgets(month: String, items: List<BudgetItem>): ApiResult<BudgetList> =
        call("PUT", "/v1/budgets", MyHomeJson.encodeToString(BudgetsInput.serializer(), BudgetsInput(month, items)), BudgetList.serializer())

    /** 예산 시작일(매달 1~28일) */
    fun budgetStartDay(): ApiResult<Int> = when (val r = call("GET", "/v1/settings", null, ServerSettings.serializer())) {
        is ApiResult.Ok -> ApiResult.Ok(r.value.settings["budget_start_day"]?.let { runCatching { it.toString().trim('"').toInt() }.getOrNull() } ?: 1, r.code)
        is ApiResult.HttpError -> r
        is ApiResult.NetworkError -> r
        is ApiResult.NotConfigured -> r
    }

    fun putBudgetStartDay(day: Int): ApiResult<JsonElement> =
        call("PUT", "/v1/settings/budget_start_day", day.toString(), JsonElement.serializer())

    /** 거래 분류 변경. rememberRule이면 "이 가맹점은 항상" 규칙을 저장한다. */
    fun editTransaction(id: Long, edit: TransactionEdit): ApiResult<EditResponse> =
        call("PATCH", "/v1/transactions/$id", MyHomeJson.encodeToString(TransactionEdit.serializer(), edit), EditResponse.serializer())

    /** 브라우저(구글 로그인)에서 받은 1회용 코드를 기기 토큰으로 바꾼다. 기기 토큰이 아직 없으므로 인증 없이 호출한다. */
    fun exchangeAppLink(code: String, verifier: String, deviceName: String): ApiResult<DeviceLoginResponse> =
        call(
            "POST",
            "/v1/auth/exchange",
            MyHomeJson.encodeToString(AppLinkExchange.serializer(), AppLinkExchange(code, verifier, deviceName)),
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
    ): ApiResult<T> = send(method, path, jsonBody?.toRequestBody(JSON), responseSerializer, auth, client)

    private fun <T> send(
        method: String,
        path: String,
        body: RequestBody?,
        responseSerializer: KSerializer<T>,
        auth: Boolean,
        http: OkHttpClient,
    ): ApiResult<T> {
        val base = baseUrl()?.takeIf { it.isNotBlank() } ?: return ApiResult.NotConfigured("서버 주소")
        val builder = try {
            Request.Builder().url(base.trimEnd('/') + path)
        } catch (e: IllegalArgumentException) {
            return ApiResult.NotConfigured("서버 주소 형식")
        }
        if (auth) {
            val tok = token()?.takeIf { it.isNotBlank() } ?: return ApiResult.NotConfigured("기기 토큰")
            builder.header("Authorization", "Bearer $tok")
        }
        builder.method(method, body ?: if (method == "POST" || method == "PUT" || method == "PATCH") ByteArray(0).toRequestBody(JSON) else null)
        return try {
            http.newCall(builder.build()).execute().use { resp ->
                val text = resp.body?.string().orEmpty()
                if (resp.isSuccessful) {
                    try {
                        ApiResult.Ok(MyHomeJson.decodeFromString(responseSerializer, text.ifEmpty { "{}" }), resp.code)
                    } catch (e: Exception) {
                        ApiResult.HttpError(resp.code, "응답 해석 실패: ${e.message}")
                    }
                } else {
                    val err = runCatching { MyHomeJson.decodeFromString(ErrorResponse.serializer(), text) }.getOrNull()
                    ApiResult.HttpError(resp.code, err?.error ?: text, err?.code)
                }
            }
        } catch (e: IOException) {
            ApiResult.NetworkError(e)
        } catch (e: IllegalArgumentException) {
            ApiResult.NotConfigured("서버 주소 형식")
        }
    }

    inline fun <reified T> get(path: String): ApiResult<T> = call("GET", path, null, serializer<T>())

    // ── 건강(핏두잉 이식). 날짜는 "yyyy-MM-dd", 시각은 "HH:mm" ──

    fun healthToday(date: String, now: String): ApiResult<HealthToday> =
        call("GET", "/v1/health/today?date=$date&now=${q(now)}", null, HealthToday.serializer())

    fun healthWeekly(date: String): ApiResult<HealthWeekly> = call("GET", "/v1/health/weekly?date=$date", null, HealthWeekly.serializer())

    fun healthMonth(month: String): ApiResult<HealthMonth> = call("GET", "/v1/health/records/month?month=$month", null, HealthMonth.serializer())

    fun healthDay(date: String): ApiResult<DayCard> = call("GET", "/v1/health/records/day?date=$date", null, DayCard.serializer())

    fun healthSettings(): ApiResult<HealthSettingsView> = call("GET", "/v1/health/settings", null, HealthSettingsView.serializer())

    /** 받은 설정 원본에 바꾼 필드만 덮어써서 보낸다(모르는 필드를 지우지 않게). */
    fun putHealthSettings(settings: JsonObject): ApiResult<HealthSettingsView> =
        call("PUT", "/v1/health/settings", MyHomeJson.encodeToString(JsonObject.serializer(), settings), HealthSettingsView.serializer())

    fun saveMeal(req: MealRequest): ApiResult<JsonElement> =
        call("POST", "/v1/health/meals", MyHomeJson.encodeToString(MealRequest.serializer(), req), JsonElement.serializer())

    fun deleteMeal(date: String, index: Int): ApiResult<JsonElement> =
        call("DELETE", "/v1/health/meals?date=$date&index=$index", null, JsonElement.serializer())

    /** checkInAt은 실제로 도착한 시각(ISO-8601). 나중에 다시 보낼 때 보낸 시각이 아니라 도착 시각으로 남게. */
    fun gymCheckIn(date: String, verifiedBy: String, checkInAt: String? = null): ApiResult<JsonElement> =
        call("POST", "/v1/health/gym/check-in", MyHomeJson.encodeToString(GymCheckIn.serializer(), GymCheckIn(date, verifiedBy, checkInAt)), JsonElement.serializer())

    fun restSummary(): ApiResult<RestSummary> = call("GET", "/v1/health/rest", null, RestSummary.serializer())

    fun useRest(date: String): ApiResult<JsonElement> =
        call("POST", "/v1/health/rest", MyHomeJson.encodeToString(RestRequest.serializer(), RestRequest(date)), JsonElement.serializer())

    fun recommendTargets(goal: String, weightKg: Double, heightCm: Int, age: Int, sex: String, activity: String, mode: String = "balanced"): ApiResult<Recommendation> =
        call(
            "GET",
            "/v1/health/targets/recommend?goal=${q(goal)}&weightKg=$weightKg&heightCm=$heightCm&age=$age&sex=${q(sex)}&activity=${q(activity)}&mode=${q(mode)}",
            null,
            Recommendation.serializer(),
        )

    // 집중 타이머 동기화(FocusModels.kt). 과목을 먼저 보내야 기록이 그 과목을 찾는다(모르는 과목이면 400)
    fun putFocusSubject(id: String, b: FocusSubjectBody): ApiResult<FocusSubjectDto> =
        call("PUT", "/v1/focus/subjects/${q(id)}", MyHomeJson.encodeToString(FocusSubjectBody.serializer(), b), FocusSubjectDto.serializer())

    fun putFocusSession(id: String, b: FocusSessionBody): ApiResult<FocusSessionDto> =
        call("PUT", "/v1/focus/sessions/${q(id)}", MyHomeJson.encodeToString(FocusSessionBody.serializer(), b), FocusSessionDto.serializer())

    fun putFocusStay(id: String, b: FocusStayBody): ApiResult<FocusStayDto> =
        call("PUT", "/v1/focus/stays/${q(id)}", MyHomeJson.encodeToString(FocusStayBody.serializer(), b), FocusStayDto.serializer())

    fun focusAll(): ApiResult<FocusAll> = call("GET", "/v1/focus/all", null, FocusAll.serializer())

    /** 바코드로 제품·영양성분 찾기(식약처 → Open Food Facts). 못 찾으면 404, 조회 불가면 503. */
    fun barcode(code: String): ApiResult<BarcodeAnswer> = call("GET", "/v1/health/foods/barcode/$code", null, BarcodeAnswer.serializer())

    /** 저장한 끼니의 사진(0~3장, JPEG)을 서버에 통째로 바꿔 둔다. 서버 저장이 꺼져 있으면 503. */
    fun putMealPhotos(date: String, index: Int, photos: List<ByteArray>): ApiResult<MealPhotoList> {
        val form = MultipartBody.Builder().setType(MultipartBody.FORM)
        photos.forEachIndexed { i, b -> form.addFormDataPart("photo", "meal$i.jpg", b.toRequestBody(JPEG)) }
        if (photos.isEmpty()) form.addFormDataPart("none", "")
        val slow = client.newBuilder().readTimeout(60, TimeUnit.SECONDS).writeTimeout(60, TimeUnit.SECONDS).build()
        return send("PUT", "/v1/health/meals/photos?date=$date&index=$index", form.build(), MealPhotoList.serializer(), true, slow)
    }

    fun mealPhotos(date: String, index: Int): ApiResult<MealPhotoList> =
        call("GET", "/v1/health/meals/photos?date=$date&index=$index", null, MealPhotoList.serializer())

    /** 서버에 저장된 끼니 사진 한 장(JPEG 바이트). */
    fun mealPhoto(date: String, index: Int, n: Int): ApiResult<ByteArray> {
        val base = baseUrl()?.takeIf { it.isNotBlank() } ?: return ApiResult.NotConfigured("서버 주소")
        val tok = token()?.takeIf { it.isNotBlank() } ?: return ApiResult.NotConfigured("기기 토큰")
        return try {
            val req = Request.Builder().url(base.trimEnd('/') + "/v1/health/meals/photos/$date/$index/$n").header("Authorization", "Bearer $tok").get().build()
            client.newCall(req).execute().use { resp ->
                if (resp.isSuccessful) ApiResult.Ok(resp.body?.bytes() ?: ByteArray(0), resp.code) else ApiResult.HttpError(resp.code, "사진 받기 실패")
            }
        } catch (e: IOException) {
            ApiResult.NetworkError(e)
        } catch (e: IllegalArgumentException) {
            ApiResult.NotConfigured("서버 주소 형식")
        }
    }

    /**
     * 끼니 사진(1~3장, JPEG)을 분석한다. 이 호출에서는 사진을 저장하지 않는다(저장은 기록한 뒤 putMealPhotos).
     * AI 호출이 오래 걸릴 수 있어 읽기 제한 시간을 늘린다.
     */
    fun analyzeMeal(photos: List<ByteArray>): ApiResult<FoodResult> {
        val form = MultipartBody.Builder().setType(MultipartBody.FORM)
        photos.forEachIndexed { i, b -> form.addFormDataPart("photo", "meal$i.jpg", b.toRequestBody(JPEG)) }
        // 서버는 Gemini를 두 번까지 부르고 55초 안에 답한다
        val slow = client.newBuilder().readTimeout(70, TimeUnit.SECONDS).writeTimeout(60, TimeUnit.SECONDS).build()
        return send("POST", "/v1/health/meals/analyze", form.build(), FoodResult.serializer(), true, slow)
    }

    private fun q(s: String): String = java.net.URLEncoder.encode(s, "UTF-8")

    companion object {
        val JSON = "application/json; charset=utf-8".toMediaType()
        val JPEG = "image/jpeg".toMediaType()

        fun defaultClient(): OkHttpClient = OkHttpClient.Builder()
            .connectTimeout(15, TimeUnit.SECONDS)
            .readTimeout(20, TimeUnit.SECONDS)
            .writeTimeout(20, TimeUnit.SECONDS)
            .build()
    }
}
