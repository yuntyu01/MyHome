package com.yuntyu.myhome.ui

import android.annotation.SuppressLint
import android.content.Context
import android.content.Intent
import android.net.Uri
import android.os.PowerManager
import android.provider.Settings
import androidx.core.app.NotificationManagerCompat
import com.yuntyu.myhome.notify.Notifications

/** 권한·설정 상태 확인과 설정 화면 열기. */
object PermissionStatus {
    fun listenerEnabled(ctx: Context): Boolean =
        NotificationManagerCompat.getEnabledListenerPackages(ctx).contains(ctx.packageName)

    fun batteryUnrestricted(ctx: Context): Boolean =
        ctx.getSystemService(PowerManager::class.java).isIgnoringBatteryOptimizations(ctx.packageName)

    fun canPostNotifications(ctx: Context): Boolean = Notifications.canPost(ctx)

    fun openListenerSettings(ctx: Context) {
        ctx.startActivity(Intent(Settings.ACTION_NOTIFICATION_LISTENER_SETTINGS).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK))
    }

    @SuppressLint("BatteryLife")
    fun requestBatteryExemption(ctx: Context) {
        val direct = Intent(Settings.ACTION_REQUEST_IGNORE_BATTERY_OPTIMIZATIONS, Uri.parse("package:${ctx.packageName}"))
            .addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
        try {
            ctx.startActivity(direct)
        } catch (e: Exception) {
            ctx.startActivity(Intent(Settings.ACTION_IGNORE_BATTERY_OPTIMIZATION_SETTINGS).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK))
        }
    }

    fun openAppDetails(ctx: Context) {
        ctx.startActivity(
            Intent(Settings.ACTION_APPLICATION_DETAILS_SETTINGS, Uri.parse("package:${ctx.packageName}"))
                .addFlags(Intent.FLAG_ACTIVITY_NEW_TASK),
        )
    }
}
