package com.yuntyu.myhome.core

import com.yuntyu.myhome.core.api.CategoryBudget
import com.yuntyu.myhome.core.api.Summary
import com.yuntyu.myhome.core.api.TransactionDto

/** 위젯 크기별 레이아웃. 작은 크기는 총 남은 돈과 상위 분류 2~3개만 보여준다. */
enum class WidgetLayout(val categoryCount: Int, val recentCount: Int) {
    SMALL(2, 0),
    MEDIUM(3, 1), // 세로 2칸(기본 크기)에 잘리지 않고 들어가는 양. 글자를 키우며 최근 결제 2 → 1(2026-11-17)
    LARGE(5, 3),
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

/** 앱 홈의 분류: 바로 보이는 것과 접어 둔 것. */
data class HomeCategories(val top: List<CategoryBudget>, val rest: List<CategoryBudget>)

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
     * 위젯에 보여줄 분류: 예산이 있는 맨 위 분류(폴더는 안의 합계) 중 사용률이 높은 순(가장 신경 써야 할 것부터).
     * 사용률이 같으면 지출이 큰 순.
     */
    fun topCategories(s: Summary, n: Int): List<CategoryBudget> =
        s.categories.filter { it.parentId == null && it.budget > 0 }
            .sortedWith(compareByDescending<CategoryBudget> { it.ratio }.thenByDescending { it.spent })
            .take(n)

    /**
     * 앱 홈에 보여줄 맨 위 분류(폴더는 안의 합계, 안은 [childrenOf]로 펼친다). 웹 홈과 같은 규칙이다: 예산을 넘긴 분류를 많이 넘긴 순으로 맨 위에, 그다음 사용률 높은 순.
     * 80% 이상 쓴 분류는 항상 보이고, 아직 안 쓴 분류는 항상 접는다. 예산도 지출도 없는 분류는 뺀다.
     */
    fun homeCategories(s: Summary, topCount: Int = 5): HomeCategories {
        val (used, unused) = s.categories
            .filter { it.parentId == null }
            .filter { it.budget > 0 || it.spent != 0L || it.settled != 0L }
            .partition { it.spent != 0L || it.settled != 0L }
        // 넘은 것(많이 넘은 순) → 한도 있는 것(많이 쓴 비율 순) → 자유(많이 쓴 순) → 예산 없음(많이 쓴 순).
        // 예산이 0이면 remaining이 음수(= 쓴 돈)라서 "넘음"으로 보면 안 된다
        fun over(c: CategoryBudget) = c.budget > 0 && c.remaining < 0
        fun rank(c: CategoryBudget) = when {
            over(c) -> 0
            !c.inBudget -> 3
            c.budget > 0 -> 1
            else -> 2
        }
        val sorted = used.sortedWith(
            compareBy<CategoryBudget> { rank(it) }
                .thenBy { if (over(it)) it.remaining else 0L }
                .thenByDescending { it.ratio }
                .thenByDescending { it.spent },
        )
        val (top, rest) = sorted.withIndex().partition { (i, c) -> i < topCount || over(c) || c.ratio >= 0.8 }
        return HomeCategories(top.map { it.value }, rest.map { it.value } + unused)
    }

    /** 폴더 바로 안의 분류(서버 순서 그대로). 예산도 지출도 없는 분류는 뺀다. */
    fun childrenOf(s: Summary, parentId: Long): List<CategoryBudget> =
        s.categories.filter { it.parentId == parentId && (it.budget > 0 || it.spent != 0L || it.settled != 0L) }

    /** 기간 라벨 "2026-10" → 올해면 "10월", 아니면 "2025년 10월". 형식이 다르면 그대로. */
    fun monthName(label: String, currentYear: Int = java.time.LocalDate.now().year): String {
        val m = Regex("""^(\d{4})-(\d{2})$""").find(label) ?: return label
        val (y, mo) = m.destructured
        return if (y.toInt() == currentYear) "${mo.toInt()}월" else "${y.toInt()}년 ${mo.toInt()}월"
    }

    /** 분류(폴더면 그 아래 전부)의 최근 결제 n개, 새것부터. 가계부에서 뺀 것은 빼고. 홈 분류 박스 미리보기 */
    fun recentIn(s: Summary, txs: List<TransactionDto>, id: Long, n: Int): List<TransactionDto> {
        val kids = s.categories.groupBy { it.parentId }
        val ids = mutableSetOf(id)
        fun walk(p: Long) { for (k in kids[p].orEmpty()) if (ids.add(k.id)) walk(k.id) }
        walk(id)
        return txs.filter { it.categoryId in ids && it.status != "excluded" }.sortedByDescending { it.occurredAt }.take(n)
    }

