# Testing a real device migration

*English | [日本語](device-migration-test.ja.md)*

Run the easier [device-to-device-backup-test.md](device-to-device-backup-test.md) first to verify the flow.

## What an emulator test cannot confirm

- **The real encrypted-backup configuration**
  - It depends on the device's actual Google account, backup setting and screen lock
- **`BackupAgent.onRestoreFinished()` fires at its real timing**
  — it is called during setup, so data is restored and the app is already signed in the first time it opens
  - A real flow has restore lag

## Prerequisites

- Devices
  - Two real Android 9+ devices with Google Play services 24220000+ (emulators will not do)
  - Device A
    - Google account, **Settings → Google → Backup → on**, and a screen lock
    - The app installed
    - Connected to Wi-Fi
  - Device B
    - Brand new or just factory reset
- Service
  - Host the backend over HTTPS
  - Build the app against that host (e.g. `-Pzerotap.rpId`)
- A cable, for the cabled transfer

## Steps

**Device A**

1. Sign in with the app.
2. Confirm:
  - The **Restore Key** card says `Registered`
  - The Backup line says `Eligible for encrypted cloud backup`, not `This device only`
    - Still `This device only` → check the settings and tap **Re-create Restore Key**

   <img src="../assets/signed-in-screen.png" width="300" alt="Restore Key card reading Registered, eligible for encrypted cloud backup, server holds 1 Restore Key">

3. Tap **Settings → Google → Backup → Back up now**.

**Device B**

4. Power the device on and walk the setup wizard to the copy-apps-and-data step.
5. Copy from device A by **cable** or **cloud**.
  - Either carries the Restore Key
  - With no cloud backup (a local-only key), the cable is required
6. Finish setup and install the app.
  - Backup data can arrive before the app itself
7. Open the app.

## Success

- The app opens on the signed-in screen from the very first launch
- A **Signed in with zero taps** banner names the redeemed key, and the **Restore Key** card shows the
  replacement
- No biometric prompt and no credential picker (unlike a passkey)

The transfer itself: [Zero-Tap Sign-In on a device restored from a backup](../assets/zero-tap-sign-in-from-backup.webm) (webm).

## Failure

- Not an error screen, just the sign-in screen (in the demo app)
  - How Restore Key errors surface is up to the app

**Troubleshooting**

| Symptom | Likely cause |
| --- | --- |
| Device B asks for a sign-in, logs `No Restore Key on this device` | The backup ran before step 2 completed, or device A's key was local-only and you chose the cloud route (local-only keys only survive a cabled transfer). |
| The app is on device B with no data at all | The restore has not finished. App data can arrive minutes after the app installs. |
| Backend: `unknown Restore Key` | Already redeemed. It is single use, so a second device restored from the same image cannot reuse it. Or the backend is pointed at a different `DB_PATH`. |
| Backend: `Restore Key verification failed` | Origin mismatch. Compare the backend's startup `origins=` line with the `apk-key-hash` in the failure. Device B running an APK signed with a different key does this. |
| Nothing reaches the backend | Check `RP_ID` and `zerotap.rpId`. |

## Migrating onward

- Device B holds a new Restore Key as soon as it signs in, so it can migrate on to device A (after a
  factory reset) or device C as is
- The key on device A is revoked
  - Up to the backend implementation. Revoking looks right from a security standpoint
  - Migrating from one device to several involves other apps too, not just yours, so it needs no special
    handling

### About logs

- The backup agent runs in its own process, and its log lives in that process's memory
- Launching the app does not show it in that process's log

**On success**

Use the backup agent's tag:

```console
$ adb logcat -d -s ZeroTapBackupAgent
I ZeroTapBackupAgent: Restore finished; signed in as jmatsu
```

The backend log shows the matching pair:

```
level=INFO msg="zero-tap sign-in" user=jmatsu revokedCredential=AbCdEf123456...
level=INFO msg="Restore Key registered" user=jmatsu credential=NewKey7890...
```
