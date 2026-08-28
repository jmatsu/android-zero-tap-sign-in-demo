# 端末間 (Device to Device) バックアップでテストする

*[English](device-to-device-backup-test.md) | 日本語*

| | 端末間 | クラウド |
| --- | --- | --- |
| 両方が同じ Google アカウント | 不要 | **必須** |
| `android:allowBackup="true"` | 不要 | 必須 (このアプリは設定済み) |
| ローカル限定のRestore Keyでも動くか | ✅ | ❌ |

エミュレーターを使う場合は [emulator-setup.ja.md](./emulator-setup.ja.md) を参照してください。

## 手順

**`zerotap-a` で**

1. アプリをインストールして起動する (`./gradlew installDebug -Pzerotap.rpId=…`)。
2. サインインする
3. **ログまたは画面にRestore Keyを作成した旨が出るまで待つ**

   <img src="../assets/signed-in-screen.png" width="300" alt="Restore Key カードが Registered、暗号化クラウドバックアップ対象、サーバーも 1 件保持と表示されている">

4. Android Studio > Running Devices > Toolbar の中の **Backup App Data**
5. Back-up Type は **Device to Device**

**`zerotap-b` で**

6. 同じ `-Pzerotap.rpId` でアプリをインストール
7. 一度開いて、サインインを求められることを確認

   <img src="../assets/sign-in-screen.png" width="300" alt="サインイン画面。フローログに Restore Key なしと表示されている">

8. 4. Android Studio > Running Devices > Toolbar の中の **Restore App Data** で先ほどのバックアップファイルを選択
9. アプリを開くとサインイン済みになっている

<img src="assets/zero-tap-sign-in-from-backup.gif" width="320">

### エミュレーター 1 台でも簡易検証は可

- 手順 6 をアンインストール + 再インストールに置き換えればよい

```console
$ adb uninstall com.github.jmatsu.zerotap
$ adb install android/apps/build/outputs/apk/app-debug.apk
```

## クラウドバックアップの場合

- 両方のエミュレーターで **同じ** Google アカウントでサインインする
    - バックアップをするため、設定 > Google > Back Up > すぐバックアップをする
- `android:allowBackup="true"` が必要
- 手順 3 でローカル限定のキーとでた場合、このフローは使えない

## うまくいかないとき

- 検証アプリでは失敗をエラーとせず、ただのサインイン画面を出すだけにしている
    - Restore Key が失効しているのかを判断するにはバックエンドの対応が必要

| 症状 | 考えられる原因 |
| --- | --- |
| `zerotap-a` で `Could not create a Restore Key` | asset links。`RP_ID` と `zerotap.rpId` が食い違っている、トンネルが落ちている、またはエミュレーターの GMS が 24220000 より古い。 |
| `Restore Key created (local only: …)` | Google アカウントがない、バックアップが無効、または画面ロックがない。端末間なら問題なし、クラウドバックアップは動作しない |
| `zerotap-b` がサインインを求め、`No Restore Key on this device` とログに出る | バックアップにキーが含まれていない。手順 3 がバックアップ前に完了していたか確認する。 |
| バックエンド: `Restore Key verification failed` | オリジンの不一致。バックエンド起動時の `origins=` 行と、失敗時の `apk-key-hash` を比べる。DeployGate や Firebase App Distribution を使っている場合は署名情報の確認を |
| バックエンド: `unknown Restore Key` | すでに引き換え済み (使い捨て)、またはバックエンドが別の `DB_PATH` を見ている |

## さらに次へ移行するケース

- 端末 B はサインインが完了した時点で新しい Restore Key になっているので、そのまま端末 A(初期化後) or Cに移行が可能
- 端末 A にあるキーは失効している
  - バックエンドの実装次第。セキュリティ的には失効が妥当に思われる
  - 同じ端末からの複数台移行については自アプリだけではなく他アプリも関連するため、特段配慮する必要はなし