    /**
     * 위젯 줄 수(분류, 최근 결제). 고를 거래가 있으면 고르기 칸(가게 한 줄 + 동그라미 한 줄)이 들어갈 자리를 비운다.
     * 작은 위젯은 동그라미가 안 들어가서 고르기를 안 보인다([showPick]).
     */
    fun rows(layout: WidgetLayout, picking: Boolean): Pair<Int, Int> = when {
        !showPick(layout, picking) -> layout.categoryCount to layout.recentCount
        else -> 0 to 0 // 고르기 칸이 아래를 가린다(사용자 요청, 2026-11-17 "밑에 내용 가리고 세로로 선택")
    }

    /** 고르기 중에 남은 돈 머리를 보일지: 중간(세로 2칸)은 동그라미 줄이 들어가게 머리를 가린다 */
    fun headWhilePicking(layout: WidgetLayout): Boolean = layout == WidgetLayout.LARGE

    /** 고르기 칸의 분류 동그라미 수(맨 밑 "없음" 줄은 따로): 중간은 세로 2칸에 들어가게 3, 크게는 4 */
    fun pickChoiceCount(layout: WidgetLayout): Int = if (layout == WidgetLayout.LARGE) 4 else 3

    /**
     * 결제 시각 "2026-11-16T19:56:00+09:00"(서버가 KST로 준다) → 오늘이면 "오늘 19:56", 어제면 "어제 19:56", 그 전은 "11/14 19:56".
     * 형식이 다르면 "".
     */
    fun whenText(iso: String, today: java.time.LocalDate): String {
        val m = Regex("""^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2})""").find(iso) ?: return ""
        val (y, mo, d, h, mi) = m.destructured
        val date = runCatching { java.time.LocalDate.of(y.toInt(), mo.toInt(), d.toInt()) }.getOrNull() ?: return ""
        val day = when (date) {
            today -> "오늘"
            today.minusDays(1) -> "어제"
            else -> "${mo.toInt()}/${d.toInt()}"
        }
        return "$day $h:$mi"
    }

    fun showPick(layout: WidgetLayout, picking: Boolean): Boolean = picking && layout != WidgetLayout.SMALL

    /**
     * 기간에 남은 날(오늘 포함). end는 기간 마지막 날(포함, 서버 PeriodDTO). 기간 밖이거나 형식이 다르면 1
     * (나누기에 쓰므로 0이 되지 않게).
     */
    fun daysLeft(end: String, today: java.time.LocalDate): Int {
        val last = runCatching { java.time.LocalDate.parse(end) }.getOrNull() ?: return 1
        return (java.time.temporal.ChronoUnit.DAYS.between(today, last) + 1).toInt().coerceAtLeast(1)
    }

    /** 하루 쓸 수 있는 돈: 남은 돈을 남은 날로 나눔(원 단위 버림). 남은 돈이 없으면 0 */
    fun perDay(remaining: Long, days: Int): Long = if (remaining <= 0) 0 else remaining / days.coerceAtLeast(1)

    /** 쓴 돈(고정으로 빠지는 돈 포함, 정산은 뺌) = 예산 - 남은 돈. 쓴 돈 / 예산으로 보일 때 둘을 더하면 예산이 되게 */
    fun used(s: Summary): Long = s.totals.budget - s.totals.remaining

    /**
     * 위젯 고르기 줄 오른쪽: 그 분류의 이번 기간 "쓴 돈 / 예산"(예산이 없으면 쓴 돈만). 좁아서 1만 이상은 만 단위 소수 한 자리
     * (버림, 넘지 않았는데 예산과 같아 보이지 않게): "18.2만 / 30만", "3.2만 / 6만", 1만 아래는 그대로 "7,000".
     */
    fun usageText(c: CategoryBudget?): String {
        if (c == null) return ""
        val spent = c.spent - c.settled
        return if (c.budget > 0) "${man(spent)} / ${man(c.budget)}" else man(spent)
    }

    /** 1만 이상은 "18.2만"(소수 한 자리 버림, .0은 뺌), 아래는 "7,000" */
    private fun man(v: Long): String {
        val abs = kotlin.math.abs(v)
        if (abs < 10_000) return (if (v < 0) "-" else "") + Money.grouped(abs)
        val tenth = abs / 1_000 // 0.1만 단위
        val text = Money.grouped(tenth / 10) + (if (tenth % 10 != 0L) ".${tenth % 10}" else "") + "만"
        return (if (v < 0) "-" else "") + text
    }

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
