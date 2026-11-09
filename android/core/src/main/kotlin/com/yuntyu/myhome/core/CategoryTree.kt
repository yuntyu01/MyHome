package com.yuntyu.myhome.core

import com.yuntyu.myhome.core.api.Category

/**
 * 분류 폴더 구조(최대 3단계). 서버 cattree와 같은 규칙이다: 경로 이름, 단계, 옮길 수 있는 폴더, 예산 합계.
 * 부모가 목록에 없거나 순환이면(있으면 안 되지만) 맨 위로 본다. docs/DECISIONS.md 「분류 폴더」
 */
class CategoryTree(cats: List<Category>) {
    val byId: Map<Long, Category> = cats.associateBy { it.id }
    private val kids: Map<Long?, List<Category>>

    init {
        kids = cats.groupBy { parentOf(it) }
            .mapValues { (_, v) -> v.sortedWith(compareBy<Category> { it.sort }.thenBy { it.id }) }
    }

    private fun parentOf(c: Category): Long? {
        val p = c.parentId ?: return null
        if (p !in byId) return null
        var cur: Long? = p
        var steps = 0
        while (cur != null) {
            if (cur == c.id || steps > MAX_DEPTH * 4) return null
            cur = byId[cur]?.parentId?.takeIf { it in byId }
            steps++
        }
        return p
    }

    fun parent(id: Long): Long? = byId[id]?.let { parentOf(it) }

    /** 바로 안의 분류(null이면 맨 위) */
    fun children(parent: Long?): List<Category> = kids[parent].orEmpty()

    /** 단계(맨 위 = 0) */
    fun depth(id: Long): Int = ancestors(id).size

    /** 아래로 몇 단계가 더 있는지(없으면 0) */
    fun height(id: Long): Int = children(id).maxOfOrNull { height(it.id) + 1 } ?: 0

    /** 위 폴더들(가까운 것부터) */
    fun ancestors(id: Long): List<Long> {
        val out = mutableListOf<Long>()
        var p = parent(id)
        while (p != null) {
            out += p
            p = parent(p)
        }
        return out
    }

    fun descendants(id: Long): List<Long> = children(id).flatMap { listOf(it.id) + descendants(it.id) }

    /** "생활비 › 식비 › 배달" */
    fun label(id: Long): String {
        val c = byId[id] ?: return ""
        return (ancestors(id).reversed().mapNotNull { byId[it]?.name } + c.name).joinToString(SEP)
    }

    /** 위에서 아래로 펼친 순서와 단계. keep이 false인 분류는 그 아래까지 뺀다. */
    fun preorder(keep: (Category) -> Boolean = { true }): List<Pair<Category, Int>> {
        val out = mutableListOf<Pair<Category, Int>>()
        fun walk(parent: Long?, d: Int) {
            for (c in children(parent)) {
                if (!keep(c)) continue
                out += c to d
                walk(c.id, d + 1)
            }
        }
        walk(null, 0)
        return out
    }

    /**
     * 분류 id(새 분류면 null, 종류 kind)를 parent 안(null = 맨 위)에 둘 수 있는지. 안 되면 이유, 되면 null.
     * 서버가 같은 검사를 한 번 더 한다.
     */
    fun placeProblem(id: Long?, kind: String, parent: Long?): String? {
        val c = id?.let { byId[it] }
        if (c?.code != null && parent != null) return "이 분류는 폴더에 넣을 수 없어요"
        if (parent == null) return null
        val p = byId[parent] ?: return "그 폴더가 없어요"
        return when {
            p.code != null -> "수입·정산·내 계좌 이체·투자·저축 안에는 넣을 수 없어요"
            p.kind != kind -> "쓰는 돈과 들어오는 돈은 섞을 수 없어요"
            p.archived -> "지운 폴더에는 넣을 수 없어요"
            id != null && (parent == id || id in ancestors(parent)) -> "자기 안으로는 옮길 수 없어요"
            depth(parent) + 1 + (id?.let { height(it) } ?: 0) >= MAX_DEPTH -> "폴더는 3단계까지예요"
            else -> null
        }
    }

    /** 옮기거나 새로 만들 때 고를 수 있는 폴더(경로 순서). 맨 위는 따로 null로 고른다. */
    fun parentChoices(id: Long?, kind: String): List<Category> =
        preorder { !it.archived }.map { it.first }.filter { placeProblem(id, kind, it.id) == null }

    /**
     * 실제로 쓰는 예산: 직접 정했으면(amount > 0) 그 값, 아니면 안 분류 예산의 합. 서버 요약과 같은 계산이다.
     */
    fun effectiveBudget(own: Map<Long, Long>, id: Long): Long {
        val mine = own[id] ?: 0L
        if (mine > 0) return mine
        return children(id).sumOf { effectiveBudget(own, it.id) }
    }

    /** 한 달 총예산: 맨 위 지출 분류의 실제 예산 합(숨긴 분류 제외) */
    fun totalBudget(own: Map<Long, Long>): Long =
        children(null).filter { it.kind == "expense" && !it.archived }.sumOf { effectiveBudget(own, it.id) }

    companion object {
        const val MAX_DEPTH = 3
        const val SEP = " › "
        const val MAX_MEMO = 300
    }
}
