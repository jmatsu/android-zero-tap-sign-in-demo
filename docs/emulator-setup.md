*English | [日本語](emulator-setup.ja.md)*

Requires **Android Studio Otter (2025.2.1)** or newer for its Backup tools.

## Emulator setup

```console
$ cd android && ./gradlew createDemoAvds     # creates zerotap-a and zerotap-b
$ $ANDROID_HOME/emulator/emulator -avd zerotap-a
$ $ANDROID_HOME/emulator/emulator -avd zerotap-b
```

## Change settings

Set these on both emulators before starting:

- Settings → Security → Screen lock → **PIN**
- Settings → Google → Backup → **on**

- Without a Google account, backup, or a screen lock, Credential Manager throws
`E2eeUnavailableException` and the app falls back to a local-only key
    - That key can still be transferred by a Device to Device backup
- The same applies to passkeys, where these are mandatory
