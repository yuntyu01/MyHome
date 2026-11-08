package com.yuntyu.myhome.health

import android.content.Context
import android.graphics.Bitmap
import android.graphics.BitmapFactory
import android.graphics.Matrix
import android.media.ExifInterface
import android.net.Uri
import androidx.core.content.FileProvider
import java.io.ByteArrayOutputStream
import java.io.File
import kotlin.math.max

/**
 * 끼니 사진은 폰에만 둔다(서버는 분석할 때만 받고 저장하지 않는다).
 * 위치: filesDir/meal_photos/{날짜}_{끼니}_{n}.jpg. 찍는 중인 사진은 tmp_로 시작한다.
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
}
