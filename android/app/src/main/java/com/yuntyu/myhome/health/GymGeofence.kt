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
import androidx.work.BackoffPolicy
import androidx.work.Constraints
import androidx.work.CoroutineWorker
import androidx.work.ExistingWorkPolicy
import androidx.work.NetworkType
import androidx.work.OneTimeWorkRequestBuilder
import androidx.work.WorkManager
import androidx.work.WorkerParameters
import androidx.work.workDataOf
import com.google.android.gms.common.api.ApiException
import com.google.android.gms.location.Geofence
import com.google.android.gms.location.GeofenceStatusCodes
import com.google.android.gms.location.GeofencingEvent
import com.google.android.gms.location.GeofencingRequest
import com.google.android.gms.location.LocationServices
import com.google.android.gms.location.Priority
import com.google.android.gms.tasks.CancellationTokenSource
import com.yuntyu.myhome.BuildConfig
import com.yuntyu.myhome.MyHomeApp
import com.yuntyu.myhome.R
import com.yuntyu.myhome.core.api.ApiResult
import com.yuntyu.myhome.core.api.UploadDecision
import com.yuntyu.myhome.core.api.describe
import com.yuntyu.myhome.core.api.toUploadDecision
import com.yuntyu.myhome.notify.Notifications
import com.yuntyu.myhome.ui.MainActivity
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.suspendCancellableCoroutine
import kotlinx.coroutines.withContext
import java.time.Instant
import java.time.LocalDate
import java.util.concurrent.TimeUnit
import kotlin.coroutines.resume

/**
 * 헬스장 자동 출석(지오펜스). 헬스장에서 "지금 위치를 헬스장으로"를 한 번 누르면, 그 반경 안에 [DWELL_MS] 머물 때
 * 시스템(Play 서비스)이 알려 주고, 그날 출석을 geofence로 남긴다. 카카오 장소 검색 없이 폰 GPS만 쓴다(2026-11-08).
 * 좌표는 이 폰에만 둔다. 서버에는 "그날 위치로 확인됨"만 간다. 백그라운드에서 알려면 위치 "항상 허용"이 필요하다.
 * 출석은 WorkManager로 보낸다(GymCheckInWorker, 날짜마다 하나, 연결되면, 실패하면 늘려 가며 다시): 지하 헬스장처럼 신호가 없어도
 * 그날 출석을 잃지 않는다. 지오펜스는 위치가 꺼지면 시스템이 지우므로 30분 주기(WidgetRefresher)에서도 다시 걸고,
 * 걸지 못한 이유는 남겨 두었다가 화면에 보여 준다(registrationError).
 */
object GymGeofence {
    private const val PREFS = "gym_geofence"
    private const val ID = "gym"
    // 디버그 빌드는 시연·확인용으로 30초
    private val DWELL_MS = if (BuildConfig.DEBUG) 30 * 1000 else 5 * 60 * 1000
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
            // 자동 출석은 여기서 켜지 않는다. 화면에서 안내(백그라운드 위치)를 보고 동의해야 켠다
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

    /** 지오펜스를 걸지 못했거나 시스템이 지운 이유(위치 꺼짐 등). 걸리면 null. */
    fun registrationError(ctx: Context): String? = prefs(ctx).getString("reg_error", null)

    private fun setRegistrationError(ctx: Context, why: String?) = prefs(ctx).edit {
        if (why == null) remove("reg_error") else putString("reg_error", why)
    }

    /** 아직 서버에 못 보낸 자동 출석 날짜(보내는 중), 없으면 null */
    fun pendingCheckIn(ctx: Context): String? = prefs(ctx).getString("pending", null)

    private fun errorText(code: Int): String = when (code) {
        GeofenceStatusCodes.GEOFENCE_NOT_AVAILABLE -> "위치가 꺼져 있어서 자동 출석이 멈췄어요. 위치를 켜면 다시 걸어요"
        GeofenceStatusCodes.GEOFENCE_INSUFFICIENT_LOCATION_PERMISSION -> "위치 권한이 모자라서 자동 출석을 걸지 못했어요"
        else -> "자동 출석을 걸지 못했어요 (${GeofenceStatusCodes.getStatusCodeString(code)})"
    }

    fun enabled(ctx: Context) = prefs(ctx).getBoolean("enabled", false) && spot(ctx) != null

    fun setEnabled(ctx: Context, on: Boolean) {
        prefs(ctx).edit { putBoolean("enabled", on) }
        if (on) register(ctx) else unregister(ctx)
    }

