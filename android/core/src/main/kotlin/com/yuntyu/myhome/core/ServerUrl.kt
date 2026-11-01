package com.yuntyu.myhome.core

import okhttp3.HttpUrl
import okhttp3.HttpUrl.Companion.toHttpUrlOrNull

/** 서버 주소 입력값 검사·정규화. */
object ServerUrl {
    /**
     * 사용자가 입력한 서버 주소를 정규화한다. 끝의 `/`를 지우고, 스킴이 없으면 https를 붙인다.
     * 토큰이 평문으로 나가지 않도록 https만 허용한다. 예외: 같은 PC·에뮬레이터·사설망 개발 서버.
     * @return 정규화된 주소. 형식이 틀리거나 허용되지 않으면 null.
     */
    fun normalize(input: String): String? {
        var s = input.trim()
        if (s.isEmpty()) return null
        if (!s.contains("://")) s = "https://$s"
        val url: HttpUrl = s.toHttpUrlOrNull() ?: return null
        if (url.scheme == "http" && !isLocalDev(url.host)) return null
        return url.toString().trimEnd('/')
    }

    private fun isLocalDev(host: String): Boolean =
        host == "localhost" || host == "127.0.0.1" || host == "10.0.2.2" ||
            host.startsWith("192.168.") || host.startsWith("10.")
}
