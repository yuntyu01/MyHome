package com.yuntyu.myhome.core

import com.yuntyu.myhome.core.api.TransactionDto

/** 거래 줄 앞 동그라미에 그릴 것 */
sealed interface TxMark {
    /** 아는 가게: 가게 색 동그라미에 짧은 글자(로고 그림은 쓰지 않는다) */
    data class Brand(val label: String, val bg: Long, val fg: Long) : TxMark

    /** 분류 그림 */
    data class Kind(val kind: MarkKind) : TxMark

    /** 그림이 없는 사용자 분류: 첫 글자와 이름으로 정한 색 */
    data class Letter(val letter: String, val bg: Long) : TxMark

    /** 분류를 아직 안 고른 결제 */
    data object Unknown : TxMark
}

enum class MarkKind(val bg: Long) {
    FOOD(0xFFFF8A3D), CAFE(0xFFA0704C), TRANSIT(0xFF3182F6), SHOPPING(0xFFF04452), LIVING(0xFF22B573),
    HOUSING(0xFF6C5CE7), SUBSCRIPTION(0xFF9B59D0), CULTURE(0xFF14B8A6), HEALTH(0xFFEF5350), EVENT(0xFFF5A623),
    ETC(0xFF6B7280), INCOME(0xFF3182F6), TRANSFER(0xFF64748B),
}

/**
 * 거래 줄 앞 표시 고르기(토스 거래 목록처럼, 2026-11-10 사용자 요청). 순서:
 * 아는 가게 이름 → 교통(티머니·지하철·택시…) → 들어온 돈·이체 → 분류 안 고름 → 분류 이름 → 그 밖.
 * 가게 로고 그림은 저작권이 있고, 받아 오려면 어느 가게를 썼는지 밖으로 나가서 가게 색과 글자만 쓴다.
 * docs/DECISIONS.md 「거래 줄 앞 표시」
 */
object TxMarks {
    private class B(val keys: List<String>, val label: String, val bg: Long, val fg: Long = WHITE)

    private const val WHITE = 0xFFFFFFFF

    // 앞에 있는 것이 먼저(쿠팡이츠 > 쿠팡, 이마트24 > 이마트). 키는 빈칸을 빼고 영문은 대문자로 비교한다
    private val brands = listOf(
        B(listOf("세븐일레븐", "7-ELEVEN", "SEVENELEVEN"), "7", WHITE, 0xFFEE7623),
        B(listOf("GS25"), "GS", 0xFF00A0E2),
        B(listOf("이마트24", "EMART24"), "24", 0xFFFFB300, 0xFF333333),
        B(listOf("이마트", "EMART"), "e", 0xFFFFB300, 0xFF333333),
        B(listOf("홈플러스"), "H", 0xFFE2231A),
        B(listOf("롯데마트"), "L", 0xFFE60012),
        B(listOf("코스트코", "COSTCO"), "C", 0xFF005DAA),
        B(listOf("스타벅스", "STARBUCKS"), "S", 0xFF00704A),
        B(listOf("메가엠지씨", "메가커피", "MGC커피", "MEGACOFFEE"), "MGC", 0xFF3B2314, 0xFFFFD200),
        B(listOf("투썸", "TWOSOME"), "투썸", 0xFFC8102E),
        B(listOf("이디야", "EDIYA"), "E", 0xFF0B2D71),
        B(listOf("빽다방"), "빽", 0xFFFFDD00, 0xFF1D4E9E),
        B(listOf("컴포즈"), "C", 0xFFFFD300, 0xFF222222),
        B(listOf("매머드"), "M", 0xFFD71920),
        B(listOf("폴바셋"), "P", 0xFF1C1C1C),
        B(listOf("할리스", "HOLLYS"), "H", 0xFFC8102E),
        B(listOf("맥도날드", "MCDONALD"), "M", 0xFFDA291C, 0xFFFFC72C),
        B(listOf("버거킹", "BURGERKING"), "BK", 0xFFF5EBDC, 0xFFD62300),
        B(listOf("롯데리아"), "L", 0xFFE30613),
        B(listOf("맘스터치"), "맘", 0xFFF5821F),
        B(listOf("서브웨이", "SUBWAY"), "S", 0xFF008938, 0xFFFFC20E),
        B(listOf("KFC"), "KFC", 0xFFE4002B),
        B(listOf("파리바게뜨", "파리바게트"), "P", 0xFF0A2D6E),
        B(listOf("뚜레쥬르"), "T", 0xFF00703C),
        B(listOf("배스킨라빈스", "BASKIN"), "BR", 0xFFFF2F87),
        B(listOf("던킨", "DUNKIN"), "D", 0xFFFF671F),
        B(listOf("쿠팡이츠", "COUPANGEATS"), "이츠", 0xFF00A6E5),
        B(listOf("쿠팡", "COUPANG"), "C", WHITE, 0xFFE5272C),
        B(listOf("배달의민족", "배민"), "배민", 0xFF2AC1BC),
        B(listOf("요기요"), "요", 0xFFFA0050),
        B(listOf("올리브영", "OLIVEYOUNG"), "O", 0xFF9BCB3C),
        B(listOf("다이소", "DAISO"), "D", 0xFFE60012),
        B(listOf("무신사", "MUSINSA"), "M", 0xFF000000),
        B(listOf("넷플릭스", "NETFLIX"), "N", 0xFF000000, 0xFFE50914),
        B(listOf("유튜브", "YOUTUBE"), "▶", 0xFFFF0000),
        B(listOf("스포티파이", "SPOTIFY"), "S", 0xFF1DB954, 0xFF111111),
        B(listOf("CGV"), "CGV", 0xFFE71A0F),
        B(listOf("메가박스", "MEGABOX"), "MB", 0xFF351F67),
        B(listOf("롯데시네마"), "LC", 0xFFED1C24),
        B(listOf("카카오페이"), "K", 0xFFFEE500, 0xFF191919),
        B(listOf("네이버페이", "NAVERPAY"), "N", 0xFF03C75A),
        B(listOf("토스페이", "토스뱅크"), "T", 0xFF3182F6),
    )

