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
    /** 알림에 찍힌 계좌 마지막 잔액과 잔액이 안 맞는 빈 돈 수(옛 서버는 안 준다) */
    val balances: List<BalanceBrief> = emptyList(),
    /** 위젯에서 동그라미 한 번으로 분류할 다음 거래(애매한 가게·처음 보는 가게 등). 없으면 null */
    val pick: Pick? = null,
    @SerialName("generated_at") val generatedAt: String = "",
)

/** 위젯에서 고를 거래 하나. choices는 동그라미(많이 쓴·전에 고른 분류 순), left는 이것을 포함해 남은 수 */
@Serializable
data class Pick(
    val id: Long,
    val merchant: String = "",
    val amount: Long = 0,
    @SerialName("occurred_at") val occurredAt: String = "",
    val reason: String = "",
    val choices: List<PickChoice> = emptyList(),
    val left: Long = 1,
)

@Serializable
data class PickChoice(val id: Long, val name: String)

/** 계좌 하나(예: 토스뱅크)의 마지막 알림 잔액. at은 그 알림 시각(ISO-8601) */
@Serializable
data class BalanceBrief(
    val source: String = "",
    val label: String = "",
    val balance: Long = 0,
    val at: String = "",
    @SerialName("open_gaps") val openGaps: Int = 0,
)

/** GET /v1/balances: 계좌마다 마지막 잔액과 아직 처리 안 한 빈 돈(최근 것부터) */
@Serializable
data class BalanceAccounts(val accounts: List<BalanceAccount> = emptyList())

@Serializable
data class BalanceAccount(
    val source: String = "",
    val label: String = "",
    val balance: Long = 0,
    val at: String = "",
    val gaps: List<BalanceGap> = emptyList(),
)

/** 잔액이 안 맞는 곳 하나. amount가 음수면 나간 돈을, 양수면 들어온 돈을 놓쳤다. 처리는 toId로 한다 */
@Serializable
data class BalanceGap(
    @SerialName("to_id") val toId: Long,
    @SerialName("from_at") val fromAt: String = "",
    @SerialName("to_at") val toAt: String = "",
    @SerialName("from_balance") val fromBalance: Long = 0,
    @SerialName("to_balance") val toBalance: Long = 0,
    @SerialName("to_merchant") val toMerchant: String = "",
    val amount: Long = 0,
)

@Serializable
data class PeriodInfo(val label: String, val start: String, val end: String)

@Serializable
data class Totals(
    /** 이 기간 한 달 예산: 한 달 전체 예산을 정했으면 그 값, 아니면 맨 위 분류 예산 합계 */
    val budget: Long = 0,
    /** 맨 위 분류 예산 합계(옛 서버는 안 준다) */
    @SerialName("category_budget") val categoryBudget: Long = 0,
    /** 정한 한 달 전체 예산(안 정했으면 null) */
    @SerialName("own_budget") val ownBudget: Long? = null,
    /** 고정으로 빠지는 돈 합계(주식 적립 등). remaining = budget - fixed - spent + settled */
    val fixed: Long = 0,
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
    /** false면 "예산 없음"(자기나 위 폴더): 한 달 예산 합계에 안 든다. true이고 예산 0이면 "자유" */
    @SerialName("in_budget") val inBudget: Boolean = true,
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
    /** false면 "예산 없음": 한 달 예산(쓸 수 있는 돈)에서 깎지 않는다. true이고 예산 0이면 "자유" */
    @SerialName("in_budget") val inBudget: Boolean = true,
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
    /** null이면 안 바꾼다(빈 글자면 지운다). 서버 pipeline.Edit과 같다 */
    val memo: String? = null,
    val merchant: String? = null,
    val amount: Long? = null,
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
    /** false면 "예산 없음"(한 달 예산에서 안 깎임). null이면 그대로 */
    @SerialName("in_budget") val inBudget: Boolean? = null,
)

/** 고정으로 빠지는 돈 한 줄(주식 적립 등): 매달 이번 달 쓸 수 있는 돈에서 미리 뺀다 */
@Serializable
data class FixedItem(val id: Long, val name: String, val amount: Long)

@Serializable
data class FixedList(val items: List<FixedItem> = emptyList())

@Serializable
data class FixedInput(val name: String? = null, val amount: Long? = null)

/** PUT /v1/budgets/total 본문. amount가 null이면 분류 예산 합계로 */
@Serializable
data class TotalBudgetInput(val month: String, val amount: Long?)

@Serializable
data class TotalsReply(val totals: Totals)

/** POST /v1/categories/{id}/move 본문. parentId 0 = 맨 위, before·after 둘 다 없으면 맨 끝 */
@Serializable
data class CategoryMove(
    @SerialName("parent_id") val parentId: Long,
    val before: Long? = null,
    val after: Long? = null,
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
