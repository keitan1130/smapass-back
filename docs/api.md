# API 契約

現在実装されている公開参照 API。ベース URL は開発時 `http://localhost:8080`。各 GET 応答は JSON。機械可読定義は [openapi.json](openapi.json)、Go 型は [entity/certificate.go](../internal/domain/entity/certificate.go) と [schema/response.go](../internal/interface/schema/response.go)。

OpenAPI は `make generate` で生成する。データ型は Go から生成し、URL・説明・制約は [openapi.base.json](openapi.base.json) で管理する。CI は生成物の更新漏れ、登録済み URL との一致、公開サンプルのスキーマ適合を検査する。生成結果を手で編集せず、元の定義を更新する。

## エンドポイント

| メソッド | パス | 応答 |
| --- | --- | --- |
| GET | `/health` | プロセス生存。`{"status":"ok"}` |
| GET | `/ready` | DB に Ping。正常200、接続不可503 |
| GET | `/certificates/{imei}` | 現在の登録者の最新の有効な証明書。未発行なら404 |
| POST | `/api/auth/challenges` | 初回登録用の5分間有効なチャレンジ。本文は imei、成功201 |
| POST | `/api/devices/key-challenges` | 鍵交換用チャレンジ。本文は imeis、アクセストークン必須、成功201 |
| POST | `/api/auth/devices` | IMEI・公開鍵・attestation・所有確認用署名を検証し、認証情報と鍵を登録。成功201、使用中409、再登録は15日ルール |
| POST | `/api/auth/refresh` | 認証情報を更新。成功200、旧トークンを再利用するとそのアプリの認証情報を失効 |
| POST | `/api/auth/revoke` | リフレッシュトークンで認証情報だけを失効。成功204 |
| GET | `/api/auth/session` | 現在の認証対象 IMEI。アクセストークン必須 |
| POST | `/api/certificates/{number}/revoke` | 現在の端末登録の証明書1件だけを失効。アクセストークン必須、成功204 |
| POST | `/api/devices` | 認証済み端末の公開鍵を、新しいチャレンジと attestation で交代。端末用アクセストークン必須 |
| POST | `/api/devices/lookup` | IMEI による自分の端末の検索。端末用アクセストークン必須 |
| POST | `/api/devices/{imei}/revoke` | 自分の登録・鍵・有効な証明書を手動失効。成功204、次の登録は即時可能。端末用アクセストークン必須 |
| GET | `/api/certificates/{number}` | 証明書 UUID または短い番号で1件。不存在404 |
| GET | `/api/certificates?ids=a,b,c` | `{"items":[...]}`。1〜50番号、重複除去、存在するものを番号昇順で返す |
| GET | `/api/certificates/{number}/records` | 発行時に含めた検証用記録。連番昇順。ページングあり |

一覧は番号を指定する方式で、全証明書の列挙 API は設けない。不明な番号は一覧結果から省略する。指定全件がなければ `items: []`。ids がない場合、空要素や形式不正の番号を含む場合は400。

番号の保存・応答は `01ABCDEFGH` の形式。入力は `01ABC-DEFGH`、小文字、空白、O→0・I/L→1 を許容する。Web は表示時に5文字ずつ区切れる。番号生成は発行 API とともに実装予定。

## 証明書詳細

[完全なデモ応答](examples/certificate.json) を参照。詳細と一括取得は同じ Certificate 型を返す。

| 項目 | 意味 |
| --- | --- |
| `id` | 証明書の UUID。端末の管理 ID ではない |
| `imei` | 端末の主 IMEI 全桁。公開 URL に使用 |
| `number` | 互換性と手入力用の番号10文字 |
| `status` | `valid` / `revoked` / `superseded`。証明書の有効状態で、暗号学的な検証結果ではない |
| `successor_number` | 再発行済みの場合の後継番号。それ以外は null |
| `issued_at` | 発行日時 |
| `is_demo` | 架空データか。true の場合は実証明書として扱わない |
| `snapshot.schema_version` | 公開スナップショットの形式。現在1 |
| `snapshot.device` | メーカー・モデル・端末名・SoC・コア数・RAM / ストレージ bytes・画面インチ / dpi・OS / SDK・取得日時 |
| `snapshot.grade` | 総合評価、ラベル、理由、算出方式のバージョン、項目別評価。`value` は未評価なら null |
| `snapshot.recording` | 記録の開始・終了・件数、欠測判定のしきい値（秒）と空白期間。しきい値 null は未評価 |
| `snapshot.battery` | 初期 / 最新の推定最大電荷 mAh、変化率 %、サイクル回数、時系列の電荷 / 温度 |
| `snapshot.os_history` | OS / SDK の変更を観測した日時の一覧。実際に OS を更新した時刻とは限らない |
| `snapshot.status_history` | 記録一覧。OS / SDK、温度、サイクル、残り電荷、推定最大電荷、起動回数、異常終了、熱的余裕 |
| `snapshot.maintenance_records` | 事業者名・種別・日時・内容。登録機能は未実装、デモは空配列 |
| `listing` | 出品先・商品 ID・IMEI 末尾4桁・登録日時。未登録なら null |

