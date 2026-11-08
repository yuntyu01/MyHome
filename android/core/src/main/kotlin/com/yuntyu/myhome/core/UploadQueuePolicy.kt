package com.yuntyu.myhome.core

/**
 * 전송 대기열 규칙. 맨 앞 항목 하나가 계속 5xx로 실패해도 뒤 항목이 막히지 않게 한다.
 * - 일시 오류가 STUCK_ATTEMPTS번 미만이면: 이번 전송을 멈추고 나중에 재시도(서버가 잠깐 죽은 경우, 순서 유지).
 * - STUCK_ATTEMPTS번 이상 실패한 항목은 대기열에 남겨 두고(다음 전송 때 다시 시도) 뒤 항목으로 넘어간다.
 * - 한 번 전송에서 연달아 MAX_CONSECUTIVE_FAILURES번 실패하면 서버 전체 문제로 보고 멈춘다(요청 폭주 방지).
 */
object UploadQueuePolicy {
    const val STUCK_ATTEMPTS = 5
    const val MAX_CONSECUTIVE_FAILURES = 3

    /**
     * 일시 오류 뒤 이 항목을 건너뛰고 다음 항목을 보낼지.
     * @param attempts 이번 실패까지 센 이 항목의 시도 횟수
     * @param consecutiveFailures 이번 전송에서 이번 실패까지 연달아 실패한 횟수
     */
    fun skipAfterRetry(attempts: Int, consecutiveFailures: Int): Boolean =
        attempts >= STUCK_ATTEMPTS && consecutiveFailures < MAX_CONSECUTIVE_FAILURES
}
