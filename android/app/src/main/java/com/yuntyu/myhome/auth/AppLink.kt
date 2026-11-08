package com.yuntyu.myhome.auth

import android.content.Context
import android.content.Intent
import android.net.Uri
import android.os.Build
import com.yuntyu.myhome.MyHomeApp
import com.yuntyu.myhome.core.AppLinkPkce
import com.yuntyu.myhome.core.api.ApiResult
import com.yuntyu.myhome.core.api.describe
import com.yuntyu.myhome.work.WorkScheduler
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.withContext

/**
 * 앱 연결: 브라우저에서 서버 /app/link를 연다 → Cloudflare Access 로그인(이메일 코드) → 서버가 myhome://auth?code&state로
 * 앱을 다시 연다 → 앱이 코드와 PKCE verifier로 기기 토큰을 받는다. 주소에는 1회용 코드만 실리고 토큰은 실리지 않는다.
 * verifier와 state는 브라우저에 다녀오는 동안 앱이 꺼져도 남도록 SharedPreferences에 둔다(10분 유효).
 */
object AppLink {
    private const val PREFS = "app_link"
    private const val TTL_MS = 10 * 60_000L

    private val _message = MutableStateFlow<String?>(null)
    /** 설정 화면에 보여 줄 마지막 결과 */
    val message: StateFlow<String?> = _message.asStateFlow()

    /** 브라우저를 연다. 서버 주소가 없으면 false. */
    fun start(context: Context): Boolean {
        val server = MyHomeApp.instance.settings.serverUrl?.takeIf { it.isNotBlank() } ?: return false
        val verifier = AppLinkPkce.newVerifier()
        val state = AppLinkPkce.newState()
        context.getSharedPreferences(PREFS, Context.MODE_PRIVATE).edit()
            .putString("verifier", verifier).putString("state", state).putLong("at", System.currentTimeMillis()).apply()
        _message.value = "브라우저에서 이메일 코드로 로그인하면 앱으로 돌아와요."
        val url = AppLinkPkce.linkUrl(server, AppLinkPkce.challengeOf(verifier), state)
        context.startActivity(Intent(Intent.ACTION_VIEW, Uri.parse(url)).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK))
        return true
    }

    fun isRedirect(uri: Uri?): Boolean =
        uri != null && uri.scheme == AppLinkPkce.REDIRECT_SCHEME && uri.host == AppLinkPkce.REDIRECT_HOST

    /** myhome://auth?code&state를 받아 기기 토큰으로 바꾼다. state가 다르거나 오래됐으면 버린다. */
    suspend fun complete(context: Context, uri: Uri): String {
        val prefs = context.getSharedPreferences(PREFS, Context.MODE_PRIVATE)
        val verifier = prefs.getString("verifier", null)
        val state = prefs.getString("state", null)
        val at = prefs.getLong("at", 0)
        // 한 번 쓴 verifier는 성공·실패와 상관없이 지운다(서버도 코드를 한 번만 받는다)
        prefs.edit().clear().apply()
        val code = uri.getQueryParameter("code")
        val msg = when {
            verifier == null || state == null || System.currentTimeMillis() - at > TTL_MS ->
                "연결 요청이 없거나 오래됐어요. 설정에서 다시 연결해 주세요."
            uri.getQueryParameter("state") != state || code.isNullOrBlank() ->
                "이 앱에서 시작한 연결이 아니어서 무시했어요."
            else -> {
                val app = MyHomeApp.instance
                val name = "${Build.MANUFACTURER} ${Build.MODEL}".trim().take(60)
                when (val r = withContext(Dispatchers.IO) { app.api.exchangeAppLink(code, verifier, name) }) {
                    is ApiResult.Ok -> {
                        withContext(Dispatchers.IO) { app.tokenStore.save(r.value.token) }
                        app.settings.authProblem = null
                        WorkScheduler.flushNow(context)
                        "연결했어요. 이 폰 전용 토큰을 저장했어요."
                    }
                    is ApiResult.HttpError -> if (r.code == 401) "연결 코드가 맞지 않거나 만료됐어요. 다시 연결해 주세요." else "서버 오류: ${r.describe()}"
                    else -> r.describe()
                }
            }
        }
        _message.value = msg
        return msg
    }
}
