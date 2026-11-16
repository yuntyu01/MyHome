package com.yuntyu.myhome.ui

import androidx.compose.animation.core.Animatable
import androidx.compose.animation.core.tween
import androidx.compose.foundation.ScrollState
import androidx.compose.foundation.background
import androidx.compose.foundation.gestures.awaitEachGesture
import androidx.compose.foundation.gestures.awaitFirstDown
import androidx.compose.foundation.gestures.drag
import androidx.compose.foundation.gestures.scrollBy
import androidx.compose.foundation.layout.Box
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.Alignment
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Icon
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableFloatStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberUpdatedState
import androidx.compose.runtime.setValue
import androidx.compose.runtime.snapshotFlow
import androidx.compose.runtime.withFrameNanos
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.drawWithContent
import androidx.compose.ui.geometry.CornerRadius
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.geometry.Size
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.StrokeCap
import androidx.compose.ui.graphics.drawscope.Stroke
import androidx.compose.ui.graphics.graphicsLayer
import androidx.compose.ui.hapticfeedback.HapticFeedbackType
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.ui.layout.LayoutCoordinates
import androidx.compose.ui.layout.onGloballyPositioned
import androidx.compose.ui.layout.positionInParent
import androidx.compose.ui.layout.positionInWindow
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.platform.LocalHapticFeedback
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.unit.dp
import androidx.compose.ui.zIndex
import kotlinx.coroutines.flow.collectLatest
import kotlinx.coroutines.launch
import com.yuntyu.myhome.core.CategoryDrag
import com.yuntyu.myhome.core.CategoryTree
import com.yuntyu.myhome.core.DropPlace
import com.yuntyu.myhome.core.api.Category

/**
 * 분류 줄을 꾹 눌러 끌어서 옮기기(2026-11-10). 놓을 자리 계산은 core [CategoryDrag], 여기는 손가락·그리기·자동 스크롤만.
 * - 줄 좌표는 스크롤 안쪽(내용) 기준으로 모은다. 손가락은 창 기준으로 받아 내용 기준으로 바꾼다(끄는 중 자동 스크롤해도 맞게).
 * - 줄의 기본 누르기(combinedClickable)는 꾹 누르기를 먹어 버려서, 누르기와 꾹 눌러 끌기를 한 제스처로 직접 받는다.
 * docs/DECISIONS.md 「분류 끌어서 옮기기」
 */
internal class CategoryDragger(val scroll: ScrollState) {
    /** 분류 id → 줄 좌표(살아 있는 객체라 읽을 때마다 지금 위치) */
    val rows = HashMap<Long, LayoutCoordinates>()
    /** 스크롤 바깥 칸(자동 스크롤 가장자리 판단) */
    var viewport: LayoutCoordinates? = null
    var drag by mutableStateOf<Dragging?>(null)
        private set
    /** 들린 줄이 손가락 위로 올라간 만큼(손가락에 가려지지 않고, 놓을 선·칸을 가리지 않게) */
    val rise = Animatable(0f)

    /** 끄는 중: 분류와 같이 움직이는 줄(그 안), 손가락(창 기준·내용 기준) */
    class Dragging(val id: Long, val kind: String, val block: Set<Long>, val startY: Float) {
        var fingerWin by mutableFloatStateOf(0f)
        var y by mutableFloatStateOf(startY)
    }

    private fun live(id: Long): LayoutCoordinates? = rows[id]?.takeIf { it.isAttached }

    /** 손잡이를 잡은 손가락(창 기준 y)으로 끌기를 시작한다 */
    fun start(tree: CategoryTree, c: Category, fingerWin: Float): Boolean {
        val co = live(c.id) ?: return false
        drag = Dragging(c.id, c.kind, setOf(c.id) + tree.descendants(c.id), co.positionInParent().y + fingerWin - co.positionInWindow().y)
            .also { it.fingerWin = fingerWin }
        return true
    }

    fun move(fingerWin: Float) {
        val d = drag ?: return
        d.fingerWin = fingerWin
        sync()
    }