    private val transit = listOf("택시", "카카오T", "티머니", "지하철", "버스", "교통카드", "후불교통", "코레일", "KORAIL", "SRT", "캐시비", "EZL")

    // CU는 "CUP"·"CUBE" 같은 영문 안에서는 안 잡히게 앞뒤가 영문이 아닐 때만
    private val cu = Regex("""(^|[^A-Z])CU([^A-Z]|$)""")

    // 분류 이름 → 그림(앞에 있는 것이 먼저: '간식'에 '식'이 들어 있어서 카페가 먼저)
    private val kinds = listOf(
        listOf("카페", "간식", "커피") to MarkKind.CAFE,
        listOf("식", "배달", "외식", "밥") to MarkKind.FOOD,
        listOf("교통", "차량", "주유") to MarkKind.TRANSIT,
        listOf("쇼핑", "의류", "패션") to MarkKind.SHOPPING,
        listOf("주거", "통신", "관리비", "월세") to MarkKind.HOUSING,
        listOf("생활", "마트", "편의점") to MarkKind.LIVING,
        listOf("구독") to MarkKind.SUBSCRIPTION,
        listOf("문화", "여가", "취미", "여행") to MarkKind.CULTURE,
        listOf("의료", "건강", "병원", "약") to MarkKind.HEALTH,
        listOf("경조사", "선물") to MarkKind.EVENT,
        listOf("수입", "월급", "용돈", "정산") to MarkKind.INCOME,
        listOf("이체", "투자", "저축") to MarkKind.TRANSFER,
        listOf("기타") to MarkKind.ETC,
    )

    private val letterColors = listOf(0xFF5C7CFA, 0xFF20C997, 0xFFFF922B, 0xFFE64980, 0xFF845EF7, 0xFF339AF0, 0xFF94D82D, 0xFFFAB005)

    private fun norm(s: String) = s.replace(" ", "").uppercase()

    fun brandOf(merchant: String): TxMark.Brand? {
        val n = norm(merchant)
        if (n.isEmpty()) return null
        brands.firstOrNull { b -> b.keys.any { norm(it) in n } }?.let { return TxMark.Brand(it.label, it.bg, it.fg) }
        if (cu.containsMatchIn(n)) return TxMark.Brand("CU", 0xFF7B2C8E, 0xFFC4D600)
        return null
    }

    fun kindOfCategory(name: String): MarkKind? = kinds.firstOrNull { (keys, _) -> keys.any { it in name } }?.second

    fun of(t: TransactionDto): TxMark {
        brandOf(t.merchant)?.let { return it }
        val n = norm(t.merchant)
        if (transit.any { norm(it) in n }) return TxMark.Kind(MarkKind.TRANSIT)
        if (t.direction == "transfer") return TxMark.Kind(MarkKind.TRANSFER)
        if (t.status == "needs_review" && t.category.isBlank()) return TxMark.Unknown
        if (t.category.isNotBlank()) {
            kindOfCategory(t.category)?.let { return TxMark.Kind(it) }
            return TxMark.Letter(t.category.take(1), letterColors[Math.floorMod(t.category.hashCode(), letterColors.size)])
        }
        return TxMark.Kind(if (t.flow == "in") MarkKind.INCOME else MarkKind.ETC)
    }
}
