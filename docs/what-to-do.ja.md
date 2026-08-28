# Zero-Tap サインインに対応するときにやること

*[English](what-to-do.md) | 日本語*

- Restore Keyは、システムが UI なしで作成・再生する WebAuthn クレデンシャルなので、作業のほとんどはPasskey対応と同じ
- Passkey対応をしていない場合、Passkey対応からやるべき
   - Zero-Tap サインインの対応のみから始めると、特にバックエンドで Passkey 対応時の考慮漏れが負債になりかねない

## Android アプリ

1. **`androidx.credentials` 1.5.0 以降**
   - Restore Credentials は Google Play services 経由で提供
2. **セッションを確認するたびに、その直後でRestore Keyを登録**
   - `AuthRepository.ensureRestoreKey()` を参照
   - サーバーが返したオプションを `CreateRestoreCredentialRequest(requestJson, isCloudBackupEnabled = true)` へ
   - `E2eeUnavailableException` を捕捉したら `isCloudBackupEnabled = false` で再試行
      - その端末は E2E 暗号化クラウドバックアップができないが、ローカル限定のキーでもケーブル移行がある
3. **起動時、サインイン UI を出す前に復元によるサインインを試す**
   - `AuthRepository.signInWithRestoreKey()` を参照
   - `GetRestoreCredentialOption` を利用
      - `null` や `NoCredentialException` は新規インストールと同義
4. **同じことを `BackupAgent.onRestoreFinished()` からも行うべき**
   - `ZeroTapBackupAgent` を参照
   - ユーザーがアプリを開くより前にアカウントを有効にできる
   - 動作時間制限が厳しいので、タイムアウトで保護しつつ、アプリ本体の処理に流せるように設計すること
5. **アプリをバックアップ対象に**
   - `android:allowBackup="true"` と `android:fullBackupOnly="true"` を設定
6. **使い切ったとき、およびサインアウト時にローカルのキーを削除**
   - `clearCredentialState(TYPE_CLEAR_RESTORE_CREDENTIAL)` を呼ぶ
   - アプリ側で持っているローカルキーも破棄しないとクラウドバックアップとデータずれが起きる
7. **Digital Asset Links を公開**
   - パッケージ名と、対応するすべての署名証明書 (debug、release、Play アプリ署名) を
   載せる
      - そのサーバー環境にアクセスしない署名は載せなくてよい
   - HTTPS必須なのでローカルサーバーでは注意

### セキュリティ上の注意

- **秘密情報を自分でバックアップに入れない**
   - バックアップ対象のトークン系を入れても似たようなことはできるが、Restore Key のみで復元可能な状態とすること
   - 一般論として、移行先の端末のセッションは移行前と異なるものが発行されるべき
- **ローカル限定のキー (E2EE なし) は weak**
   - ケーブルを使った端末間データ移行でのみ使うが、サポート・許容するかはサービス次第
      - 画面ロックなしでも動作してしまうので、通常は制限した方がよいと思われる
   - バックアップで動いていないことをユーザーに提示するかもサービス次第
- **ユーザー本人以外が引き換える可能性を前提に置く**
   - Zero-Tap Sign-In で作ったセッションは厳格な本人確認は終わっていないとみなすべきか
   - 重要な操作 (決済、パスワード変更、アカウント削除) ではPasskeyやパスワードを要求することが望ましい

## バックエンド

1. **Restore Keyは独立したクレデンシャル種別として保存**
   - WebAuthn の検証はPasskeyと同じだが、保持する期間や個数に異なる制約がかかることが想定される
2. **復元アサーションに User Present / User Verified フラグを要求しない**
   - Zero-Tap Sign-In の assertion はどちらもなし
   - 他の要素は検証がいるので注意。`backend/webauthn.go` を参照
3. **引き換えに成功したらクレデンシャルを削除**
   - セッション発行と同じ、あるいはそのあとのトランザクションで削除
   - そのとき、新しいキーを返すこと
4. **アカウントと端末（アプリ)の組ごとにRestore Keyは最大 1 つ**
   - 2つの Restore Key が1端末上に存在することはないと考えてよい
5. **セッションがどう確立されたかを記録**
   - password, passkey, restore key etc.
   - アプリ側でも、サーバー側の認可判定でも、セッションの元を区別できる方が良い
6. **サインアウト時に失効**
   - `POST /api/restore/revoke` 参照
7. **`/.well-known/assetlinks.json` を HTTPS で配信**
   - アプリのパッケージ名と証明書の fingerprint を載せる

### セキュリティ上の注意

- **自前検証をしない**
   - Restore Key だからユーザー検証を要求しない、といった自前分岐コードは書くべきでない
   - FIDOライブラリに適切なパラメータを渡すことで実現するべき
- **使い捨て**
   - バックアップイメージは何度でも復元できるので、使ったら破棄を守らないと実質的な永続セッションになる
- **オリジンの検証**
   - 第三者アプリからのセッション確立を防ぐ
- **`restore` のセッションはPasskeyのセッションより保証度が低いものとして扱う**
   - Zero-Tap Sign-In で作ったセッションは厳格な本人確認は終わっていないとみなすべきか
      - 重要な操作 (決済、パスワード変更、アカウント削除) ではPasskeyやパスワードを要求することが望ましい
- **復元サインインにレート制限とログを入れる**
   - 端末移行のたびに呼ばれるようになるが、Spike するものではない
- **エンドポイントの認証は必須**
  - Restore Keyの登録は認証(有効なセッション)必須
  - 移行後の、Restore Keyの差し替えエンドポイントは署名を利用した認証でブロックする
- **賞味期限をつける**
   - 90日間 inactive なセッションであれば、Restore Key を失効させるといった機構があるとより安全
   - 有効なセッションで特定のエンドポイントを叩くと Restore Key の expiration を延ばすなどするとよい
