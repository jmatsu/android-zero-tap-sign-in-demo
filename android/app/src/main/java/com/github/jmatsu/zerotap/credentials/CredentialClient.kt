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
 * Every Credential Manager call this app makes. If you are adding Zero-Tap
 * Sign-In to your own app, the three Restore Key methods below are very nearly
 * the whole client-side API surface, and they all come from
 * `androidx.credentials`.
 *
 * Passkeys and Restore Keys are both WebAuthn credentials and both take the same
 * server-issued JSON. What differs is who drives the ceremony:
 *
 * - **Passkey** — the user is in front of the screen and answers a prompt, on
 *   creation and on every use.
 * - **Restore Key** — the system creates it and later replays it with no UI at
 *   all, and only ever on a device that inherited a backup.
 *
 * Two consequences worth internalising before you write any of this. Because the
 * Restore Key calls show nothing, they are safe to make from places that have no
 * activity on screen — a backup agent, for instance. And because they show
 * nothing, there is no user to interpret a failure for you: every one of them
 * has to be handled in code, which is what the rest of this file is about.
 */
class CredentialClient(private val credentialManager: CredentialManager) {

    // The two ordinary passkey calls, here for contrast. Same requestJson, from
    // the same shape of endpoint — but these suspend on a user-facing prompt,
    // and they need a Context that can actually show one.
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
     * Registers the Restore Key. Shows nothing, asks nothing.
     *
     * Call it after every successful sign-in, by any method — see
     * `AuthRepository.signedIn`. There is no good reason to put it behind a
     * button, and a button would defeat the point.
     *
     * `isCloudBackupEnabled` decides how far the key can travel. True asks for it
     * to go into end-to-end encrypted cloud backup, which is what survives
     * "set up a new phone from the cloud". A device that cannot do E2EE backup
     * — no Google account, backup off, or no screen lock — throws
     * [E2eeUnavailableException] instead. Do not give up there: retry with false
     * for a local-only key, which still survives a cabled device-to-device
     * transfer.
     *
     * Record which of the two you got. The promise you can make the user is
     * different, and this call is the only moment you can tell them apart.
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
     * Asks for the Restore Key that came across with the backup, if there was
     * one.
     *
     * Call this at startup, before you decide to show a sign-in screen. Null is
     * the ordinary answer, not an error: it is what every fresh install that was
     * not restored from anywhere returns. Treat it as "carry on to the normal
     * sign-in flow", never as a failure worth putting in front of the user.
     */
    suspend fun getRestoreKey(context: Context, requestJson: String): String? = try {
        val request = GetCredentialRequest(listOf(GetRestoreCredentialOption(requestJson)))
        val credential = credentialManager.getCredential(context, request).credential as RestoreCredential
        credential.authenticationResponseJson
    } catch (_: NoCredentialException) {
        null
    } catch (e: GetCredentialException) {
        // Play services signals "there is no Restore Key here" through several
        // different exception types depending on its version, and new ones have
        // appeared over time. Catching the base class and reading it as "no key"
        // is deliberate: the alternative is an app that shows an error screen on
        // a perfectly ordinary first launch.
        Log.i(TAG, "No Restore Key available on this device: ${e.type}")
        null
    }

    /**
     * Deletes this device's Restore Key. Call it in both places the key stops
     * being valid: immediately after the server redeems it — it is single use,
     * so the local copy is already dead — and on sign-out, alongside the
     * server-side revoke.
     */
    suspend fun clearRestoreKey() {
        credentialManager.clearCredentialState(
            ClearCredentialStateRequest(ClearCredentialStateRequest.TYPE_CLEAR_RESTORE_CREDENTIAL),
        )
    }

    private companion object {
        const val TAG = "CredentialClient"
    }
}

/**
 * What [CredentialClient.createRestoreKey] produced: the response to post to the
 * server, and how far the resulting key can travel.
 */
data class CreateRestoreKeyResult(
    val responseJson: String,
    /**
     * True when the key went into end-to-end encrypted cloud backup and will
     * survive setting up a new phone from the cloud. False when the device could
     * not manage that and the key is local-only: still fine for a cabled
     * device-to-device transfer, useless once the old phone is in a drawer.
     */
    val cloudBackup: Boolean,
)
