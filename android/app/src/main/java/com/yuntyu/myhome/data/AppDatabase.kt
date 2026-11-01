package com.yuntyu.myhome.data

import android.content.Context
import androidx.room.Database
import androidx.room.Room
import androidx.room.RoomDatabase

@Database(
    entities = [PendingEvent::class, SendLogEntry::class, RecentApp::class],
    version = 1,
    exportSchema = true,
)
abstract class AppDatabase : RoomDatabase() {
    abstract fun pendingEvents(): PendingEventDao
    abstract fun sendLog(): SendLogDao
    abstract fun recentApps(): RecentAppDao

    companion object {
        fun create(context: Context): AppDatabase =
            Room.databaseBuilder(context, AppDatabase::class.java, "myhome.db").build()
    }
}
