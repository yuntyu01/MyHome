package com.yuntyu.myhome.ui

import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp

/** 절전 설정 안내(삼성 등): 앱이 잠들어 결제 알림을 놓치지 않게 하는 메뉴 위치. 문제 해결 화면에서 연다 */
@Composable
fun PermissionScreen(modifier: Modifier, resumeTick: Int) {
    val ctx = LocalContext.current
    val m = LocalModo.current
    Column(modifier.fillMaxSize().verticalScroll(rememberScrollState()).padding(bottom = 32.dp)) {
        Text(
            "폰이 절전하려고 MyHome을 재우면 결제 알림을 놓칠 수 있어요. 아래 설정을 한 번만 해 두면 돼요. 폰마다 메뉴 이름이 조금 다를 수 있어요.",
            Modifier.padding(horizontal = Gutter, vertical = 8.dp), fontSize = 15.sp, color = m.muted, lineHeight = 22.sp,
        )
        SectionLabel("삼성 갤럭시")
        for (line in listOf(
            "설정 > 애플리케이션 > MyHome > 배터리 > '제한 없음'",
            "설정 > 배터리 > 백그라운드 사용 한도 > '사용하지 않는 앱을 절전 상태로 전환' 끄기",
            "같은 화면의 '절전 예외 앱'에 MyHome 추가",
            "최근 앱 화면에서 MyHome 아이콘을 길게 눌러 '이 앱 잠그기'",
        )) {
            Text(line, Modifier.padding(horizontal = Gutter, vertical = 8.dp), fontSize = 15.sp, lineHeight = 22.sp)
        }
        FlatRow("MyHome 앱 정보 열기", onClick = { PermissionStatus.openAppDetails(ctx) })
        Hairline()
        SectionLabel("원래 결제 알림 소리")
        Text(
            "가계부에 적으면 MyHome이 카드·은행 앱 알림을 치워요. 소리가 거슬리면 그 앱의 알림을 '무음'으로 바꿔 주세요. 알림을 '끄면' 읽을 수도 없으니 꼭 '무음'으로요.",
            Modifier.padding(horizontal = Gutter, vertical = 8.dp), fontSize = 15.sp, lineHeight = 22.sp,
        )
        SectionLabel("Android 13 이상에서 '제한된 설정'이 뜨면")
        Text(
            "설정 > 애플리케이션 > MyHome > 오른쪽 위 ⋮ > '제한된 설정 허용'을 누른 뒤 알림 읽기를 다시 켜 주세요.",
            Modifier.padding(horizontal = Gutter, vertical = 8.dp), fontSize = 15.sp, lineHeight = 22.sp,
        )
    }
}
