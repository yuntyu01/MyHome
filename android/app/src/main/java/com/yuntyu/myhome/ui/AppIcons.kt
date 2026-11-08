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
private fun line(name: String, block: PathBuilder.() -> Unit): ImageVector =
    ImageVector.Builder(name = name, defaultWidth = 24.dp, defaultHeight = 24.dp, viewportWidth = 24f, viewportHeight = 24f).apply {
        path(stroke = SolidColor(Color.Black), strokeLineWidth = 2f, strokeLineCap = StrokeCap.Round, strokeLineJoin = StrokeJoin.Round, pathBuilder = block)
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