`snapshot` は発行時点の値。`status`・後継・`listing` は現在値。取得不能な測定値は null。null を0や「異常なし」に置き換えない。日時は RFC 3339、単位はフィールド名に含める。配列に表示順の意味がある履歴は時刻 / 連番昇順で格納する。

評価ロジックは未実装で、サンプルの A は表示確認用。記録欠測のしきい値も未決のためデモでは null。この状態で「記録は途切れていない」と判断しない。

## 検証用記録

`limit` は既定100、1〜500。`offset` は既定0、0〜1,000,000。上限外・整数以外は400。存在しない証明書は、ページが空でも404。

[応答例](examples/records.json)：

```json
{"items": [], "total": 0, "limit": 100, "offset": 0}
```

各記録には連番・種別・収集時刻、`canonical_payload`（署名対象の UTF-8 文字列）、SHA-256 hex、前ハッシュ、Base64 署名、公開鍵と方式、正規化方式、アンカー状態、チェーン・トランザクション・Merkle 情報を含む。

- `canonical_payload` は JSON 文字列をデコードして得られる文字列の UTF-8 を、そのままハッシュする。API 応答全体や JSONB を再シリアライズしてハッシュしない。
- `anchor_state` は `unanchored` / `pending` / `confirmed` / `failed`。これはサーバーの保存状態で、ブラウザでの一致を保証しない。
- 未記録なら chain / transaction_hash / merkle_root / leaf_index は null、merkle_proof は空配列。`confirmed` と署名検証は別の事実。
- proof の各要素は `{ "side": "left" または "right", "hash": "32 bytes の小文字 hex" }`。具体的な leaf / node ハッシュの方式は未確定。
- ページをまたいで全件取得する。公開記録の集合は発行時に固定する設計。
- デモは `canonicalization: demo-json-sort-v1`、`signature_algorithm: demo-not-a-signature`。JCS / 実署名として検証しない。

## エラーと共通 HTTP 処理

```json
{"error":{"code":"not_found","message":"certificate not found"}}
```

| HTTP | code の例 | 意味 |
| --- | --- | --- |
| 400 | `invalid_number`, `bad_request` | 番号・ids・ページングの入力不正 |
| 404 | `not_found` | 証明書 / ルートが存在しない |
| 405 | `method_not_allowed` | 未対応の HTTP メソッド |
| 429 | `rate_limited` | レート制限 |
| 500 | `internal_error` | 予期しない内部障害 |
| 503 | `unavailable` | DB 不通または処理タイムアウト |

API 全体に直接接続元 IP ごとのメモリ内レート制限（10リクエスト/秒相当・バースト10）を置く。ヘルスチェックは対象外。単一インスタンスの開発設定で、プロキシ配下では同じ IP に集約される。`X-Forwarded-For` は信用しない。共有ストアと信頼するプロキシの範囲は本番時に設計する。

`CORS_ORIGINS` の明示 Origin に GET / POST / OPTIONS と Authorization ヘッダーを許可する。認証 Cookie は使わない。API は `Cache-Control: no-store`、`X-Robots-Tag: noindex, nofollow` を返す。公開番号は閲覧用の秘密情報として扱い、HTTP ログに URI / query は残さない。

API 処理には5秒の context timeout、readiness には2秒を設定。ブラウザの検証は API 障害・RPC 障害を「照合できず」として扱い、不一致と混同しない。

## 未実装 API

ログ登録、証明書発行・再発行、出品先の更新、事業者の整備記録登録は未実装。`/api/me/bookmarks` と画像生成 API は提供しない。Web 側でブラウザ保存・カード画像生成を担当する。

## 端末登録と再登録

認証、入力、15日ルール、公開入口の詳細は [device-registration.md](device-registration.md)。公開証明書 API のパス中の `number` と一括取得の `ids` は、証明書 UUID も受け付ける。

認証情報の有効期間、保存、再登録と失効の違いは [authentication.md](authentication.md) を参照。CORS は閲覧用 Web の接続制限であり、書き込み権限には使わない。

初回登録・鍵交換は `proof` が必須。証明書チェーンと所有確認用署名は非公開として扱う。[Android 鍵登録](android-key-registration.md) の条件・エラー・接続手順を参照。
