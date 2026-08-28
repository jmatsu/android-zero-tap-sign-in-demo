package com.github.jmatsu.zerotap.data

import java.time.Instant
import java.time.ZoneId
import java.time.format.DateTimeFormatter

/**
 * The app's two timestamp formats.
 *
 * [DateTimeFormatter] is immutable and thread-safe, unlike SimpleDateFormat,
 * which matters because these are shared statics written from the backup
 * agent's process as well as the UI.
 */
object Timestamps {
    private val TIME_OF_DAY = DateTimeFormatter.ofPattern("HH:mm:ss")
    private val DATE_TIME = DateTimeFormatter.ofPattern("yyyy-MM-dd HH:mm:ss")

    fun timeOfDay(millis: Long): String = format(TIME_OF_DAY, millis)

    fun dateTime(millis: Long): String = format(DATE_TIME, millis)

    private fun format(formatter: DateTimeFormatter, millis: Long): String =
        formatter.format(Instant.ofEpochMilli(millis).atZone(ZoneId.systemDefault()))
}
