package com.yuntyu.myhome.core

import java.nio.ByteBuffer
import java.security.MessageDigest

/**
 * 로컬 대기열 중복 방지 키. 같은 알림이 여러 번 전달돼도(onNotificationPosted 재호출) 한 번만 넣는다.
 * 서버의 content_hash와 같은 재료(패키지·게시시각·본문)를 쓴다.
 */
object DedupeKey {
    fun of(packageName: String, postedAt: Long, title: String, text: String, bigText: String): String {
        val md = MessageDigest.getInstance("SHA-256")
        fun part(b: ByteArray) {
            md.update(ByteBuffer.allocate(4).putInt(b.size).array())
            md.update(b)
        }
        part(packageName.toByteArray())
        part(ByteBuffer.allocate(8).putLong(postedAt).array())
        part(title.toByteArray())
        part(text.toByteArray())
        part(bigText.toByteArray())
        return md.digest().joinToString("") { "%02x".format(it) }
    }
}
