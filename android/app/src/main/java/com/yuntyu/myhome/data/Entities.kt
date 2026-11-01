package com.yuntyu.myhome.data

import androidx.room.Entity
import androidx.room.Index
import androidx.room.PrimaryKey

/** 서버로 보낼 알림 원문 대기열. 전송에 성공하면 지운다. */
@Entity(tableName = "pending_events", indices = [Index(value = ["dedupeKey"], unique = true)])
data class PendingEvent(
    @PrimaryKey(autoGenerate = true) val id: Long = 0,
    val packageName: String,
    val title: String,
    val text: String,
    val bigText: String,
    /** StatusBarNotification.postTime (Unix ms) */
    val postedAt: Long,
    /** 전송 성공 후 알림을 지우기 위한 StatusBarNotification.key */
    val notificationKey: String,
    val dedupeKey: String,
    val createdAt: Long,
    val attempts: Int = 0,
    val lastError: String? = null,
    /** PENDING: 전송 대기, REJECTED: 서버가 형식 오류로 거부(보존, 수동 확인) */
    val state: String = STATE_PENDING,
) {
    companion object {
        const val STATE_PENDING = "PENDING"
        const val STATE_REJECTED = "REJECTED"
    }
}

/** 전송 로그. 최근 50건만 남긴다. 본문은 저장하지 않는다. */
@Entity(tableName = "send_log")
data class SendLogEntry(
    @PrimaryKey(autoGenerate = true) val id: Long = 0,
    val at: Long,
    val packageName: String,
    val success: Boolean,
    val message: String,
)

/** "최근 알림" 화면용. 어떤 앱이 알림을 보내는지 보여주고 수집 대상을 고르게 한다. 기기 안에만 저장. */
@Entity(tableName = "recent_apps")
data class RecentApp(
    @PrimaryKey val packageName: String,
    val appLabel: String,
    val lastTitle: String,
    val lastSeenAt: Long,
    val count: Int,
)
