package com.yuntyu.myhome.ui

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.material3.Icon
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.alpha
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import com.yuntyu.myhome.core.MarkKind
import com.yuntyu.myhome.core.TxMark

private fun MarkKind.icon(): ImageVector = when (this) {
    MarkKind.FOOD -> FoodIcon
    MarkKind.CAFE -> CupIcon
    MarkKind.TRANSIT -> BusIcon
    MarkKind.SHOPPING -> BagIcon
    MarkKind.LIVING -> BasketIcon
    MarkKind.HOUSING -> HomeLineIcon
    MarkKind.SUBSCRIPTION -> RepeatIcon
    MarkKind.CULTURE -> TicketIcon
    MarkKind.HEALTH -> HeartIcon
    MarkKind.EVENT -> GiftIcon
    MarkKind.ETC -> DotsIcon
    MarkKind.INCOME -> InArrowIcon
    MarkKind.TRANSFER -> SwapIcon
}

/** 거래 줄 앞 동그라미(core TxMarks가 고른 것): 가게 색 + 글자, 분류 그림, 첫 글자, 또는 "?" */
@Composable
fun TxMarkView(mark: TxMark, modifier: Modifier = Modifier, size: Dp = 42.dp, dim: Boolean = false) {
    val m = LocalModo.current
    val bg = when (mark) {
        is TxMark.Brand -> Color(mark.bg)
        is TxMark.Kind -> Color(mark.kind.bg)
        is TxMark.Letter -> Color(mark.bg)
        TxMark.Unknown -> m.raised
    }
    Box(
        modifier.size(size).alpha(if (dim) 0.4f else 1f).clip(CircleShape).background(bg),
        contentAlignment = Alignment.Center,
    ) {
        when (mark) {
            is TxMark.Brand -> Text(
                mark.label, color = Color(mark.fg), fontWeight = FontWeight.Black, maxLines = 1,
                fontSize = when (mark.label.length) { 1 -> 19.sp; 2 -> 14.sp; else -> 11.sp },
            )
            is TxMark.Kind -> Icon(mark.kind.icon(), contentDescription = null, tint = Color.White, modifier = Modifier.size(size * 0.5f))
            is TxMark.Letter -> Text(mark.letter, color = Color.White, fontWeight = FontWeight.Bold, fontSize = 17.sp)
            TxMark.Unknown -> Text("?", color = m.muted, fontWeight = FontWeight.Bold, fontSize = 18.sp)
        }
    }
}
