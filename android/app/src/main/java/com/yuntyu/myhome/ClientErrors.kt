package com.yuntyu.myhome

import android.content.Context
import com.yuntyu.myhome.core.api.ApiResult
import com.yuntyu.myhome.core.api.ClientError
import kotlinx.coroutines.launch
import java.io.File
import java.time.OffsetDateTime

/**
 * 폰에서만 나는 오류를 서버 로그로 보낸다(2026-11-15: 건강 사진 오류가 서버에 아무 흔적도 없어서 원인을 못 찾았다).
 * - 앱이 죽으면 스택을 파일에 남기고(그 순간엔 네트워크를 못 쓴다) 다음 실행 때 보낸다.
 * - 화면에 띄운 오류 메시지(사진 분석·저장·카메라 등)는 바로 보낸다. 실패하면 버린다(화면은 이미 알렸다).
 * 알림 원문·금액·사진은 넣지 않는다. 서버는 DB에 넣지 않고 로그에만 남긴다(POST /v1/client-errors).
 */
object ClientErrors {
    private const val FILE = "last_crash.txt"
    private const val MAX = 6000

    private fun version() = "${BuildConfig.VERSION_NAME}(${BuildConfig.VERSION_CODE})"

    /** 앱이 죽을 때 스택을 남긴다. 원래 처리기(시스템의 "앱이 중지됨")는 그대로 부른다 */
    fun install(context: Context) {
        val file = File(context.filesDir, FILE)
        val previous = Thread.getDefaultUncaughtExceptionHandler()
        Thread.setDefaultUncaughtExceptionHandler { t, e ->
            runCatching {
                val text = "${OffsetDateTime.now()}\n${version()}\nthread=${t.name}\n${e.stackTraceToString()}"
                file.writeText(text.take(MAX))
            }
            previous?.uncaughtException(t, e)
        }
    }

    /** 지난번에 죽은 기록이 있으면 보낸다. 보내면 지운다 */
    fun flushCrash(app: MyHomeApp) {
        val file = File(app.filesDir, FILE)
        if (!file.exists()) return
        app.ioScope.launch {
            val lines = runCatching { file.readText() }.getOrNull() ?: return@launch
            val at = lines.substringBefore('\n')
            val r = app.api.clientError(ClientError("crash", lines.substringAfter('\n'), version(), at))
            if (r is ApiResult.Ok) file.delete()
        }
    }

    /** 화면에 띄운 오류를 보낸다(기다리지 않는다) */
    fun report(kind: String, message: String, error: Throwable? = null) {
        val app = runCatching { MyHomeApp.instance }.getOrNull() ?: return
        val text = (message + (error?.let { "\n" + it.stackTraceToString() } ?: "")).take(MAX)
        app.ioScope.launch { runCatching { app.api.clientError(ClientError(kind, text, version(), OffsetDateTime.now().toString())) } }
    }
}
