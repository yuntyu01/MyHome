package com.yuntyu.myhome.core

import com.yuntyu.myhome.core.api.Category

/** 끌어서 놓을 자리: parent(null = 맨 위) 안에서 before 바로 앞, after 바로 뒤, 둘 다 null이면 맨 끝 */
data class DropPlace(val parent: Long?, val before: Long? = null, val after: Long? = null)

/**
 * 분류 줄을 꾹 눌러 끌 때 놓을 자리(화면과 따로 시험할 수 있게 여기 둔다). 서버 api.MoveCategory와 같은 순서 규칙이다.
 * - 줄 가운데에 놓으면 그 분류 안 맨 끝(폴더가 된다), 위쪽 절반은 그 줄 앞, 아래쪽 절반은 그 줄 뒤(폴더면 안 맨 앞).
 * - 마지막 줄보다 아래면 맨 위 단계 맨 끝. 원래 자리(끄는 줄이 빠진 빈칸)나 규칙(3단계·종류)에 안 맞으면 놓을 곳 없음.
 * docs/DECISIONS.md 「분류 끌어서 옮기기」
 */
object CategoryDrag {
    /** 화면의 한 줄: 분류 id와 위·아래 끝(같은 좌표계) */
    data class Row(val id: Long, val top: Float, val bottom: Float)

    /** 놓을 곳과 표시: into면 그 줄을 칠하고, 아니면 lineY에 lineDepth 단계로 선을 긋는다 */
    data class Drop(val place: DropPlace, val into: Long? = null, val lineY: Float = 0f, val lineDepth: Int = 0)

    /**
     * rows: 같은 종류(쓰는 돈/들어오는 돈)의 보이는 줄을 위에서부터, 끄는 분류와 그 안은 뺀 것.
     * hole: 끄는 분류(와 그 안)가 원래 있던 칸. y: 손가락 위치.
     */
    fun target(tree: CategoryTree, dragId: Long, rows: List<Row>, hole: ClosedFloatingPointRange<Float>?, y: Float): Drop? {
        val kind = tree.byId[dragId]?.kind ?: return null
        if (rows.isEmpty() || (hole != null && y in hole)) return null
        fun ok(parent: Long?) = tree.placeProblem(dragId, kind, parent) == null
        fun before(r: Row): Drop? {
            val p = tree.parent(r.id)
            return if (ok(p)) Drop(DropPlace(p, before = r.id), lineY = r.top, lineDepth = tree.depth(r.id)) else null
        }
        fun after(i: Int): Drop? {
            val r = rows[i]
            val next = rows.getOrNull(i + 1)
            if (next != null && tree.parent(next.id) == r.id) {
                return if (ok(r.id)) Drop(DropPlace(r.id, before = next.id), lineY = r.bottom, lineDepth = tree.depth(next.id)) else null
            }
            val p = tree.parent(r.id)
            return if (ok(p)) Drop(DropPlace(p, after = r.id), lineY = r.bottom, lineDepth = tree.depth(r.id)) else null
        }
        if (y < rows.first().top) return before(rows.first())
        if (y >= rows.last().bottom) return Drop(DropPlace(null), lineY = rows.last().bottom, lineDepth = 0)
        val i = rows.indexOfFirst { y >= it.top && y < it.bottom }
        if (i < 0) return null
        val r = rows[i]
        val rel = (y - r.top) / (r.bottom - r.top).coerceAtLeast(1f)
        // 폴더는 가운데를 넓게(안에 넣기 쉽게), 그냥 분류는 좁게(순서 바꾸기 쉽게)
        val band = if (tree.children(r.id).any { !it.archived && it.id != dragId }) 0.2f else 0.3f
        if (rel >= band && rel <= 1 - band && ok(r.id)) return Drop(DropPlace(r.id), into = r.id)
        return if (rel < 0.5f) before(r) else after(i)
    }

    /** place 안의 새 순서(특수 분류 제외). 없는 기준이면 맨 끝 */
    fun order(tree: CategoryTree, id: Long, place: DropPlace): List<Long> {
        val sibs = tree.children(place.parent).filter { it.id != id && it.code == null }.map { it.id }.toMutableList()
        val anchor = place.before ?: place.after
        val i = anchor?.let { sibs.indexOf(it) } ?: -1
        val at = when {
            i < 0 -> sibs.size
            place.after != null -> i + 1
            else -> i
        }
        sibs.add(at, id)
        return sibs
    }

    /** 놓아도 바뀌는 게 없는지(같은 폴더, 같은 순서) */
    fun same(tree: CategoryTree, id: Long, place: DropPlace): Boolean =
        tree.parent(id) == place.parent &&
            order(tree, id, place) == tree.children(place.parent).filter { it.code == null }.map { it.id }

    /** 옮긴 뒤 목록(서버 답을 기다리지 않고 먼저 보여 주려고). 들어간 폴더 순서를 10, 20, 30…으로 다시 매긴다 */
    fun moved(cats: List<Category>, tree: CategoryTree, id: Long, place: DropPlace): List<Category> {
        val sort = order(tree, id, place).withIndex().associate { (i, c) -> c to (i + 1) * 10 }
        return cats.map { c -> sort[c.id]?.let { s -> c.copy(parentId = place.parent, sort = s) } ?: c }
    }
}
