package com.yuntyu.myhome.core

import com.yuntyu.myhome.core.NoticePolicy.Held
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class NoticePolicyTest {
    @Test fun dismissOnlyKnown() {
        assertTrue(NoticePolicy.dismissNow("dismiss"))
        assertTrue("예전 서버(notice 없음)는 예전처럼 지움", NoticePolicy.dismissNow(null))
        assertTrue(NoticePolicy.dismissNow(""))
        assertFalse(NoticePolicy.dismissNow("wait"))
        assertFalse(NoticePolicy.dismissNow("keep"))
    }

    @Test fun recheckSoonOnlyWhileWaitingForAI() {
        val now = 10_000_000L
        val fresh = now - 60_000
        val old = now - NoticePolicy.FAST_WINDOW_MS - 1
        assertTrue(NoticePolicy.recheckSoon(listOf(Held(fresh, "wait")), now))
        assertFalse("남김은 주기 작업 때만", NoticePolicy.recheckSoon(listOf(Held(fresh, "keep")), now))
        assertFalse("AI가 오래 막히면 주기 작업으로", NoticePolicy.recheckSoon(listOf(Held(old, "wait")), now))
        assertFalse(NoticePolicy.recheckSoon(emptyList(), now))
    }

    @Test fun forgetAfterThreeDays() {
        val now = 1_000_000_000L
        assertFalse(NoticePolicy.forget(now - 60_000, now))
        assertTrue(NoticePolicy.forget(now - NoticePolicy.FORGET_AFTER_MS - 1, now))
    }
}
