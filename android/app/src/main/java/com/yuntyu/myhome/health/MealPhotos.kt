package com.yuntyu.myhome.health

import android.content.Context
import android.graphics.Bitmap
import android.graphics.BitmapFactory
import android.graphics.Matrix
import android.media.ExifInterface
import android.net.Uri
import androidx.core.content.FileProvider
import androidx.core.content.edit
import com.yuntyu.myhome.MyHomeApp
import com.yuntyu.myhome.core.api.ApiResult
import java.io.ByteArrayOutputStream
import java.io.File
import kotlin.math.max

/**
 * 끼니 사진. 기록을 저장하면 서버(VM 디스크)에도 올려 두고, 폰의 사진은 보기용 캐시로 쓴다.
 * 폰 보관 기간이 지나 지워진 사진은 필요할 때 서버에서 다시 받는다(ensureLocal).
 * 위치: filesDir/meal_photos/{날짜}_{끼니}_{n}.jpg. 찍는 중인 사진은 tmp_로 시작한다.
 * 올리기 전에 "올릴 것"으로 표시해 두고(올리는 중에 앱이 죽어도 남게) 끝나면 지운다. 남은 끼니는 위젯 갱신 주기에 다시 올린다(retryPending).
 * 올릴 것으로 표시된 끼니와 아직 서버에 못 보낸 끼니(MealOutbox)의 사진은 보관 기간이 지나도 지우지 않는다.
 */
object MealPhotos {
    private const val DIR = "meal_photos"
    /** 서버로 보내는 사진의 긴 변(핏두잉과 같음) */
    private const val LONG_SIDE = 1024

    private fun dir(context: Context) = File(context.filesDir, DIR).apply { mkdirs() }

    /** 카메라가 찍은 사진을 쓸 임시 파일과 그 content:// 주소 */
    fun newCapture(context: Context): Pair<File, Uri> {
        val f = File(dir(context), "tmp_${System.currentTimeMillis()}.jpg")
        return f to FileProvider.getUriForFile(context, "${context.packageName}.files", f)
    }

    /** 앨범에서 고른 사진을 임시 파일로 복사한다. */
    fun copyIn(context: Context, uri: Uri): File? = runCatching {
        val f = File(dir(context), "tmp_${System.nanoTime()}.jpg")
        context.contentResolver.openInputStream(uri)!!.use { input -> f.outputStream().use { input.copyTo(it) } }
        f
    }.getOrNull()

    /** 긴 변 1024px, 회전 반영, JPEG 85로 줄인 바이트(서버 전송·보관 공용). */
    fun downscaled(file: File): ByteArray? = runCatching {
        val bounds = BitmapFactory.Options().apply { inJustDecodeBounds = true }
        BitmapFactory.decodeFile(file.path, bounds)
        var sample = 1
        while (max(bounds.outWidth, bounds.outHeight) / (sample * 2) >= LONG_SIDE) sample *= 2
        var bmp = BitmapFactory.decodeFile(file.path, BitmapFactory.Options().apply { inSampleSize = sample }) ?: return null
        val scale = LONG_SIDE.toFloat() / max(bmp.width, bmp.height)
        val m = Matrix()
        if (scale < 1f) m.postScale(scale, scale)
        when (ExifInterface(file.path).getAttributeInt(ExifInterface.TAG_ORIENTATION, ExifInterface.ORIENTATION_NORMAL)) {
            ExifInterface.ORIENTATION_ROTATE_90 -> m.postRotate(90f)
            ExifInterface.ORIENTATION_ROTATE_180 -> m.postRotate(180f)
            ExifInterface.ORIENTATION_ROTATE_270 -> m.postRotate(270f)
        }
        if (!m.isIdentity) bmp = Bitmap.createBitmap(bmp, 0, 0, bmp.width, bmp.height, m, true)
        ByteArrayOutputStream().use { out ->
            bmp.compress(Bitmap.CompressFormat.JPEG, 85, out)
            out.toByteArray()
        }
    }.getOrNull()

    /** 저장한 끼니의 사진을 바꾼다(전에 있던 같은 끼니 사진은 지운다). */
    fun keep(context: Context, date: String, index: Int, photos: List<ByteArray>) {
        delete(context, date, index)
        photos.forEachIndexed { i, b -> File(dir(context), "${date}_${index}_$i.jpg").writeBytes(b) }
    }

    fun photosOf(context: Context, date: String, index: Int): List<File> =
        dir(context).listFiles { f -> f.name.startsWith("${date}_${index}_") }.orEmpty().sortedBy { it.name }

    fun delete(context: Context, date: String, index: Int) = photosOf(context, date, index).forEach { it.delete() }

