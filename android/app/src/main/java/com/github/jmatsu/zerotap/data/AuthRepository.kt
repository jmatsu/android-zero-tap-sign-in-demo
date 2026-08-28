package com.github.jmatsu.zerotap.data

import android.content.Context
import com.github.jmatsu.zerotap.R
import com.github.jmatsu.zerotap.credentials.CredentialClient
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow

/**
 * Drives every sign-in path in the demo.
 *
 * The interesting one is [signInWithRestoreKey]: it is the whole reason the
 * app can come up already signed in on a device the user has never touched
 * before.
 */
class AuthRepository(
    private val backend: BackendClient,
    private val credentials: CredentialClient,
    private val session: SessionStore,
    private val appState: AppStateStore,
    private val log: EventLog,
) {
    private val _restoreKeyStatus = MutableStateFlow(RestoreKeyStatus(record = appState.restoreKey))

    /** Observable restore-key state, so the UI can show it rather than infer it. */
    val restoreKeyStatus: StateFlow<RestoreKeyStatus> = _restoreKeyStatus.asStateFlow()

    private val _user = MutableStateFlow<UserView?>(null)

    /** The account as the server last described it, updated by every response. */
    val user: StateFlow<UserView?> = _user.asStateFlow()

    fun currentSession(): Session? = session.load()

    fun lastKnownUsername(): String? = appState.lastKnownUsername

    suspend fun signUp(context: Context, username: String, password: String): Session {
        log.info(R.string.log_creating_account, username)
        return signedIn(context, backend.signUp(username, password))
    }

    suspend fun signInWithPassword(context: Context, username: String, password: String): Session {
        log.info(R.string.log_signing_in_with_password)
        return signedIn(context, backend.signInWithPassword(username, password))
    }

    // ------------------------------------------------------------- passkeys

    suspend fun registerPasskey(context: Context) {
        val token = token() ?: error(context.getString(R.string.error_not_signed_in))
        log.info(R.string.log_passkey_asking_options)
        val begin = backend.beginPasskeyRegistration(token)

        log.info(R.string.log_passkey_handing_to_credential_manager)
        val credentialJson = credentials.createPasskey(context, begin.optionsJson())

        _user.value = backend.finishPasskeyRegistration(token, begin.ceremonyId, credentialJson).user
        log.success(R.string.log_passkey_registered)
    }

    suspend fun signInWithPasskey(context: Context): Session {
        log.info(R.string.log_passkey_asking_challenge)
        val begin = backend.beginPasskeySignIn()

        val credentialJson = credentials.getPasskey(context, begin.optionsJson())
        log.info(R.string.log_passkey_assertion_signed)

        return signedIn(context, backend.finishPasskeySignIn(begin.ceremonyId, credentialJson))
    }

    // -------------------------------------------------------- Restore Keys

    /**
     * Registers a Restore Key for the signed-in account, unless this install
     * already has one.
     *
     * Nothing is shown to the user. The key is what a future device will use to
     * prove, without asking anyone, that it inherited this account.
     */
    suspend fun ensureRestoreKey(context: Context, force: Boolean = false) {
        val token = token() ?: return
        if (appState.hasRestoreKey && !force) {
            log.info(R.string.log_restore_already_registered)
            return
        }

        try {
            log.info(R.string.log_restore_asking_options)
            val begin = backend.beginRestoreRegistration(token)

            val result = credentials.createRestoreKey(context, begin.optionsJson())
            val registered = backend.finishRestoreRegistration(token, begin.ceremonyId, result.responseJson)
            _user.value = registered.user

            val record = RestoreKeyRecord(
                credentialId = registered.credentialId,
                createdAt = System.currentTimeMillis(),
                cloudBackup = result.cloudBackup,
            )
            setRestoreKey(record)

            if (result.cloudBackup) {
                log.success(R.string.log_restore_created_cloud, record.credentialId.short())
            } else {
                log.success(R.string.log_restore_created_local, record.credentialId.short())
            }
        } catch (e: CancellationException) {
            throw e
        } catch (e: Exception) {
            // A missing Restore Key costs the user nothing today; it only means
            // the next device transfer will ask them to sign in by hand.
            setRestoreKey(null, failure = e.describe())
            log.error(R.string.log_restore_create_failed, e.describe())
        }
    }

    /**
     * The zero-tap path, run at startup on a device that was just restored.
     *
     * Returns null when this device holds no Restore Key, which is the normal
     * outcome of an ordinary fresh install.
     */
    suspend fun signInWithRestoreKey(context: Context): Session? {
        log.info(R.string.log_restore_looking)
        val begin = backend.beginRestoreSignIn()

        val credentialJson = credentials.getRestoreKey(context, begin.optionsJson())
        if (credentialJson == null) {
            // App data can be restored without the Restore Key coming with it,
            // for example when the previous device could not back the key up to
            // an end-to-end encrypted cloud. The restored record would then be a
            // lie, and would stop the next sign-in from creating a replacement.
            setRestoreKey(null)
            log.info(R.string.log_restore_none)
            return null
        }

        log.info(R.string.log_restore_verifying)
        val response = backend.finishRestoreSignIn(begin.ceremonyId, credentialJson)
        val restored = remember(response, redeemedRestoreKeyId = response.revokedCredentialId)

        log.success(R.string.log_restore_signed_in, restored.username)
        response.revokedCredentialId?.let { log.info(R.string.log_restore_revoked_key, it.short()) }

        // The server already destroyed its half, so the copy sitting on this
        // device is dead weight. Clear it before minting the replacement.
        forgetRestoreKey()
        ensureRestoreKey(context)
        return restored
    }

    // ------------------------------------------------------------- sign out

    suspend fun signOut(context: Context) {
        val token = token()

        if (token != null) {
            runCatching { backend.revokeRestoreKeys(token) }
                .onSuccess { log.info(R.string.log_restore_revoked_account) }
                .onFailure { log.error(R.string.log_restore_revoke_failed, it.describe()) }
            runCatching { backend.signOut(token) }
        }

        forgetRestoreKey()
        session.clear()
        _user.value = null
        log.info(R.string.log_signed_out)
    }

    /**
     * Re-reads the account from the server.
     *
     * Only worth calling when nothing else just returned it — every sign-in and
     * registration response already carries the current [UserView].
     */
    suspend fun refreshUser() {
        val token = token() ?: return
        runCatching { backend.me(token) }.getOrNull()?.let { _user.value = it }
    }

    // -------------------------------------------------------------- helpers

    /** Writes both halves of the restore-key fact, so they cannot drift apart. */
    private fun setRestoreKey(record: RestoreKeyRecord?, failure: String? = null) {
        appState.restoreKey = record
        _restoreKeyStatus.value = RestoreKeyStatus(record = record, failure = failure)
    }

    private suspend fun forgetRestoreKey() {
        setRestoreKey(null)
        runCatching { credentials.clearRestoreKey() }
            .onSuccess { log.info(R.string.log_restore_cleared) }
            .onFailure { log.error(R.string.log_restore_clear_failed, it.describe()) }
    }

    private fun token(): String? = session.load()?.token

    /** Records a fresh session and arms this device for the next transfer. */
    private suspend fun signedIn(context: Context, response: AuthResponse): Session {
        val newSession = remember(response)
        ensureRestoreKey(context)
        return newSession
    }

    private fun remember(response: AuthResponse, redeemedRestoreKeyId: String? = null): Session {
        val newSession = Session(
            token = response.token,
            username = response.user.username,
            method = SignInMethod.of(response.method),
            signedInAt = System.currentTimeMillis(),
            redeemedRestoreKeyId = redeemedRestoreKeyId,
        )
        session.save(newSession)
        appState.lastKnownUsername = newSession.username
        _user.value = response.user
        return newSession
    }

    // JsonObject.toString() already emits valid JSON, so there is nothing to
    // re-serialise here.
    private fun BeginResponse.optionsJson(): String = requestJson.toString()
}

/** Credential ids are long and opaque; the first few characters are enough to follow along. */
fun String.short(): String = if (length > 12) take(12) + "\u2026" else this

/** Turns exceptions into something worth putting on screen. */
fun Throwable.describe(): String = when (this) {
    is BackendException -> message ?: "HTTP $status"
    else -> message ?: this::class.java.simpleName
}
