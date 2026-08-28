package com.github.jmatsu.zerotap

import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.activity.viewModels
import com.github.jmatsu.zerotap.ui.AuthViewModel
import com.github.jmatsu.zerotap.ui.ZeroTapScreen
import com.github.jmatsu.zerotap.ui.ZeroTapTheme

class MainActivity : ComponentActivity() {

    private val graph by lazy { (application as ZeroTapApp).graph }

    private val viewModel: AuthViewModel by viewModels { AuthViewModel.factory(graph) }

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)

        setContent {
            ZeroTapTheme {
                ZeroTapScreen(viewModel = viewModel, settings = graph.settings, activity = this)
            }
        }

        // The second attempt at the zero-tap sign-in. ZeroTapBackupAgent already
        // tried the moment the restore landed, but that callback is best-effort
        // and some restore routes skip it entirely. Repeating it here is cheap,
        // idempotent, and what makes the path reliable rather than lucky.
        viewModel.onStart(this)
    }
}