    fun hasForeground(ctx: Context) =
        ContextCompat.checkSelfPermission(ctx, Manifest.permission.ACCESS_FINE_LOCATION) == PackageManager.PERMISSION_GRANTED

    /**
     * 지오펜스는 Android 10부터 "항상 허용"(백그라운드 위치)이 있어야 앱이 꺼져 있어도 울린다.
     * Play 위치 권한 선언 전에는 매니페스트에 그 권한이 없어서(BuildConfig.GYM_AUTO=false) 늘 false다.
     */
    fun hasBackground(ctx: Context) = BuildConfig.GYM_AUTO && (
        Build.VERSION.SDK_INT < Build.VERSION_CODES.Q ||
            ContextCompat.checkSelfPermission(ctx, Manifest.permission.ACCESS_BACKGROUND_LOCATION) == PackageManager.PERMISSION_GRANTED
        )

    /** 위치로 출석의 결과. */
    sealed interface HereResult {
        data class Done(val distanceM: Int) : HereResult
        data class TooFar(val distanceM: Int, val allowedM: Int) : HereResult
        data class Failed(val why: String) : HereResult
        /** 반경 안이지만 지금 보내지 못해서 연결되면 보낸다 */
        data class Queued(val distanceM: Int) : HereResult
    }

    /**
     * 지금 위치가 저장한 헬스장 반경 안이면 오늘 출석을 geofence로 남긴다(앱을 연 채로 누를 때, 자동 출석이 꺼져 있어도 쓴다).
     * 허용 거리 = 반경 + 위치 오차(최대 100m).
     */
    suspend fun checkInHere(ctx: Context): HereResult {
        val s = spot(ctx) ?: return HereResult.Failed("헬스장 위치를 먼저 저장해 주세요")
        val loc = currentLocation(ctx) ?: return HereResult.Failed("위치를 잡지 못했어요. 위치(GPS)를 켜 주세요")
        val d = FloatArray(1)
        Location.distanceBetween(loc.latitude, loc.longitude, s.lat, s.lng, d)
        val allowed = s.radiusM + minOf(loc.accuracy, 100f).toInt()
        if (d[0] > allowed) return HereResult.TooFar(d[0].toInt(), allowed)
        val today = LocalDate.now().toString()
        val at = Instant.now().toString()
        val r = MyHomeApp.instance.api.gymCheckIn(today, "geofence", at)
        return when {
            r is ApiResult.Ok -> {
                onCheckedIn(ctx, today, notify = false)
                HereResult.Done(d[0].toInt())
            }
            // 신호가 없거나 서버가 잠깐 안 되면 연결될 때 보낸다
            r.toUploadDecision() == UploadDecision.RETRY -> {
                enqueueCheckIn(ctx, today, at, notify = false)
                HereResult.Queued(d[0].toInt())
            }
            else -> HereResult.Failed("출석을 남기지 못했어요: ${r.describe()}")
        }
    }

