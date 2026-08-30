# Zero-Tap サインインに対応するときにやること

*[English](what-to-do.md) | 日本語*

- Restore Key は、システムが UI なしで作成・再生する WebAuthn クレデンシャル。本人確認もない (== weak)
   - Android
      - UI を挟まないため、Passkey とデータフローが異なる
      - 端末上で、決して Passkey と Restore Key を同一視しないこと (セキュリティレベルが違うので、統一にはリスクしかない)
   - バックエンド
      - 処理・必要な知識の殆どは Passkey 対応と同じ
- Passkey対応をしていない場合
   - バックエンド
      - Passkey 対応を念頭において作業を開始するとよい (考慮なしに Zero-Tap サインインの対応のみから始める利点は特に見当たらない)

## Android アプリ

### チェックリスト

- [ ] `androidx.credentials` を 1.5.0 以降にする
- [ ] ローカルの Restore Key の新規作成・登録
   - [ ] サインイン時
   - [ ] Restore Key を消費するセッション発行時
   - [ ] セッションが有効であることを確認した時 (新規作成はせず、登録リクエストのみで可)
- [ ] ローカルの Restore Key の破棄
   - [ ] サインアウト時
   - [ ] Restore Key を消費したとき
- [ ] Restore Key によるサインインの試行
   - [ ] サインイン画面を出す前
   - [ ] `BackupAgent#onRestoreFinished` での実装
      - [ ] `android:fullBackupOnly="true"` の設定
- [ ] アプリデータをクラウドバックアップ対象にする
   - [ ] `android:allowBackup="true"`
   - API 31 以上
      - [ ] `android:dataExtractionRules` の設定
   - API 31 未満
      - [ ] `android:fullBackupContent` の設定
- [ ] Digital Asset Links 対応
   - [ ] パッケージ名と署名を共有する
   - [ ] HTTPS で到達する検証環境を手にいれる
- [ ] セキュリティ項目
   - [ ] その他の秘密情報をバックアップ対象にしない
      - バックアップ対象のトークン系を入れても似たようなことはできるが、Restore Key のみで復元可能な状態とすること
   - [ ] ユーザー本人以外が引き換える可能性を前提に置く
      - Zero-Tap Sign-In で作ったセッションは厳格な本人確認は終わっていないとみなすべきか
      - 重要な操作 (決済、パスワード変更、アカウント削除) ではPasskeyやパスワードを要求することが望ましい

### 補足・注意事項

1. 古い Play Services バージョンでは動作しない
2. セッションが有効であることを確認した直後にRestore Keyを登録する
   - `AuthRepository.ensureRestoreKey()` を参照
   - サーバーが返したオプションを `CreateRestoreCredentialRequest(requestJson, isCloudBackupEnabled = true)` へ
   - `E2eeUnavailableException` を捕捉したら `isCloudBackupEnabled = false` で再試行
      - その端末は E2E 暗号化クラウドバックアップができないが、ローカル限定のキーでもケーブル移行(D2D)があるため考慮にいれる
3. Restore Key を消費したとき、およびサインアウト時にローカルの Restore Key を必ず削除する
   - `clearCredentialState(TYPE_CLEAR_RESTORE_CREDENTIAL)` を呼んで破棄する
      - アプリ側で持っているローカルキーが破棄されないと、クラウドバックアップとデータズレが起き、予期しないタイミングで Zero-Tap Sign-In が動作しかねない
4. 毎起動時、サインイン UI を出す前に Zero-Tap Sign-In を試す
   - `AuthRepository.signInWithRestoreKey()` を参照
   - `GetRestoreCredentialOption` を利用
      - `null` や `NoCredentialException` は新規インストールと同義
   - SharedPreferences などで状態管理し、throttle すること
      - Restore Key (バックアップデータ) の復元が初回起動よりも遅くなることがあるため、初回起動時のみの処理では不足する
      - 何らかの方法で一度でもログインをしたなら、それ以上試さなくてもよいはず
5. `BackupAgent.onRestoreFinished()` は ZeroTapBackupAgent を参照
   - ユーザーがアプリを開くより前に、バックアップデータが復元されたあとバックグラウンド(別プロセス)でアカウントを有効にできる
   - 動作制限が厳しいので、タイムアウトで保護しつつ、アプリ本体の処理に流せるように設計すること
   - ContentProvider、カスタム Application クラスは触れない
   - ファイル操作をする場合はマルチプロセスを考慮すること
6. アプリデータをクラウドバックアップ対象に
   - `android:allowBackup="true"` は必須
   - BackupAgent を実装しなくても、`android:fullBackupOnly="true"` の設定もしておくべき
