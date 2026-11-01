package com.yuntyu.myhome.data

import android.content.Context
import androidx.core.content.edit
import com.yuntyu.myhome.MyHomeApp
import com.yuntyu.myhome.core.api.ApiResult
import com.yuntyu.myhome.core.api.MyHomeJson
import com.yuntyu.myhome.core.api.Summary
import com.yuntyu.myhome.core.api.describe
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.withContext

/**
 * 이번 기간 요약. 마지막 응답을 기기에 저장해서 위젯과 앱이 오프라인에서도 바로 그린다.
 * 저장하는 건 요약 숫자와 최근 몇 건의 가맹점·금액뿐이다(앱 백업에서 제외됨).
 */
class SummaryRepository(context: Context) {
    private val prefs = context.getSharedPreferences("summary_cache", Context.MODE_PRIVATE)

    data class State(val summary: Summary?, val updatedAt: Long, val error: String?)

    private val _state = MutableStateFlow(State(loadCached(), prefs.getLong(KEY_AT, 0), null))
    val state: StateFlow<State> = _state.asStateFlow()

    fun cached(): Summary? = _state.value.summary

    private fun loadCached(): Summary? = prefs.getString(KEY_JSON, null)?.let {
        runCatching { MyHomeJson.decodeFromString(Summary.serializer(), it) }.getOrNull()
    }

    /** 서버에서 요약을 다시 받는다. 실패하면 캐시를 유지하고 오류만 기록한다. */
    suspend fun refresh(): ApiResult<Summary> = withContext(Dispatchers.IO) {
        val r = MyHomeApp.instance.api.summary(recent = 10)
        when (r) {
            is ApiResult.Ok -> {
                val now = System.currentTimeMillis()
                prefs.edit {
                    putString(KEY_JSON, MyHomeJson.encodeToString(Summary.serializer(), r.value))
                    putLong(KEY_AT, now)
                }
                _state.value = State(r.value, now, null)
            }
            else -> _state.value = _state.value.copy(error = r.describe())
        }
        r
    }

    private companion object {
        const val KEY_JSON = "json"
        const val KEY_AT = "at"
    }
}
