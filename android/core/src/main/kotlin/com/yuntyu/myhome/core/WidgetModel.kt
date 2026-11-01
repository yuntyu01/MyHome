package com.yuntyu.myhome.core

import com.yuntyu.myhome.core.api.CategoryBudget
import com.yuntyu.myhome.core.api.Summary
import com.yuntyu.myhome.core.api.TransactionDto

/** 위젯 크기별 레이아웃. 작은 크기는 총 남은 돈과 상위 분류 2~3개만 보여준다. */
enum class WidgetLayout(val categoryCount: Int, val recentCount: Int) {
    SMALL(2, 0),
    MEDIUM(3, 3),
    LARGE(5, 4),
    ;

    companion object {
        /** 위젯 크기(dp)로 레이아웃을 고른다. */
        fun forSize(widthDp: Float, heightDp: Float): WidgetLayout = when {
            widthDp < 200f || heightDp < 110f -> SMALL
            heightDp < 250f -> MEDIUM
            else -> LARGE
        }
    }
}

/** 남은 돈 상태: 위젯·앱 색 표시용 */
enum class BudgetState { OK, WARN, OVER, NO_BUDGET }

object WidgetModel {
    fun stateOf(c: CategoryBudget): BudgetState = when {
        c.budget <= 0 -> BudgetState.NO_BUDGET
        c.remaining < 0 -> BudgetState.OVER
        c.ratio >= 0.8 -> BudgetState.WARN
        else -> BudgetState.OK
    }

    /**
     * 위젯에 보여줄 분류: 예산이 있는 분류 중 사용률이 높은 순(가장 신경 써야 할 것부터).
     * 사용률이 같으면 지출이 큰 순.
     */
    fun topCategories(s: Summary, n: Int): List<CategoryBudget> =
        s.categories.filter { it.budget > 0 }
            .sortedWith(compareByDescending<CategoryBudget> { it.ratio }.thenByDescending { it.spent })
            .take(n)

    /** 최근 결제: 나간 돈(지출·송금 등) 위주, 확인 필요 건도 포함. */
    fun recentPayments(s: Summary, n: Int): List<TransactionDto> =
        s.recent.filter { it.flow == "out" || it.status == "needs_review" }.take(n)

    /** "2026-11-02T02:20:06+09:00" → "11/02 02:20" (서버가 KST로 준다) */
    fun shortTime(iso: String): String {
        val m = Regex("""\d{4}-(\d{2})-(\d{2})T(\d{2}):(\d{2})""").find(iso) ?: return ""
        val (mo, d, h, mi) = m.destructured
        return "$mo/$d $h:$mi"
    }
}
