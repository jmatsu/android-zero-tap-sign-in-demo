package com.github.jmatsu.zerotap.data

import android.content.Context
import com.github.jmatsu.zerotap.R
import com.github.jmatsu.zerotap.credentials.CredentialClient
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow

/**
 * Every sign-in path in the demo, and the ordering rules that make the restore
 * path actually work.
 *
 * If you read one file on the Android side, read this one. [CredentialClient]
 * shows *which* APIs to call; this shows *when*, and that is where integrations
 * go wrong. Three rules, each of which fails quietly when you skip it:
 *
 * 1. Register a Restore Key after every successful sign-in ([signedIn]).
 *    Skip it and the account is simply never armed for a transfer.
 * 2. Try to redeem one before showing a sign-in screen ([signInWithRestoreKey],
 *    called from the backup agent and again at startup). Skip it and the key you
 *    carefully registered is never used.
 * 3. When one is redeemed, clear the local copy and immediately register a
 *    replacement. Skip it and the first transfer works, the second does not --
 *    which is a bug you will not find without testing two transfers in a row.
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
     * Rule 1. Registers a Restore Key for the signed-in account, unless this
     * install already has one.
     *
     * Nothing is shown to the user, which is exactly why it is safe to call
     * after every sign-in. The key is what a future device will use to prove,
     * without asking anyone, that it inherited this account.
     *
     * The `hasRestoreKey` short-circuit is not just an optimisation: running the
     * create ceremony on every sign-in is what the Restore Credentials guidance
     * tells you not to do. The record travels with the backup, so a restored
     * install correctly reports that it already holds a key.
     *
     * [force] exists for the demo's "recreate" button. Production code would
     * only ever call this without it.
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
            // Never fatal, and never worth interrupting anyone for: the user is
            // signed in either way. All a failure costs is that the *next*
            // device transfer will ask them to sign in by hand. Record why, so
            // the UI can say so, and carry on.
            setRestoreKey(null, failure = e.describe())
            log.error(R.string.log_restore_create_failed, e.describe())
        }
    }

    /**
     * Rule 2, and rule 3 in its second half. The zero-tap path: run it at
     * startup, and from the backup agent, before you conclude that the user
     * needs to sign in.
     *
     * Returns null when this device holds no Restore Key. That is the ordinary
     * outcome of an ordinary fresh install — show your sign-in screen and say
     * nothing about it.
     */
    suspend fun signInWithRestoreKey(context: Context): Session? {
        log.info(R.string.log_restore_looking)
        val begin = backend.beginRestoreSignIn()

        val credentialJson = credentials.getRestoreKey(context, begin.optionsJson())
        if (credentialJson == null) {
            // Subtle, and worth guarding explicitly: app data can arrive without
            // the Restore Key. A device that could only make a local-only key
            // still backs its SharedPreferences up to the cloud, so the restored
            // record claims a key this device does not have — and
            // ensureRestoreKey would then short-circuit and never create a real
            // one, leaving the install permanently unarmed. Clearing the record
            // here is what breaks that cycle.
            setRestoreKey(null)
            log.info(R.string.log_restore_none)
            return null
        }

        log.info(R.string.log_restore_verifying)
        val response = backend.finishRestoreSignIn(begin.ceremonyId, credentialJson)
        val restored = remember(response, redeemedRestoreKeyId = response.revokedCredentialId)

        log.success(R.string.log_restore_signed_in, restored.username)
        response.revokedCredentialId?.let { log.info(R.string.log_restore_revoked_key, it.short()) }

        // Rule 3. The server destroyed its half on redemption, so the copy on
        // this device is already dead. Clear it, then immediately arm the device
        // again for whatever transfer comes next — without this pair of calls
        // the first transfer works and the second silently does not.
        forgetRestoreKey()
        ensureRestoreKey(context)
        return restored
    }

    // ------------------------------------------------------------- sign out

    /**
     * Signs out, and disarms the device in both directions: the server drops the
     * account's Restore Key and the device drops its copy.
     *
     * Skipping the revoke would leave a signed-out phone still able to hand its
     * account to whatever device inherits its backup.
     */
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

    /**
     * Writes the stored record and the observable status together, so the thing
     * that survives a transfer and the thing the UI shows cannot drift apart.
     */
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

    /**
     * Records a fresh session and arms this device for the next transfer. Rule 1,
     * applied uniformly: every sign-in method funnels through here, so none of
     * them can forget.
     */
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
