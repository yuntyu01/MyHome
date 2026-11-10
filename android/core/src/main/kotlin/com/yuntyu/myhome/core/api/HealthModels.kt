package com.yuntyu.myhome.core.api

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.JsonObject

/*
 * 건강(핏두잉 이식) API 모델. /v1/health 아래 JSON 이름은 핏두잉 서버와 같다(camelCase).
 * 서버가 필드를 더하거나 빼도 깨지지 않게 전부 기본값을 둔다.
 */

@Serializable
data class Nutrition(val kcal: Int = 0, val carbs: Int = 0, val protein: Int = 0, val fat: Int = 0)

/** AI가 알아본 음식 하나. 숫자는 그 음식 전체 기준이고, portion은 그중 먹은 비율(null = 다). */
@Serializable
data class FoodItem(
    val name: String = "",
    val amount: String = "",
    val kcal: Int? = null,
    val carbs: Int? = null,
    val protein: Int? = null,
    val fat: Int? = null,
    val portion: Double? = null,
)

/** 사진 분석 결과이자 끼니 저장 요청의 ai 부분. 합계는 음식 합(음식별 portion 반영). */
@Serializable
data class FoodResult(
    val items: List<FoodItem> = emptyList(),
    val kcal: Int = 0,
    val carbs: Int = 0,
    val protein: Int = 0,
    val fat: Int = 0,
    val confidence: Double = 0.0,
)

@Serializable
data class MealView(
    val date: String = "",
    val index: Int = 0,
    val deadline: String = "",
    val barcode: String? = null,
    val items: List<FoodItem>? = null,
    val nutrition: Nutrition = Nutrition(),
    val base: Nutrition = Nutrition(),
    val portion: Double = 1.0,
    val sharedMeal: Boolean = false,
    val confidence: Double = 0.0,
    val loggedAt: String = "",
)

@Serializable
data class MealRequest(
    val date: String,
    val index: Int,
    val ai: FoodResult,
    val portion: Double = 1.0,
    val sharedMeal: Boolean = false,
    val barcode: String? = null,
    /** 먹은 시각(ISO-8601, 오프셋 포함). 없으면 서버가 저장한 시각으로 */
    val loggedAt: String? = null,
)

/** 오늘 식단 표 한 줄. 아직 안 먹은 끼니는 target(남은 목표를 남은 끼니로 나눈 값), 놓친 끼니는 둘 다 없다. */
@Serializable
data class MealRow(
    val index: Int = 0,
    val deadline: String = "",
    val logged: Boolean = false,
    val missed: Boolean = false,
    val nutrition: Nutrition? = null,
    val target: Nutrition? = null,
)

@Serializable
data class GymWeek(
    val enabled: Boolean = false,
    val done: Int = 0,
    val planned: Int = 0,
    val scheduledToday: Boolean = false,
    val arriveBy: String = "",
)

@Serializable
data class Balance(val total: Int = 0, val left: Int = 0)

/** 휴식권(헬스 쉬는 날). 주 단위로 월요일에 다시 채워진다. */
@Serializable
data class RestSummary(val weekKey: String = "", val rest: Balance = Balance())

@Serializable
data class HealthToday(
    val date: String = "",
    val streak: Int = 0,
    val loggedCount: Int = 0,
    val mealCount: Int = 0,
    val total: Nutrition = Nutrition(),
    val targets: Nutrition = Nutrition(),
    val rows: List<MealRow>? = null,
    val gym: GymWeek = GymWeek(),
    val coupons: RestSummary = RestSummary(),
)

@Serializable
data class WeekDay(
    val index: Int = 0,
    val date: String = "",
    val state: String = "",
    val current: Nutrition? = null,
    val last: Nutrition? = null,
)

@Serializable
data class Furthest(val index: Int = 0, val diff: Int = 0)

@Serializable
data class WeekSummary(val completed: Int = 0, val achieved: Int = 0, val furthest: Furthest? = null)

