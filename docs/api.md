# API 契約

現在実装されている公開参照 API。ベース URL は開発時 `http://localhost:8080`。各 GET 応答は JSON。機械可読定義は [openapi.json](openapi.json)、Go 型は [entity/certificate.go](../internal/domain/entity/certificate.go) と [schema/response.go](../internal/interface/schema/response.go)。

OpenAPI は `make generate` で生成する。データ型は Go から生成し、URL・説明・制約は [openapi.base.json](openapi.base.json) で管理する。CI は生成物の更新漏れ、登録済み URL との一致、公開サンプルのスキーマ適合を検査する。生成結果を手で編集せず、元の定義を更新する。

## エンドポイント

| メソッド | パス | 応答 |
| --- | --- | --- |
| GET | `/health` | プロセス生存。`{"status":"ok"}` |
| GET | `/ready` | DB に Ping。正常200、接続不可503 |
| GET | `/api/devices/{imei}` | 認証不要の端末状態。登録終了中・所有者交代後・証明書未発行でも、既知のIMEIなら200 |
| POST | `/api/devices/{imei}/logs` | 署名付きスペック・状態計測を1件送信。Bearer必須。新規201、同一再送200 |
| GET | `/api/devices/{imei}/history` | 認証不要の状態計測履歴。全登録期間を通じた連番順。limit / offsetでページング |
| GET | `/certificates/{imei}` | IMEIに対応する最新の発行時スナップショット。登録終了・持ち主交代・証明書失効後も参照可。未発行なら404 |
| POST | `/api/auth/challenges` | 初回登録用の5分間有効なチャレンジ。本文は imei、成功201 |
| POST | `/api/devices/key-challenges` | 鍵交換用チャレンジ。本文は imeis、アクセストークン必須、成功201 |
| POST | `/api/auth/devices` | IMEI・公開鍵・attestation・所有確認用署名を検証し、認証情報と鍵を登録。成功201、使用中409、再登録は15日ルール |
| POST | `/api/auth/refresh` | 認証情報を更新。成功200、旧トークンを再利用するとそのアプリの認証情報を失効 |
| POST | `/api/auth/revoke` | リフレッシュトークンで認証情報だけを失効。成功204 |
| GET | `/api/auth/session` | 現在の認証対象 IMEI。アクセストークン必須 |
| POST | `/api/certificates/{imei}/revoke` | IMEIに対応する現在の登録期間の最新証明書を失効。アクセストークン必須、成功204 |
| POST | `/api/devices` | 認証済み端末の公開鍵を、新しいチャレンジと attestation で交代。端末用アクセストークン必須 |
| POST | `/api/devices/lookup` | IMEI による自分の端末の検索。端末用アクセストークン必須 |
| POST | `/api/devices/{imei}/revoke` | 自分の登録・認証情報・鍵を手動失効。端末状態・履歴は公開を継続。成功204、次の登録は即時可能。端末用アクセストークン必須 |
| GET | `/api/certificates/{imei}` | IMEIに対応する最新の発行時スナップショット。失効済みも状態を明示して返す。未発行404 |
| GET | `/api/certificates?imeis=a,b,c` | `{"items":[...]}`。1〜50 IMEI、重複・同一端末の副IMEIを統合、発行済みの最新スナップショットを主IMEI昇順で返す |
| GET | `/api/certificates/{imei}/records` | 同じIMEIの全登録期間で発行スナップショットに含めた検証用記録の重複なし集合。連番昇順。ページングあり |

公開指定はIMEIのみ。証明書番号・証明書UUIDによる指定と過去の証明書単位の公開参照は廃止した。一括取得には `imeis` を指定する。全件列挙APIは設けない。指定は1〜50件で、空要素・形式不正・チェックディジット不正は400。発行済みスナップショットがない端末は一覧から省略し、全件該当なしは `items: []`。

各端末の全登録期間から発行日時降順・同時刻なら内部UUID降順で最新の発行時スナップショットを選ぶ。失効済みでも `status` を明示して返し、古い別のスナップショットには戻らない。主IMEIと副IMEIは同じ端末へ解決する。登録終了後は旧出品者の `listing` を返さない。

明示的な証明書失効は、現在の登録期間に属する最新の1件を対象にする。現在の登録期間に証明書がない場合は404、失効済みへの再送は204、後継へ置き換え済みなら409。端末登録・鍵・認証情報は維持する。公開レスポンスに `id`、`number`、`successor_number` は含めない。

## 中古販売のための継続参照

`GET /api/devices/{imei}` は登録の有効性にかかわらず、既知の端末の状態を返す。`registration_status` は `active` / `released`。`spec` と `status` は全登録期間を通じた最新の観測値、`record_count` と最初・最後の収集日時は端末の全ログを対象にする。未計測の値・未発行の `certificate` は null。発行済みの `certificate.snapshot` は発行時点の固定値であり、現在の観測値とは区別する。

`GET /api/devices/{imei}/history` は状態計測の履歴を全登録期間にわたって返す。所有者の識別情報・認証情報・attestation は公開しない。これらの測定値が正しいことや署名検証済みであることを、このAPIは保証しない。

