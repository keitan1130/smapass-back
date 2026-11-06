# Androidからの計測ログ送信

`POST /api/devices/{imei}/logs` で端末スペックまたは状態計測を1件保存する。端末登録済みのアクセストークンと、その登録期間の有効なハードウェア鍵が必要。保存後は `GET /api/devices/{id}` の `history` から状態・計測履歴を取得できる。Androidアプリ・Web画面のコードは別リポジトリの担当。

## 送信する手順

1. [端末登録](android-key-registration.md) を完了し、アクセストークンとAndroid Keystoreの鍵エイリアスを保持する。
2. `POST /api/devices/lookup` にBearerトークンと `{"imei":"..."}` を送る。応答の登録済みの `imei`、`key_id`、`next_sequence`、`previous_hash` を使う。署名対象にも同じ登録IMEIを入れる。
3. 下記のpayloadを作り、RFC 8785 JCSで正規化する。未取得の値も省略せず `null` にする。`spec` と `status` は片方だけをオブジェクトにする。
4. 正規化した文字列のUTF-8バイト列を、その鍵の `SHA256withECDSA` で署名する。返されるASN.1 DER署名を標準Base64へ変換し、正規化文字列とともに送る。ハッシュを先に渡して二重にSHA-256をかけない。
5. 応答を受け取るまでは同じ正規化文字列・同じ署名を保持する。通信結果が不明なときはその2つをそのまま再送する。受理後に次の連番・前ハッシュへ進める。

```http
POST /api/devices/490154203237518/logs
Authorization: Bearer <access_token>
Content-Type: application/json

{
  "canonical_payload": "<下記payloadをJCS正規化したJSON文字列>",
  "signature_der": "<UTF-8正本へのECDSA署名DERを標準Base64にした値>"
}
```

署名対象のpayloadの例（この整形済みJSON自体は正規化後の署名対象ではない）：

```json
{
  "schema_version": 1,
  "imei": "490154203237518",
  "key_id": "11111111-1111-4111-8111-111111111111",
  "sequence": 1,
  "type": "status",
  "collected_at": "2026-01-01T00:00:00Z",
  "previous_hash": null,
  "spec": null,
  "status": {
    "os_version": "16",
    "sdk_int": 36,
    "battery_temp_c": 32.3,
    "battery_cycle_count": 162,
    "battery_charge_mah": 3200,
    "battery_full_charge_mah": 4400,
    "boot_count": 46,
    "had_abnormal_exit": false,
    "thermal_headroom": 0.3123456789012345
  }
}
```

`type: "spec"` の場合は `status: null` とし、`spec` に次を指定する。

```json
{
  "manufacturer": "Google",
  "model": "Pixel 8",
  "soc": "Google Tensor G3",
  "cpu_cores": 9,
  "ram_bytes": 8589934592,
  "storage_bytes": 137438953472,
  "screen_size_inch": 6.2,
  "screen_density_dpi": 428
}
```

## 形式と検証

- JCSは [RFC 8785](https://www.rfc-editor.org/rfc/rfc8785) に従う。[Go実装](https://github.com/cyberphone/json-canonicalization/tree/master/go) を固定バージョンで使用する。単なるJSONキーのソートとは区別する。
- `schema_version` は1。`type` は `spec` / `status`。`inspection` は受け付けない。全フィールドを含め、NULLはJSONの `null` とする。重複キー、未知の項目、不正Unicode、過度のネスト、正規化されていない文字列は400。
- 署名には `key_id` とIMEI・連番・時刻・前ハッシュ・計測値の全体を含める。サーバーが正本のSHA-256を計算するため、別のハッシュ値は送らない。
- `collected_at` はUTCのRFC 3339。小数秒は最大6桁で不要な末尾0を省く。1970年以降、サーバー現在時刻の5分後まで。過去のオフライン計測も受け付けるが、失効した鍵による新規送信は拒否する。
- 連番は端末全体で1から続き、所有者交代・鍵交換でもリセットしない。1件目だけ `previous_hash: null`。それ以降は直前の正本のSHA-256小文字hex64桁を指定する。連番は1〜9007199254740990、容量bytesは1〜9007199254740991。
- メーカー・モデルは1〜128 UTF-8 bytes、SoCはnullまたは1〜128 bytes。OSバージョンは1〜64 bytes。前後の空白と制御文字は拒否する。個人が設定した端末名、所有者ID、認証情報、attestation、自由記述は送れない。
- CPUコア数・dpi・SDKは1〜32767。画面インチは0.01〜99.99、小数2桁まで。温度は−9999.9〜9999.9℃、小数1桁まで。サイクル・電荷・起動回数は0〜2147483647、満充電容量は1〜2147483647。不明な数値・boolはnullとし、0 / falseで代用しない。
- `thermal_headroom` は有限のbinary64値またはnull。0〜1には固定しない。PostgreSQLにもDOUBLE PRECISIONで保存する。
- HTTP本文は最大128 KiB、正規化payloadはUTF-8で最大64 KiB。署名はP-256のASN.1 DER、標準Base64。Base64urlやIEEE P1363の固定64バイト署名は受け付けない。

## 応答・再送・持ち主の交代

新規保存は201、完全に同じ送信の再試行は200。

```json
{
  "imei": "490154203237518",
  "sequence": 1,
  "payload_hash": "<正本のSHA-256小文字hex64桁>",
  "received_at": "2026-11-05T09:00:00Z",
  "signature_verified": true,
  "duplicate": false
}
```

`received_at` はサーバー受信時刻。`signature_verified` は登録済み公開鍵で署名が検証できたという意味で、計測値の真実性、IMEI自体の実機同一性、ブロックチェーン記録を保証しない。

元ログ・投影・最終検証済みログ受信時刻は同じトランザクションで保存し、失敗時はすべてロールバックする。再送は署名も含めて一致する場合だけ元の受信時刻を返し、3日ルールの活動時刻を延長しない。同じ登録期間での鍵交換後も既存送信の完全一致再送は可能だが、旧鍵で新しいログは追加できない。

409は連番の飛び・前ハッシュの不一致・同一連番への異なる送信。端末検索で現在の `next_sequence` / `previous_hash` を確認し、保存済みの送信と矛盾していないか調べる。古い送信を異なる内容で上書きしない。401では認証情報を確認し、403では現在の鍵と署名形式を確認する。

売却時は [登録終了API](device-registration.md#現在の登録者による手動失効) を使う。次の持ち主は新しく登録して、同じ登録IMEIの次の連番・前ハッシュを引き継ぐ。過去の状態・計測履歴は引き続き公開取得できる。旧持ち主の認証情報と鍵は無効。

## 共通テストデータと範囲

[署名ベクタ](examples/measurement-vector.json) にはpayload・JCS正本・SHA-256・公開鍵・DER署名を含め、Goで検証する。[送信本文](examples/measurement-request.json) と [正規化ベクタ](examples/jcs-vectors.json) はAndroid・ブラウザ実装でも同じ結果になるか確認するための素材。署名ベクタの鍵はテスト専用で、本番の端末登録には使わない。サンプルのIMEI・鍵IDを送っても、その鍵が現在の登録鍵でなければ拒否される。

このAPIは署名検証・正本と投影の保存を担当する。保存後は最新証明書表示と `/api/devices/{id}/records` に自動で反映される。発行操作は不要。評価算出・チェーン送信は未実装。
