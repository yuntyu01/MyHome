package com.yuntyu.myhome.core

import java.time.LocalDate
import java.time.LocalDateTime
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class FocusTest {
    private fun kst(s: String): Long = LocalDateTime.parse(s).atZone(FocusDay.KST).toInstant().toEpochMilli()
    private val h = 3_600_000L
    private val min = 60_000L

    @Test
    fun dayStartsAtFiveKst() {
        assertEquals(LocalDate.parse("2026-11-08"), FocusDay.dayOf(kst("2026-11-09T04:59:59")))
        assertEquals(LocalDate.parse("2026-11-09"), FocusDay.dayOf(kst("2026-11-09T05:00:00")))
        assertEquals(LocalDate.parse("2026-11-09"), FocusDay.dayOf(kst("2026-11-09T23:59:00")))
        assertEquals(LocalDate.parse("2026-11-09"), FocusDay.dayOf(kst("2026-11-10T02:00:00")))
        assertEquals(kst("2026-11-09T05:00:00"), FocusDay.startOf(LocalDate.parse("2026-11-09")))
        assertEquals(kst("2026-11-10T05:00:00"), FocusDay.endOf(LocalDate.parse("2026-11-09")))
    }

    @Test
    fun splitAcrossFiveButNotMidnight() {
        // 자정을 넘어도 같은 공부 날
        assertEquals(
            listOf(LocalDate.parse("2026-11-09") to 2 * h),
            FocusDay.split(kst("2026-11-09T23:00:00"), kst("2026-11-10T01:00:00")),
        )
        // 05:00을 넘으면 두 날로 나뉜다
        assertEquals(
            listOf(LocalDate.parse("2026-11-08") to h, LocalDate.parse("2026-11-09") to h),
            FocusDay.split(kst("2026-11-09T04:00:00"), kst("2026-11-09T06:00:00")),
        )
        // 경계에서 시작하면 그날 하나
        assertEquals(
            listOf(LocalDate.parse("2026-11-09") to h),
            FocusDay.split(kst("2026-11-09T05:00:00"), kst("2026-11-09T06:00:00")),
        )
        // 여러 날: 8일 새벽 1시간, 9일·10일 하루씩, 11일 1시간
        assertEquals(
            listOf(1L, 24L, 24L, 1L),
            FocusDay.split(kst("2026-11-09T04:00:00"), kst("2026-11-11T06:00:00")).map { it.second / h },
        )
        assertEquals(emptyList<Pair<LocalDate, Long>>(), FocusDay.split(kst("2026-11-09T06:00:00"), kst("2026-11-09T06:00:00")))
    }

    @Test
    fun totalsPerSubjectAndDay() {
        val day = LocalDate.parse("2026-11-09")
        val sessions = listOf(
            FocusSpan("코테", kst("2026-11-09T09:00:00"), kst("2026-11-09T10:30:00")),
            FocusSpan("CS", kst("2026-11-09T11:00:00"), kst("2026-11-09T11:20:00")),
            FocusSpan("코테", kst("2026-11-09T14:00:00"), kst("2026-11-09T14:30:00")),
            // 전날 밤 04시~06시: 오늘 몫은 1시간
            FocusSpan("CS", kst("2026-11-09T04:00:00"), kst("2026-11-09T06:00:00")),
            // 다른 날
            FocusSpan("코테", kst("2026-11-10T06:00:00"), kst("2026-11-10T07:00:00")),
        )
        val by = FocusStats.bySubject(sessions, day)
        assertEquals(2 * h, by["코테"])
        assertEquals(h + 20 * min, by["CS"])
        assertEquals(3 * h + 20 * min, FocusStats.focusTotal(sessions, day))
    }

    @Test
    fun stayTotalCountsOverlapOnce() {
        val day = LocalDate.parse("2026-11-09")
        val stays = listOf(
            FocusSpan("도서관", kst("2026-11-09T09:00:00"), kst("2026-11-09T12:00:00")),
            FocusSpan("카페", kst("2026-11-09T11:00:00"), kst("2026-11-09T13:00:00")),
            FocusSpan("도서관", kst("2026-11-09T15:00:00"), kst("2026-11-09T16:00:00")),
        )
        assertEquals(5 * h, FocusStats.stayTotal(stays, day))
        assertEquals(0L, FocusStats.stayTotal(emptyList(), day))
    }

    @Test
    fun ratio() {
        assertEquals(67, FocusStats.ratioPercent(2 * h, 3 * h))
        assertNull(FocusStats.ratioPercent(h, 0))
        assertEquals(150, FocusStats.ratioPercent(3 * h, 2 * h))
    }

    @Test
    fun stayRules() {
        val t = kst("2026-11-09T09:00:00")
        assertFalse(StayRules.keep(t, t + 2 * min))
        assertTrue(StayRules.keep(t, t + 3 * min))
        assertFalse(StayRules.keep(t, t + 17 * h))
        assertTrue(StayRules.resumes(t, t + 4 * min))
        assertFalse(StayRules.resumes(t, t + 6 * min))
        assertFalse(StayRules.resumes(null, t))
        assertFalse(StayRules.resumes(t, t - min))
    }

    @Test
    fun subjectNamesAndColors() {
        assertEquals("코테 준비", FocusSubjects.cleanName("  코테   준비 "))
        assertNull(FocusSubjects.cleanName("   "))
        assertNull(FocusSubjects.cleanName("가".repeat(41)))
        assertEquals(FocusSubjects.PALETTE[1], FocusSubjects.nextColor(listOf(FocusSubjects.PALETTE[0].lowercase())))
        assertTrue(FocusSubjects.validColor("#3f51b5"))
        assertFalse(FocusSubjects.validColor("3F51B5"))
    }

    @Test
    fun formats() {
        assertEquals("0:00:00", formatHms(0))
        assertEquals("1:08:05", formatHms(h + 8 * min + 5_000))
        assertEquals("12:00:00", formatHms(12 * h))
        assertEquals("0:00:00", formatHms(-5))
        assertEquals("3시간 20분", formatHm(3 * h + 20 * min))
        assertEquals("2시간", formatHm(2 * h))
        assertEquals("45분", formatHm(45 * min + 30_000))
    }
}