    private fun pending(ctx: Context): PendingIntent = PendingIntent.getBroadcast(
        ctx,
        7,
        Intent(ctx, GymGeofenceReceiver::class.java),
        // Play 서비스가 전환 정보를 채워 넣으므로 MUTABLE이어야 한다
        PendingIntent.FLAG_UPDATE_CURRENT or (if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.S) PendingIntent.FLAG_MUTABLE else 0),
    )

    /**
     * 켜져 있고 권한이 있으면 지오펜스를 (다시) 건다. 재부팅·앱 업데이트·앱 시작·화면·30분 주기 때 부른다
     * (지오펜스는 재부팅·위치 끄기 때 지워진다). 결과(실패 이유, 걸리면 null)는 남기고 onResult로도 알린다.
     */
    @SuppressLint("MissingPermission")
    fun register(ctx: Context, onResult: ((String?) -> Unit)? = null) {
        val s = spot(ctx)
        if (s == null || !enabled(ctx) || !hasForeground(ctx) || !hasBackground(ctx)) {
            unregister(ctx)
            setRegistrationError(ctx, null) // 꺼짐·권한 없음은 화면이 따로 알린다
            onResult?.invoke(null)
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
        runCatching {
            LocationServices.getGeofencingClient(ctx).addGeofences(req, pending(ctx))
                .addOnSuccessListener { setRegistrationError(ctx, null); onResult?.invoke(null) }
                .addOnFailureListener { e ->
                    val why = (e as? ApiException)?.let { errorText(it.statusCode) } ?: "자동 출석을 걸지 못했어요 (${e.message})"
                    setRegistrationError(ctx, why)
                    onResult?.invoke(why)
                }
        }.onFailure { e ->
            val why = "자동 출석을 걸지 못했어요 (${e.message})"
            setRegistrationError(ctx, why)
            onResult?.invoke(why)
        }
    }

    /** 시스템이 지오펜스 오류를 알려 왔다(위치를 끄면 GEOFENCE_NOT_AVAILABLE과 함께 지오펜스가 지워진다). */
    fun onError(ctx: Context, code: Int) = setRegistrationError(ctx, errorText(code))

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

    /** 헬스장에 머문 것을 시스템이 알려 왔다. 그날 처음이면 출석 보내기를 건다(신호가 없어도 연결되면 보낸다). */
    fun onDwell(ctx: Context) {
        val today = LocalDate.now().toString()
        if (prefs(ctx).getString("last", null) == today) return
        enqueueCheckIn(ctx, today, Instant.now().toString(), notify = true)
    }

    /** 그날 출석 보내기를 건다. 날짜마다 하나(이미 걸려 있으면 그대로), 연결되면, 실패하면 30초부터 늘려 가며 다시. */
    fun enqueueCheckIn(ctx: Context, date: String, at: String, notify: Boolean) {
        prefs(ctx).edit { putString("pending", date) }
        val req = OneTimeWorkRequestBuilder<GymCheckInWorker>()
            .setInputData(workDataOf("date" to date, "at" to at, "notify" to notify))
            .setConstraints(Constraints.Builder().setRequiredNetworkType(NetworkType.CONNECTED).build())
            .setBackoffCriteria(BackoffPolicy.EXPONENTIAL, 30, TimeUnit.SECONDS)
            .build()
        WorkManager.getInstance(ctx).enqueueUniqueWork("gym-checkin-$date", ExistingWorkPolicy.KEEP, req)
    }

    /** 출석이 서버에 남았다: 같은 날 다시 보내지 않고, 그날 헬스 알림을 빼고, 자동 출석이면 조용히 알린다. */
    fun onCheckedIn(ctx: Context, date: String, notify: Boolean) {
        val p = prefs(ctx)
        val wasPending = p.getString("pending", null) == date
        p.edit {
            putString("last", date)
            if (wasPending) remove("pending")
        }
        MyHomeApp.instance.healthLocal.setGymDone(date, true)
        Reminders.reschedule(ctx)
        if (!notify || !Notifications.canPost(ctx)) return
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

    /** 서버가 거절해서 그만둔 출석은 "보내는 중" 표시를 지운다. */
    internal fun onCheckInDropped(ctx: Context, date: String) {
        val p = prefs(ctx)
        if (p.getString("pending", null) == date) p.edit { remove("pending") }
    }
}

/** 출석 보내기. 연결·서버 문제와 토큰 문제는 다시(토큰을 고치면 보내진다), 서버가 거절하면 그만둔다. */
class GymCheckInWorker(context: Context, params: WorkerParameters) : CoroutineWorker(context, params) {
    override suspend fun doWork(): Result = withContext(Dispatchers.IO) {
        val date = inputData.getString("date") ?: return@withContext Result.failure()
        val notify = inputData.getBoolean("notify", false)
        val r = MyHomeApp.instance.api.gymCheckIn(date, "geofence", inputData.getString("at"))
        when (r.toUploadDecision()) {
            UploadDecision.SENT -> {
                GymGeofence.onCheckedIn(applicationContext, date, notify)
                Result.success()
            }
            UploadDecision.REJECTED -> {
                GymGeofence.onCheckInDropped(applicationContext, date)
                Result.failure()
            }
            UploadDecision.RETRY, UploadDecision.AUTH_FAILED -> Result.retry()
        }
    }
}

/** Play 서비스 지오펜스 전환 수신. 네트워크는 쓰지 않고 출석 보내기만 건다(GymCheckInWorker). 오류는 남겨서 화면에 보인다. */
class GymGeofenceReceiver : BroadcastReceiver() {
    override fun onReceive(context: Context, intent: Intent) {
        val ev = GeofencingEvent.fromIntent(intent) ?: return
        if (ev.hasError()) {
            GymGeofence.onError(context.applicationContext, ev.errorCode)
            return
        }
        if (ev.geofenceTransition != Geofence.GEOFENCE_TRANSITION_DWELL) return
        GymGeofence.onDwell(context.applicationContext)
    }
}
