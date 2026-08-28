package com.github.jmatsu.zerotap.data

import androidx.annotation.StringRes
import com.github.jmatsu.zerotap.R
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonObject

/**
 * The options half of a WebAuthn ceremony.
 *
 * [requestJson] is a `PublicKeyCredentialCreationOptionsJSON` or
 * `PublicKeyCredentialRequestOptionsJSON` exactly as the server built it. The
 * app never inspects it, it only re-serialises it and hands it to Credential
 * Manager, which keeps the challenge opaque to the client.
 */
@Serializable
data class BeginResponse(
    val ceremonyId: String,
    val requestJson: JsonObject,
)

/** The credential half: whatever Credential Manager produced, passed straight back. */
@Serializable
data class FinishRequest(
    val ceremonyId: String,
    val credential: JsonElement,
)

/** Reply to both register/finish endpoints. */
@Serializable
data class RegisterResponse(
    val credentialId: String,
    val user: UserView,
)

@Serializable
data class PasswordRequest(
    val username: String,
    val password: String,
)

@Serializable
data class AuthResponse(
    val token: String,
    val method: String,
    val user: UserView,
    /** Set only by the restore flow: the Restore Key the server just destroyed. */
    val revokedCredentialId: String? = null,
)

@Serializable
data class UserView(
    val username: String,
    val displayName: String,
    val credentials: List<CredentialView>? = null,
) {
    val restoreKeyCount: Int get() = countOf(CredentialKind.RESTORE)
    val passkeyCount: Int get() = countOf(CredentialKind.PASSKEY)

    private fun countOf(kind: CredentialKind) = credentials.orEmpty().count { it.kind == kind.wire }
}

@Serializable
data class CredentialView(
    val id: String,
    val kind: String,
    val createdAt: String,
)

/** The credential kinds the server distinguishes, as they appear on the wire. */
enum class CredentialKind(val wire: String) {
    PASSKEY("passkey"),
    RESTORE("restore"),
}

@Serializable
data class ErrorResponse(val error: String)

/** Identifies how the current session was established, for display. */
enum class SignInMethod(@StringRes val label: Int) {
    PASSWORD(R.string.method_password),
    PASSKEY(R.string.method_passkey),
    RESTORE(R.string.method_restore),
    UNKNOWN(R.string.method_unknown);

    companion object {
        fun of(raw: String): SignInMethod = entries.firstOrNull { it.name.equals(raw, ignoreCase = true) } ?: UNKNOWN
    }
}

data class Session(
    val token: String,
    val username: String,
    val method: SignInMethod,
    val signedInAt: Long = 0L,
    /**
     * The Restore Key this session was created by redeeming, if any. Survives
     * a process restart, so the UI can still report a zero-tap sign-in that
     * happened inside the backup agent before the activity ever ran.
     */
    val redeemedRestoreKeyId: String? = null,
)

/**
 * What the UI shows about this install's Restore Key.
 *
 * [state] and [inSync] are derived rather than stored, so they cannot
 * contradict the record they describe.
 */
data class RestoreKeyStatus(
    val record: RestoreKeyRecord? = null,
    /** Why the last create attempt failed, when there is no record. */
    val failure: String? = null,
    /**
     * Count the server reports, which should agree with [record]. Filled in
     * from the account view rather than stored, so registering a key updates
     * it along with everything else the response carries.
     */
    val serverCount: Int? = null,
) {
    enum class State { NONE, REGISTERED, FAILED }

    val state: State
        get() = when {
            record != null -> State.REGISTERED
            failure != null -> State.FAILED
            else -> State.NONE
        }

    /**
     * Android keeps a single Restore Key per package name and the server
     * replaces rather than accumulates, so the server should hold exactly one
     * key when this device has one, and none otherwise.
     */
    val inSync: Boolean
        get() = serverCount == null || serverCount == expectedServerCount

    val expectedServerCount: Int get() = if (record != null) 1 else 0
}
