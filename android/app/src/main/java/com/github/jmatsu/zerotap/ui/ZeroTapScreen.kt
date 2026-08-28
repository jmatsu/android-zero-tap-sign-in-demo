package com.github.jmatsu.zerotap.ui

import android.app.Activity
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material3.Button
import androidx.compose.material3.Card
import androidx.compose.material3.CardDefaults
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.LocalContentColor
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.input.PasswordVisualTransformation
import androidx.compose.ui.res.pluralStringResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import com.github.jmatsu.zerotap.R
import com.github.jmatsu.zerotap.Settings
import com.github.jmatsu.zerotap.data.LogEntry
import com.github.jmatsu.zerotap.data.LogLevel
import com.github.jmatsu.zerotap.data.RestoreKeyStatus
import com.github.jmatsu.zerotap.data.Session
import com.github.jmatsu.zerotap.data.SignInMethod
import com.github.jmatsu.zerotap.data.Timestamps
import com.github.jmatsu.zerotap.data.short

@Composable
fun ZeroTapScreen(viewModel: AuthViewModel, settings: Settings, activity: Activity) {
    val state by viewModel.state.collectAsStateWithLifecycle()
    val log by viewModel.log.collectAsStateWithLifecycle()

    Scaffold { padding ->
        Column(
            modifier = Modifier
                .fillMaxSize()
                .padding(padding)
                .padding(16.dp),
            verticalArrangement = Arrangement.spacedBy(12.dp),
        ) {
            Header(rpId = settings.rpId)

            if (state.busy) {
                LinearProgressIndicator(modifier = Modifier.fillMaxWidth())
            }

            state.error?.let { message ->
                Text(
                    text = message,
                    color = MaterialTheme.colorScheme.error,
                    style = MaterialTheme.typography.bodyMedium,
                )
            }

            when {
                !state.startupCheckDone -> StartupCheck(expectedUsername = state.expectedUsername)

                else -> state.session?.let { session ->
                    SignedIn(
                        session = session,
                        passkeyCount = state.passkeyCount,
                        restoreKey = state.restoreKey,
                        busy = state.busy,
                        onCreatePasskey = { viewModel.registerPasskey(activity) },
                        onRecreateRestoreKey = { viewModel.recreateRestoreKey(activity) },
                        onSignOut = { viewModel.signOut(activity) },
                    )
                } ?: SignedOut(
                    busy = state.busy,
                    settings = settings,
                    onSignIn = { user, password -> viewModel.signInWithPassword(activity, user, password) },
                    onSignUp = { user, password -> viewModel.signUp(activity, user, password) },
                    onPasskey = { viewModel.signInWithPasskey(activity) },
                )
            }

            HorizontalDivider()
            EventLogView(entries = log, modifier = Modifier.weight(1f))
        }
    }
}

@Composable
private fun Header(rpId: String) {
    Column {
        Text(stringResource(R.string.header_title), style = MaterialTheme.typography.headlineSmall)
        Text(
            text = stringResource(R.string.header_relying_party, rpId),
            style = MaterialTheme.typography.bodySmall,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
        )
    }
}

@Composable
private fun StartupCheck(expectedUsername: String?) {
    Card(modifier = Modifier.fillMaxWidth()) {
        Row(
            modifier = Modifier.padding(16.dp),
            horizontalArrangement = Arrangement.spacedBy(16.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            CircularProgressIndicator()
            Column {
                Text(stringResource(R.string.startup_checking), style = MaterialTheme.typography.titleMedium)
                Text(
                    text = expectedUsername?.let { stringResource(R.string.startup_restored_from, it) }
                        ?: stringResource(R.string.startup_maybe_transferred),
                    style = MaterialTheme.typography.bodySmall,
                )
            }
        }
    }
}

@Composable
private fun SignedOut(
    busy: Boolean,
    settings: Settings,
    onSignIn: (String, String) -> Unit,
    onSignUp: (String, String) -> Unit,
    onPasskey: () -> Unit,
) {
    // Prefilled with the account the backend seeds, and saveable so a rotation
    // does not wipe what has been typed.
    var username by rememberSaveable { mutableStateOf(settings.demoUsername) }
    var password by rememberSaveable { mutableStateOf(settings.demoPassword) }

    Card(modifier = Modifier.fillMaxWidth()) {
        Column(
            modifier = Modifier.padding(16.dp),
            verticalArrangement = Arrangement.spacedBy(12.dp),
        ) {
            Text(stringResource(R.string.sign_in_title), style = MaterialTheme.typography.titleMedium)

            OutlinedTextField(
                value = username,
                onValueChange = { username = it },
                label = { Text(stringResource(R.string.field_username)) },
                singleLine = true,
                modifier = Modifier.fillMaxWidth(),
            )
            OutlinedTextField(
                value = password,
                onValueChange = { password = it },
                label = { Text(stringResource(R.string.field_password)) },
                singleLine = true,
                visualTransformation = PasswordVisualTransformation(),
                keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Password),
                modifier = Modifier.fillMaxWidth(),
            )

            Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                Button(
                    onClick = { onSignIn(username.trim(), password) },
                    enabled = !busy && username.isNotBlank() && password.isNotBlank(),
                ) { Text(stringResource(R.string.action_sign_in)) }

                OutlinedButton(
                    onClick = { onSignUp(username.trim(), password) },
                    enabled = !busy && username.isNotBlank() && password.length >= 8,
                ) { Text(stringResource(R.string.action_create_account)) }
            }

            if (settings.demoUsername.isNotEmpty()) {
                Text(
                    text = stringResource(R.string.sign_in_prefilled_note),
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }

            HorizontalDivider()

            TextButton(onClick = onPasskey, enabled = !busy) {
                Text(stringResource(R.string.action_sign_in_with_passkey))
            }
        }
    }
}

