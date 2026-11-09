package com.yuntyu.myhome.focus

import android.annotation.SuppressLint
import android.app.PendingIntent
import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import android.location.Location
import android.os.Build
import androidx.core.content.edit
import com.google.android.gms.common.api.ApiException
import com.google.android.gms.location.Geofence
import com.google.android.gms.location.GeofenceStatusCodes
import com.google.android.gms.location.GeofencingEvent
import com.google.android.gms.location.GeofencingRequest
import com.google.android.gms.location.LocationServices
import com.yuntyu.myhome.MyHomeApp
import com.yuntyu.myhome.core.FocusSubjects
import com.yuntyu.myhome.core.StayRules
import com.yuntyu.myhome.data.FocusStay
import com.yuntyu.myhome.health.GymGeofence
import kotlinx.coroutines.launch
import org.json.JSONArray
import org.json.JSONObject
import java.util.UUID

/**
 * 공부 장소에 머문 시간(지오펜스 들어옴~나감). 순공 시간(타이머)과 견주어 "머문 5시간 중 순공 3시간"을 보여 준다.
 * 장소에 있을 때 "지금 위치를 공부 장소로"를 눌러 좌표를 저장한다. 좌표는 이 폰에만 두고, 서버에는 장소 이름과 머문 시각만 간다.
 *
 * 헬스장 자동 출석(GymGeofence)과 같은 권한("항상 허용")을 쓰지만 지오펜스 묶음(PendingIntent)은 따로다:
 * removeGeofences(PendingIntent)는 그 PendingIntent로 건 것을 모두 지우므로, 같이 쓰면 한쪽을 끌 때 다른 쪽도 꺼진다.
 *
 * 들어와 있는 동안(나감 전)은 설정에 들고 있다가 나갈 때 한 줄로 남긴다(StayRules: 3분 미만 버림, 5분 안에 다시 들어오면 이어 감,
 * 16시간 넘으면 나감을 놓친 것으로 보고 버림). 지오펜스는 위치를 끄면 지워져서 앱 시작·재부팅·30분 주기에 다시 건다.
 */
object StudyPlaces {
    private const val PREFS = "study_places"
    private const val FENCE_PREFIX = "study:"
    const val DEFAULT_RADIUS = 150
    const val MAX_PLACES = 10

    data class Place(val id: String, val name: String, val lat: Double, val lng: Double, val radiusM: Int)

    /** 지금 들어와 있는 곳. resumed면 이미 남긴 줄(stayId)을 이어 가는 중이다 */
    data class Open(val placeId: String, val name: String, val stayId: String, val enteredAt: Long, val resumed: Boolean)

    private fun prefs(ctx: Context) = ctx.getSharedPreferences(PREFS, Context.MODE_PRIVATE)

    fun places(ctx: Context): List<Place> {
        val raw = prefs(ctx).getString("places", null) ?: return emptyList()
        return runCatching {
            val a = JSONArray(raw)
            (0 until a.length()).map { i ->
                val o = a.getJSONObject(i)
                Place(o.getString("id"), o.getString("name"), o.getDouble("lat"), o.getDouble("lng"), o.optInt("radius", DEFAULT_RADIUS))
            }
        }.getOrDefault(emptyList())
    }

    private fun savePlaces(ctx: Context, list: List<Place>) {
        val a = JSONArray()
        for (p in list) a.put(JSONObject().put("id", p.id).put("name", p.name).put("lat", p.lat).put("lng", p.lng).put("radius", p.radiusM))
        prefs(ctx).edit { putString("places", a.toString()) }
    }

    /** 지금 위치를 공부 장소로. 이름이 비었거나 너무 길면 null */
    fun add(ctx: Context, name: String, loc: Location): Place? {
        val clean = FocusSubjects.cleanName(name) ?: return null
        val list = places(ctx)
        if (list.size >= MAX_PLACES) return null
        val p = Place(UUID.randomUUID().toString(), clean, loc.latitude, loc.longitude, DEFAULT_RADIUS)
        savePlaces(ctx, list + p)
        register(ctx)
        return p
    }

