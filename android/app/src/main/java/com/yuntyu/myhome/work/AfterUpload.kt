package com.yuntyu.myhome.work

import android.content.Context
import com.yuntyu.myhome.widget.WidgetRefresher

/** 이벤트 전송 성공 직후 처리: 위젯을 즉시 갱신한다(Phase 7에서 예산 알림 추가). */
object AfterUpload {
    suspend fun onEventsSent(context: Context) {
        WidgetRefresher.refresh(context)
    }
}
