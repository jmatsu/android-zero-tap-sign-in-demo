# What to do when supporting Zero-Tap Sign-In

*English | [日本語](what-to-do.ja.md)*

- A Restore Key is a WebAuthn credential the system creates and replays with no UI. There is no identity
  verification either (== weak)
   - Android
      - Because no UI is involved, the data flow differs from a passkey
      - On the device, never treat a passkey and a Restore Key as the same thing (their security levels
      differ, so unifying them is all risk and no gain)
   - Backend
      - Most of the work, and the knowledge it needs, is the same as supporting passkeys
- If you have not supported passkeys yet
   - Backend
      - Start the work with passkey support in mind (there is no real upside to starting with Zero-Tap
      Sign-In alone and no consideration for passkeys)

## Android App

### Checklist

- [ ] Move `androidx.credentials` to 1.5.0 or later
- [ ] Create and register a local Restore Key
   - [ ] On sign-in
   - [ ] When issuing a session that spends a Restore Key
   - [ ] After confirming the session is valid (no need to create a new one; a registration request is enough)
- [ ] Discard the local Restore Key
   - [ ] On sign-out
   - [ ] When a Restore Key is spent
- [ ] Attempt a sign-in with the Restore Key
   - [ ] Before showing the sign-in screen
   - [ ] From `BackupAgent#onRestoreFinished`
      - [ ] Set `android:fullBackupOnly="true"`
- [ ] Make the app data eligible for cloud backup
   - [ ] `android:allowBackup="true"`
   - API 31+
      - [ ] Set `android:dataExtractionRules`
   - Below API 31
      - [ ] Set `android:fullBackupContent`
- [ ] Digital Asset Links
   - [ ] Share the package name and the signing certificates
   - [ ] Get a verification environment reachable over HTTPS
- [ ] Security items
   - [ ] Keep other secrets out of the backup
      - Backed-up tokens can achieve something similar, but the Restore Key should be the only thing that
      restores authority
   - [ ] Assume the key may be redeemed by someone other than the user
      - A session created by Zero-Tap Sign-In should be treated as not having completed strict
      identity verification
      - For sensitive operations (payments, password change, account deletion), require a passkey or password

### Notes and caveats

1. It does not work on older Play services versions
2. Register a Restore Key right after confirming the session is valid
   - See `AuthRepository.ensureRestoreKey()`
   - Pass the options the server returned to
     `CreateRestoreCredentialRequest(requestJson, isCloudBackupEnabled = true)`
   - On `E2eeUnavailableException`, retry with `isCloudBackupEnabled = false`
      - That device cannot do E2E encrypted cloud backup, but a local-only key still covers a cabled
      transfer (D2D), so account for it
3. Always delete the local Restore Key when it is spent and on sign-out
   - Call `clearCredentialState(TYPE_CLEAR_RESTORE_CREDENTIAL)` to discard it
      - If the local key the app holds is not discarded, it drifts from the cloud backup and Zero-Tap
      Sign-In can fire at an unexpected moment
4. On every launch, try Zero-Tap Sign-In before showing the sign-in UI
   - See `AuthRepository.signInWithRestoreKey()`
   - Use `GetRestoreCredentialOption`
      - `null` or `NoCredentialException` just means a fresh install
   - Track the state (e.g. in SharedPreferences) and throttle it
      - Restoring the Restore Key (the backup data) can land later than the first launch, so doing it only
      on first launch is not enough
      - Once the user has signed in by any means, there should be no need to try again
5. For `BackupAgent.onRestoreFinished()`, see `ZeroTapBackupAgent`
   - It can activate the account in the background (in a separate process) once the backup data is
   restored, before the user opens the app
   - The runtime limits are tight, so guard it with a timeout and design it to fall back to the app itself
   - ContentProviders and a custom Application class are not available
   - If you touch files, account for multi-process access
6. Make the app data eligible for cloud backup
   - `android:allowBackup="true"` is mandatory
   - Set `android:fullBackupOnly="true"` as well, even if you do not implement a BackupAgent
