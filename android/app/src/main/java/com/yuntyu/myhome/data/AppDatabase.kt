package com.yuntyu.myhome.data

import android.content.Context
import androidx.room.AutoMigration
import androidx.room.Database
import androidx.room.Room
import androidx.room.RoomDatabase

// 2: 집중 타이머(과목·기록·머묾) 표를 더했다. 표만 늘어서 Room이 스키마(app/schemas)를 보고 옮긴다
@Database(
    entities = [PendingEvent::class, SendLogEntry::class, RecentApp::class, FocusSubject::class, FocusSession::class, FocusStay::class],
    version = 2,
    exportSchema = true,
    autoMigrations = [AutoMigration(from = 1, to = 2)],
)
abstract class AppDatabase : RoomDatabase() {
    abstract fun pendingEvents(): PendingEventDao
    abstract fun sendLog(): SendLogDao
    abstract fun recentApps(): RecentAppDao
    abstract fun focus(): FocusDao

    companion object {
        fun create(context: Context): AppDatabase =
            Room.databaseBuilder(context, AppDatabase::class.java, "myhome.db").build()
    }
}