7. Digital Asset Links を公開
   - パッケージ名と、対応するすべての署名証明書 (debug、release、Play アプリ署名) を
   Android開発者が主導的に引き渡す
      - そのサーバー環境にアクセスしない署名は載せなくてよい
   - HTTPS必須なのでローカルサーバーでは注意
8. ローカル限定の Restore Key (E2EE なし) は weak
   - D2D(ケーブルを使った端末間データ移行)でのみ使う
      - ロックなしでも動作してしまうが、Google の告知を見るに対応必須?
   - バックアップで動いていないことをユーザーに提示するかどうかは開発チーム判断
9. ユーザー本人以外が引き換える可能性を前提に置いた確認機構を設けるかどうかは開発チーム判断
   - Zero-Tap Sign-In で作ったセッションの場合、厳格な本人確認は終わっていない
   - 重要な操作 (決済、パスワード変更、アカウント削除) ではPasskeyやパスワードを要求することが望ましいといえるか
10. `adb backup` コマンドはターゲットSDK 31 以上だと debuggable を要求する
   - App Signing を使用している場合、完全な本番環境で `adb backup` を使えないため、検証がやや面倒

## バックエンド

### チェックリスト

- [ ] 自前で検証機構を作らず、既存のFIDO準拠ライブラリを選定する
- [ ] Restore Key 情報を管理できるテーブル等を用意
- [ ] Restore Key に関するエンドポイントを作成する
   - [ ] Restore Key の登録 (分散transaction)
   - [ ] Restore Key の登録解除
   - [ ] Restore Key を利用したサインイン (分散transaction)
- [ ] サインアウトでセッションに紐づく Restore Key を失効する
- [ ] `/.well-known/assetlinks.json` を HTTPS で配信
   - [ ] アプリのパッケージ名と証明書の fingerprint を載せる(Android開発者から受け取る)
- [ ] セキュリティ項目
   - [ ] Restore Key は使い捨てであることを念頭に置いた設計にする
      - バックアップイメージは何度でも復元できるので、使い捨て原則を守らないと実質的な永続セッションになる
   - [ ] 第三者アプリからのセッション確立を防ぐため、オリジンを検証する
   - [ ] `restore` のセッションはPasskeyのセッションより保証度が低いものとして扱うべき
      - Zero-Tap Sign-In で作ったセッションは厳格な本人確認は終わっていない
         - 重要な操作 (決済、パスワード変更、アカウント削除) ではPasskeyやパスワードを要求することが望ましい
   - [ ] Restore Key によるサインインは、それに紐づいていたセッションの紐付けし直しではない
      - 移行先の端末のセッションは移行前と異なるものが発行されるべき
         - 本人確認がないので、Passkey によるセッションとはレベルが異なる
   - [ ] 復元サインインにレート制限とログを入れる
      - 端末移行のたびに呼ばれるようになるが、Spike するものではない
   - [ ] エンドポイントの認証・検証
      - Restore Key を利用したサインインは署名を利用した検証でブロックする
      - その他のエンドポイントは有効なセッションで管理
   - [ ] Restore Key に賞味期限をつける
      - 90日間 inactive なセッション(に紐づいていたRestore Key)であれば、Restore Key を失効させるといった機構があるとより安全
      - 有効なセッションで特定のエンドポイントを叩いたときに、Restore Key の expiration を延ばすなどするとよい
   - [ ] セッションがどう確立されたかを記録
      - password/passkey/restore key などセッション確立の根拠になるものを判断できるとよい

### 補足・注意事項

1. Restore Keyは独立したクレデンシャル種別として保存
   - WebAuthn の検証はPasskeyと同じだが、保持する期間や個数に異なる制約がかかることが想定される
2. 復元アサーションに User Present / User Verified フラグを要求しない
   - Zero-Tap Sign-In の assertion はどちらもなし
   - 他の要素は検証がいるので注意。`backend/webauthn.go` を参照
3. 引き換えに成功したら Restore Key を削除
   - セッション発行と同じ、あるいはそのあとのトランザクションで削除
   - そのとき、新しいキーをクライアントに返すことを忘れない
4. アカウントと端末(アプリ)の組ごとにRestore Keyは最大 1 つ
   - 2つの Restore Key が1端末上に存在することはないと考えてよい
   - アカウントには複数の Restore Key (≒ 端末)が紐づくであろうし、失効済み・漏れのセッションを含めると複数のセッションが1つのRestore Keyを参照することもある
5. サインアウト時に失効
   - `POST /api/restore/revoke` 参照

## 免責事項

本ドキュメントに掲載されている情報は参考情報であり、その正確性や完全性を保証するものではありません。本ドキュメントの利用、または記載内容に基づいて行われた一切の行為によって生じた損害やトラブルについて、作者およびコントリビューターは理由の如何を問わず一切の責任を負いかねます。ご利用は自己責任にてお願いいたします。