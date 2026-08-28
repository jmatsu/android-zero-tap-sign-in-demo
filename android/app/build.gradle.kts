plugins {
    alias(libs.plugins.android.application)
    alias(libs.plugins.kotlin.compose)
    alias(libs.plugins.kotlin.serialization)
}

// The Relying Party ID. Three things have to agree or Credential Manager
// refuses to create anything: this value, the backend's RP_ID, and the hostname
// serving /.well-known/assetlinks.json over HTTPS with this app's package name
// and signing certificate listed in it.
//
// It is a Gradle property only so the demo can follow a throwaway tunnel
// hostname. In a real app it is a constant per build type, and the certificate
// published under it is your release certificate — from Play App Signing if you
// use it, not your upload key.
val rpId: String = (project.findProperty("zerotap.rpId") as String?)?.trim().orEmpty().ifEmpty { "localhost" }

// Demo scaffolding: prefills the sign-in form so the flow can be exercised
// without typing. Nothing below the UI reads these.
val demoUsername: String = (project.findProperty("zerotap.demoUsername") as String?)?.trim() ?: "demo"
val demoPassword: String = (project.findProperty("zerotap.demoPassword") as String?)?.trim() ?: "demo-password"

android {
    namespace = "com.github.jmatsu.zerotap"
    compileSdk = 37

    val debugKeystore = rootProject.file("debug.keystore")
    val debugKeystorePassword = "android"
    val debugKeyAlias = "androiddebugkey"

    signingConfigs {
        getByName("debug") {
            storeFile = debugKeystore
            storePassword = debugKeystorePassword
            keyAlias = debugKeyAlias
            keyPassword = debugKeystorePassword
        }
    }

    defaultConfig {
        applicationId = "com.github.jmatsu.zerotap"
        // Restore Credentials ships in Google Play services rather than the
        // platform, so it reaches back to Android 9 — but it needs a recent
        // Play services (24220000+), and on an emulator a Google Play system
        // image. Devices without Play services have no Restore Keys at all,
        // which is why CredentialClient treats their absence as ordinary rather
        // than as a fault.
        minSdk = 28
        targetSdk = 36
        versionCode = 1
        versionName = "1.0"

        buildConfigField("String", "RP_ID", "\"$rpId\"")
        buildConfigField("String", "BACKEND_BASE_URL", "\"https://$rpId\"")
        buildConfigField("String", "DEMO_USERNAME", "\"$demoUsername\"")
        buildConfigField("String", "DEMO_PASSWORD", "\"$demoPassword\"")
    }

    buildFeatures {
        compose = true
        buildConfig = true
    }

    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }
}

kotlin {
    compilerOptions {
        jvmTarget.set(org.jetbrains.kotlin.gradle.dsl.JvmTarget.JVM_17)
    }
}

dependencies {
    implementation(libs.androidx.core.ktx)
    implementation(libs.androidx.activity.compose)
    implementation(libs.androidx.lifecycle.runtime.compose)
    implementation(libs.androidx.lifecycle.viewmodel.compose)

    implementation(platform(libs.androidx.compose.bom))
    implementation(libs.androidx.compose.ui)
    implementation(libs.androidx.compose.material3)
    implementation(libs.androidx.compose.ui.tooling.preview)
    debugImplementation(libs.androidx.compose.ui.tooling)

    // The entire client-side dependency for this feature.
    // credentials-play-services-auth is the provider that actually implements
    // Restore Keys; the API artifact alone compiles happily and then fails at
    // runtime with no provider to serve the request.
    implementation(libs.androidx.credentials)
    implementation(libs.androidx.credentials.play.services.auth)

    implementation(libs.kotlinx.coroutines.android)
    implementation(libs.kotlinx.serialization.json)
    implementation(libs.okhttp)
}