@Composable
private fun SignedIn(
    session: Session,
    passkeyCount: Int?,
    restoreKey: RestoreKeyStatus,
    busy: Boolean,
    onCreatePasskey: () -> Unit,
    onRecreateRestoreKey: () -> Unit,
    onSignOut: () -> Unit,
) {
    Column(verticalArrangement = Arrangement.spacedBy(12.dp)) {
        if (session.method == SignInMethod.RESTORE) {
            ZeroTapBanner(session)
        }

        Card(modifier = Modifier.fillMaxWidth()) {
            Column(
                modifier = Modifier.padding(16.dp),
                verticalArrangement = Arrangement.spacedBy(8.dp),
            ) {
                Text(
                    text = stringResource(R.string.signed_in_as, session.username),
                    style = MaterialTheme.typography.titleMedium,
                )
                Text(
                    text = stringResource(R.string.signed_in_with, stringResource(session.method.label)),
                    style = MaterialTheme.typography.bodyMedium,
                )
                passkeyCount?.let { count ->
                    Text(
                        text = pluralStringResource(R.plurals.passkeys_registered, count, count),
                        style = MaterialTheme.typography.bodySmall,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                }

                Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    Button(onClick = onCreatePasskey, enabled = !busy) {
                        Text(stringResource(R.string.action_create_passkey))
                    }
                    OutlinedButton(onClick = onSignOut, enabled = !busy) {
                        Text(stringResource(R.string.action_sign_out))
                    }
                }
            }
        }

        RestoreKeyCard(status = restoreKey, busy = busy, onRecreate = onRecreateRestoreKey)
    }
}

/**
 * The payoff screen: this install signed itself in with a key it inherited,
 * without anyone touching the device.
 */
@Composable
internal fun ZeroTapBanner(session: Session) {
    Card(
        modifier = Modifier.fillMaxWidth(),
        colors = CardDefaults.cardColors(
            containerColor = MaterialTheme.colorScheme.primaryContainer,
            contentColor = MaterialTheme.colorScheme.onPrimaryContainer,
        ),
    ) {
        Column(
            modifier = Modifier.padding(16.dp),
            verticalArrangement = Arrangement.spacedBy(4.dp),
        ) {
            Text(
                text = stringResource(R.string.banner_title),
                style = MaterialTheme.typography.titleMedium,
                fontWeight = FontWeight.Bold,
            )
            Text(
                text = stringResource(R.string.banner_body),
                style = MaterialTheme.typography.bodyMedium,
            )
            session.redeemedRestoreKeyId?.let { id ->
                LabelledValue(stringResource(R.string.label_key_redeemed), id.short())
                Text(
                    text = stringResource(R.string.banner_revoked_note),
                    style = MaterialTheme.typography.bodySmall,
                )
            }
            if (session.signedInAt > 0L) {
                LabelledValue(stringResource(R.string.label_signed_in_at), rememberTimestamp(session.signedInAt))
            }
        }
    }
}

