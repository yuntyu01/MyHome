package com.yuntyu.myhome.auth

import android.content.Context
import android.os.Build
import com.kakao.sdk.auth.model.OAuthToken
import com.kakao.sdk.common.model.ClientError
import com.kakao.sdk.common.model.ClientErrorCause
import com.kakao.sdk.user.UserApiClient
import com.yuntyu.myhome.BuildConfig
import com.yuntyu.myhome.MyHomeApp
import com.yuntyu.myhome.core.api.ApiResult
import com.yuntyu.myhome.core.api.describe
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.suspendCancellableCoroutine
import kotlinx.coroutines.withContext
import kotlin.coroutines.resume
import kotlin.coroutines.resumeWithException

/**
 * 카카오 로그인 → 서버가 카카오 토큰으로 회원번호 검증 → 기기 토큰 발급 → Keystore 저장.
 * 카카오 액세스 토큰은 서버로 한 번 보내고 버린다(앱에 보관하지 않음).
 */
object KakaoLogin {
    val available: Boolean get() = BuildConfig.KAKAO_NATIVE_APP_KEY.isNotBlank()

    sealed interface Outcome {
        data object Success : Outcome
        data object Cancelled : Outcome
        data class Failed(val message: String) : Outcome
    }

    /** @param context Activity 컨텍스트(카카오 로그인 화면을 띄운다) */
    suspend fun login(context: Context): Outcome {
        if (!available) return Outcome.Failed("카카오 네이티브 앱 키가 빌드에 없습니다(local.properties kakao.nativeAppKey)")
        val app = MyHomeApp.instance
        if (app.settings.serverUrl == null) return Outcome.Failed("서버 주소를 먼저 저장하세요")

        val kakaoToken = try {
            kakaoAccessToken(context)
        } catch (e: CancellationException) {
            throw e
        } catch (e: ClientError) {
            if (e.reason == ClientErrorCause.Cancelled) return Outcome.Cancelled
            return Outcome.Failed("카카오 로그인 실패: ${e.msg}")
        } catch (e: Throwable) {
            return Outcome.Failed("카카오 로그인 실패: ${e.message}")
        }

        val deviceName = "${Build.MANUFACTURER} ${Build.MODEL}".trim().take(60)
        return when (val r = withContext(Dispatchers.IO) { app.api.kakaoDeviceLogin(kakaoToken, deviceName) }) {
            is ApiResult.Ok -> {
                withContext(Dispatchers.IO) { app.tokenStore.save(r.value.token) }
                app.settings.authProblem = null
                Outcome.Success
            }
            is ApiResult.HttpError -> if (r.code == 403) {
                Outcome.Failed("허용되지 않은 카카오 계정입니다. 서버 ALLOWED_KAKAO_IDS에 본인 회원번호를 넣으세요. (${r.message})")
            } else {
                Outcome.Failed("서버 오류: ${r.describe()}")
            }
            else -> Outcome.Failed(r.describe())
        }
    }

    /** 카카오톡이 있으면 카카오톡으로, 없거나 실패하면 카카오계정(웹)으로 로그인한다. */
    private suspend fun kakaoAccessToken(context: Context): String {
        val client = UserApiClient.instance
        if (client.isKakaoTalkLoginAvailable(context)) {
            try {
                return suspendLogin { cb -> client.loginWithKakaoTalk(context, callback = cb) }
            } catch (e: CancellationException) {
                throw e
            } catch (e: ClientError) {
                // 사용자가 카카오톡 로그인 화면에서 직접 취소하면 계정 로그인으로 넘어가지 않는다.
                if (e.reason == ClientErrorCause.Cancelled) throw e
            } catch (_: Throwable) {
                // 카카오톡에 연결된 계정이 없는 경우 등: 카카오계정 로그인으로 재시도
            }
        }
        return suspendLogin { cb -> client.loginWithKakaoAccount(context, callback = cb) }
    }

    private suspend fun suspendLogin(start: ((OAuthToken?, Throwable?) -> Unit) -> Unit): String =
        suspendCancellableCoroutine { cont ->
            start { token, error ->
                when {
                    error != null -> if (cont.isActive) cont.resumeWithException(error)
                    token != null -> if (cont.isActive) cont.resume(token.accessToken)
                    else -> if (cont.isActive) cont.resumeWithException(IllegalStateException("토큰 없음"))
                }
            }
        }
}
