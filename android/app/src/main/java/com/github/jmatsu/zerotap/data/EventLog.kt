package com.github.jmatsu.zerotap.data

import android.content.Context
import android.util.Log
import androidx.annotation.StringRes
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.update

enum class LogLevel { INFO, SUCCESS, ERROR }

data class LogEntry(
    val timestamp: String,
    val message: String,
    val level: LogLevel,
)

/**
 * An on-screen trace of the auth flow. Demo scaffolding, but the useful kind: a
 * zero-tap sign-in is invisible by definition, so without something like this
 * there is nothing to look at when it works and nothing to read when it does not.
 * A logcat-only version of this is worth keeping while you bring your own
 * integration up — especially for the backup agent, whose process you cannot
 * attach a debugger to in time.
 *
 * Entries are resolved against the app locale as they are written, which keeps
 * the layers below the UI free of Compose while still leaving every message
 * translatable. A log is a record of what already happened, so entries written
 * before a language change stay in the language they were written in.
 */
class EventLog(context: Context) {
    private val appContext = context.applicationContext

    private val _entries = MutableStateFlow<List<LogEntry>>(emptyList())
    val entries: StateFlow<List<LogEntry>> = _entries

    fun info(@StringRes message: Int, vararg args: Any) = add(getString(message, args), LogLevel.INFO)
    fun success(@StringRes message: Int, vararg args: Any) = add(getString(message, args), LogLevel.SUCCESS)
    fun error(@StringRes message: Int, vararg args: Any) = add(getString(message, args), LogLevel.ERROR)

    /** For text that is already resolved, such as a message the backend sent. */
    fun error(message: String) = add(message, LogLevel.ERROR)

    private fun getString(@StringRes message: Int, args: Array<out Any>): String =
        appContext.getString(message, *args)

    private fun add(message: String, level: LogLevel) {
        Log.i(TAG, "[$level] $message")
        _entries.update { it + LogEntry(Timestamps.timeOfDay(System.currentTimeMillis()), message, level) }
    }

    private companion object {
        const val TAG = "ZeroTap"
    }
}
