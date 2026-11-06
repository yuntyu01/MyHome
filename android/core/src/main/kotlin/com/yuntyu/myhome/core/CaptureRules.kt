package com.yuntyu.myhome.core

/**
 * 어떤 알림을 수집할지 결정한다. 알림 내용은 해석하지 않는다(해석은 서버 담당).
 * 패키지명은 하드코딩하지 않고, 사용자가 "최근 알림" 화면에서 고른 목록만 쓴다.
 */
object CaptureRules {
    /** 앱이 스스로 띄우는 테스트 알림 표시. 서버는 이 제목이 붙은 원문을 거래로 만들지 않는다. */
    const val TEST_TITLE_MARKER = "[MyHome 테스트]"

    data class Candidate(
        val packageName: String,
        val isOngoing: Boolean,
        val isGroupSummary: Boolean,
        val title: String,
        val text: String,
        val bigText: String,
    )

    /**
     * "최근 알림" 목록에 남길지. 고르기 전의 앱도 보여야 하므로 선택 여부는 보지 않는다.
     * 묶음 요약은 안드로이드가 알림이 쌓이면 제목 없이 따로 만드는 알림이라, 넣으면 건수가 늘고 마지막 제목이 빈칸이 된다.
     */
    fun shouldList(c: Candidate, ownPackage: String): Boolean {
        if (c.packageName == ownPackage || c.isOngoing || c.isGroupSummary) return false
        return !(c.title.isBlank() && c.text.isBlank() && c.bigText.isBlank())
    }

    fun shouldCapture(c: Candidate, selectedPackages: Set<String>, ownPackage: String): Boolean {
        // 진행 중 알림(음악, 다운로드 등)과 묶음 요약 알림은 결제 정보가 아니다.
        if (c.isOngoing || c.isGroupSummary) return false
        if (c.title.isBlank() && c.text.isBlank() && c.bigText.isBlank()) return false
        if (c.packageName == ownPackage) return c.title.startsWith(TEST_TITLE_MARKER)
        return c.packageName in selectedPackages
    }
}
