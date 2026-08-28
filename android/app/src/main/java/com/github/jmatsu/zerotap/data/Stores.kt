package com.github.jmatsu.zerotap.data

import android.content.Context
import androidx.core.content.edit

/**
 * The session token, deliberately kept out of every backup (see
 * res/xml/data_extraction_rules.xml). A token identifies one install, so
 * letting it ride along to device B would hide the whole point of the demo.
 *
 * The sign-in method is stored alongside it, which is what lets the UI still
 * report a zero-tap sign-in that happened in the backup agent's process, before
 * the activity ever ran.
 */
class SessionStore(context: Context) {
    private val prefs = context.getSharedPreferences(NAME, Context.MODE_PRIVATE)

    fun load(): Session? {
        val token = prefs.getString(KEY_TOKEN, null) ?: return null
        return Session(
            token = token,
            username = prefs.getString(KEY_USERNAME, "").orEmpty(),
            method = SignInMethod.of(prefs.getString(KEY_METHOD, "").orEmpty()),
            signedInAt = prefs.getLong(KEY_SIGNED_IN_AT, 0L),
            redeemedRestoreKeyId = prefs.getString(KEY_REDEEMED_KEY, null),
        )
    }

    fun save(session: Session) = prefs.edit {
        putString(KEY_TOKEN, session.token)
        putString(KEY_USERNAME, session.username)
        putString(KEY_METHOD, session.method.name)
        putLong(KEY_SIGNED_IN_AT, session.signedInAt)
        putString(KEY_REDEEMED_KEY, session.redeemedRestoreKeyId)
    }

    fun clear() = prefs.edit { clear() }

    private companion object {
        const val NAME = "session"
        const val KEY_TOKEN = "token"
        const val KEY_USERNAME = "username"
        const val KEY_METHOD = "method"
        const val KEY_SIGNED_IN_AT = "signed_in_at"
        const val KEY_REDEEMED_KEY = "redeemed_restore_key_id"
    }
}

/**
 * What this install knows about its Restore Key.
 *
 * Recording it avoids running a create-restore-key ceremony on every sign-in,
 * as the Restore Credentials guidance recommends, and it gives the UI something
 * concrete to show instead of a bare "probably fine".
 */
data class RestoreKeyRecord(
    val credentialId: String,
    val createdAt: Long,
    /** False when the device could not do E2EE backup and the key is local-only. */
    val cloudBackup: Boolean,
)

/**
 * State that is meant to survive a device transfer.
 *
 * Note that the Restore Key record travels with the backup too. On the new
 * device it describes the key that came across, which is exactly what the UI
 * wants to say before the zero-tap sign-in replaces it.
 */
class AppStateStore(context: Context) {
    private val prefs = context.getSharedPreferences(NAME, Context.MODE_PRIVATE)

    var restoreKey: RestoreKeyRecord?
        get() {
            val id = prefs.getString(KEY_ID, null) ?: return null
            return RestoreKeyRecord(
                credentialId = id,
                createdAt = prefs.getLong(KEY_CREATED_AT, 0L),
                cloudBackup = prefs.getBoolean(KEY_CLOUD_BACKUP, false),
            )
        }
        set(value) = prefs.edit {
            if (value == null) {
                remove(KEY_ID).remove(KEY_CREATED_AT).remove(KEY_CLOUD_BACKUP)
            } else {
                putString(KEY_ID, value.credentialId)
                putLong(KEY_CREATED_AT, value.createdAt)
                putBoolean(KEY_CLOUD_BACKUP, value.cloudBackup)
            }
        }

    /** Cheaper than reading [restoreKey] back just to null-check it. */
    val hasRestoreKey: Boolean get() = prefs.contains(KEY_ID)

    /** Purely cosmetic: lets device B say who it expects before it signs in. */
    var lastKnownUsername: String?
        get() = prefs.getString(KEY_LAST_USERNAME, null)
        set(value) = prefs.edit { putString(KEY_LAST_USERNAME, value) }

    private companion object {
        const val NAME = "app_state"
        const val KEY_ID = "restore_key_id"
        const val KEY_CREATED_AT = "restore_key_created_at"
        const val KEY_CLOUD_BACKUP = "restore_key_cloud_backup"
        const val KEY_LAST_USERNAME = "last_known_username"
    }
}
