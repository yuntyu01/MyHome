package com.yuntyu.myhome.data

import androidx.room.Dao
import androidx.room.Entity
import androidx.room.Index
import androidx.room.PrimaryKey
import androidx.room.Query
import androidx.room.Upsert
import kotlinx.coroutines.flow.Flow

/*
 * 집중 타이머 기록. 이 폰이 원본이고(신호가 없어도 재고 멈춘다), 서버에는 FocusSync가 올려 둔다(보관·웹용).
 * id는 앱이 만든 UUID라 같은 줄을 여러 번 보내도 서버에 한 줄이다. 시각은 Unix ms.
 * synced=false인 줄만 보낸다. 보내는 사이에 고쳤으면(updatedAt이 바뀜) 다음에 다시 보낸다.
 */

/** 과목(할 일). 지우지 않고 숨긴다(지난 기록이 가리키고 있어서) */
@Entity(tableName = "focus_subjects")
data class FocusSubject(
    @PrimaryKey val id: String,
    val name: String,
    /** "#RRGGBB" */
    val color: String,
    val sort: Int,
    val archived: Boolean = false,
    val updatedAt: Long,
    val synced: Boolean = false,
)

/** 타이머를 켠 한 번(순공). 도는 중인 것은 여기 없고 FocusTimer가 들고 있다 */
@Entity(tableName = "focus_sessions", indices = [Index("startedAt"), Index("synced")])
data class FocusSession(
    @PrimaryKey val id: String,
    val subjectId: String,
    val startedAt: Long,
    val endedAt: Long,
    val deleted: Boolean = false,
    val updatedAt: Long,
    val synced: Boolean = false,
)

/** 공부 장소에 머문 한 번(지오펜스 들어옴~나감). 장소는 이름만 남긴다(좌표는 StudyPlaces 설정에만) */
@Entity(tableName = "focus_stays", indices = [Index("enteredAt"), Index("synced")])
data class FocusStay(
    @PrimaryKey val id: String,
    val place: String,
    val enteredAt: Long,
    val exitedAt: Long,
    val deleted: Boolean = false,
    val updatedAt: Long,
    val synced: Boolean = false,
)

@Dao
interface FocusDao {
    @Query("SELECT * FROM focus_subjects WHERE archived = 0 ORDER BY sort, name")
    fun subjects(): Flow<List<FocusSubject>>

    @Query("SELECT * FROM focus_subjects ORDER BY sort, name")
    fun allSubjects(): Flow<List<FocusSubject>>

    @Query("SELECT * FROM focus_subjects WHERE id = :id")
    suspend fun subject(id: String): FocusSubject?

    @Query("SELECT COALESCE(MAX(sort), 0) FROM focus_subjects")
    suspend fun maxSort(): Int

    @Query("SELECT COUNT(*) FROM focus_subjects")
    suspend fun subjectCount(): Int

    @Upsert
    suspend fun upsertSubject(s: FocusSubject)

    @Upsert
    suspend fun upsertSubjects(list: List<FocusSubject>)

    /** [from, to)와 겹치는 기록(지운 것 빼고) */
    @Query("SELECT * FROM focus_sessions WHERE deleted = 0 AND endedAt > :from AND startedAt < :to ORDER BY startedAt")
    fun sessionsBetween(from: Long, to: Long): Flow<List<FocusSession>>

    @Query("SELECT * FROM focus_sessions WHERE id = :id")
    suspend fun session(id: String): FocusSession?

    @Upsert
    suspend fun upsertSession(s: FocusSession)

    @Upsert
    suspend fun upsertSessions(list: List<FocusSession>)

    @Query("SELECT * FROM focus_stays WHERE deleted = 0 AND exitedAt > :from AND enteredAt < :to ORDER BY enteredAt")
    fun staysBetween(from: Long, to: Long): Flow<List<FocusStay>>

    @Query("SELECT * FROM focus_stays WHERE id = :id")
    suspend fun stay(id: String): FocusStay?

    /** 그 장소의 마지막 머묾(흔들림으로 바로 다시 들어오면 이어 간다) */
    @Query("SELECT * FROM focus_stays WHERE place = :place AND deleted = 0 ORDER BY exitedAt DESC LIMIT 1")
    suspend fun lastStay(place: String): FocusStay?

    @Upsert
    suspend fun upsertStay(s: FocusStay)

    @Upsert
    suspend fun upsertStays(list: List<FocusStay>)

    // 서버로 보낼 것(과목 → 기록 → 머묾 순서로 보낸다)
    @Query("SELECT * FROM focus_subjects WHERE synced = 0 LIMIT :limit")
    suspend fun unsyncedSubjects(limit: Int = 100): List<FocusSubject>

    @Query("SELECT * FROM focus_sessions WHERE synced = 0 ORDER BY startedAt LIMIT :limit")
    suspend fun unsyncedSessions(limit: Int = 100): List<FocusSession>

    @Query("SELECT * FROM focus_stays WHERE synced = 0 ORDER BY enteredAt LIMIT :limit")
    suspend fun unsyncedStays(limit: Int = 100): List<FocusStay>

    /** 보낸 그대로일 때만 보냄 표시(보내는 사이에 고쳤으면 다음에 다시 보낸다) */
    @Query("UPDATE focus_subjects SET synced = 1 WHERE id = :id AND updatedAt = :updatedAt")
    suspend fun markSubjectSynced(id: String, updatedAt: Long)

    @Query("UPDATE focus_sessions SET synced = 1 WHERE id = :id AND updatedAt = :updatedAt")
    suspend fun markSessionSynced(id: String, updatedAt: Long)

    @Query("UPDATE focus_stays SET synced = 1 WHERE id = :id AND updatedAt = :updatedAt")
    suspend fun markStaySynced(id: String, updatedAt: Long)
}
