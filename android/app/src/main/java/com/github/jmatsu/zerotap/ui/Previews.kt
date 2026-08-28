package com.github.jmatsu.zerotap.ui

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.padding
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.tooling.preview.Preview
import androidx.compose.ui.unit.dp
import com.github.jmatsu.zerotap.data.RestoreKeyRecord
import com.github.jmatsu.zerotap.data.RestoreKeyStatus
import com.github.jmatsu.zerotap.data.Session
import com.github.jmatsu.zerotap.data.SignInMethod

/**
 * The restore-key states are hard to reach by hand — one of them needs an
 * actual device transfer — so they are laid out here for the preview pane.
 */
@Preview(showBackground = true, heightDp = 900)
@Composable
private fun RestoreKeyStatesPreview() {
    val now = 1_772_000_000_000L

    ZeroTapTheme {
        Column(
            modifier = Modifier.padding(16.dp),
            verticalArrangement = Arrangement.spacedBy(12.dp),
        ) {
            ZeroTapBanner(
                session = Session(
                    token = "…",
                    username = "jmatsu",
                    method = SignInMethod.RESTORE,
                    signedInAt = now,
                    redeemedRestoreKeyId = "AbCdEf1234567890",
                ),
            )

            RestoreKeyCard(
                status = RestoreKeyStatus(
                    record = RestoreKeyRecord("NewKey7890abcdef", now, cloudBackup = true),
                    serverCount = 1,
                ),
                busy = false,
                onRecreate = {},
            )

            RestoreKeyCard(
                status = RestoreKeyStatus(
                    record = RestoreKeyRecord("LocalOnly123456", now, cloudBackup = false),
                    serverCount = 1,
                ),
                busy = false,
                onRecreate = {},
            )

            RestoreKeyCard(
                status = RestoreKeyStatus(
                    failure = "registration verification failed: Error validating origin",
                    serverCount = 0,
                ),
                busy = false,
                onRecreate = {},
            )

            RestoreKeyCard(
                status = RestoreKeyStatus(serverCount = 0),
                busy = false,
                onRecreate = {},
            )

            // The device thinks it has a key the server does not know about.
            RestoreKeyCard(
                status = RestoreKeyStatus(
                    record = RestoreKeyRecord("OutOfStep123456", now, cloudBackup = true),
                    serverCount = 0,
                ),
                busy = false,
                onRecreate = {},
            )
        }
    }
}
