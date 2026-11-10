package com.yuntyu.myhome.ui

import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ColumnScope
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.text.selection.DisableSelection
import androidx.compose.foundation.text.selection.SelectionContainer
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.rememberModalBottomSheetState
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color

/*
 * 꾹 눌러 글자 고르기(복사). 앱 화면 전체는 AppRoot의 SelectionContainer 하나가 맡는다.
 *
 * 🔴 대화상자·메뉴·아래 판은 따로 뜨는 창이라 바깥 SelectionContainer에 글자가 붙으면, 그 글자를 꾹 누를 때
 *    "layouts are not part of the same hierarchy"로 앱이 죽는다(Compose 1.7: 창이 다르면 공통 조상이 없다).
 *    그래서 material3 AlertDialog·DropdownMenu·ModalBottomSheet를 바로 쓰지 말고 아래 Modo* 를 쓴다.
 *    대화상자·아래 판은 창 안에 자기 SelectionContainer를 두고(그 안 글자도 복사된다), 메뉴와 버튼은 고르기를 끈다.
 */

/** material3 AlertDialog와 같다. 제목·본문은 꾹 눌러 복사할 수 있고, 버튼 글자는 고르지 않는다 */
@Composable
fun ModoAlertDialog(
    onDismissRequest: () -> Unit,
    confirmButton: @Composable () -> Unit,
    modifier: Modifier = Modifier,
    dismissButton: (@Composable () -> Unit)? = null,
    title: (@Composable () -> Unit)? = null,
    text: (@Composable () -> Unit)? = null,
) {
    androidx.compose.material3.AlertDialog(
        onDismissRequest = onDismissRequest,
        confirmButton = { DisableSelection { confirmButton() } },
        modifier = modifier,
        dismissButton = dismissButton?.let { b -> { DisableSelection { b() } } },
        title = title?.let { t -> { SelectionContainer { t() } } },
        text = text?.let { t -> { SelectionContainer { t() } } },
    )
}

/** material3 DropdownMenu와 같다. 메뉴 글자는 누르는 것이라 고르기를 끈다 */
@Composable
fun ModoDropdownMenu(expanded: Boolean, onDismissRequest: () -> Unit, modifier: Modifier = Modifier, content: @Composable ColumnScope.() -> Unit) {
    androidx.compose.material3.DropdownMenu(expanded = expanded, onDismissRequest = onDismissRequest, modifier = modifier) {
        val scope = this
        DisableSelection { scope.content() }
    }
}

/** material3 ModalBottomSheet(처음부터 다 펼침, 바탕색)와 같다. 판 안 글자도 꾹 눌러 복사할 수 있다 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun ModoBottomSheet(
    onDismissRequest: () -> Unit,
    containerColor: Color = LocalModo.current.bg,
    content: @Composable ColumnScope.() -> Unit,
) {
    val sheetState = rememberModalBottomSheetState(skipPartiallyExpanded = true)
    androidx.compose.material3.ModalBottomSheet(onDismissRequest = onDismissRequest, sheetState = sheetState, containerColor = containerColor) {
        SelectionContainer { Column(Modifier.fillMaxWidth(), content = content) }
    }
}
