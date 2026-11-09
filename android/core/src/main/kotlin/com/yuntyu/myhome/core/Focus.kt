package com.yuntyu.myhome.core

import java.time.Instant
import java.time.LocalDate
import java.time.ZoneId

/**
 * 집중 타이머(열품타처럼 과목마다 순공 시간을 재고, 공부 장소에 머문 시간과 견준다)의 계산.
 * 화면·저장과 떨어진 순수 함수라 여기서 시험한다.
 *
 * 하루는 05:00 KST에 바뀐다(서버 focus.DayStartHour와 같다). 새벽 2시까지 공부해도 전날 순공으로 센다.
 */
object FocusDay {
    const val DAY_START_HOUR = 5
    val KST: ZoneId = ZoneId.of("Asia/Seoul")

    /** 그 시각이 속한 공부 날 */
    fun dayOf(ms: Long): LocalDate =
        Instant.ofEpochMilli(ms).atZone(KST).minusHours(DAY_START_HOUR.toLong()).toLocalDate()

    /** 공부 날의 시작(그날 05:00 KST) */
    fun startOf(day: LocalDate): Long = day.atTime(DAY_START_HOUR, 0).atZone(KST).toInstant().toEpochMilli()

    /** 공부 날의 끝(다음 날 05:00 KST, 포함 안 함) */
    fun endOf(day: LocalDate): Long = startOf(day.plusDays(1))

    /** [startMs, endMs) 중 그 공부 날 안에 든 길이(ms) */
    fun overlap(startMs: Long, endMs: Long, day: LocalDate): Long {
        val s = maxOf(startMs, startOf(day))
        val e = minOf(endMs, endOf(day))
        return if (e > s) e - s else 0
    }

    /** [startMs, endMs)를 공부 날마다 자른 조각(날, 길이 ms). 05:00을 넘는 기록은 두 날로 나뉜다 */
    fun split(startMs: Long, endMs: Long): List<Pair<LocalDate, Long>> {
        if (endMs <= startMs) return emptyList()
        val out = mutableListOf<Pair<LocalDate, Long>>()
        var s = startMs
        while (s < endMs) {
            val d = dayOf(s)
            val e = minOf(endMs, endOf(d))
            out += d to (e - s)
            s = e
        }
        return out
    }
}

/** 한 구간. key는 과목 id(순공)나 장소 이름(머문 시간) */
data class FocusSpan(val key: String, val startMs: Long, val endMs: Long)

object FocusStats {
    /** 그 날의 과목별 순공(ms). 진행 중인 타이머는 지금까지로 넣어서 부른다 */
    fun bySubject(sessions: List<FocusSpan>, day: LocalDate): Map<String, Long> {
        val out = LinkedHashMap<String, Long>()
        for (s in sessions) {
            val ms = FocusDay.overlap(s.startMs, s.endMs, day)
            if (ms > 0) out[s.key] = (out[s.key] ?: 0L) + ms
        }
        return out
    }

    /** 그 날의 순공 합계(ms). 타이머는 한 번에 하나만 돌지만, 겹쳐 들어와도 두 번 세지 않게 합집합으로 잰다 */
    fun focusTotal(sessions: List<FocusSpan>, day: LocalDate): Long = unionWithin(sessions, day)

    /** 그 날 공부 장소에 머문 시간(ms). 두 장소가 겹치는 시간은 한 번만 센다 */
    fun stayTotal(stays: List<FocusSpan>, day: LocalDate): Long = unionWithin(stays, day)

    /** 집중률: 머문 시간 중 타이머를 켠 비율(%, 반올림). 머문 시간이 없으면 null. 장소 밖에서 잰 시간도 있어서 100을 넘을 수 있다 */
    fun ratioPercent(focusMs: Long, stayMs: Long): Int? =
        if (stayMs <= 0) null else ((focusMs * 100 + stayMs / 2) / stayMs).toInt().coerceAtMost(999)

