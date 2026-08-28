*[English](emulator-setup.md) | 日本語*

バックアップ ツールを使うため、**Android Studio Otter (2025.2.1)** 以降が必要

## エミュレーターのセットアップ

```console
$ cd android && ./gradlew createDemoAvds     # zerotap-a と zerotap-b を作成
$ $ANDROID_HOME/emulator/emulator -avd zerotap-a
$ $ANDROID_HOME/emulator/emulator -avd zerotap-b
```

## 設定の変更

起動前に、両方のエミュレーターで設定する

- 設定 → セキュリティ → 画面ロック → **PIN**
- 設定 → Google → バックアップ → **オン**

- Google アカウント、バックアップ、画面ロックのいずれかが欠けていると、Credential Manager は
`E2eeUnavailableException` を投げ、アプリはローカル限定のキーにフォールバックする
    - それでも端末間 (Device to Device) バックアップでは移行可能
- Passkey も同様で、これらは必須

