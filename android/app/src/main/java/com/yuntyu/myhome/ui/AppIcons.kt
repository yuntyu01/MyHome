package com.yuntyu.myhome.ui

import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.SolidColor
import androidx.compose.ui.graphics.StrokeCap
import androidx.compose.ui.graphics.StrokeJoin
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.graphics.vector.PathBuilder
import androidx.compose.ui.graphics.vector.path
import androidx.compose.ui.unit.dp

/** material-icons-core에 없는 선 아이콘. 색은 Icon의 tint가 입힌다. */
private fun line(name: String, width: Float = 2f, block: PathBuilder.() -> Unit): ImageVector =
    ImageVector.Builder(name = name, defaultWidth = 24.dp, defaultHeight = 24.dp, viewportWidth = 24f, viewportHeight = 24f).apply {
        path(stroke = SolidColor(Color.Black), strokeLineWidth = width, strokeLineCap = StrokeCap.Round, strokeLineJoin = StrokeJoin.Round, pathBuilder = block)
    }.build()

/** 투자: 오르는 꺾은선(웹 하단바와 같음) */
val InvestIcon: ImageVector by lazy {
    line("Invest") {
        moveTo(4f, 17f); lineTo(9f, 12f); lineTo(13f, 16f); lineTo(20f, 8f)
        moveTo(15f, 8f); lineTo(20f, 8f); lineTo(20f, 13f)
    }
}

/** 건강: 맥박선 */
val HealthIcon: ImageVector by lazy {
    line("Health") {
        moveTo(2f, 12f); lineTo(6f, 12f); lineTo(9f, 4f); lineTo(15f, 20f); lineTo(18f, 12f); lineTo(22f, 12f)
    }
}

/** 카메라 */
val CameraIcon: ImageVector by lazy {
    line("Camera") {
        moveTo(3f, 8f); lineTo(7f, 8f); lineTo(9f, 5f); lineTo(15f, 5f); lineTo(17f, 8f); lineTo(21f, 8f)
        lineTo(21f, 19f); lineTo(3f, 19f); close()
        moveTo(15f, 13.5f)
        arcTo(3f, 3f, 0f, true, true, 9f, 13.5f)
        arcTo(3f, 3f, 0f, true, true, 15f, 13.5f)
    }
}

/** 홈: 지붕 있는 집 */
val HomeLineIcon: ImageVector by lazy {
    line("Home") {
        moveTo(3f, 11f); lineTo(12f, 4f); lineTo(21f, 11f); lineTo(21f, 20f); lineTo(15f, 20f); lineTo(15f, 14f)
        lineTo(9f, 14f); lineTo(9f, 20f); lineTo(3f, 20f); close()
    }
}

/** 거래: 점과 줄 세 개 */
val ListLineIcon: ImageVector by lazy {
    line("List") {
        moveTo(8f, 6f); lineTo(21f, 6f); moveTo(8f, 12f); lineTo(21f, 12f); moveTo(8f, 18f); lineTo(21f, 18f)
        moveTo(3.5f, 6f); lineTo(3.6f, 6f); moveTo(3.5f, 12f); lineTo(3.6f, 12f); moveTo(3.5f, 18f); lineTo(3.6f, 18f)
    }
}

/** 설정: 가운데 원과 바퀴살 */
val GearLineIcon: ImageVector by lazy {
    line("Gear") {
        moveTo(15f, 12f); arcTo(3f, 3f, 0f, true, true, 9f, 12f); arcTo(3f, 3f, 0f, true, true, 15f, 12f)
        moveTo(12f, 2.5f); lineTo(12f, 5f); moveTo(12f, 19f); lineTo(12f, 21.5f)
        moveTo(2.5f, 12f); lineTo(5f, 12f); moveTo(19f, 12f); lineTo(21.5f, 12f)
        moveTo(5.3f, 5.3f); lineTo(7.1f, 7.1f); moveTo(16.9f, 16.9f); lineTo(18.7f, 18.7f)
        moveTo(5.3f, 18.7f); lineTo(7.1f, 16.9f); moveTo(16.9f, 7.1f); lineTo(18.7f, 5.3f)
    }
}

val ChevronRightIcon: ImageVector by lazy { line("ChevronRight") { moveTo(9f, 6f); lineTo(15f, 12f); lineTo(9f, 18f) } }

