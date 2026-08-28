# Testing with a Device to Device backup

*English | [日本語](device-to-device-backup-test.ja.md)*

| | Device to Device | Cloud |
| --- | --- | --- |
| Same Google account on both | not needed | **required** |
| `android:allowBackup="true"` | not needed | required (this app sets it) |
| Works with a local-only Restore Key | ✅ | ❌ |

If you would like to use emulators, see [emulator-setup.md](./emulator-setup.md).

## Steps

**On `zerotap-a`**

1. Install and run the app (`./gradlew installDebug -Pzerotap.rpId=…`).
2. Sign in.
3. **Wait until the log or the screen says the Restore Key was created.**

   <img src="../assets/signed-in-screen.png" width="300" alt="Restore Key card reading Registered, eligible for encrypted cloud backup, server holds 1 Restore Key">

4. Android Studio > Running Devices > toolbar > **Backup App Data**
5. Backup type → **Device to Device**

**On `zerotap-b`**

6. Install the app with the same `-Pzerotap.rpId`.
7. Open it once and confirm it asks you to sign in.

   <img src="../assets/sign-in-screen.png" width="300" alt="Sign-in screen, flow log reading: no Restore Key on this device">

8. Android Studio > Running Devices > toolbar > **Restore App Data**, and pick the backup you just made.
9. Open the app. It is already signed in.

The transfer itself: [Zero-Tap Sign-In on a device restored from a backup](../assets/zero-tap-sign-in-from-backup.webm) (webm).

### A single emulator is enough for a quick check

- Replace step 6 with uninstall + reinstall

```console
$ adb uninstall com.github.jmatsu.zerotap
$ adb install android/apps/build/outputs/apk/app-debug.apk
```

## Cloud backup

- Sign both emulators in to the **same** Google account
    - To take a backup: Settings > Google > Back up > Back up now
- `android:allowBackup="true"` is required
- If step 3 reported a local-only key, this route will not work

## When it does not work

- The demo app does not treat a failure as an error; it just shows the sign-in screen
    - Telling whether the Restore Key was revoked needs backend support

| Symptom | Likely cause |
| --- | --- |
| `Could not create a Restore Key` on `zerotap-a` | Asset links: `RP_ID` and `zerotap.rpId` disagree, the tunnel is down, or the emulator's GMS is older than 24220000. |
| `Restore Key created (local only: …)` | No Google account, backup disabled, or no screen lock. Fine for Device to Device, does not work for cloud backup. |
| `zerotap-b` asks for a sign-in, logs `No Restore Key on this device` | The backup did not include the key. Confirm step 3 completed before the backup. |
| Backend: `Restore Key verification failed` | Origin mismatch. Compare the backend's startup `origins=` line with the `apk-key-hash` in the failure. If you use DeployGate or Firebase App Distribution, check the signing info. |
| Backend: `unknown Restore Key` | Already redeemed (single use), or the backend is pointed at a different `DB_PATH`. |

## Migrating onward

- Device B holds a new Restore Key as soon as it signs in, so it can migrate on to device A (after a
  factory reset) or device C as is
- The key on device A is revoked
  - Up to the backend implementation. Revoking looks right from a security standpoint
  - Migrating from one device to several involves other apps too, not just yours, so it needs no special
    handling
