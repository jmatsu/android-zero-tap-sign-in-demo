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
 * This is the whole difference between a fast sign-in and a zero-tap one. By the
 * time the user first taps the icon, the account is already live: no spinner, no
 * "signing you in", nothing to look at.
 *
 * It is cheap because the app declares `android:fullBackupOnly="true"`. Auto
 * Backup still moves all the data, so this agent exists purely for
 * [onRestoreFinished] and [onBackup]/[onRestore] stay empty.
 *
 * Three things to know before you copy it:
 *
 * - It runs in its own process, before any activity, and the [android.app.Application]
 *   object there is not the one your UI uses. Build what you need from the plain
 *   [android.content.Context] the agent itself is — see `AuthGraph`. This is the
 *   detail that catches people out with a DI framework.
 * - It is best-effort. The callback can be skipped entirely, and it runs against
 *   a short budget the framework enforces. `MainActivity` makes the same call on
 *   startup, and that retry is what makes the flow correct; treat this agent as
 *   an optimisation, never as the only path.
 * - Only a genuine restore runs it. Android Studio's Backup/Restore tooling does
 *   not, so the emulator route exercises the activity path instead. See
 *   docs/device-migration-test.md for the one that gets here.
 */
class ZeroTapBackupAgent : BackupAgent() {

    override fun onRestoreFinished() {
        super.onRestoreFinished()

        val graph = AuthGraph(this)
        try {
            // runBlocking, unusually, is right here: onRestoreFinished is a
            // synchronous callback, and returning early would let the framework
            // tear the process down in the middle of the ceremony.
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
            // Nothing to do but log it. The activity retries on first launch, so
            // a timeout here costs the user a moment, not their session — which
            // is exactly why the retry has to exist.
            Log.w(TAG, "Restore sign-in did not complete in the backup agent", e)
        }
    }

    // Unused: android:fullBackupOnly="true" leaves the data to Auto Backup, so
    // this class is a restore hook and nothing else.
    override fun onBackup(oldState: ParcelFileDescriptor?, data: BackupDataOutput?, newState: ParcelFileDescriptor?) = Unit

    override fun onRestore(data: BackupDataInput?, appVersionCode: Int, newState: ParcelFileDescriptor?) = Unit

    private companion object {
        const val TAG = "ZeroTapBackupAgent"
        const val RESTORE_TIMEOUT_MS = 20_000L
    }
}
