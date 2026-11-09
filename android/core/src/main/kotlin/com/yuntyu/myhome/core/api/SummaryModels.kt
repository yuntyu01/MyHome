package com.yuntyu.myhome.core.api

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable

/** GET /v1/summary 응답(위젯·앱 홈). 서버가 필드를 추가해도 무시한다. */
@Serializable
data class Summary(
    val period: PeriodInfo,
    val totals: Totals,
    val categories: List<CategoryBudget> = emptyList(),
    val recent: List<TransactionDto> = emptyList(),
    @SerialName("needs_review") val needsReview: Long = 0,
    /** 최근 7일 동안 서버가 읽지 못했거나 처리하지 못한 알림 수(웹 관리 > 읽지 못한 알림) */
    @SerialName("failed_events") val failedEvents: Long = 0,
    @SerialName("generated_at") val generatedAt: String = "",
)

@Serializable
data class PeriodInfo(val label: String, val start: String, val end: String)

@Serializable
data class Totals(
    val budget: Long = 0,
    val spent: Long = 0,
    val settled: Long = 0,
    val remaining: Long = 0,
    val income: Long = 0,
    val investment: Long = 0,
    @SerialName("own_transfer") val ownTransfer: Long = 0,
)

/**
 * 분류별 예산 현황. 폴더는 아래 분류까지 더한 값이고, 목록은 폴더 다음에 그 안 순서다.
 * 맨 위 분류(parentId == null)만 더하면 총계가 된다. docs/DECISIONS.md 「분류 폴더」
 */
@Serializable
data class CategoryBudget(
    val id: Long,
    val name: String,
    @SerialName("parent_id") val parentId: Long? = null,
    val depth: Int = 0,
    /** "생활비 › 식비". 옛 서버는 안 주니 비어 있으면 name을 쓴다 */
    val path: String = "",
    /** 이 분류에 직접 정한 예산. 옛 서버는 안 준다(null이면 budget을 직접 정한 것으로 본다) */
    @SerialName("own_budget") val ownBudget: Long? = null,
    /** 실제로 쓰는 예산: 직접 정했으면 그 값, 안 정한 폴더면 아래 예산의 합 */
    val budget: Long = 0,
    val spent: Long = 0,
    val settled: Long = 0,
    val remaining: Long = 0,
    val ratio: Double = 0.0,
)

@Serializable
data class TransactionDto(
    val id: Long,
    @SerialName("occurred_at") val occurredAt: String,
    val amount: Long,
    val flow: String,
    val merchant: String = "",
    val memo: String = "",
    val direction: String = "",
    @SerialName("category_id") val categoryId: Long? = null,
    val category: String = "",
    val status: String = "",
    @SerialName("review_reason") val reviewReason: String? = null,
    val source: String = "",
    /** 규칙에 없는 가맹점에 AI가 추천한 분류(서버가 확신이 낮으면 null). 확정은 사용자가 한 번 눌러서 한다. */
    val suggestion: Suggestion? = null,
)

@Serializable
data class Suggestion(
    @SerialName("category_id") val categoryId: Long,
    val category: String = "",
    val confidence: Double = 0.0,
)

@Serializable
data class TransactionList(val transactions: List<TransactionDto> = emptyList())

@Serializable
data class Category(
    val id: Long,
    val name: String,
    val kind: String,
    val code: String? = null,
    val sort: Int = 0,
    val archived: Boolean = false,
    /** 넣어 둔 폴더(맨 위면 null) */
    @SerialName("parent_id") val parentId: Long? = null,
    /** 사용자가 적은 설명. AI 분류 추천이 참고한다 */
    val memo: String = "",
)

@Serializable
data class CategoryList(val categories: List<Category> = emptyList())

/** PATCH /v1/transactions/{id} 요청 */
@Serializable
data class TransactionEdit(
    @SerialName("category_id") val categoryId: Long? = null,
    @SerialName("settle_category_id") val settleCategoryId: Long? = null,
    val status: String? = null,
    @SerialName("remember_rule") val rememberRule: Boolean = false,
)

@Serializable
data class EditResponse(
    val transaction: TransactionDto,
    @SerialName("rule_id") val ruleId: Long? = null,
    @SerialName("also_updated") val alsoUpdated: Int = 0,
)

/** POST /v1/transactions 요청(현금 등 직접 입력). flow는 "out"(지출) 또는 "in" */
@Serializable
data class ManualTransaction(
    val amount: Long,
    val merchant: String = "",
    val memo: String = "",
    @SerialName("category_id") val categoryId: Long,
    val flow: String = "out",
    /** ISO-8601. 없으면 서버가 지금 시각으로 */
    @SerialName("occurred_at") val occurredAt: String? = null,
)

@Serializable
data class CreatedTransaction(val transaction: TransactionDto)

@Serializable
data class CategoryInput(
    val name: String? = null,
    val kind: String? = null,
    val archived: Boolean? = null,
    /** 넣을 폴더. 0이면 맨 위로, null이면 그대로 */
    @SerialName("parent_id") val parentId: Long? = null,
    val memo: String? = null,
)

@Serializable
data class BudgetItem(
    @SerialName("category_id") val categoryId: Long,
    val category: String = "",
    val amount: Long,
)

@Serializable
data class BudgetList(val period: PeriodInfo? = null, val items: List<BudgetItem> = emptyList())

@Serializable
data class BudgetsInput(val month: String, val items: List<BudgetItem>)

@Serializable
data class ServerSettings(val settings: Map<String, kotlinx.serialization.json.JsonElement> = emptyMap())