    fun rename(ctx: Context, id: String, name: String): Boolean {
        val clean = FocusSubjects.cleanName(name) ?: return false
        savePlaces(ctx, places(ctx).map { if (it.id == id) it.copy(name = clean) else it })
        return true
    }

    fun setRadius(ctx: Context, id: String, radiusM: Int) {
        savePlaces(ctx, places(ctx).map { if (it.id == id) it.copy(radiusM = radiusM) else it })
        register(ctx)
    }

    /** 장소를 지운다. 들어와 있던 중이면 지금으로 나감을 남긴다 */
    suspend fun remove(ctx: Context, id: String) {
        onExit(ctx, id, System.currentTimeMillis())
        savePlaces(ctx, places(ctx).filterNot { it.id == id })
        register(ctx)
    }

    fun enabled(ctx: Context) = prefs(ctx).getBoolean("enabled", false)

    /** 자동 기록 켜기·끄기. 끄면 들어와 있던 곳은 지금으로 나감을 남긴다 */
    suspend fun setEnabled(ctx: Context, on: Boolean) {
        prefs(ctx).edit { putBoolean("enabled", on) }
        if (!on) {
            val now = System.currentTimeMillis()
            for (o in openStays(ctx)) onExit(ctx, o.placeId, now)
        }
        register(ctx)
    }

    fun registrationError(ctx: Context): String? = prefs(ctx).getString("reg_error", null)

    private fun setRegistrationError(ctx: Context, why: String?) = prefs(ctx).edit {
        if (why == null) remove("reg_error") else putString("reg_error", why)
    }

    private fun errorText(code: Int): String = when (code) {
        GeofenceStatusCodes.GEOFENCE_NOT_AVAILABLE -> "위치가 꺼져 있어서 머문 시간 기록이 멈췄어요. 위치를 켜면 다시 걸어요"
        GeofenceStatusCodes.GEOFENCE_INSUFFICIENT_LOCATION_PERMISSION -> "위치 권한이 모자라서 머문 시간 기록을 걸지 못했어요"
        else -> "머문 시간 기록을 걸지 못했어요 (${GeofenceStatusCodes.getStatusCodeString(code)})"
    }

    fun onError(ctx: Context, code: Int) = setRegistrationError(ctx, errorText(code))