val ChevronLeftIcon: ImageVector by lazy { line("ChevronLeft") { moveTo(15f, 6f); lineTo(9f, 12f); lineTo(15f, 18f) } }

/** 오른쪽 위 화살표(다른 화면으로 가기: 홈 "투자 ↗") */
val ArrowUpRightIcon: ImageVector by lazy { line("ArrowUpRight") { moveTo(7f, 17f); lineTo(17f, 7f); moveTo(9f, 7f); lineTo(17f, 7f); lineTo(17f, 15f) } }

/** 아래 꺾쇠(고르기 펼치기: 홈 "11월 ⌄") */
val ChevronDownIcon: ImageVector by lazy { line("ChevronDown") { moveTo(6f, 9f); lineTo(12f, 15f); lineTo(18f, 9f) } }

/** 앨범: 사진 틀 + 산 */
val GalleryIcon: ImageVector by lazy {
    line("Gallery") {
        moveTo(5f, 4f); lineTo(19f, 4f); lineTo(19f, 20f); lineTo(5f, 20f); close()
        moveTo(5f, 16f); lineTo(10f, 11f); lineTo(14f, 15f); lineTo(16f, 13f); lineTo(19f, 16f)
        moveTo(15.5f, 8.5f); lineTo(15.6f, 8.5f)
    }
}

/** 바코드: 모서리 괄호 + 세로줄 */
val BarcodeIcon: ImageVector by lazy {
    line("Barcode") {
        moveTo(3f, 7f); lineTo(3f, 4f); lineTo(6f, 4f); moveTo(18f, 4f); lineTo(21f, 4f); lineTo(21f, 7f)
        moveTo(21f, 17f); lineTo(21f, 20f); lineTo(18f, 20f); moveTo(6f, 20f); lineTo(3f, 20f); lineTo(3f, 17f)
        moveTo(7f, 8f); lineTo(7f, 16f); moveTo(10f, 8f); lineTo(10f, 16f); moveTo(13f, 8f); lineTo(13f, 16f); moveTo(17f, 8f); lineTo(17f, 16f)
    }
}

/** 직접 입력: 연필 */
val PencilIcon: ImageVector by lazy {
    line("Pencil") { moveTo(4f, 20f); lineTo(5f, 15.5f); lineTo(15.5f, 5f); lineTo(19f, 8.5f); lineTo(8.5f, 19f); close(); moveTo(13f, 7.5f); lineTo(16.5f, 11f) }
}

/** 손전등 켜짐: 번개 */
val FlashIcon: ImageVector by lazy { line("Flash") { moveTo(13f, 3f); lineTo(6f, 13f); lineTo(11f, 13f); lineTo(10f, 21f); lineTo(18f, 10f); lineTo(13f, 10f); close() } }

/** 손전등 꺼짐: 번개 + 빗금 */
val FlashOffIcon: ImageVector by lazy {
    line("FlashOff") {
        moveTo(13f, 3f); lineTo(6f, 13f); lineTo(11f, 13f); lineTo(10f, 21f); lineTo(18f, 10f); lineTo(13f, 10f); close()
        moveTo(4f, 4f); lineTo(20f, 20f)
    }
}

val PlusIcon: ImageVector by lazy { line("Plus") { moveTo(12f, 5f); lineTo(12f, 19f); moveTo(5f, 12f); lineTo(19f, 12f) } }

/** 닫기·지우기(×) */
val CloseIcon: ImageVector by lazy { line("Close") { moveTo(6.5f, 6.5f); lineTo(17.5f, 17.5f); moveTo(17.5f, 6.5f); lineTo(6.5f, 17.5f) } }

val SearchIcon: ImageVector by lazy {
    line("Search") {
        moveTo(18f, 11f); arcTo(7f, 7f, 0f, true, true, 4f, 11f); arcTo(7f, 7f, 0f, true, true, 18f, 11f)
        moveTo(20f, 20f); lineTo(16.2f, 16.2f)
    }
}

val CheckIcon: ImageVector by lazy { line("Check") { moveTo(5f, 12.5f); lineTo(9.5f, 17f); lineTo(19f, 7.5f) } }

/** 칠한 아이콘(재생·멈춤처럼 면으로 그리는 것) */
private fun filled(name: String, block: PathBuilder.() -> Unit): ImageVector =
    ImageVector.Builder(name = name, defaultWidth = 24.dp, defaultHeight = 24.dp, viewportWidth = 24f, viewportHeight = 24f).apply {
        path(fill = SolidColor(Color.Black), pathBuilder = block)
    }.build()

