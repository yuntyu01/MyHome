package com.yuntyu.myhome.security

import android.content.Context
import android.security.keystore.KeyGenParameterSpec
import android.security.keystore.KeyProperties
import android.util.Base64
import androidx.core.content.edit
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import java.security.KeyStore
import javax.crypto.Cipher
import javax.crypto.KeyGenerator
import javax.crypto.SecretKey
import javax.crypto.spec.GCMParameterSpec

/**
 * 기기 토큰을 Android Keystore의 AES-256-GCM 키로 암호화해서 저장한다.
 * 키는 Keystore 밖으로 나오지 않는다. 암호문과 IV만 SharedPreferences에 둔다.
 */
class TokenStore(context: Context) {
    private val prefs = context.getSharedPreferences("secure_token", Context.MODE_PRIVATE)
    private val _hasToken = MutableStateFlow(prefs.contains(KEY_CIPHER))
    val hasToken: StateFlow<Boolean> = _hasToken.asStateFlow()

    @Volatile private var cached: String? = null

    fun save(token: String) {
        val cipher = Cipher.getInstance(TRANSFORMATION)
        cipher.init(Cipher.ENCRYPT_MODE, key())
        val ct = cipher.doFinal(token.toByteArray(Charsets.UTF_8))
        prefs.edit {
            putString(KEY_CIPHER, Base64.encodeToString(ct, Base64.NO_WRAP))
            putString(KEY_IV, Base64.encodeToString(cipher.iv, Base64.NO_WRAP))
        }
        cached = token
        _hasToken.value = true
    }

    fun read(): String? {
        cached?.let { return it }
        val ct = prefs.getString(KEY_CIPHER, null) ?: return null
        val iv = prefs.getString(KEY_IV, null) ?: return null
        return try {
            val cipher = Cipher.getInstance(TRANSFORMATION)
            cipher.init(Cipher.DECRYPT_MODE, key(), GCMParameterSpec(128, Base64.decode(iv, Base64.NO_WRAP)))
            String(cipher.doFinal(Base64.decode(ct, Base64.NO_WRAP)), Charsets.UTF_8).also { cached = it }
        } catch (e: Exception) {
            // 키가 사라졌거나(앱 데이터 복원 등) 손상됨: 토큰을 다시 받아야 한다.
            null
        }
    }

    fun clear() {
        prefs.edit { clear() }
        cached = null
        _hasToken.value = false
    }

    private fun key(): SecretKey {
        val ks = KeyStore.getInstance(ANDROID_KEYSTORE).apply { load(null) }
        (ks.getEntry(ALIAS, null) as? KeyStore.SecretKeyEntry)?.let { return it.secretKey }
        val gen = KeyGenerator.getInstance(KeyProperties.KEY_ALGORITHM_AES, ANDROID_KEYSTORE)
        gen.init(
            KeyGenParameterSpec.Builder(ALIAS, KeyProperties.PURPOSE_ENCRYPT or KeyProperties.PURPOSE_DECRYPT)
                .setBlockModes(KeyProperties.BLOCK_MODE_GCM)
                .setEncryptionPaddings(KeyProperties.ENCRYPTION_PADDING_NONE)
                .setKeySize(256)
                .build(),
        )
        return gen.generateKey()
    }

    private companion object {
        const val ANDROID_KEYSTORE = "AndroidKeyStore"
        const val ALIAS = "myhome_device_token"
        const val TRANSFORMATION = "AES/GCM/NoPadding"
        const val KEY_CIPHER = "token_ct"
        const val KEY_IV = "token_iv"
    }
}
