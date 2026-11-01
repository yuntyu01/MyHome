package com.yuntyu.myhome.work

import android.content.Context

/** 이벤트 전송 성공 후 처리. Phase 6에서 위젯 갱신, Phase 7에서 예산 알림을 붙인다. */
object AfterUpload {
    suspend fun onEventsSent(context: Context) {
        // Phase 2: 아직 할 일 없음
    }
}
