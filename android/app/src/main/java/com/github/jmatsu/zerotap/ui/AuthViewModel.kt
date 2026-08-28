package com.github.jmatsu.zerotap.ui

import android.content.Context
import androidx.lifecycle.ViewModel
import androidx.lifecycle.ViewModelProvider
import androidx.lifecycle.viewModelScope
import androidx.lifecycle.viewmodel.initializer
import androidx.lifecycle.viewmodel.viewModelFactory
import com.github.jmatsu.zerotap.AuthGraph
import com.github.jmatsu.zerotap.R
import com.github.jmatsu.zerotap.data.AuthRepository
import com.github.jmatsu.zerotap.data.EventLog
import com.github.jmatsu.zerotap.data.LogEntry
import com.github.jmatsu.zerotap.data.RestoreKeyStatus
import com.github.jmatsu.zerotap.data.Session
import com.github.jmatsu.zerotap.data.describe
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.SharingStarted
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.stateIn
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch

data class AuthUiState(
    val busy: Boolean = false,
    val startupCheckDone: Boolean = false,
    val session: Session? = null,
    val passkeyCount: Int? = null,
    val expectedUsername: String? = null,
    val restoreKey: RestoreKeyStatus = RestoreKeyStatus(),
    val error: String? = null,
)

class AuthViewModel(
    private val repository: AuthRepository,
    private val eventLog: EventLog,
) : ViewModel() {

    private val local = MutableStateFlow(AuthUiState())

    /**
     * Kept out of [AuthUiState] on purpose: the log grows throughout a sign-in,
     * and folding it into the screen state would recompose the whole UI on
     * every entry.
     */
    val log: StateFlow<List<LogEntry>> = eventLog.entries

    val state: StateFlow<AuthUiState> =
        combine(local, repository.restoreKeyStatus, repository.user) { base, restoreKey, user ->
            base.copy(
                restoreKey = restoreKey.copy(serverCount = user?.restoreKeyCount),
                passkeyCount = user?.passkeyCount,
            )
        }.stateIn(viewModelScope, SharingStarted.Eagerly, AuthUiState())

    private var startupAttempted = false

    /**
     * The startup half of rule 2, run once per process: if this install has no
     * session but does hold a Restore Key from a previous device, sign in
     * silently.
     *
     * It runs even though `ZeroTapBackupAgent` already tried. That callback is
     * best-effort and some restore routes skip it altogether, so doing it here
     * as well is what turns the zero-tap path from lucky into reliable.
     *
     * Note the ordering the UI leans on: nothing decides to show a sign-in screen
     * until `startupCheckDone` flips, so a restored install never flashes a
     * sign-in form on its way to being signed in. Copy that if you copy nothing
     * else from the UI layer.
     *
     * The first read of either preferences file blocks until it has loaded, so it
     * happens here inside a coroutine rather than in the constructor.
     */
    fun onStart(context: Context) {
        if (startupAttempted) return
        startupAttempted = true

        launchWork(finally = { local.update { it.copy(startupCheckDone = true) } }) {
            local.update { it.copy(expectedUsername = repository.lastKnownUsername()) }

            val existing = repository.currentSession()
            if (existing != null) {
                eventLog.info(R.string.log_already_signed_in, existing.username)
                local.update { it.copy(session = existing) }
                repository.ensureRestoreKey(context)
                repository.refreshUser()
            } else {
                repository.signInWithRestoreKey(context)?.let { restored ->
                    local.update { it.copy(session = restored) }
                }
            }
        }
    }

    fun signUp(context: Context, username: String, password: String) =
        signIn { repository.signUp(context, username, password) }

    fun signInWithPassword(context: Context, username: String, password: String) =
        signIn { repository.signInWithPassword(context, username, password) }

    fun signInWithPasskey(context: Context) =
        signIn { repository.signInWithPasskey(context) }

    fun registerPasskey(context: Context) = launchWork { repository.registerPasskey(context) }

    fun recreateRestoreKey(context: Context) = launchWork {
        repository.ensureRestoreKey(context, force = true)
    }

    fun signOut(context: Context) = launchWork {
        repository.signOut(context)
        local.update { it.copy(session = null) }
    }

    private fun signIn(block: suspend () -> Session) = launchWork {
        val session = block()
        local.update { it.copy(session = session) }
    }

    private fun launchWork(finally: () -> Unit = {}, block: suspend () -> Unit) {
        viewModelScope.launch {
            local.update { it.copy(busy = true, error = null) }
            try {
                block()
            } catch (e: CancellationException) {
                throw e
            } catch (e: Exception) {
                eventLog.error(e.describe())
                local.update { it.copy(error = e.describe()) }
            } finally {
                local.update { it.copy(busy = false) }
                finally()
            }
        }
    }

    companion object {
        fun factory(graph: AuthGraph): ViewModelProvider.Factory = viewModelFactory {
            initializer { AuthViewModel(graph.repository, graph.log) }
        }
    }
}
