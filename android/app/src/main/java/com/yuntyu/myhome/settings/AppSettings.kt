package com.yuntyu.myhome.settings

import android.content.Context
import androidx.core.content.edit
import com.yuntyu.myhome.BuildConfig
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow

/**
 * 비밀이 아닌 앱 설정. 토큰은 여기 두지 않고 TokenStore(Keystore 암호화)에 둔다.
 * 화면이 바로 반영되도록 StateFlow로도 노출한다.
 */
class AppSettings(context: Context) {
    private val prefs = context.getSharedPreferences("settings", Context.MODE_PRIVATE)

    data class Snapshot(
        val serverUrl: String?,
        val selectedPackages: Set<String>,
        val autoDelete: Boolean,
        val budgetAlerts: Boolean,
    )

    private val _state = MutableStateFlow(load())
    val state: StateFlow<Snapshot> = _state.asStateFlow()

    private fun load() = Snapshot(
        serverUrl = prefs.getString(KEY_SERVER_URL, null) ?: BuildConfig.DEFAULT_SERVER_URL.ifBlank { null },
        selectedPackages = prefs.getStringSet(KEY_PACKAGES, emptySet())?.toSet() ?: emptySet(),
        autoDelete = prefs.getBoolean(KEY_AUTO_DELETE, true),
        budgetAlerts = prefs.getBoolean(KEY_BUDGET_ALERTS, true),
    )

    private fun refresh() {
        _state.value = load()
    }

    val serverUrl: String? get() = _state.value.serverUrl
    val selectedPackages: Set<String> get() = _state.value.selectedPackages
    val autoDelete: Boolean get() = _state.value.autoDelete
    val budgetAlerts: Boolean get() = _state.value.budgetAlerts

    fun setServerUrl(url: String?) {
        prefs.edit { if (url == null) remove(KEY_SERVER_URL) else putString(KEY_SERVER_URL, url) }
        refresh()
    }

    fun setPackageSelected(pkg: String, selected: Boolean) {
        val next = selectedPackages.toMutableSet().apply { if (selected) add(pkg) else remove(pkg) }
        prefs.edit { putStringSet(KEY_PACKAGES, next) }
        refresh()
    }

    fun setAutoDelete(on: Boolean) {
        prefs.edit { putBoolean(KEY_AUTO_DELETE, on) }
        refresh()
    }

    fun setBudgetAlerts(on: Boolean) {
        prefs.edit { putBoolean(KEY_BUDGET_ALERTS, on) }
        refresh()
    }

    /** 마지막 인증 실패 메시지(토큰 폐기 등). 홈 화면 경고용. */
    var authProblem: String?
        get() = prefs.getString(KEY_AUTH_PROBLEM, null)
        set(v) = prefs.edit { if (v == null) remove(KEY_AUTH_PROBLEM) else putString(KEY_AUTH_PROBLEM, v) }

    private companion object {
        const val KEY_SERVER_URL = "server_url"
        const val KEY_PACKAGES = "selected_packages"
        const val KEY_AUTO_DELETE = "auto_delete"
        const val KEY_BUDGET_ALERTS = "budget_alerts"
        const val KEY_AUTH_PROBLEM = "auth_problem"
    }
}
