# Android の端末・公開鍵登録

## 登録するものと検証条件

利用者のアカウントやログインは使わず、端末は引き続き IMEI 全桁で管理する。初回はサーバーのチャレンジを取得し、Android Keystore で作った公開鍵・attestation 証明書チェーン・秘密鍵での署名を送る。検証に成功した場合だけ、端末の登録期間・公開鍵・アプリ用の認証情報を同じトランザクションで保存する。

採用する鍵は ECDSA P-256 / SHA-256（`ES256`）。公開鍵は PEM SubjectPublicKeyInfo。今回の所有確認用署名は Android の `SHA256withECDSA` が返す ASN.1 DER を標準 Base64 にして `signature_der` に入れる。ログの署名対象・JCS・署名の転送形式は #3 の対象であり、このチャレンジ形式と混同しない。

検証ポリシーは `android-hardware-v1` とし、次を必須にする。

- 埋め込みの Google 公式 Android attestation ルートへ到達する証明書チェーン。クライアントが送った自己署名ルートを信頼しない。
- チェーン全体の署名と有効期間、Google の失効リストを確認。失効・停止された証明書は拒否する。
- 鍵・attestation とも Trusted Environment または StrongBox。ソフトウェア鍵は拒否する。
- ハードウェア側で生成された P-256 鍵、署名用途、SHA-256。インポート鍵や別アルゴリズムは拒否する。
- ブートローダーがロックされ、Verified Boot が `Verified`。Android 8 以降の OS バージョンと有効な形式の OS パッチ年月を持つこと。パッチの新しさの上限はこの版では設けない。
- 許可した `applicationId` が1件だけ含まれ、提示されたすべてのアプリ署名証明書 SHA-256 が許可一覧に入ること。
- 証明書内の attestation challenge とサーバーの nonce が一致し、登録する公開鍵でサーバーの署名対象バイト列を検証できること。

attestation 拡張はルート側から最初のものだけを信頼する。この API はその証明書自身が登録対象のリーフ鍵であるチェーンだけを受け入れる。攻撃者が下位証明書と偽の attestation を継ぎ足したチェーンは拒否する。

信頼ルートは `internal/infrastructure/attestation/google_roots.json` に Google 公式の2ルートを同梱した（2026-11-01取得）。2026年に使われる新ルートにも対応する。更新時は公式配布元と照合して変更する。失効リストは固定の Google HTTPS URL から取得し、HTTP のキャッシュ有効期間内、最大1時間だけキャッシュする。期限切れ後に取得できなければ503とし、古いリストで検証を続けない。

この版は証明書の有効期間をすべて厳密に確認する。Google が信頼継続を案内している一部の期限切れ factory-key チェーンにも、有効期間を無視する例外は設けない。そのような旧端末や、Google のルートへ到達しない端末・ソフトウェア鍵しかないエミュレーターは登録できない。

## アプリの設定

```dotenv
# 例示値をそのまま使わず、実際のアプリに合わせる。
ATTESTATION_PACKAGE_NAME=
ATTESTATION_SIGNING_SHA256=
ATTESTATION_REQUIRE_IMEI=false
```

`ATTESTATION_PACKAGE_NAME` は Android の `app/build.gradle.kts` の `applicationId`。アプリを内部で区別する名前で、例は `com.example.smapass`。画面上のアプリ名ではない。

`ATTESTATION_SIGNING_SHA256` はアプリ署名証明書の SHA-256。64桁の16進数か `:` 区切りを受け付ける。複数の許可済み署名はカンマ区切りで指定できる。Play App Signing を使う場合は、配布 APK のアプリ署名証明書を使い、アップロード鍵と取り違えない。デバッグ版と配布版は環境ごとに設定を分ける。

両方未設定なら公開の参照 API は動くが、チャレンジの発行・初回登録・鍵交換は503。片方だけの設定や不正な値は起動エラーにする。検証を省略する開発用スイッチや、本番でテストルートを追加する設定はない。

## IMEI の検証範囲

通常の key attestation は「許可アプリが作ったハードウェア鍵」を確認するもので、申告された IMEI がその実機の識別子かまで必ず証明するものではない。`key_attested` と `imei_attested` を分けて返し、IMEI 自体の attestation がない場合は `imei_attested: false` にする。

証明書に IMEI のハードウェア attestation がある場合は、主 IMEI と登録対象のすべての IMEI に一致することも確認する。不一致は設定にかかわらず拒否する。

`ATTESTATION_REQUIRE_IMEI=true` にすると、IMEI 自体の attestation も必須になる。Android の ID attestation は対応端末と権限・管理条件が必要で、通常の一般アプリで利用できるとは限らない。この設定を有効にする場合は、対象端末とアプリの権限を先に確認する。`false` の場合も、IMEI の実機同一性まで確認済みとは表示しない。

## 初回登録・初期化後の再登録