    private fun pending(ctx: Context): PendingIntent = PendingIntent.getBroadcast(
        ctx,
        8,
        Intent(ctx, StudyGeofenceReceiver::class.java),
        // Play 서비스가 전환 정보를 채워 넣으므로 MUTABLE이어야 한다
        PendingIntent.FLAG_UPDATE_CURRENT or (if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.S) PendingIntent.FLAG_MUTABLE else 0),
    )

    /** 켜져 있고 권한이 있으면 장소마다 지오펜스를 (다시) 건다. 이미 들어와 있으면 곧바로 들어옴이 온다 */
    @SuppressLint("MissingPermission")
    fun register(ctx: Context, onResult: ((String?) -> Unit)? = null) {
        val list = places(ctx)
        if (list.isEmpty() || !enabled(ctx) || !GymGeofence.hasForeground(ctx) || !GymGeofence.hasBackground(ctx)) {
            runCatching { LocationServices.getGeofencingClient(ctx).removeGeofences(pending(ctx)) }
            setRegistrationError(ctx, null) // 꺼짐·권한 없음은 화면이 따로 알린다
            onResult?.invoke(null)
            return
        }
        val req = GeofencingRequest.Builder()
            .setInitialTrigger(GeofencingRequest.INITIAL_TRIGGER_ENTER)
            .addGeofences(
                list.map {
                    Geofence.Builder()
                        .setRequestId(FENCE_PREFIX + it.id)
                        .setCircularRegion(it.lat, it.lng, it.radiusM.toFloat())
                        .setExpirationDuration(Geofence.NEVER_EXPIRE)
                        .setTransitionTypes(Geofence.GEOFENCE_TRANSITION_ENTER or Geofence.GEOFENCE_TRANSITION_EXIT)
                        .build()
                },
            )
            .build()
        val client = LocationServices.getGeofencingClient(ctx)
        runCatching {
            // 지운 장소가 남지 않게 통째로 지우고 다시 건다
            client.removeGeofences(pending(ctx)).addOnCompleteListener {
                client.addGeofences(req, pending(ctx))
                    .addOnSuccessListener { setRegistrationError(ctx, null); onResult?.invoke(null) }
                    .addOnFailureListener { e ->
                        val why = (e as? ApiException)?.let { errorText(it.statusCode) } ?: "머문 시간 기록을 걸지 못했어요 (${e.message})"
                        setRegistrationError(ctx, why)
                        onResult?.invoke(why)
                    }
            }
        }.onFailure { e ->
            val why = "머문 시간 기록을 걸지 못했어요 (${e.message})"
            setRegistrationError(ctx, why)
            onResult?.invoke(why)
        }
    }

    fun openStays(ctx: Context): List<Open> {
        val p = prefs(ctx)
        return p.all.keys.filter { it.startsWith("open:") }.mapNotNull { k ->
            val parts = p.getString(k, null)?.split('|') ?: return@mapNotNull null
            if (parts.size < 4) return@mapNotNull null
            Open(k.removePrefix("open:"), parts.drop(3).joinToString("|"), parts[0], parts[1].toLongOrNull() ?: return@mapNotNull null, parts[2] == "1")
        }
    }

    private fun setOpen(ctx: Context, o: Open) = prefs(ctx).edit {
        putString("open:${o.placeId}", "${o.stayId}|${o.enteredAt}|${if (o.resumed) 1 else 0}|${o.name}")
    }

    private fun clearOpen(ctx: Context, placeId: String) = prefs(ctx).edit { remove("open:$placeId") }

    /** 들어왔다. 이미 들어와 있으면(다시 걸 때 오는 들어옴) 그대로, 같은 곳에서 5분 안에 나갔다 왔으면 앞 기록을 이어 간다 */
    suspend fun onEnter(ctx: Context, placeId: String, at: Long) {
        if (openStays(ctx).any { it.placeId == placeId }) return
        val place = places(ctx).firstOrNull { it.id == placeId } ?: return
        val last = MyHomeApp.instance.db.focus().lastStay(place.name)
        val open = if (last != null && StayRules.resumes(last.exitedAt, at)) {
            Open(placeId, place.name, last.id, last.enteredAt, resumed = true)
        } else {
            Open(placeId, place.name, UUID.randomUUID().toString(), at, resumed = false)
        }
        setOpen(ctx, open)
    }

    /** 나갔다. 남길 만큼 머물렀으면 한 줄로 남기고 서버로 보낸다 */
    suspend fun onExit(ctx: Context, placeId: String, at: Long) {
        val o = openStays(ctx).firstOrNull { it.placeId == placeId } ?: return
        clearOpen(ctx, placeId)
        if (!StayRules.keep(o.enteredAt, at)) return
        val dao = MyHomeApp.instance.db.focus()
        val now = System.currentTimeMillis()
        val prev = if (o.resumed) dao.stay(o.stayId) else null
        dao.upsertStay(
            prev?.copy(exitedAt = at, updatedAt = now, synced = false)
                ?: FocusStay(o.stayId, o.name, o.enteredAt, at, updatedAt = now),
        )
        FocusSync.enqueue(ctx)
    }
}

/** Play 서비스 지오펜스 전환 수신(공부 장소). 들어옴·나감을 남긴다 */
class StudyGeofenceReceiver : BroadcastReceiver() {
    override fun onReceive(context: Context, intent: Intent) {
        val ev = GeofencingEvent.fromIntent(intent) ?: return
        val ctx = context.applicationContext
        if (ev.hasError()) {
            StudyPlaces.onError(ctx, ev.errorCode)
            return
        }
        val ids = ev.triggeringGeofences.orEmpty().map { it.requestId.removePrefix("study:") }
        val at = System.currentTimeMillis()
        val transition = ev.geofenceTransition
        val pending = goAsync()
        MyHomeApp.instance.ioScope.launch {
            try {
                for (id in ids) {
                    when (transition) {
                        Geofence.GEOFENCE_TRANSITION_ENTER -> StudyPlaces.onEnter(ctx, id, at)
                        Geofence.GEOFENCE_TRANSITION_EXIT -> StudyPlaces.onExit(ctx, id, at)
                    }
                }
            } finally {
                pending.finish()
            }
        }
    }
}
