plugins {
    alias(libs.plugins.android.application) apply false
    alias(libs.plugins.kotlin.compose) apply false
    alias(libs.plugins.kotlin.serialization) apply false
}

// Creating the emulators is not something Gradle can do properly on its own:
// Gradle Managed Devices (android.testOptions.managedDevices) only exist for
// the duration of an instrumented test run, are wiped between runs, and cannot
// use a Google Play system image — which is exactly what Restore Credentials
// needs. So this task just forwards to the SDK tools, which can.
tasks.register<Exec>("createDemoAvds") {
    group = "zero-tap"
    description = "Creates the zerotap-a and zerotap-b emulators used to test a device transfer."

    // Override with -Pzerotap.avdApi=36
    val apiLevel = (project.findProperty("zerotap.avdApi") as String?)?.trim().orEmpty()

    workingDir = rootDir.parentFile
    commandLine(listOfNotNull("./scripts/create-demo-avds.sh", apiLevel.ifEmpty { null }))
}