1. `POST /api/auth/challenges` に `{"imei":"490154203237518"}` を送る。
2. 応答の `attestation_challenge` を標準 Base64 から復号し、Android の `KeyGenParameterSpec.Builder.setAttestationChallenge()` に渡して新しい P-256 鍵を生成する。
3. `signing_payload` を Base64 から復号する。そのバイト列を再構築・JSON 再エンコードせず、生成した鍵の `SHA256withECDSA` で署名する。
4. `KeyStore.getCertificateChain()` の証明書をリーフから順に DER → 標準 Base64 にする。公開鍵は X.509 SubjectPublicKeyInfo → PEM にする。
5. 次の登録 API に送る。成功は201で、従来と同じトークン一式を返す。この時点で公開鍵の登録も完了している。

```http
POST /api/auth/devices
Content-Type: application/json

{
  "imei": "490154203237518",
  "public_key": "-----BEGIN PUBLIC KEY-----\n...\n-----END PUBLIC KEY-----\n",
  "proof": {
    "challenge_id": "<チャレンジ応答の id>",
    "certificate_chain": ["<リーフのDERをBase64>", "<中間証明書など>", "<ルート>"] ,
    "signature_der": "<署名DERをBase64>"
  }
}
```

チャレンジは5分間有効で、用途・IMEI・署名対象にひも付く。成功した初回登録のチャレンジは再利用できない。検証や登録条件の確認が失敗した場合は、端末・鍵・認証情報を途中まで保存しない。

初期化後は新しい秘密鍵を生成し、同じ手順で登録する。既存の登録が使用中なら409。現在の登録者による手動失効後、または最終検証済みログから15日後に再登録を許可する。attestation を検証することを理由に、この15日ルールを迂回しない。旧鍵は失効させ、公開鍵と過去ログは残す。

登録後は `GET /api/auth/session` と `POST /api/devices/lookup` で確認できる。Android アプリの実装・実機接続は別リポジトリの担当で、ここではバックエンドと接続仕様を実装している。

## 鍵交換・副 IMEI の登録

アクセストークンを付け、`POST /api/devices/key-challenges` に `{"imeis":["490154203237518"]}` を送る。そのチャレンジで新しい鍵と証明書を生成し、`POST /api/devices` へ従来の `imeis` / `public_key` に上と同じ `proof` を加えて送る。副 IMEI を追加する場合は `imeis` に合わせて指定する。

チャレンジは現在のアプリ登録に結び付く。他アプリ登録のチャレンジや、初回登録のチャレンジで鍵交換はできない。保存直前にも同じトランザクション内でトークンと端末への権限を確認する。

成功は200。旧鍵の失効、新鍵と非公開の検証証跡の保存、チャレンジの使用済みへの更新を1トランザクションで行う。証明書・署名・nonce・用途・対象・期限の確認が失敗しても旧鍵は有効なまま残る。同じ成功済みリクエストの再送は、5分以内で、同じアプリ登録と現在の同じ鍵に一致する場合だけ同じ結果を返す。それ以外の使用済みチャレンジは409。

既存鍵と同じ公開鍵を新しいチャレンジへ使い回すことは拒否する。Android Keystore では新しい attestation challenge を設定した新しい鍵を生成する。過去の鍵の再利用・失効解除・公開鍵の上書き・履歴の削除は行わない。

## 保存・移行・エラー

`000004_key_attestation` がチャレンジと検証日時・IMEI 検証状態を追加する。既存の未検証鍵やデモ鍵は自動で検証済みにしない。公開鍵と方式、attestation 証跡などを変更・削除する DB 操作を拒否し、失効だけを追記できるようにする。

証明書チェーン、検証ポリシー、検証日時、アプリ情報、所有確認用署名は `device_keys.attestation` に非公開で保存する。公開証明書や `/records` には流さない。公開の過去ログには、引き続きそのログの元の公開鍵を返す。検証日時は鍵登録時の確認結果で、継続的な端末状態の監視や計測値そのものの正しさを示すものではない。

| 状況 | HTTP |
| --- | --- |
| IMEI・公開鍵・JSONの形式不正 | 400 |
| トークン不正・失効済み | 401 |
| attestation・起動状態・アプリ・所有確認用署名・IMEI証明の不一致 | 403 |
| 別端末への操作 | 404 |
| チャレンジの期限・用途・対象・再利用の不正、使用中のIMEI、鍵競合 | 409 |
| レート制限 | 429 |
| 検証設定がない、Google失効リストが確認できない | 503 |

テストは合成 CA と証明書・実際の ECDSA 署名・一時 PostgreSQL を使う。Google 公式のルートをテスト CA と置き換える機構は integration テストのビルドだけに含め、アプリの DI では使わない。Android 実機からの attestation が通ったことや、計測ログ・外部チェーンの検証が完成したことは意味しない。

## 公式資料

- [Android: hardware-backed key attestation の検証](https://developer.android.com/privacy-and-security/security-key-attestation)
- [AOSP: Key and ID attestation の ASN.1 と各フィールド](https://source.android.com/docs/security/features/keystore/attestation)
- [Google: 信頼ルートの公式配布](https://android.googleapis.com/attestation/root)
- [Google: 証明書失効状態](https://android.googleapis.com/attestation/status)
- [Android: applicationId の設定](https://developer.android.com/build/configure-app-module)
