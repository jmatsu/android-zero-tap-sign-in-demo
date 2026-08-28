# What to do when supporting Zero-Tap Sign-In

*English | [日本語](what-to-do.ja.md)*

- A Restore Key is a WebAuthn credential the system creates and replays with no UI, so most of the work is
  the same as supporting passkeys
- If you have not supported passkeys yet, do that first
   - Starting with Zero-Tap Sign-In alone tends to leave gaps on the backend that become debt

## Android App

1. **`androidx.credentials` 1.5.0+**
   - Restore Credentials ships through Google Play services
2. **Register a Restore Key right after every session check**
   - See `AuthRepository.ensureRestoreKey()`
   - Pass the options the server returned to
     `CreateRestoreCredentialRequest(requestJson, isCloudBackupEnabled = true)`
   - On `E2eeUnavailableException`, retry with `isCloudBackupEnabled = false`
      - That device cannot do E2E encrypted cloud backup, but a local-only key still survives a cabled
      transfer
3. **Try a restore sign-in at startup, before showing any sign-in UI**
   - See `AuthRepository.signInWithRestoreKey()`
   - Use `GetRestoreCredentialOption`
      - `null` or `NoCredentialException` just means a fresh install
4. **Do the same from `BackupAgent.onRestoreFinished()`**
   - See `ZeroTapBackupAgent`
   - It can activate the account before the user opens the app
   - The time budget is tight, so guard it with a timeout and design it to fall back to the app itself
5. **Let the app be backed up**
   - Set `android:allowBackup="true"` and `android:fullBackupOnly="true"`
6. **Clear the local key when it is spent and on sign-out**
   - Call `clearCredentialState(TYPE_CLEAR_RESTORE_CREDENTIAL)`
   - Drop your own record of the key too, or it drifts from the cloud backup
7. **Publish Digital Asset Links**
   - List the package name and every signing certificate you support (debug, release, Play App Signing)
      - Certificates that never reach that server environment do not need to be listed
   - HTTPS is required, so watch out on a local server

### Security notes

- **Never put secrets in the backup yourself**
   - Backed-up tokens can achieve something similar, but the Restore Key should be the only thing that
   restores authority
   - In general, the migrated device should be issued a different session from the original
- **A local-only key (no E2EE) is weak**
   - It only works for a cabled device-to-device transfer; whether you support it is your call
      - It works even with no screen lock, so restricting it is usually the safer choice
   - Whether you tell the user it is not covered by backup is also your call
- **Assume the key may be redeemed by someone other than the user**
   - A session created by Zero-Tap Sign-In should be treated as not having completed strict
   identity verification
   - For sensitive operations (payments, password change, account deletion), require a passkey or password

## Backend

1. **Store Restore Keys as their own credential kind**
   - WebAuthn verification is the same as a passkey, but retention period and count will have
   different constraints
2. **Do not require the User Present / User Verified flags on restore assertions**
   - A Zero-Tap Sign-In assertion carries neither
   - Everything else still has to be verified. See `backend/webauthn.go`
3. **Delete the credential on successful redemption**
   - In the same transaction that issues the session, or right after
   - Return a new key at that point
4. **At most one Restore Key per account and device (app) pair**
   - You can assume two Restore Keys never coexist on one device
5. **Record how the session was established**
   - password, passkey, restore key, etc.
   - Both the app and your own authorization checks benefit from telling them apart
6. **Revoke on sign-out**
   - See `POST /api/restore/revoke`
7. **Serve `/.well-known/assetlinks.json`** over HTTPS
   - With the app's package name and certificate fingerprints

### Security notes

- **Do not hand-roll verification**
   - Do not write branches like "skip user verification because it is a Restore Key"
   - Achieve it by passing the right parameters to the FIDO library
- **Single use**
   - A backup image can be restored any number of times, so skipping the delete-on-use turns it into a
   permanent session
- **Verify the origin**
   - Prevents a third-party app from establishing a session
- **Treat a `restore` session as lower assurance than a passkey one**
   - A session created by Zero-Tap Sign-In should be treated as not having completed strict
   identity verification
      - For sensitive operations (payments, password change, account deletion), require a passkey or password
- **Rate-limit and log restore sign-ins**
   - They happen once per device transfer, so they should never spike
- **Authentication on the endpoints is mandatory**
  - Restore Key registration requires authentication (a valid session)
  - The post-migration Restore Key replacement endpoint is gated by the signature instead
- **Give the key an expiry**
   - Revoking the Restore Key after, say, 90 days of an inactive session is safer
   - Extending the expiration when a valid session hits certain endpoints works well
