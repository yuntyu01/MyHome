package com.yuntyu.myhome.core

import com.yuntyu.myhome.core.AppOrder.App
import org.junit.Assert.assertEquals
import org.junit.Test

class AppOrderTest {
    private val now = 1_800_000_000_000L
    private val day = 24 * 60 * 60 * 1000L

    // 최근 알림 앱(최근 것부터) → 은행·카드·페이 → 배달 → 쇼핑 → 고른 앱, 나머지는 이름순
    @Test fun recentThenBankThenDeliveryThenShop() {
        val apps = listOf(
            App("a.game", "게임"), App("a.memo", "메모"), App("a.cp", "쿠팡"), App("a.bm", "배달의민족"),
            App("a.kb", "KB Pay"), App("a.ts", "토스", now - 2 * day), App("a.msg", "메시지", now - day),
            App("a.old", "옛날앱", now - 40 * day), App("a.sel", "고른앱"),
        )
        val (top, rest) = AppOrder.split(apps, pinned = setOf("a.sel"), now = now)
        assertEquals(listOf("메시지", "토스", "KB Pay", "배달의민족", "쿠팡", "고른앱"), top.map { it.label })
        assertEquals(listOf("게임", "메모", "옛날앱"), rest.map { it.label })
    }

    @Test fun groups() {
        assertEquals(AppOrder.Group.DELIVERY, AppOrder.group("배달의민족"))
        assertEquals(AppOrder.Group.DELIVERY, AppOrder.group("쿠팡이츠")) // 쿠팡(쇼핑)보다 배달이 먼저
        assertEquals(AppOrder.Group.PAY, AppOrder.group("신한 SOL뱅크"))
        assertEquals(AppOrder.Group.SHOP, AppOrder.group("무신사"))
        assertEquals(null, AppOrder.group("카메라"))
    }
}