/** 집중(하단바): 스톱워치 */
val TimerIcon: ImageVector by lazy {
    line("Timer") {
        moveTo(10f, 2.5f); lineTo(14f, 2.5f)
        moveTo(12f, 2.5f); lineTo(12f, 5f)
        moveTo(19.5f, 13f)
        arcTo(7.5f, 7.5f, 0f, true, true, 4.5f, 13f)
        arcTo(7.5f, 7.5f, 0f, true, true, 19.5f, 13f)
        moveTo(12f, 13f); lineTo(12f, 9f)
        moveTo(18.5f, 6.5f); lineTo(17.2f, 7.8f)
    }
}

/** 재생(과목 동그라미 안) */
val PlayFilledIcon: ImageVector by lazy { filled("Play") { moveTo(8f, 5.5f); lineTo(19f, 12f); lineTo(8f, 18.5f); close() } }

/** 멈춤(돌고 있는 과목 동그라미 안) */
val PauseFilledIcon: ImageVector by lazy {
    filled("Pause") {
        moveTo(7f, 5f); lineTo(10.5f, 5f); lineTo(10.5f, 19f); lineTo(7f, 19f); close()
        moveTo(13.5f, 5f); lineTo(17f, 5f); lineTo(17f, 19f); lineTo(13.5f, 19f); close()
    }
}

/** 장소 핀 */
val PinIcon: ImageVector by lazy {
    line("Pin") {
        moveTo(12f, 21f)
        curveTo(12f, 21f, 5f, 14.5f, 5f, 9.5f)
        arcTo(7f, 7f, 0f, false, true, 19f, 9.5f)
        curveTo(19f, 14.5f, 12f, 21f, 12f, 21f)
        close()
        moveTo(14.5f, 9.5f)
        arcTo(2.5f, 2.5f, 0f, true, true, 9.5f, 9.5f)
        arcTo(2.5f, 2.5f, 0f, true, true, 14.5f, 9.5f)
    }
}

// ---- 거래 줄 앞 분류 그림(TxMarkView). 동그라미 안에 흰색으로 그린다 ----

/** 식비: 포크와 나이프 */
val FoodIcon: ImageVector by lazy {
    line("Food") {
        moveTo(5f, 3f); lineTo(5f, 8f); quadTo(5f, 11f, 7.5f, 11f); quadTo(10f, 11f, 10f, 8f); lineTo(10f, 3f)
        moveTo(7.5f, 3f); lineTo(7.5f, 21f)
        moveTo(18f, 21f); lineTo(18f, 3f); quadTo(14f, 6f, 14f, 12.5f); lineTo(18f, 12.5f)
    }
}

/** 카페: 김 나는 잔 */
val CupIcon: ImageVector by lazy {
    line("Cup") {
        moveTo(4f, 9f); lineTo(16f, 9f); lineTo(15f, 19f); quadTo(15f, 20f, 14f, 20f); lineTo(6f, 20f); quadTo(5f, 20f, 5f, 19f); close()
        moveTo(16f, 11f); quadTo(20f, 11f, 20f, 14f); quadTo(20f, 17f, 15.5f, 17f)
        moveTo(8f, 3f); quadTo(7f, 4.5f, 8f, 6f); moveTo(12f, 3f); quadTo(11f, 4.5f, 12f, 6f)
    }
}

/** 교통: 버스 */
val BusIcon: ImageVector by lazy {
    line("Bus") {
        moveTo(6f, 3f); lineTo(18f, 3f); quadTo(20f, 3f, 20f, 5f); lineTo(20f, 17f); lineTo(4f, 17f); lineTo(4f, 5f); quadTo(4f, 3f, 6f, 3f); close()
        moveTo(4f, 10f); lineTo(20f, 10f)
        moveTo(7f, 17f); lineTo(7f, 20f); moveTo(17f, 17f); lineTo(17f, 20f)
        moveTo(7.5f, 13.5f); lineTo(7.6f, 13.5f); moveTo(16.4f, 13.5f); lineTo(16.5f, 13.5f)
    }
}

