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

@Serializable
data class CategoryBudget(
    val id: Long,
    val name: String,
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