/** Always-visible state of the key that will carry this account to the next device. */
@Composable
internal fun RestoreKeyCard(
    status: RestoreKeyStatus,
    busy: Boolean,
    onRecreate: () -> Unit,
) {
    val (accent, label) = when (status.state) {
        RestoreKeyStatus.State.REGISTERED ->
            MaterialTheme.colorScheme.primary to stringResource(R.string.restore_key_state_registered)

        RestoreKeyStatus.State.FAILED ->
            MaterialTheme.colorScheme.error to stringResource(R.string.restore_key_state_failed)

        RestoreKeyStatus.State.NONE ->
            MaterialTheme.colorScheme.onSurfaceVariant to stringResource(R.string.restore_key_state_none)
    }

    Card(modifier = Modifier.fillMaxWidth()) {
        Column(
            modifier = Modifier.padding(16.dp),
            verticalArrangement = Arrangement.spacedBy(6.dp),
        ) {
            Row(
                modifier = Modifier.fillMaxWidth(),
                horizontalArrangement = Arrangement.SpaceBetween,
                verticalAlignment = Alignment.CenterVertically,
            ) {
                Text(stringResource(R.string.restore_key_title), style = MaterialTheme.typography.titleMedium)
                Text(
                    text = label,
                    style = MaterialTheme.typography.labelLarge,
                    color = accent,
                    fontWeight = FontWeight.Bold,
                )
            }

            val record = status.record
            when {
                record != null -> {
                    LabelledValue(stringResource(R.string.label_key), record.credentialId.short())
                    LabelledValue(stringResource(R.string.label_created), rememberTimestamp(record.createdAt))
                    LabelledValue(
                        label = stringResource(R.string.label_backup),
                        value = stringResource(
                            if (record.cloudBackup) R.string.backup_cloud_eligible else R.string.backup_local_only,
                        ),
                    )
                    Text(
                        text = stringResource(R.string.restore_key_next_device_note),
                        style = MaterialTheme.typography.bodySmall,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                }

                status.failure != null -> {
                    Text(
                        text = status.failure,
                        style = MaterialTheme.typography.bodyMedium,
                        color = MaterialTheme.colorScheme.error,
                    )
                    Text(
                        text = stringResource(R.string.restore_key_failure_note),
                        style = MaterialTheme.typography.bodySmall,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                }

                else -> Text(
                    text = stringResource(R.string.restore_key_none),
                    style = MaterialTheme.typography.bodyMedium,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }

            status.serverCount?.let { count ->
                val held = pluralStringResource(R.plurals.server_restore_key_count, count, count)
                LabelledValue(
                    label = stringResource(R.string.label_server_holds),
                    value = if (status.inSync) held else stringResource(R.string.restore_key_out_of_step, held),
                    valueColor = if (status.inSync) null else MaterialTheme.colorScheme.error,
                )
            }

            TextButton(onClick = onRecreate, enabled = !busy) {
                Text(
                    stringResource(
                        if (record != null) {
                            R.string.action_recreate_restore_key
                        } else {
                            R.string.action_create_restore_key
                        },
                    ),
                )
            }
        }
    }
}

@Composable
private fun LabelledValue(label: String, value: String, valueColor: Color? = null) {
    Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
        Text(
            text = label,
            style = MaterialTheme.typography.bodySmall,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
            modifier = Modifier.width(96.dp),
        )
        Text(
            text = value,
            style = MaterialTheme.typography.bodySmall,
            fontFamily = FontFamily.Monospace,
            color = valueColor ?: LocalContentColor.current,
        )
    }
}

/** Formatting allocates, and these recompose with every log line. */
@Composable
private fun rememberTimestamp(millis: Long): String = remember(millis) { Timestamps.dateTime(millis) }

@Composable
private fun EventLogView(entries: List<LogEntry>, modifier: Modifier = Modifier) {
    val listState = rememberLazyListState()

    LaunchedEffect(entries.size) {
        if (entries.isNotEmpty()) listState.animateScrollToItem(entries.lastIndex)
    }

    Column(modifier = modifier) {
        Text(stringResource(R.string.log_view_title), style = MaterialTheme.typography.titleSmall)
        Spacer(Modifier.height(4.dp))
        LazyColumn(
            state = listState,
            modifier = Modifier
                .fillMaxWidth()
                .weight(1f)
                .background(MaterialTheme.colorScheme.surfaceVariant, RoundedCornerShape(8.dp))
                .padding(8.dp),
            verticalArrangement = Arrangement.spacedBy(2.dp),
        ) {
            items(entries) { entry ->
                Text(
                    text = stringResource(R.string.log_entry, entry.timestamp, entry.message),
                    style = MaterialTheme.typography.bodySmall,
                    fontFamily = FontFamily.Monospace,
                    color = entry.level.color(),
                )
            }
        }
    }
}

@Composable
private fun LogLevel.color(): Color = when (this) {
    LogLevel.INFO -> MaterialTheme.colorScheme.onSurfaceVariant
    LogLevel.SUCCESS -> MaterialTheme.colorScheme.primary
    LogLevel.ERROR -> MaterialTheme.colorScheme.error
}
