package com.yuntyu.myhome.ui

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.combinedClickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.IntrinsicSize
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ColumnScope
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.RowScope
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.selection.DisableSelection
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Switch
import androidx.compose.material3.SwitchDefaults
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import java.text.SimpleDateFormat
import java.util.Date
import java.util.Locale
import java.util.TimeZone

/**
 * 누르면 동작하는 칸. 화면 안에서는 clickable 대신 이것을 쓴다(하단바·머리처럼 글자 고르기 밖이면 clickable도 된다).
 * 앱 전체가 꾹 눌러 글자 고르기다. 글자 위를 꾹 누르면 clickable이어도 골라지기만 하지만, 손가락이 글자를 살짝 벗어나
 * 줄의 빈 곳을 꾹 눌렀다 떼면 clickable은 누르기로 친다(복사하려다 분류 판이 열림). 길게 누르기를 받아서 버리면
 * 그때도 열리지 않고, 글자 고르기·복사는 그대로 된다(SelectionTest). 인자는 clickable과 같은 이름·순서다.
 */
@OptIn(androidx.compose.foundation.ExperimentalFoundationApi::class)
fun Modifier.tap(enabled: Boolean = true, onClickLabel: String? = null, role: Role? = null, onClick: () -> Unit): Modifier =
    combinedClickable(enabled = enabled, onClickLabel = onClickLabel, role = role, onLongClick = {}, onClick = onClick)

/** modo 카드: 테두리·그림자 없이 바탕색만 다른 둥근 블록(16dp). 제목은 카드 위의 작은 회색 글자. */
@Composable
fun SectionCard(title: String, modifier: Modifier = Modifier, content: @Composable ColumnScope.() -> Unit) {
    val m = LocalModo.current
    Column(modifier.fillMaxWidth()) {
        Text(
            title,
            style = MaterialTheme.typography.labelLarge,
            color = m.muted,
            modifier = Modifier.padding(start = 6.dp, bottom = 8.dp),
        )
        Column(
            Modifier.fillMaxWidth().clip(RoundedCornerShape(16.dp)).background(m.card).padding(horizontal = 18.dp, vertical = 16.dp),
            verticalArrangement = Arrangement.spacedBy(10.dp),
            content = content,
        )
    }
}

/** 상태 한 줄: 왼쪽 점(정상: 강조색, 주의: 경고색) + 제목 + 설명. */
@Composable
fun StatusRow(label: String, ok: Boolean, detail: String? = null) {
    val m = LocalModo.current
    Row(verticalAlignment = Alignment.CenterVertically) {
        Box(Modifier.size(10.dp).clip(CircleShape).background(if (ok) m.accent else m.over))
        Spacer(Modifier.width(12.dp))
        Column {
            Text(label, style = MaterialTheme.typography.bodyLarge)
            if (detail != null) Text(detail, style = MaterialTheme.typography.bodySmall, color = m.muted)
        }
    }
}

/** 얇은 둥근 진행 막대(modo 슬라이더 트랙처럼 5dp). */
@Composable
fun ModoBar(ratio: Double, color: Color, modifier: Modifier = Modifier) {
    val m = LocalModo.current
    Box(modifier.fillMaxWidth().height(5.dp).clip(RoundedCornerShape(3.dp)).background(m.raised)) {
        Box(
            Modifier.fillMaxWidth(ratio.toFloat().coerceIn(0f, 1f)).height(5.dp).clip(RoundedCornerShape(3.dp)).background(color),
        )
    }
}

private val KST: TimeZone = TimeZone.getTimeZone("Asia/Seoul")

fun formatTime(ms: Long, pattern: String = "MM/dd HH:mm"): String =
    SimpleDateFormat(pattern, Locale.KOREA).apply { timeZone = KST }.format(Date(ms))


// ── 줄 디자인(가계부·설정): 카드 대신 가로로 꽉 찬 줄 + 1dp 선. 시안 docs/DECISIONS.md 「앱 화면 단순화」 ──

/** 화면 좌우 여백 */
val Gutter: Dp = 24.dp

/** 1dp 구분선 */
@Composable
fun Hairline(modifier: Modifier = Modifier) {
    Box(modifier.fillMaxWidth().height(1.dp).background(LocalModo.current.line))
}

