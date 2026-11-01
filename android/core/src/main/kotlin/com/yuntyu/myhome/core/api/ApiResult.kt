package com.yuntyu.myhome.core.api

import java.io.IOException

/** HTTP 호출 결과. 예외 대신 값으로 다뤄서 WorkManager 재시도 판단을 단순하게 한다. */
sealed interface ApiResult<out T> {
    data class Ok<T>(val value: T, val code: Int) : ApiResult<T>
    data class HttpError(val code: Int, val message: String) : ApiResult<Nothing>
    data class NetworkError(val cause: IOException) : ApiResult<Nothing>

    /** 설정 누락(서버 주소·토큰 없음). 네트워크를 타지 않는다. */
    data class NotConfigured(val what: String) : ApiResult<Nothing>
}

/** 수집 전송 결과에 따른 다음 행동. */
enum class UploadDecision {
    /** 저장됨(새로 또는 중복). 대기열에서 지우고 알림을 제거한다. */
    SENT,

    /** 일시적 문제(네트워크, 5xx, 429). 나중에 다시 보낸다. */
    RETRY,

    /** 토큰이 틀렸거나 폐기됨. 사용자 조치가 필요하다. 대기열은 유지한다. */
    AUTH_FAILED,

    /** 서버가 형식 오류로 거부(4xx). 다시 보내도 같다. 실패 상태로 남겨둔다(삭제하지 않음). */
    REJECTED,
}

fun ApiResult<*>.toUploadDecision(): UploadDecision = when (this) {
    is ApiResult.Ok -> UploadDecision.SENT
    is ApiResult.NetworkError -> UploadDecision.RETRY
    is ApiResult.NotConfigured -> UploadDecision.AUTH_FAILED
    is ApiResult.HttpError -> when {
        code == 401 || code == 403 -> UploadDecision.AUTH_FAILED
        code == 408 || code == 429 || code >= 500 -> UploadDecision.RETRY
        else -> UploadDecision.REJECTED
    }
}

fun ApiResult<*>.describe(): String = when (this) {
    is ApiResult.Ok -> "HTTP $code"
    is ApiResult.HttpError -> "HTTP $code ${message.take(120)}"
    is ApiResult.NetworkError -> "네트워크 오류: ${cause.message ?: cause.javaClass.simpleName}"
    is ApiResult.NotConfigured -> "설정 필요: $what"
}
