package com.github.jmatsu.zerotap

import android.app.Application
import android.content.Context
import androidx.credentials.CredentialManager
import com.github.jmatsu.zerotap.credentials.CredentialClient
import com.github.jmatsu.zerotap.data.AppStateStore
import com.github.jmatsu.zerotap.data.AuthRepository
import com.github.jmatsu.zerotap.data.BackendClient
import com.github.jmatsu.zerotap.data.EventLog
import com.github.jmatsu.zerotap.data.SessionStore
import kotlinx.serialization.json.Json

/**
 * Hand-rolled dependency container, built from a plain [Context] rather than
 * from [Application], and safe to create more than once.
 *
 * That is deliberate, and it is the constraint to check first if you use a DI
 * framework: `ZeroTapBackupAgent` runs in a process where the [Application]
 * object is not the one your UI uses, so everything the restore sign-in touches
 * has to be constructible from the agent itself.
 */
class AuthGraph(context: Context) {
    private val appContext = context.applicationContext

    private val json = Json {
        ignoreUnknownKeys = true
        encodeDefaults = true
    }

    /** Build-time facts, so nothing below the composition root reads BuildConfig. */
    val settings = Settings(
        rpId = BuildConfig.RP_ID,
        demoUsername = BuildConfig.DEMO_USERNAME,
        demoPassword = BuildConfig.DEMO_PASSWORD,
    )

    val log = EventLog(appContext)

    val repository = AuthRepository(
        backend = BackendClient(BuildConfig.BACKEND_BASE_URL, json),
        credentials = CredentialClient(CredentialManager.create(appContext)),
        session = SessionStore(appContext),
        appState = AppStateStore(appContext),
        log = log,
    )
}

data class Settings(
    val rpId: String,
    val demoUsername: String,
    val demoPassword: String,
)

class ZeroTapApp : Application() {
    val graph: AuthGraph by lazy { AuthGraph(this) }
}
