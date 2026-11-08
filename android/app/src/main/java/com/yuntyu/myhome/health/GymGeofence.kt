package com.yuntyu.myhome.health

import android.Manifest
import android.annotation.SuppressLint
import android.app.PendingIntent
import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import android.content.pm.PackageManager
import android.location.Location
import android.os.Build
import androidx.core.app.NotificationCompat
import androidx.core.content.ContextCompat
import androidx.core.content.edit
import com.google.android.gms.location.Geofence
import com.google.android.gms.location.GeofencingEvent
import com.google.android.gms.location.GeofencingRequest
import com.google.android.gms.location.LocationServices
import com.google.android.gms.location.Priority
import com.google.android.gms.tasks.CancellationTokenSource
import com.yuntyu.myhome.MyHomeApp
import com.yuntyu.myhome.R
import com.yuntyu.myhome.core.api.ApiResult
import com.yuntyu.myhome.notify.Notifications
import com.yuntyu.myhome.ui.MainActivity
import kotlinx.coroutines.launch
import kotlinx.coroutines.suspendCancellableCoroutine
import java.time.LocalDate
import kotlin.coroutines.resume

/**
 * 헬스장 자동 출석(지오펜스). 헬스장에서 "지금 위치를 헬스장으로"를 한 번 누르면, 그 반경 안에 [DWELL_MS] 머물 때
 * 시스템(Play 서비스)이 알려 주고, 그날 출석을 geofence로 남긴다. 카카오 장소 검색 없이 폰 GPS만 쓴다(2026-11-08).
 * 좌표는 이 폰에만 둔다. 서버에는 "그날 위치로 확인됨"만 간다. 백그라운드에서 알려면 위치 "항상 허용"이 필요하다.
 */
object GymGeofence {
    private const val PREFS = "gym_geofence"
    private const val ID = "gym"
    private const val DWELL_MS = 5 * 60 * 1000
    const val DEFAULT_RADIUS = 150
    private const val NOTIFY_ID = 4001

    data class Spot(val lat: Double, val lng: Double, val radiusM: Int)

    private fun prefs(ctx: Context) = ctx.getSharedPreferences(PREFS, Context.MODE_PRIVATE)

    fun spot(ctx: Context): Spot? {
        val p = prefs(ctx)
        if (!p.contains("lat")) return null
        return Spot(
            java.lang.Double.longBitsToDouble(p.getLong("lat", 0)),
            java.lang.Double.longBitsToDouble(p.getLong("lng", 0)),
            p.getInt("radius", DEFAULT_RADIUS),
        )
    }

    fun save(ctx: Context, loc: Location, radiusM: Int = spot(ctx)?.radiusM ?: DEFAULT_RADIUS) {
        prefs(ctx).edit {
            putLong("lat", java.lang.Double.doubleToRawLongBits(loc.latitude))
            putLong("lng", java.lang.Double.doubleToRawLongBits(loc.longitude))
            putInt("radius", radiusM)
            putBoolean("enabled", true)
        }
        register(ctx)
    }

    fun setRadius(ctx: Context, radiusM: Int) {
        prefs(ctx).edit { putInt("radius", radiusM) }
        register(ctx)
    }

    fun clear(ctx: Context) {
        unregister(ctx)
        prefs(ctx).edit { clear() }
    }

    fun enabled(ctx: Context) = prefs(ctx).getBoolean("enabled", false) && spot(ctx) != null

    fun setEnabled(ctx: Context, on: Boolean) {
        prefs(ctx).edit { putBoolean("enabled", on) }
        if (on) register(ctx) else unregister(ctx)
    }

    fun hasForeground(ctx: Context) =
        ContextCompat.checkSelfPermission(ctx, Manifest.permission.ACCESS_FINE_LOCATION) == PackageManager.PERMISSION_GRANTED

    /** 지오펜스는 Android 10부터 "항상 허용"(백그라운드 위치)이 있어야 앱이 꺼져 있어도 울린다. */
    fun hasBackground(ctx: Context) = Build.VERSION.SDK_INT < Build.VERSION_CODES.Q ||
        ContextCompat.checkSelfPermission(ctx, Manifest.permission.ACCESS_BACKGROUND_LOCATION) == PackageManager.PERMISSION_GRANTED

