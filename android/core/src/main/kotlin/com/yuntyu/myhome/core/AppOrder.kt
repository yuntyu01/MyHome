package com.yuntyu.myhome.core

/**
 * 알림 읽을 앱 고르기 화면의 순서(사용자 요청, 2026-11-15 "최근 알림 앱, 은행 앱, 배달 앱 위주로 위에").
 * 위: 최근 30일에 알림이 온 앱(최근 것부터) → 은행·카드·페이 → 배달 → 쇼핑 → 이미 고른 앱. 아래: 나머지 이름순.
 * 이름에 든 낱말로만 가린다(패키지명은 코드에 박지 않는다: 앱 업데이트·지역에 따라 달라서). 순서만 바꾸고 고르는 건 사용자다.
 */
object AppOrder {
    enum class Group(val words: List<String>) {
        PAY(listOf("카드", "페이", "pay", "뱅크", "bank", "은행", "토스", "toss", "국민", "kb", "신한", "하나", "우리", "농협", "nh",
            "기업", "ibk", "네이버", "카카오", "새마을", "우체국", "케이뱅크", "카카오뱅크", "월렛", "wallet", "증권", "머니", "money", "결제", "티머니")),
        DELIVERY(listOf("배달", "배민", "요기요", "이츠", "eats", "땡겨요", "baemin", "yogiyo", "위메프오")),
        SHOP(listOf("쿠팡", "coupang", "11번가", "g마켓", "gmarket", "옥션", "무신사", "컬리", "kurly", "ssg", "올리브영", "당근", "번개장터",
            "지그재그", "에이블리", "오늘의집", "알리", "테무", "temu")),
    }

    /** 이름으로 본 앱 갈래(없으면 null). 배달 앱 이름에 "페이"가 들어도 배달로 본다 */
    fun group(label: String): Group? {
        val l = label.lowercase()
        return listOf(Group.DELIVERY, Group.PAY, Group.SHOP).firstOrNull { g -> g.words.any { l.contains(it) } }
    }

    const val RECENT_MS = 30L * 24 * 60 * 60 * 1000

    data class App(val pkg: String, val label: String, val lastNotified: Long = 0)

    /** (위에 둘 앱, 나머지) 순서대로 */
    fun split(apps: List<App>, pinned: Set<String>, now: Long): Pair<List<App>, List<App>> {
        fun recent(a: App) = a.lastNotified > 0 && now - a.lastNotified <= RECENT_MS
        val (top, rest) = apps.partition { recent(it) || group(it.label) != null || it.pkg in pinned }
        val ordered = top.sortedWith(
            compareBy<App>({ if (recent(it)) 0 else 1 }, { if (recent(it)) -it.lastNotified else 0L }, { group(it.label)?.ordinal ?: Group.entries.size }, { it.label.lowercase() }),
        )
        return ordered to rest.sortedBy { it.label.lowercase() }
    }
}
