package com.yuntyu.myhome.core

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class ServerUrlTest {
    @Test fun addsHttpsAndTrimsSlash() {
        assertEquals("https://money.example.com", ServerUrl.normalize(" money.example.com/ "))
        assertEquals("https://money.example.com/base", ServerUrl.normalize("https://money.example.com/base/"))
    }

    @Test fun rejectsPlainHttpExceptLocalDev() {
        assertNull(ServerUrl.normalize("http://money.example.com"))
        assertEquals("http://10.0.2.2:8080", ServerUrl.normalize("http://10.0.2.2:8080"))
        assertEquals("http://192.168.0.5:8080", ServerUrl.normalize("http://192.168.0.5:8080/"))
    }

    @Test fun rejectsGarbage() {
        assertNull(ServerUrl.normalize(""))
        assertNull(ServerUrl.normalize("ftp://x"))
    }
}

class MoneyTest {
    @Test fun won() {
        assertEquals("0원", Money.won(0))
        assertEquals("1,000원", Money.won(1000))
        assertEquals("12,345원", Money.won(12345))
        assertEquals("1,234,567원", Money.won(1234567))
        assertEquals("-500원", Money.won(-500))
    }

    @Test fun compact() {
        assertEquals("9,800", Money.compact(9800))
        assertEquals("12만", Money.compact(123_456))
        assertEquals("1억 2,000만", Money.compact(120_000_000))
        assertEquals("-30만", Money.compact(-300_000))
    }
}

class CaptureRulesTest {
    private fun c(pkg: String, title: String = "합성 제목", ongoing: Boolean = false, summary: Boolean = false) =
        CaptureRules.Candidate(pkg, ongoing, summary, title, "1,000원", "")

    private val selected = setOf("test.card")

    @Test fun capturesOnlySelected() {
        assertTrue(CaptureRules.shouldCapture(c("test.card"), selected, "own.app"))
        assertFalse(CaptureRules.shouldCapture(c("test.other"), selected, "own.app"))
    }

    @Test fun skipsOngoingAndSummary() {
        assertFalse(CaptureRules.shouldCapture(c("test.card", ongoing = true), selected, "own.app"))
        assertFalse(CaptureRules.shouldCapture(c("test.card", summary = true), selected, "own.app"))
    }

    @Test fun ownTestNotificationOnly() {
        assertTrue(CaptureRules.shouldCapture(c("own.app", title = CaptureRules.TEST_TITLE_MARKER + " 결제"), selected, "own.app"))
        assertFalse(CaptureRules.shouldCapture(c("own.app", title = "다른 알림"), selected, "own.app"))
    }

    @Test fun skipsEmpty() {
        val empty = CaptureRules.Candidate("test.card", false, false, "", " ", "")
        assertFalse(CaptureRules.shouldCapture(empty, selected, "own.app"))
    }
}

class DedupeKeyTest {
    @Test fun matchesServerContentHash() {
        // 서버(Go) ingest.ContentHash와 같은 입력 → 같은 값이어야 한다.
        val key = DedupeKey.of("test.pkg", 1793000000000, "합성 제목", "1,000원", "")
        assertEquals("919513cf190f2e05d9e6cf021369c8eb8b0da9137b9cc9347d34c94476c78043", key)
    }

    @Test fun differsByTime() {
        val a = DedupeKey.of("p", 1, "t", "x", "")
        val b = DedupeKey.of("p", 2, "t", "x", "")
        assertFalse(a == b)
    }
}
