package com.yuntyu.myhome.health

import android.content.Context
import java.io.File
import java.time.LocalDate

/**
 * 몸 사진(사용자 요청, 2026-11-10). 폰에만 둔다: 서버로 보내지 않고, 앱 백업도 꺼져 있다(allowBackup=false).
 * 위치: filesDir/body_photos/{날짜}_{찍은 시각 ms}.jpg, 긴 변 1024px로 줄여서(끼니 사진과 같은 줄이기).
 */
object BodyPhotos {
    private const val DIR = "body_photos"

    private fun dir(context: Context) = File(context.filesDir, DIR).apply { mkdirs() }

    /** 찍은 임시 사진을 몸 사진으로 옮긴다(줄여서). 원본 임시 파일은 지운다 */
    fun keep(context: Context, src: File, date: LocalDate): File? {
        val bytes = MealPhotos.downscaled(src)
        src.delete()
        bytes ?: return null
        val f = File(dir(context), "${date}_${System.currentTimeMillis()}.jpg")
        return runCatching { f.writeBytes(bytes); f }.getOrNull()
    }

    /** 날짜 최근 순(같은 날은 나중에 찍은 것 먼저) */
    fun all(context: Context): List<Pair<LocalDate, File>> =
        dir(context).listFiles { f -> f.name.endsWith(".jpg") }.orEmpty()
            .mapNotNull { f -> runCatching { LocalDate.parse(f.name.substringBefore('_')) to f }.getOrNull() }
            .sortedWith(compareByDescending<Pair<LocalDate, File>> { it.first }.thenByDescending { it.second.name })

    fun delete(file: File) {
        file.delete()
    }
}