@Serializable
data class HealthWeekly(
    val weekStart: String = "",
    val days: List<WeekDay>? = null,
    val avg: Nutrition? = null,
    val lastAvg: Nutrition? = null,
    val targets: Nutrition = Nutrition(),
    val bandPct: Int = 10,
    val summary: Map<String, WeekSummary>? = null,
)

@Serializable
data class CalendarDay(
    val date: String = "",
    val state: String = "none",
    val loggedMeals: Int = 0,
    val gym: Boolean = false,
)

@Serializable
data class MonthSummary(val fullDays: Int = 0, val gymDays: Int = 0)

@Serializable
data class HealthMonth(
    val month: String = "",
    val mealCount: Int = 0,
    val summary: MonthSummary = MonthSummary(),
    val days: List<CalendarDay>? = null,
)

@Serializable
data class DayCard(
    val date: String = "",
    val total: Nutrition = Nutrition(),
    val targets: Nutrition = Nutrition(),
    val meals: List<MealView>? = null,
    val gym: Boolean = false,
    val gymVerifiedBy: String? = null,
)

@Serializable
data class MealSlot(val index: Int, val deadline: String)

@Serializable
data class GymPlan(
    val enabled: Boolean = false,
    val days: List<Int> = emptyList(), // 0=월 … 6=일
    val weeklyTarget: Int = 0,
    val arriveBy: String = "19:00",
)

@Serializable
data class SleepPlan(val enabled: Boolean = false, val bedTime: String = "23:30", val wakeTime: String = "07:00")

/**
 * 건강 설정. 서버에는 이 앱이 모르는 필드도 있을 수 있으므로, 저장할 때는 받은 원본(JsonObject)에
 * 바꾼 필드만 덮어써서 보낸다(HealthSettingsPatch).
 */
@Serializable
data class HealthSettings(
    val goal: String = "keep",
    val heightCm: Int = 0,
    val weightKg: Double = 0.0,
    val sex: String? = null,
    val age: Int? = null,
    val activity: String? = null,
    val meals: List<MealSlot> = emptyList(),
    val gym: GymPlan? = null,
    val sleep: SleepPlan? = null,
    val targets: Nutrition? = null,
    val notifications: Boolean? = null,
)

/** GET/PUT /v1/health/settings 응답. 핏두잉처럼 {settings: {...}}로 감싸 온다. */
@Serializable
data class HealthSettingsView(val settings: JsonObject = JsonObject(emptyMap()))

@Serializable
data class Recommendation(
    val kcal: Int = 0,
    val carbs: Int = 0,
    val protein: Int = 0,
    val fat: Int = 0,
    @SerialName("maintenanceKcal") val maintenanceKcal: Double = 0.0,
    @SerialName("goalAdjustPct") val goalAdjustPct: Double = 0.0,
)

@Serializable
data class GymCheckIn(val date: String, val verifiedBy: String, val checkInAt: String? = null)

@Serializable
data class RestRequest(val date: String)

/** 서버에 저장된 끼니 사진 목록(GET·PUT /v1/health/meals/photos). */
@Serializable
data class MealPhotoList(val photos: List<MealPhotoInfo> = emptyList())

@Serializable
data class MealPhotoInfo(val n: Int, val bytes: Int = 0, val sha256: String = "")

/** GET /v1/health/foods/barcode/{code}. 영양성분을 모르면 result가 null이다. */
@Serializable
data class BarcodeAnswer(val product: BarcodeProduct = BarcodeProduct(), val result: FoodResult? = null)

@Serializable
data class BarcodeProduct(val barcode: String = "", val name: String = "", val maker: String = "", val amount: String = "")

/** 하루 몸무게(GET·PUT /v1/health/body) */
@Serializable
data class Weight(val date: String, val weightKg: Double)

@Serializable
data class WeightList(val weights: List<Weight> = emptyList())

/** 저울 사진에서 읽은 몸무게(POST /v1/health/body/scan). weightKg 0 = 못 읽음 */
@Serializable
data class ScaleReading(val weightKg: Double = 0.0, val confidence: Double = 0.0)
