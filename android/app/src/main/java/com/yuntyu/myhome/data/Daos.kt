package com.yuntyu.myhome.data

import androidx.room.Dao
import androidx.room.Insert
import androidx.room.OnConflictStrategy
import androidx.room.Query
import androidx.room.Transaction
import kotlinx.coroutines.flow.Flow

@Dao
interface PendingEventDao {
    /** 같은 dedupeKey가 있으면 무시하고 -1을 반환한다. */
    @Insert(onConflict = OnConflictStrategy.IGNORE)
    suspend fun insert(e: PendingEvent): Long

    @Query("SELECT * FROM pending_events WHERE state = 'PENDING' ORDER BY postedAt, id LIMIT :limit")
    suspend fun nextBatch(limit: Int): List<PendingEvent>

    @Query("DELETE FROM pending_events WHERE id = :id")
    suspend fun delete(id: Long)

    @Query("UPDATE pending_events SET attempts = attempts + 1, lastError = :error WHERE id = :id")
    suspend fun markAttempt(id: Long, error: String)

    @Query("UPDATE pending_events SET state = 'REJECTED', attempts = attempts + 1, lastError = :error WHERE id = :id")
    suspend fun markRejected(id: Long, error: String)

    @Query("UPDATE pending_events SET state = 'PENDING' WHERE state = 'REJECTED'")
    suspend fun requeueRejected(): Int

    @Query("SELECT COUNT(*) FROM pending_events WHERE state = 'PENDING'")
    fun pendingCount(): Flow<Int>

    @Query("SELECT COUNT(*) FROM pending_events WHERE state = 'REJECTED'")
    fun rejectedCount(): Flow<Int>
}

// @Transaction 기본 구현이 있는 DAO는 인터페이스 대신 추상 클래스로 둔다(Room 호환성).
@Dao
abstract class SendLogDao {
    @Insert
    protected abstract suspend fun insertRaw(e: SendLogEntry)

    @Query("DELETE FROM send_log WHERE id NOT IN (SELECT id FROM send_log ORDER BY id DESC LIMIT :keep)")
    protected abstract suspend fun trim(keep: Int)

    @Transaction
    open suspend fun add(e: SendLogEntry) {
        insertRaw(e)
        trim(KEEP)
    }

    @Query("SELECT * FROM send_log ORDER BY id DESC LIMIT :limit")
    abstract fun recent(limit: Int = KEEP): Flow<List<SendLogEntry>>

    companion object {
        const val KEEP = 50
    }
}

@Dao
abstract class RecentAppDao {
    @Query("SELECT * FROM recent_apps WHERE packageName = :pkg")
    protected abstract suspend fun get(pkg: String): RecentApp?

    @Insert(onConflict = OnConflictStrategy.REPLACE)
    protected abstract suspend fun upsert(a: RecentApp)

    @Transaction
    open suspend fun record(pkg: String, label: String, title: String, now: Long) {
        val prev = get(pkg)
        upsert(RecentApp(pkg, label, title.take(60), now, (prev?.count ?: 0) + 1))
    }

    @Query("SELECT * FROM recent_apps ORDER BY lastSeenAt DESC LIMIT 200")
    abstract fun all(): Flow<List<RecentApp>>

    @Query("DELETE FROM recent_apps WHERE lastSeenAt < :before")
    abstract suspend fun deleteOlderThan(before: Long)
}