    /**
     * 임시 사진을 지우고, 보관 기간(일)이 지난 사진을 지운다. 기록 자체는 서버에 남는다.
     * 아직 서버에 없는 사진(올릴 것으로 표시됨, 보내지 못한 끼니)은 남긴다.
     */
    fun purge(context: Context, keepDays: Int) {
        val files = dir(context).listFiles().orEmpty()
        val dayAgo = System.currentTimeMillis() - 24 * 3600_000L
        files.filter { it.name.startsWith("tmp_") && it.lastModified() < dayAgo }.forEach { it.delete() }
        if (keepDays <= 0) return
        val cutoff = java.time.LocalDate.now().minusDays(keepDays.toLong()).toString()
        val keep = pendingKeys(context) + MealOutbox.keys(context)
        files.filter { f ->
            // 이름: {날짜}_{끼니}_{n}.jpg
            val key = f.name.split("_").let { p -> if (p.size >= 3) "${p[0]}|${p[1]}" else "" }
            !f.name.startsWith("tmp_") && f.name.take(10) < cutoff && key !in keep
        }.forEach { it.delete() }
    }

    private const val PREFS = "meal_photos"
    private const val KEY_PENDING = "pending_upload"

    /** "날짜|끼니" 열쇠(올릴 것 표시와 MealOutbox가 같이 쓴다) */
    fun key(date: String, index: Int) = "$date|$index"

    /** 이 끼니 사진을 올릴 것으로 표시한다(올리기 전에, 또는 끼니를 아직 못 보냈을 때). */
    fun markPending(context: Context, date: String, index: Int) = setPending(context, key(date, index), true)

    /**
     * 그 끼니의 폰 사진을 서버에 올린다(바꾸기). 올리기 전에 표시해 두고, 끝나면 지운다(실패하면 남아서 다음에 다시).
     * 폰에 사진이 없으면(보관 기간이 지나 지워짐) 올리지 않고 표시만 지운다: 0장을 올리면 서버 사진이 지워진다.
     * 서버 저장이 꺼져 있거나(503) 사진·끼니가 맞지 않으면(400·404, 끼니가 지워짐) 그만둔다.
     */
    fun upload(context: Context, date: String, index: Int): Boolean {
        val k = key(date, index)
        val photos = photosOf(context, date, index).map { it.readBytes() }
        if (photos.isEmpty()) {
            setPending(context, k, false)
            return true
        }
        setPending(context, k, true)
        val r = MyHomeApp.instance.api.putMealPhotos(date, index, photos)
        val done = r is ApiResult.Ok || (r is ApiResult.HttpError && r.code in setOf(400, 404, 503))
        if (done) setPending(context, k, false)
        return done
    }

    private fun pendingKeys(context: Context): Set<String> =
        context.getSharedPreferences(PREFS, Context.MODE_PRIVATE).getStringSet(KEY_PENDING, emptySet()).orEmpty().toSet()

    /** 전에 올리지 못한 끼니 사진을 다시 올린다. 끼니 기록을 아직 못 보냈으면(MealOutbox) 기록을 보낸 뒤에 올린다. */
    fun retryPending(context: Context) {
        val waiting = MealOutbox.keys(context)
        for (k in pendingKeys(context)) {
            val (date, idx) = k.split("|").let { it[0] to (it.getOrNull(1)?.toIntOrNull() ?: 0) }
            if (idx == 0 || k in waiting) continue
            upload(context, date, idx)
        }
    }

    @Synchronized
    private fun setPending(context: Context, key: String, on: Boolean) {
        val prefs = context.getSharedPreferences(PREFS, Context.MODE_PRIVATE)
        val cur = prefs.getStringSet(KEY_PENDING, emptySet()).orEmpty().toSet()
        val next = if (on) cur + key else cur - key
        if (next != cur) prefs.edit(commit = true) { putStringSet(KEY_PENDING, next) }
    }

    /** 폰에 그 끼니 사진이 없으면 서버에서 받아 둔다. @return 새로 받았으면 true */
    fun ensureLocal(context: Context, date: String, index: Int): Boolean {
        if (photosOf(context, date, index).isNotEmpty()) return false
        val api = MyHomeApp.instance.api
        val list = (api.mealPhotos(date, index) as? ApiResult.Ok)?.value?.photos.orEmpty()
        var got = false
        for (p in list) {
            val b = (api.mealPhoto(date, index, p.n) as? ApiResult.Ok)?.value ?: continue
            if (b.isEmpty()) continue
            File(dir(context), "${date}_${index}_${p.n}.jpg").writeBytes(b)
            got = true
        }
        return got
    }
}
