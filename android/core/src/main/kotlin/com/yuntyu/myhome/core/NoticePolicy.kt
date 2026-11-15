package com.yuntyu.myhome.core

/**
 * 원래 결제 알림을 언제 지울지(사용자 요청, 2026-11-15): 가계부가 알아본 결제만 지운다.
 * 서버가 원문마다 dismiss(지움)·wait(아직 모름: 처리 전·AI 판정 전)·keep(남김: 못 읽음·결제 아님·분류 모름)을 준다.
 * wait·keep은 붙들어 두고 다시 묻는다: AI 판정을 기다리는 동안은 자주, 그 뒤엔 주기 작업 때.
 * keep도 사용자가 분류하면 dismiss가 되므로 그때 지운다. 사용자가 알림을 치웠거나 오래되면 잊는다.
 */
object NoticePolicy {
    const val DISMISS = "dismiss"
    const val WAIT = "wait"
    const val KEEP = "keep"

    /** AI 판정을 자주 다시 묻는 기간(전송부터). 보통 몇십 초 안에 나지만 AI가 막히면 이 뒤로는 주기 작업 때만 묻는다. */
    const val FAST_WINDOW_MS = 15 * 60 * 1000L

    /** 이보다 오래 붙든 알림은 잊는다(서버의 "읽지 못한 알림"도 7일만 센다). */
    const val FORGET_AFTER_MS = 3 * 24 * 60 * 60 * 1000L

    /** 한 번에 묻는 원문 수(서버 상한과 같다) */
    const val MAX_IDS = 100

    /**
     * 앱마다 지우기 방식(사용자 요청, 2026-11-15: 고른 앱을 묶음으로 나눠 끌어 넣기).
     * ALL: 보낸 알림은 읽든 못 읽든 다 지움(광고 많은 앱 등), KNOWN: 가계부가 알아본 결제만(기본), NONE: 안 지움.
     */
    enum class Mode(val key: String, val label: String) {
        ALL("all", "다 지움"),
        KNOWN("known", "알아본 것만"),
        NONE("none", "안 지움"),
        ;

        companion object {
            fun of(key: String?): Mode? = entries.firstOrNull { it.key == key }
        }
    }

    /** 보낸 뒤 할 일: 지금 지움 / 붙들고 서버에 다시 물음 / 그대로 둠 */
    enum class Action { DISMISS, HOLD, LEAVE }

    /** 그 앱의 방식과 서버 notice로 보낸 뒤 할 일을 정한다. */
    fun afterSend(mode: Mode, notice: String?): Action = when (mode) {
        Mode.NONE -> Action.LEAVE
        Mode.ALL -> Action.DISMISS
        Mode.KNOWN -> if (dismissNow(notice)) Action.DISMISS else Action.HOLD
    }

    /** 수집 응답의 notice로 바로 지울지. 비어 있으면 예전 서버라서 예전처럼 바로 지운다. */
    fun dismissNow(notice: String?): Boolean = notice.isNullOrEmpty() || notice == DISMISS

    data class Held(val sentAt: Long, val state: String)

    /** AI 판정을 기다리는 알림이 아직 FAST_WINDOW 안에 있으면 곧 다시 묻는다. */
    fun recheckSoon(held: List<Held>, now: Long): Boolean =
        held.any { it.state == WAIT && now - it.sentAt < FAST_WINDOW_MS }

    fun forget(sentAt: Long, now: Long): Boolean = now - sentAt > FORGET_AFTER_MS
}