    /** 손가락 내용 좌표 = 창 좌표 − (줄의 창 위치 − 줄의 내용 위치). 스크롤이 움직여도 맞는다 */
    fun sync() {
        val d = drag ?: return
        val co = live(d.id) ?: return
        d.y = d.fingerWin - co.positionInWindow().y + co.positionInParent().y
    }

    fun stop() { drag = null }

    /** 끄는 줄이 손가락을 따라 움직인 거리 */
    fun lift(id: Long): Float = drag?.let { if (id in it.block) it.y - it.startY - rise.value else 0f } ?: 0f

    fun target(tree: CategoryTree): CategoryDrag.Drop? {
        val d = drag ?: return null
        val rowsNow = tree.preorder { !it.archived }
            .filter { it.first.kind == d.kind && it.first.id !in d.block }
            .mapNotNull { (c, _) -> live(c.id)?.let { co -> val t = co.positionInParent().y; CategoryDrag.Row(c.id, t, t + co.size.height) } }
        val held = d.block.mapNotNull { live(it) }
        val hole = if (held.isEmpty()) null else held.minOf { it.positionInParent().y }..held.maxOf { it.positionInParent().y + it.size.height }
        return CategoryDrag.target(tree, d.id, rowsNow, hole, d.y)
    }
}

@Composable
internal fun rememberCategoryDragger(scroll: ScrollState): CategoryDragger {
    val d = remember(scroll) { CategoryDragger(scroll) }
    val density = LocalDensity.current
    val dragging = d.drag
    // 끄는 손가락이 위·아래 가장자리에 가까우면 그쪽으로 천천히 스크롤. 가장자리가 아니거나 더 갈 데가 없으면
    // 프레임을 돌리지 않고 손가락이 움직일 때까지 쉰다(계속 돌면 화면이 쉬지 못한다)
    LaunchedEffect(dragging) {
        if (dragging == null) return@LaunchedEffect
        val edge = with(density) { 64.dp.toPx() }
        val maxStep = with(density) { 20.dp.toPx() } // 두 프레임에 한 번이라 실제로는 프레임당 10dp까지
        launch {
            d.rise.snapTo(0f)
            d.rise.animateTo(with(density) { 34.dp.toPx() }, tween(140))
        }
        snapshotFlow { dragging.fingerWin }.collectLatest { f ->
            while (true) {
                val vp = d.viewport?.takeIf { it.isAttached } ?: break
                val top = vp.positionInWindow().y
                val bottom = top + vp.size.height
                val step = when {
                    f < top + edge -> -maxStep * ((top + edge - f) / edge).coerceIn(0f, 1f)
                    f > bottom - edge -> maxStep * ((f - (bottom - edge)) / edge).coerceIn(0f, 1f)
                    else -> 0f
                }
                if (step == 0f) break
                withFrameNanos { }
                if (d.scroll.scrollBy(step) == 0f) break
                withFrameNanos { } // 스크롤한 자리가 배치된 뒤 손가락 내용 좌표를 다시 맞춘다
                d.sync()
            }
        }
    }
    return d
}

/** 스크롤 바깥에 붙인다(verticalScroll 앞) */
internal fun Modifier.dragViewport(d: CategoryDragger): Modifier = onGloballyPositioned { d.viewport = it }

/** 스크롤 안쪽에 붙인다(verticalScroll 뒤): 안에 넣을 줄은 옅게 칠하고, 앞뒤 자리는 그 단계 들여쓰기로 선을 긋는다 */
internal fun Modifier.dropMarks(d: CategoryDragger, drop: CategoryDrag.Drop?, color: Color): Modifier = drawWithContent {
    val into = drop?.into?.let { id -> d.rows[id]?.takeIf { it.isAttached } }
    if (into != null) {
        drawRoundRect(
            color.copy(alpha = 0.18f),
            topLeft = Offset(8.dp.toPx(), into.positionInParent().y),
            size = Size(size.width - 16.dp.toPx(), into.size.height.toFloat()),
            cornerRadius = CornerRadius(12.dp.toPx()),
        )
    }
    drawContent()
    if (into != null) {
        val w = 2.dp.toPx()
        drawRoundRect(
            color,
            topLeft = Offset(8.dp.toPx() + w / 2, into.positionInParent().y + w / 2),
            size = Size(size.width - 16.dp.toPx() - w, into.size.height - w),
            cornerRadius = CornerRadius(12.dp.toPx()),
            style = Stroke(w),
        )
    }
    if (drop != null && drop.into == null) {
        val x = (Gutter + FolderIndent * drop.lineDepth).toPx()
        drawLine(color, Offset(x, drop.lineY), Offset(size.width - Gutter.toPx(), drop.lineY), strokeWidth = 2.5.dp.toPx(), cap = StrokeCap.Round)
        drawCircle(color, radius = 4.dp.toPx(), center = Offset(x, drop.lineY))
    }
}