7. Publish Digital Asset Links
   - The Android developer takes the lead in handing over the package name and every signing certificate
   you support (debug, release, Play App Signing)
      - Certificates that never reach that server environment do not need to be listed
   - HTTPS is required, so watch out on a local server
8. A local-only Restore Key (no E2EE) is weak
   - It is used only for D2D (a cabled device-to-device transfer)
      - It works even with no screen lock, but going by Google's announcements, is supporting it
      mandatory?
   - Whether you tell the user it is not covered by backup is the development team's call
9. Whether you add a confirmation step that assumes someone other than the user may redeem the key is the
development team's call
   - A session created by Zero-Tap Sign-In has not completed strict identity verification
   - Arguably, sensitive operations (payments, password change, account deletion) should require a passkey
   or password
10. `adb backup` requires a debuggable build once the target SDK is 31 or higher
   - With Play App Signing you cannot use `adb backup` against a fully production build, which makes
   verification somewhat awkward

## Backend

### Checklist

- [ ] Pick an existing FIDO-compliant library instead of building verification yourself
- [ ] Prepare a table (or equivalent) to manage Restore Key records
- [ ] Add the Restore Key endpoints
   - [ ] Restore Key registration (distributed transaction)
   - [ ] Restore Key deregistration
   - [ ] Sign-in with a Restore Key (distributed transaction)
- [ ] Revoke the Restore Key tied to the session on sign-out
- [ ] Serve `/.well-known/assetlinks.json` over HTTPS
   - [ ] List the app's package name and certificate fingerprints (received from the Android developer)
- [ ] Security items
   - [ ] Design around the fact that a Restore Key is single use
      - A backup image can be restored any number of times, so breaking the single-use rule turns it into
      an effectively permanent session
   - [ ] Verify the origin, to prevent a third-party app from establishing a session
   - [ ] Treat a `restore` session as lower assurance than a passkey one
      - A session created by Zero-Tap Sign-In has not completed strict identity verification
         - For sensitive operations (payments, password change, account deletion), require a passkey or
         password
   - [ ] A sign-in with a Restore Key is not a re-binding of the session the key was tied to
      - The migrated device should be issued a different session from the original
         - There is no identity verification, so it is not at the same level as a passkey session
   - [ ] Rate-limit and log restore sign-ins
      - They get called on every device transfer, but they should never spike
   - [ ] Authentication and verification on the endpoints
      - Gate sign-in with a Restore Key on signature verification
      - Gate the other endpoints on a valid session
   - [ ] Give the Restore Key an expiry
      - Revoking the Restore Key when the session it was tied to has been inactive for 90 days is safer
      - Extending the Restore Key's expiration when a valid session hits certain endpoints works well
   - [ ] Record how the session was established
      - Being able to tell what a session is based on (password / passkey / restore key) is valuable

### Notes and caveats

1. Store Restore Keys as their own credential kind
   - WebAuthn verification is the same as a passkey, but retention period and count are expected to have
   different constraints
2. Do not require the User Present / User Verified flags on restore assertions
   - A Zero-Tap Sign-In assertion carries neither
   - Everything else still has to be verified. See `backend/webauthn.go`
3. Delete the Restore Key on successful redemption
   - In the same transaction that issues the session, or right after
   - Do not forget to return a new key to the client at that point
4. At most one Restore Key per account and device (app) pair
   - You can assume two Restore Keys never coexist on one device
   - An account will have several Restore Keys (roughly, several devices) tied to it, and counting revoked
   or leaked sessions, several sessions can reference one Restore Key
5. Revoke on sign-out
   - See `POST /api/restore/revoke`

## Disclaimer

The information in this document is provided for reference only, with no guarantee of its accuracy or
completeness. The author and contributors accept no responsibility whatsoever for any damage or trouble
arising from the use of this document or from any action taken based on its contents. Use it at your own
risk.