/** 하단 탭 화면의 큰 제목. trailing에 달 넘기기·아이콘 등을 둔다 */
@Composable
fun PageTitle(text: String, modifier: Modifier = Modifier, trailing: @Composable RowScope.() -> Unit = {}) {
    Row(
        modifier.fillMaxWidth().heightIn(min = 64.dp).padding(start = Gutter, end = 8.dp, top = 8.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Text(text, Modifier.weight(1f), fontSize = 26.sp, fontWeight = FontWeight.Bold, maxLines = 1)
        trailing()
    }
}

/** 묶음 머리: 작은 회색 글자. action이 있으면 오른쪽에 글자 링크 */
@Composable
fun SectionLabel(text: String, modifier: Modifier = Modifier, action: String? = null, onAction: (() -> Unit)? = null) {
    val m = LocalModo.current
    Row(modifier.fillMaxWidth().padding(start = Gutter, end = Gutter, top = 24.dp, bottom = 4.dp), verticalAlignment = Alignment.CenterVertically) {
        Text(text, Modifier.weight(1f), fontSize = 14.sp, color = m.muted)
        if (action != null && onAction != null) {
            Text(action, Modifier.tap(role = Role.Button, onClick = onAction).padding(vertical = 4.dp, horizontal = 2.dp), fontSize = 14.sp, color = m.muted)
        }
    }
}

/**
 * 한 줄: 왼쪽 제목(+설명), 오른쪽 값, 누르면 동작(오른쪽 꺾쇠). 박스 없이 가로로 꽉 찬다.
 * leading에는 점·아이콘, trailing에는 스위치·체크를 둔다.
 */
@Composable
fun FlatRow(
    title: String,
    modifier: Modifier = Modifier,
    sub: String? = null,
    value: String? = null,
    titleColor: Color? = null,
    subColor: Color? = null,
    valueColor: Color? = null,
    bold: Boolean = false,
    chevron: Boolean = true,
    leading: (@Composable () -> Unit)? = null,
    trailing: (@Composable () -> Unit)? = null,
    onClick: (() -> Unit)? = null,
) {
    val m = LocalModo.current
    Row(
        modifier.fillMaxWidth().heightIn(min = 56.dp)
            .then(if (onClick != null) Modifier.tap(onClick = onClick) else Modifier)
            .padding(horizontal = Gutter, vertical = 12.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(14.dp),
    ) {
        leading?.invoke()
        Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(2.dp)) {
            Text(title, fontSize = 16.sp, fontWeight = if (bold) FontWeight.Bold else FontWeight.Normal, color = titleColor ?: MaterialTheme.colorScheme.onBackground)
            if (!sub.isNullOrBlank()) Text(sub, fontSize = 14.sp, color = subColor ?: m.muted, lineHeight = 20.sp)
        }
        if (!value.isNullOrEmpty()) Text(value, fontSize = 15.sp, color = valueColor ?: m.muted, textAlign = TextAlign.End, maxLines = 1)
        trailing?.invoke()
        if (onClick != null && chevron && trailing == null) Icon(ChevronRightIcon, contentDescription = null, tint = m.faint, modifier = Modifier.size(18.dp))
    }
}

/** 켜고 끄는 줄(줄 전체를 눌러도 바뀐다) */
@Composable
fun SwitchRow(title: String, checked: Boolean, sub: String? = null, onChange: (Boolean) -> Unit) {
    val m = LocalModo.current
    FlatRow(
        title,
        sub = sub,
        trailing = {
            Switch(
                checked = checked,
                onCheckedChange = onChange,
                colors = SwitchDefaults.colors(
                    checkedTrackColor = m.cta, checkedThumbColor = m.onCta, checkedBorderColor = m.cta,
                    uncheckedTrackColor = m.line, uncheckedThumbColor = m.muted, uncheckedBorderColor = m.line,
                ),
            )
        },
        onClick = { onChange(!checked) },
    )
}

/** 화면의 주인공 버튼: 가로로 꽉 찬 56dp. 바깥 여백은 부르는 쪽이 준다 */
@Composable
fun PrimaryButton(text: String, modifier: Modifier = Modifier, enabled: Boolean = true, onClick: () -> Unit) {
    val m = LocalModo.current
    Box(
        modifier.fillMaxWidth().height(56.dp).clip(RoundedCornerShape(14.dp)).background(if (enabled) m.cta else m.line)
            .tap(enabled = enabled, role = Role.Button, onClick = onClick),
        contentAlignment = Alignment.Center,
    ) {
        // 버튼 글자는 꾹 눌러도 고르지 않는다(누르는 것이라)
        DisableSelection { Text(text, fontSize = 17.sp, fontWeight = FontWeight.Bold, color = if (enabled) m.onCta else m.faint, maxLines = 1) }
    }
}

/** "+ 현금으로 쓴 돈 직접 적기" 같은 조용한 추가 줄 */
@Composable
fun AddRow(text: String, modifier: Modifier = Modifier, onClick: () -> Unit) {
    val m = LocalModo.current
    Row(
        modifier.fillMaxWidth().heightIn(min = 56.dp).tap(role = Role.Button, onClick = onClick).padding(horizontal = Gutter, vertical = 14.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        Icon(PlusIcon, contentDescription = null, tint = m.muted, modifier = Modifier.size(20.dp))
        Text(text, fontSize = 16.sp, color = m.muted)
    }
}

/** 상태 점 */
@Composable
fun Dot(color: Color, size: Dp = 8.dp) {
    Box(Modifier.size(size).clip(CircleShape).background(color))
}

/** 고르기 동그라미: 고르면 채운 원 + 체크 */
@Composable
fun CheckCircle(checked: Boolean) {
    val m = LocalModo.current
    if (checked) {
        Box(Modifier.size(24.dp).clip(CircleShape).background(m.cta), contentAlignment = Alignment.Center) {
            Icon(CheckIcon, contentDescription = "선택됨", tint = m.onCta, modifier = Modifier.size(16.dp))
        }
    } else {
        Box(Modifier.size(24.dp).clip(CircleShape).border(1.5.dp, m.faint, CircleShape))
    }
}

/** 글자 탭(밑줄로 고른 것을 표시). 글자 크기에 맞춘 상자 대신 쓴다 */
@Composable
fun TextTabs(items: List<String>, selected: Int, modifier: Modifier = Modifier, onSelect: (Int) -> Unit) {
    val m = LocalModo.current
    Column(modifier.fillMaxWidth()) {
        DisableSelection { Row(Modifier.fillMaxWidth().padding(horizontal = Gutter), horizontalArrangement = Arrangement.spacedBy(20.dp)) {
            items.forEachIndexed { i, label ->
                val on = i == selected
                // 밑줄 폭을 글자 폭에 맞춘다(IntrinsicSize.Max 없이 fillMaxWidth를 쓰면 첫 탭이 줄 전체를 차지한다)
                Column(Modifier.width(IntrinsicSize.Max).tap(role = Role.Tab) { onSelect(i) }.padding(top = 10.dp), horizontalAlignment = Alignment.CenterHorizontally) {
                    Text(label, fontSize = 15.sp, fontWeight = if (on) FontWeight.Bold else FontWeight.Normal, color = if (on) MaterialTheme.colorScheme.onBackground else m.muted, maxLines = 1)
                    Spacer(Modifier.height(8.dp))
                    Box(Modifier.height(2.dp).fillMaxWidth().background(if (on) MaterialTheme.colorScheme.onBackground else Color.Transparent))
                }
            }
        } }
        Hairline()
    }
}

/** 비었을 때 안내: 제목 + 설명, 가운데가 아니라 왼쪽 정렬(줄 디자인과 맞춘다) */
@Composable
fun EmptyNote(title: String, sub: String? = null, modifier: Modifier = Modifier) {
    val m = LocalModo.current
    Column(modifier.fillMaxWidth().padding(horizontal = Gutter, vertical = 28.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
        Text(title, fontSize = 16.sp, color = MaterialTheme.colorScheme.onBackground)
        if (sub != null) Text(sub, fontSize = 14.sp, color = m.muted, lineHeight = 20.sp)
    }
}

/** 사용자에게 보여 줄 쉬운 오류 문장(HTTP 코드·영어 메시지 대신). 자세한 건 문제 해결 화면에서 */
fun com.yuntyu.myhome.core.api.ApiResult<*>.plain(): String = when (this) {
    is com.yuntyu.myhome.core.api.ApiResult.Ok -> ""
    is com.yuntyu.myhome.core.api.ApiResult.NetworkError -> "인터넷 연결을 확인해 주세요"
    is com.yuntyu.myhome.core.api.ApiResult.NotConfigured -> "먼저 로그인해 주세요"
    is com.yuntyu.myhome.core.api.ApiResult.HttpError -> when {
        code == 401 || code == 403 -> "로그인이 풀렸어요. 설정에서 다시 로그인해 주세요"
        code in 400..499 && message.isNotBlank() && message.any { it in '가'..'힣' } -> message.removePrefix("invalid input: ").take(80)
        code >= 500 -> "서버에 잠깐 문제가 있어요. 조금 뒤에 다시 해 주세요"
        else -> "요청을 처리하지 못했어요 ($code)"
    }
}

/** 밑줄만 있는 입력칸(상자 대신). 값·라벨·키보드 종류만 받는다 */
@Composable
fun LineField(
    value: String,
    onChange: (String) -> Unit,
    label: String,
    modifier: Modifier = Modifier,
    keyboardType: androidx.compose.ui.text.input.KeyboardType = androidx.compose.ui.text.input.KeyboardType.Text,
    secret: Boolean = false,
    /** 1보다 크면 여러 줄(설명 같은 긴 글) */
    maxLines: Int = 1,
) {
    val m = LocalModo.current
    androidx.compose.material3.TextField(
        value = value,
        onValueChange = onChange,
        label = { Text(label) },
        singleLine = maxLines <= 1,
        maxLines = maxLines,
        keyboardOptions = androidx.compose.foundation.text.KeyboardOptions(keyboardType = keyboardType),
        visualTransformation = if (secret) androidx.compose.ui.text.input.PasswordVisualTransformation() else androidx.compose.ui.text.input.VisualTransformation.None,
        colors = androidx.compose.material3.TextFieldDefaults.colors(
            focusedContainerColor = Color.Transparent, unfocusedContainerColor = Color.Transparent, disabledContainerColor = Color.Transparent,
            focusedIndicatorColor = MaterialTheme.colorScheme.onBackground, unfocusedIndicatorColor = m.line,
            focusedLabelColor = m.muted, unfocusedLabelColor = m.muted, cursorColor = MaterialTheme.colorScheme.onBackground,
        ),
        modifier = modifier.fillMaxWidth(),
    )
}