    private fun pending(ctx: Context): PendingIntent = PendingIntent.getBroadcast(
        ctx,
        7,
        Intent(ctx, GymGeofenceReceiver::class.java),
        // Play 서비스가 전환 정보를 채워 넣으므로 MUTABLE이어야 한다
        PendingIntent.FLAG_UPDATE_CURRENT or (if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.S) PendingIntent.FLAG_MUTABLE else 0),
    )

    /** 켜져 있고 권한이 있으면 지오펜스를 (다시) 건다. 재부팅·앱 업데이트·앱 시작 때 부른다(지오펜스는 재부팅 때 지워진다). */
    @SuppressLint("MissingPermission")
    fun register(ctx: Context) {
        val s = spot(ctx)
        if (s == null || !enabled(ctx) || !hasForeground(ctx) || !hasBackground(ctx)) {
            unregister(ctx)
            return
        }
        val fence = Geofence.Builder()
            .setRequestId(ID)
            .setCircularRegion(s.lat, s.lng, s.radiusM.toFloat())
            .setExpirationDuration(Geofence.NEVER_EXPIRE)
            .setTransitionTypes(Geofence.GEOFENCE_TRANSITION_DWELL)
            .setLoiteringDelay(DWELL_MS)
            .build()
        val req = GeofencingRequest.Builder().setInitialTrigger(GeofencingRequest.INITIAL_TRIGGER_DWELL).addGeofence(fence).build()
        runCatching { LocationServices.getGeofencingClient(ctx).addGeofences(req, pending(ctx)) }
    }

    fun unregister(ctx: Context) {
        runCatching { LocationServices.getGeofencingClient(ctx).removeGeofences(pending(ctx)) }
    }

    /** 지금 위치 한 번(정확도 높게). 권한이 없거나 못 잡으면 null. */
    @SuppressLint("MissingPermission")
    suspend fun currentLocation(ctx: Context): Location? {
        if (!hasForeground(ctx)) return null
        val cts = CancellationTokenSource()
        return suspendCancellableCoroutine { cont ->
            LocationServices.getFusedLocationProviderClient(ctx).getCurrentLocation(Priority.PRIORITY_HIGH_ACCURACY, cts.token)
                .addOnSuccessListener { cont.resume(it) }
                .addOnFailureListener { cont.resume(null) }
            cont.invokeOnCancellation { cts.cancel() }
        }
    }

    /** 헬스장에 머문 것을 시스템이 알려 왔다. 그날 처음이면 출석을 남기고 조용히 알린다. */
    fun onDwell(ctx: Context) {
        val today = LocalDate.now().toString()
        val p = prefs(ctx)
        if (p.getString("last", null) == today) return
        val r = MyHomeApp.instance.api.gymCheckIn(today, "geofence")
        if (r !is ApiResult.Ok) return // 다음 머묾(또는 다시 들어올 때)에 다시 한다
        p.edit { putString("last", today) }
        if (!Notifications.canPost(ctx)) return
        val open = PendingIntent.getActivity(
            ctx,
            4,
            Intent(ctx, MainActivity::class.java).putExtra(MainActivity.EXTRA_SCREEN, "health_gym")
                .addFlags(Intent.FLAG_ACTIVITY_NEW_TASK or Intent.FLAG_ACTIVITY_CLEAR_TOP),
            PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT,
        )
        val n = NotificationCompat.Builder(ctx, Notifications.CH_HEALTH)
            .setSmallIcon(R.drawable.ic_stat_notify)
            .setContentTitle("헬스장 도착을 기록했어요")
            .setContentText("위치로 오늘 출석을 남겼어요")
            .setContentIntent(open)
            .setAutoCancel(true)
            .setSilent(true)
            .build()
        Notifications.notifyChecked(ctx, NOTIFY_ID, n)
    }
}

/** Play 서비스 지오펜스 전환 수신. 네트워크 호출이 있어 goAsync로 잠깐 더 산다. */
class GymGeofenceReceiver : BroadcastReceiver() {
    override fun onReceive(context: Context, intent: Intent) {
        val ev = GeofencingEvent.fromIntent(intent) ?: return
        if (ev.hasError() || ev.geofenceTransition != Geofence.GEOFENCE_TRANSITION_DWELL) return
        val done = goAsync()
        MyHomeApp.instance.ioScope.launch {
            try {
                GymGeofence.onDwell(context.applicationContext)
            } finally {
                done.finish()
            }
        }
    }
}