/**
 * 끌 수 있는 분류 줄: 줄을 누르면 onTap, 오른쪽 손잡이(≡)를 잡으면 바로 들려서(진동) 손가락을 따라오고, 놓으면 onDrop(자리가 바뀔 때만).
 * 글자 쪽은 꾹 눌러도 끌리지 않는다(사용자 요청, 2026-11-16 "글자 쪽 누르면 드래그되는 거 고쳐"). 손잡이는 content가 받은 handle을
 * 줄 오른쪽에 그린다. 폴더면 안의 줄도 같이 들린다(그 줄들도 이 함수로 그려야 한다).
 */
@Composable
internal fun DragRow(
    d: CategoryDragger,
    tree: CategoryTree,
    c: Category,
    onTap: () -> Unit,
    onDrop: (Long, DropPlace) -> Unit,
    content: @Composable (handle: @Composable () -> Unit) -> Unit,
) {
    val m = LocalModo.current
    val haptic = LocalHapticFeedback.current
    val drop by rememberUpdatedState(onDrop)
    val treeNow by rememberUpdatedState(tree)
    val cat by rememberUpdatedState(c)
    val held = d.drag?.block?.contains(c.id) == true
    // 손잡이 좌표: 줄이 손가락을 따라 움직여도(graphicsLayer) 창 기준으로 바꿔 쓰면 맞는다
    var handleAt by remember { mutableStateOf<LayoutCoordinates?>(null) }
    val handle: @Composable () -> Unit = {
        Box(
            Modifier
                // 줄 높이(56dp)를 늘리지 않게 높이는 32dp, 손가락이 닿기 쉽게 폭은 넓게
                .size(width = 44.dp, height = 32.dp)
                .onGloballyPositioned { handleAt = it }
                .pointerInput(c.id) {
                    awaitEachGesture {
                        val down = awaitFirstDown(requireUnconsumed = false)
                        val at = handleAt?.takeIf { it.isAttached } ?: return@awaitEachGesture
                        if (!d.start(treeNow, cat, at.localToWindow(down.position).y)) return@awaitEachGesture
                        down.consume()
                        haptic.performHapticFeedback(HapticFeedbackType.LongPress)
                        try {
                            val finished = drag(down.id) { ch ->
                                handleAt?.takeIf { it.isAttached }?.let { co -> d.move(co.localToWindow(ch.position).y) }
                                ch.consume()
                            }
                            if (finished) {
                                val target = d.target(treeNow)
                                if (target != null && !CategoryDrag.same(treeNow, cat.id, target.place)) drop(cat.id, target.place)
                            }
                        } finally {
                            d.stop()
                        }
                    }
                }
                .semantics { contentDescription = "${c.name} 끌어서 옮기기" },
            contentAlignment = Alignment.Center,
        ) {
            Icon(HandleIcon, contentDescription = null, tint = if (held) MaterialTheme.colorScheme.onBackground else m.faint, modifier = Modifier.size(20.dp))
        }
    }
    Box(
        Modifier
            .onGloballyPositioned { d.rows[c.id] = it }
            .zIndex(if (held) 1f else 0f)
            .graphicsLayer {
                translationY = d.lift(c.id)
                if (held) {
                    shadowElevation = 8.dp.toPx()
                    shape = RoundedCornerShape(14.dp)
                    clip = true
                    scaleX = 0.96f
                    scaleY = 0.96f
                }
            }
            .background(if (held) m.raised else Color.Transparent)
            .tap(onClickLabel = "${c.name} 고치기", role = Role.Button, onClick = onTap),
    ) { content(handle) }
}
