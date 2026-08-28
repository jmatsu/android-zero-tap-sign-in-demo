package com.github.jmatsu.zerotap.backup

import android.app.backup.BackupAgent
import android.app.backup.BackupDataInput
import android.app.backup.BackupDataOutput
import android.os.ParcelFileDescriptor
import android.util.Log
import com.github.jmatsu.zerotap.AuthGraph
import kotlinx.coroutines.runBlocking
import kotlinx.coroutines.withTimeout

/**
 * Signs the user in the moment their data lands on the new device.
 *
 * The app declares `android:fullBackupOnly="true"`, so Auto Backup moves the
 * data and this agent exists only for [onRestoreFinished]. Doing the restore
 * sign-in here rather than waiting for the first launch means the account is
 * already live before the user ever opens the app, which is what makes the
 * sign-in genuinely zero-tap rather than merely fast.
 *
 * MainActivity retries the same call on startup, so nothing breaks if this
 * callback is skipped or times out.
 */
class ZeroTapBackupAgent : BackupAgent() {

    override fun onRestoreFinished() {
        super.onRestoreFinished()

        val graph = AuthGraph(this)
        try {
            runBlocking {
                withTimeout(RESTORE_TIMEOUT_MS) {
                    val session = graph.repository.signInWithRestoreKey(this@ZeroTapBackupAgent)
                    if (session == null) {
                        Log.i(TAG, "Restore finished with no Restore Key available")
                    } else {
                        Log.i(TAG, "Restore finished; signed in as ${session.username}")
                    }
                }
            }
        } catch (e: Exception) {
            // The backup framework gives an agent a short, hard budget. Failing
            // here is recoverable: the activity tries again on first launch.
            Log.w(TAG, "Restore sign-in did not complete in the backup agent", e)
        }
    }

    // Not used: Auto Backup handles the data because of android:fullBackupOnly.
    override fun onBackup(oldState: ParcelFileDescriptor?, data: BackupDataOutput?, newState: ParcelFileDescriptor?) = Unit

    override fun onRestore(data: BackupDataInput?, appVersionCode: Int, newState: ParcelFileDescriptor?) = Unit

    private companion object {
        const val TAG = "ZeroTapBackupAgent"
        const val RESTORE_TIMEOUT_MS = 20_000L
    }
}
