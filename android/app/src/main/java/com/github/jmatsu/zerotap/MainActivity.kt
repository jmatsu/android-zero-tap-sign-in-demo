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

        // The backup agent already tries this the moment the restore lands, but
        // that callback is best-effort, so the activity retries on first launch.
        viewModel.onStart(this)
    }
}