/** 쇼핑: 손잡이 달린 봉투 */
val BagIcon: ImageVector by lazy {
    line("Bag") {
        moveTo(5f, 8f); lineTo(19f, 8f); lineTo(18f, 21f); lineTo(6f, 21f); close()
        moveTo(9f, 8f); lineTo(9f, 6.5f); arcTo(3f, 3f, 0f, false, true, 15f, 6.5f); lineTo(15f, 8f)
    }
}

/** 생활: 장바구니 */
val BasketIcon: ImageVector by lazy {
    line("Basket") {
        moveTo(3f, 10f); lineTo(21f, 10f); lineTo(19f, 20f); lineTo(5f, 20f); close()
        moveTo(8f, 10f); lineTo(11f, 4f); moveTo(16f, 10f); lineTo(13f, 4f)
        moveTo(9f, 13.5f); lineTo(9f, 16.5f); moveTo(12f, 13.5f); lineTo(12f, 16.5f); moveTo(15f, 13.5f); lineTo(15f, 16.5f)
    }
}

/** 구독: 돌아가는 화살표 */
val RepeatIcon: ImageVector by lazy {
    line("Repeat") {
        moveTo(20f, 12f); arcTo(8f, 8f, 0f, true, true, 17.66f, 6.34f)
        moveTo(18f, 2.6f); lineTo(17.66f, 6.34f); lineTo(13.9f, 6.1f)
    }
}

/** 문화·여가: 표 */
val TicketIcon: ImageVector by lazy {
    line("Ticket") {
        moveTo(3f, 7f); lineTo(21f, 7f); lineTo(21f, 10f); arcTo(2f, 2f, 0f, false, false, 21f, 14f); lineTo(21f, 17f)
        lineTo(3f, 17f); lineTo(3f, 14f); arcTo(2f, 2f, 0f, false, false, 3f, 10f); close()
        moveTo(14f, 8.5f); lineTo(14f, 9.5f); moveTo(14f, 11.5f); lineTo(14f, 12.5f); moveTo(14f, 14.5f); lineTo(14f, 15.5f)
    }
}

/** 의료·건강: 하트 */
val HeartIcon: ImageVector by lazy {
    line("Heart") {
        moveTo(12f, 20f); curveTo(5f, 15f, 3f, 11f, 3f, 8.5f); curveTo(3f, 6f, 5f, 4f, 7.5f, 4f); curveTo(9.5f, 4f, 11f, 5.2f, 12f, 7f)
        curveTo(13f, 5.2f, 14.5f, 4f, 16.5f, 4f); curveTo(19f, 4f, 21f, 6f, 21f, 8.5f); curveTo(21f, 11f, 19f, 15f, 12f, 20f); close()
    }
}

/** 경조사: 선물 상자 */
val GiftIcon: ImageVector by lazy {
    line("Gift") {
        moveTo(4f, 11f); lineTo(20f, 11f); lineTo(20f, 21f); lineTo(4f, 21f); close()
        moveTo(3f, 7f); lineTo(21f, 7f); lineTo(21f, 11f); lineTo(3f, 11f); close()
        moveTo(12f, 7f); lineTo(12f, 21f)
        moveTo(12f, 7f); curveTo(10f, 3f, 6f, 4f, 8f, 7f); moveTo(12f, 7f); curveTo(14f, 3f, 18f, 4f, 16f, 7f)
    }
}

/** 기타: 점 세 개 */
val DotsIcon: ImageVector by lazy {
    line("Dots", width = 3f) {
        moveTo(6f, 12f); lineTo(6.05f, 12f); moveTo(12f, 12f); lineTo(12.05f, 12f); moveTo(18f, 12f); lineTo(18.05f, 12f)
    }
}

/** 들어온 돈: 아래로 들어오는 화살표 */
val InArrowIcon: ImageVector by lazy {
    line("InArrow") {
        moveTo(12f, 4f); lineTo(12f, 15f); moveTo(7f, 10f); lineTo(12f, 15f); lineTo(17f, 10f); moveTo(5f, 19.5f); lineTo(19f, 19.5f)
    }
}

/** 이체: 오가는 화살표 */
val SwapIcon: ImageVector by lazy {
    line("Swap") {
        moveTo(4f, 8f); lineTo(18f, 8f); moveTo(15f, 5f); lineTo(18f, 8f); lineTo(15f, 11f)
        moveTo(20f, 16f); lineTo(6f, 16f); moveTo(9f, 13f); lineTo(6f, 16f); lineTo(9f, 19f)
    }
}