    private fun unionWithin(spans: List<FocusSpan>, day: LocalDate): Long {
        val from = FocusDay.startOf(day)
        val to = FocusDay.endOf(day)
        val clipped = spans.mapNotNull { s ->
            val a = maxOf(s.startMs, from)
            val b = minOf(s.endMs, to)
            if (b > a) a to b else null
        }.sortedBy { it.first }
        var total = 0L
        var curStart = -1L
        var curEnd = -1L
        for ((a, b) in clipped) {
            if (curEnd < 0 || a > curEnd) {
                if (curEnd >= 0) total += curEnd - curStart
                curStart = a
                curEnd = b
            } else if (b > curEnd) {
                curEnd = b
            }
        }
        if (curEnd >= 0) total += curEnd - curStart
        return total
    }
}

/**
 * 공부 장소에 머문 기록 규칙(지오펜스 들어옴·나감). 위치가 흔들려 들락날락하는 것을 거른다.
 * - 3분보다 짧게 머문 것은 버린다(지나가기만 한 것)
 * - 나간 지 5분 안에 같은 곳에 다시 들어오면 앞 기록을 이어 간다(흔들림)
 * - 16시간보다 긴 것은 나감을 놓친 것으로 보고 버린다
 */
object StayRules {
    const val MIN_STAY_MS = 3 * 60_000L
    const val MERGE_GAP_MS = 5 * 60_000L
    const val MAX_STAY_MS = 16 * 3_600_000L

    /** 들어올 때: 같은 장소의 마지막 기록이 [MERGE_GAP_MS] 안에 끝났으면 그것을 이어 간다 */
    fun resumes(lastExitMs: Long?, enterMs: Long): Boolean =
        lastExitMs != null && enterMs >= lastExitMs && enterMs - lastExitMs <= MERGE_GAP_MS

    /** 나갈 때: 남길 기록인가 */
    fun keep(enteredMs: Long, exitedMs: Long): Boolean {
        val d = exitedMs - enteredMs
        return d >= MIN_STAY_MS && d <= MAX_STAY_MS
    }
}

/** 과목 이름·색 */
object FocusSubjects {
    const val MAX_NAME = 40

    /** 과목 색(열품타처럼 한눈에 갈리게 채도 있는 색). 새 과목은 아직 안 쓴 색부터 */
    val PALETTE = listOf("#3F51B5", "#F57C2A", "#9C27B0", "#5E35B1", "#2196F3", "#E91E63", "#26A69A", "#8D6E63")

    fun nextColor(used: Collection<String>): String =
        PALETTE.firstOrNull { c -> used.none { it.equals(c, ignoreCase = true) } } ?: PALETTE[used.size % PALETTE.size]

    /** 앞뒤 공백을 지운 이름. 비었거나 너무 길면 null */
    fun cleanName(raw: String): String? {
        val t = raw.trim().replace(Regex("\\s+"), " ")
        return if (t.isEmpty() || t.length > MAX_NAME) null else t
    }

    private val HEX = Regex("^#[0-9A-Fa-f]{6}$")
    fun validColor(c: String): Boolean = HEX.matches(c)
}

/** "1:08:05"처럼 시:분:초(열품타와 같은 모양). 음수는 0 */
fun formatHms(ms: Long): String {
    val total = (ms.coerceAtLeast(0) / 1000)
    val h = total / 3600
    val m = (total % 3600) / 60
    val s = total % 60
    return "%d:%02d:%02d".format(h, m, s)
}

/** "3시간 20분", "45분", "0분" */
fun formatHm(ms: Long): String {
    val min = ms.coerceAtLeast(0) / 60_000
    val h = min / 60
    val m = min % 60
    return when {
        h > 0 && m > 0 -> "${h}시간 ${m}분"
        h > 0 -> "${h}시간"
        else -> "${m}분"
    }
}