売却時に登録を終了しても、計測履歴と発行時スナップショットは閲覧できる。次の持ち主には新しい登録期間・認証情報・鍵を発行し、同じ主IMEIへ結び付ける。認証済みの端末検索・鍵登録応答の `next_sequence` / `previous_hash` で続きの位置を確認できる。旧持ち主の認証情報と鍵は使えない。計測ログの送信は `POST /api/devices/{imei}/logs`。形式とAndroid実装例は [計測ログ送信](measurement-ingestion.md) を参照。

## 証明書詳細

[完全なデモ応答](examples/certificate.json) を参照。詳細と一括取得は同じ Certificate 型を返す。

| 項目 | 意味 |
| --- | --- |
| `imei` | 端末の主 IMEI 全桁。公開 URL に使用 |
| `status` | `valid` / `revoked` / `superseded`。証明書の有効状態で、暗号学的な検証結果ではない |
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

`snapshot` は発行時点の値。`status`・`listing` は現在値。取得不能な測定値は null。null を0や「異常なし」に置き換えない。日時は RFC 3339、単位はフィールド名に含める。配列に表示順の意味がある履歴は時刻 / 連番昇順で格納する。

評価ロジックは未実装で、サンプルの A は表示確認用。記録欠測のしきい値も未決のためデモでは null。この状態で「記録は途切れていない」と判断しない。

## 検証用記録

`limit` は既定100、1〜500。`offset` は既定0、0〜1,000,000。上限外・整数以外は400。未登録のIMEIは404。既知の端末に公開対象の記録がまだない場合は空配列・total: 0。

[応答例](examples/records.json)：

```json
{"items": [], "total": 0, "limit": 100, "offset": 0}
```

各記録には連番・種別・収集時刻、`canonical_payload`（署名対象の UTF-8 文字列）、SHA-256 hex、前ハッシュ、Base64 署名、公開鍵と方式、正規化方式、アンカー状態、チェーン・トランザクション・Merkle 情報を含む。

- `canonical_payload` は JSON 文字列をデコードして得られる文字列の UTF-8 を、そのままハッシュする。API 応答全体や JSONB を再シリアライズしてハッシュしない。
- `anchor_state` は `unanchored` / `pending` / `confirmed` / `failed`。これはサーバーの保存状態で、ブラウザでの一致を保証しない。
- 未記録なら chain / transaction_hash / merkle_root / leaf_index は null、merkle_proof は空配列。`confirmed` と署名検証は別の事実。
- proof の各要素は `{ "side": "left" または "right", "hash": "32 bytes の小文字 hex" }`。具体的な leaf / node ハッシュの方式は未確定。
- ページをまたいで全件取得する。各発行時のログ集合を保持し、IMEIでの参照は全登録期間の公開済みログを重複なく返す。新しい記録の発行・公開によって増える。
- デモは `canonicalization: demo-json-sort-v1`、`signature_algorithm: demo-not-a-signature`。JCS / 実署名として検証しない。

## エラーと共通 HTTP 処理

```json
{"error":{"code":"not_found","message":"certificate not found"}}
```

| HTTP | code の例 | 意味 |
| --- | --- | --- |
| 400 | `invalid_device`, `invalid_measurement`, `bad_request` | IMEI・imeis・ページングの入力不正 |
| 403 | `measurement_signature_rejected` | 計測署名不正・現在の検証済み鍵ではない |
| 409 | `measurement_conflict` | 連番・前ハッシュ・同一連番の内容が競合 |
| 413 | `http_error` | 計測送信本文が128 KiBを超過 |
| 404 | `not_found` | 証明書 / ルートが存在しない |
| 405 | `method_not_allowed` | 未対応の HTTP メソッド |
| 429 | `rate_limited` | レート制限 |
| 500 | `internal_error` | 予期しない内部障害 |
| 503 | `unavailable` | DB 不通または処理タイムアウト |

API 全体に直接接続元 IP ごとのメモリ内レート制限（10リクエスト/秒相当・バースト10）を置く。ヘルスチェックは対象外。単一インスタンスの開発設定で、プロキシ配下では同じ IP に集約される。`X-Forwarded-For` は信用しない。共有ストアと信頼するプロキシの範囲は本番時に設計する。

`CORS_ORIGINS` の明示 Origin に GET / POST / OPTIONS と Authorization ヘッダーを許可する。認証 Cookie は使わない。API は `Cache-Control: no-store`、`X-Robots-Tag: noindex, nofollow` を返す。IMEIは公開識別子として扱い、HTTP ログに URI / query は残さない。

API 処理には5秒の context timeout、readiness には2秒を設定。ブラウザの検証は API 障害・RPC 障害を「照合できず」として扱い、不一致と混同しない。

## 未実装 API

証明書発行・再発行、出品先の更新、事業者の整備記録登録は未実装。`/api/me/bookmarks` と画像生成 API は提供しない。Web 側でブラウザ保存・カード画像生成を担当する。

## 端末登録と再登録

認証、入力、15日ルール、公開入口の詳細は [device-registration.md](device-registration.md)。公開証明書APIのパスは `imei`、一括取得のクエリは `imeis` を使う。

認証情報の有効期間、保存、再登録と失効の違いは [authentication.md](authentication.md) を参照。CORS は閲覧用 Web の接続制限であり、書き込み権限には使わない。

初回登録・鍵交換は `proof` が必須。証明書チェーンと所有確認用署名は非公開として扱う。[Android 鍵登録](android-key-registration.md) の条件・エラー・接続手順を参照。
