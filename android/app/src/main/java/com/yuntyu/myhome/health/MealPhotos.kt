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
 * 올리기에 실패한 끼니는 기억해 두었다가 위젯 갱신 주기에 다시 올린다(retryPending).
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

    /** 임시 사진을 지우고, 보관 기간(일)이 지난 사진을 지운다. 기록 자체는 서버에 남는다. */
    fun purge(context: Context, keepDays: Int) {
        val files = dir(context).listFiles().orEmpty()
        val dayAgo = System.currentTimeMillis() - 24 * 3600_000L
        files.filter { it.name.startsWith("tmp_") && it.lastModified() < dayAgo }.forEach { it.delete() }
        if (keepDays <= 0) return
        val cutoff = java.time.LocalDate.now().minusDays(keepDays.toLong()).toString()
        files.filter { !it.name.startsWith("tmp_") && it.name.take(10) < cutoff }.forEach { it.delete() }
    }

    private const val PREFS = "meal_photos"
    private const val KEY_PENDING = "pending_upload"

    /** 그 끼니의 폰 사진을 서버에 올린다(바꾸기). 실패하면 나중에 다시 올리도록 기억한다. 서버 저장이 꺼져 있으면(503) 그만둔다. */
    fun upload(context: Context, date: String, index: Int): Boolean {
        val photos = photosOf(context, date, index).map { it.readBytes() }
        val r = MyHomeApp.instance.api.putMealPhotos(date, index, photos)
        val done = r is ApiResult.Ok || (r is ApiResult.HttpError && (r.code == 503 || r.code == 400))
        setPending(context, "$date|$index", !done)
        return done
    }

    /** 전에 올리지 못한 끼니 사진을 다시 올린다. */
    fun retryPending(context: Context) {
        val pending = context.getSharedPreferences(PREFS, Context.MODE_PRIVATE).getStringSet(KEY_PENDING, emptySet()).orEmpty().toList()
        for (key in pending) {
            val (date, idx) = key.split("|").let { it[0] to (it.getOrNull(1)?.toIntOrNull() ?: 0) }
            if (idx == 0 || !upload(context, date, idx)) continue
        }
    }

    private fun setPending(context: Context, key: String, on: Boolean) {
        val prefs = context.getSharedPreferences(PREFS, Context.MODE_PRIVATE)
        val cur = prefs.getStringSet(KEY_PENDING, emptySet()).orEmpty()
        val next = if (on) cur + key else cur - key
        if (next != cur) prefs.edit { putStringSet(KEY_PENDING, next) }
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
