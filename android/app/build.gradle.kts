plugins {
    alias(libs.plugins.android.application)
    alias(libs.plugins.kotlin.compose)
    alias(libs.plugins.kotlin.serialization)
}

// The Relying Party ID must match the hostname the backend serves, and the
// backend must publish this app's signing certificate under that hostname.
val rpId: String = (project.findProperty("zerotap.rpId") as String?)?.trim().orEmpty().ifEmpty { "localhost" }

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
        // Restore Credentials is available from Android 9 through Google Play services.
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

    implementation(libs.androidx.credentials)
    implementation(libs.androidx.credentials.play.services.auth)

    implementation(libs.kotlinx.coroutines.android)
    implementation(libs.kotlinx.serialization.json)
    implementation(libs.okhttp)
}
