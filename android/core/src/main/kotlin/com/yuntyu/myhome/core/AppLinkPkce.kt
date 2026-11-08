package com.yuntyu.myhome.core

import java.net.URLEncoder
import java.security.MessageDigest
import java.security.SecureRandom
import java.util.Base64

/**
 * 앱 연결(구글 로그인 → 앱 복귀)의 PKCE(RFC 7636, S256).
 * 앱이 verifier를 만들어 폰에만 두고, 브라우저에는 그 해시(challenge)만 보낸다. 서버가 돌려준 1회용 코드는
 * verifier가 있어야 기기 토큰으로 바뀌므로, 다른 앱이 myhome://auth 주소를 가로채도 쓸 수 없다.
 */
object AppLinkPkce {
    /** 앱이 서버를 여는 주소의 경로와 서버가 돌려보내는 주소(서버에 고정돼 있다) */
    const val LINK_PATH = "/app/link"
    const val REDIRECT_SCHEME = "myhome"
    const val REDIRECT_HOST = "auth"

    private val random = SecureRandom()
    private val b64 = Base64.getUrlEncoder().withoutPadding()

    private fun randomB64(bytes: Int): String = ByteArray(bytes).also(random::nextBytes).let(b64::encodeToString)

    /** 43자(32바이트) verifier */
    fun newVerifier(): String = randomB64(32)

    fun newState(): String = randomB64(24)

    fun challengeOf(verifier: String): String =
        b64.encodeToString(MessageDigest.getInstance("SHA-256").digest(verifier.toByteArray(Charsets.US_ASCII)))

    fun linkUrl(serverUrl: String, challenge: String, state: String): String =
        serverUrl.trimEnd('/') + LINK_PATH + "?challenge=" + URLEncoder.encode(challenge, "UTF-8") + "&state=" + URLEncoder.encode(state, "UTF-8")
}
