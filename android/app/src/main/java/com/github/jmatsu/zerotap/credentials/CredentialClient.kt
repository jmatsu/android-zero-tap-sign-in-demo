package com.github.jmatsu.zerotap.credentials

import android.content.Context
import android.util.Log
import androidx.credentials.ClearCredentialStateRequest
import androidx.credentials.CreatePublicKeyCredentialRequest
import androidx.credentials.CreatePublicKeyCredentialResponse
import androidx.credentials.CreateRestoreCredentialRequest
import androidx.credentials.CreateRestoreCredentialResponse
import androidx.credentials.CredentialManager
import androidx.credentials.GetCredentialRequest
import androidx.credentials.GetPublicKeyCredentialOption
import androidx.credentials.GetRestoreCredentialOption
import androidx.credentials.PublicKeyCredential
import androidx.credentials.RestoreCredential
import androidx.credentials.exceptions.GetCredentialException
import androidx.credentials.exceptions.NoCredentialException
import androidx.credentials.exceptions.restorecredential.E2eeUnavailableException

/**
 * Everything this app does with Credential Manager.
 *
 * Passkeys and Restore Keys are both WebAuthn credentials and both take the
 * same server-issued JSON; the difference is who drives the ceremony. A passkey
 * needs the user in front of the screen, a Restore Key is created and replayed
 * by the system with no UI at all, which is exactly what makes zero-tap
 * sign-in possible after a device transfer.
 */
class CredentialClient(private val credentialManager: CredentialManager) {

    suspend fun createPasskey(context: Context, requestJson: String): String {
        val response = credentialManager.createCredential(
            context = context,
            request = CreatePublicKeyCredentialRequest(requestJson),
        ) as CreatePublicKeyCredentialResponse
        return response.registrationResponseJson
    }

    suspend fun getPasskey(context: Context, requestJson: String): String {
        val request = GetCredentialRequest(listOf(GetPublicKeyCredentialOption(requestJson)))
        val credential = credentialManager.getCredential(context, request).credential as PublicKeyCredential
        return credential.authenticationResponseJson
    }

    /**
     * Creates the Restore Key. No UI is shown.
     *
     * The key is backed up to the cloud when the device qualifies for
     * end-to-end encrypted backup (Google account, backup enabled, screen
     * lock). When it does not, Credential Manager throws
     * [E2eeUnavailableException] and the only sensible fallback is a local-only
     * key, which still survives a cabled device-to-device transfer.
     */
    suspend fun createRestoreKey(context: Context, requestJson: String): CreateRestoreKeyResult = try {
        createRestoreKey(context, requestJson, cloudBackup = true)
    } catch (_: E2eeUnavailableException) {
        Log.i(TAG, "End-to-end encrypted backup unavailable; falling back to a local-only Restore Key")
        createRestoreKey(context, requestJson, cloudBackup = false)
    }

    private suspend fun createRestoreKey(
        context: Context,
        requestJson: String,
        cloudBackup: Boolean,
    ): CreateRestoreKeyResult {
        val response = credentialManager.createCredential(
            context = context,
            request = CreateRestoreCredentialRequest(requestJson, isCloudBackupEnabled = cloudBackup),
        ) as CreateRestoreCredentialResponse
        return CreateRestoreKeyResult(response.responseJson, cloudBackup)
    }

    /**
     * Asks for the Restore Key that came across with the backup.
     *
     * Returns null when this device has none, which is the ordinary case on a
     * fresh install that was not restored from anywhere.
     */
    suspend fun getRestoreKey(context: Context, requestJson: String): String? = try {
        val request = GetCredentialRequest(listOf(GetRestoreCredentialOption(requestJson)))
        val credential = credentialManager.getCredential(context, request).credential as RestoreCredential
        credential.authenticationResponseJson
    } catch (_: NoCredentialException) {
        null
    } catch (e: GetCredentialException) {
        // Play services reports "no restore credential available" through a few
        // different exception types depending on version, and none of them mean
        // the app is broken: it just has nothing to restore.
        Log.i(TAG, "No Restore Key available on this device: ${e.type}")
        null
    }

    /** Deletes the local Restore Key. Called after it is redeemed and on sign-out. */
    suspend fun clearRestoreKey() {
        credentialManager.clearCredentialState(
            ClearCredentialStateRequest(ClearCredentialStateRequest.TYPE_CLEAR_RESTORE_CREDENTIAL),
        )
    }

    private companion object {
        const val TAG = "CredentialClient"
    }
}

data class CreateRestoreKeyResult(
    val responseJson: String,
    /** False when the key is local-only because the device cannot do E2EE backup. */
    val cloudBackup: Boolean,
)
