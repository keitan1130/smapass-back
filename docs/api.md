# API 契約

証明書は端末 UUID にひも付く最新の観測値と全登録期間の履歴。発行・再発行・証明書単位の失効は行わない。同じ URL は新しい計測の保存後に更新される。検索には15桁のIMEIを使い、登録した1個のIMEIから対応する端末固有のランダムUUIDへ解決する。詳細・一括表示・検証ログはUUIDのみで取得する。UUIDは持ち主や鍵の交代で変わらない。

## 公開参照

公開参照は認証不要。公開状態の端末のみ取得でき、登録終了中・再登録可能な端末は公開扱い。初回登録・持ち主交代後は非公開。

| メソッド | パス | 用途 |
| --- | --- | --- |
| GET | `/api/devices/search?imeis=a,b,c` | IMEIからUUIDへの検索。応答は `items: [{imei, id}]` の対応表のみ |
| GET | `/api/devices/{id}?limit=100&offset=0` | 最新スペック・状態と、全期間の状態計測履歴 |
| GET | `/api/devices?ids=a,b,c` | 一覧・比較用の最新概要を一括取得。履歴は含めない |
| GET | `/api/devices/{id}/records?limit=100&offset=0` | 全期間の全ログの署名・ハッシュ・チェーン検証用データ |
| GET | `/health` | プロセス生存 |
| GET | `/ready` | DB 接続確認。正常200、接続不可503 |

検索指定 `imeis` は1〜50件。空要素・形式不正・チェックディジット不正は400。同じIMEI入力は重複を除き、未知または非公開のIMEIは省略する。登録IMEIとUUIDは1対1で対応する。該当なしは `items: []`。全件列挙は提供しない。

表示一括指定 `ids` は1〜50 UUID。重複を除き、UUID昇順で概要を返す。未知または非公開のUUIDは省略する。不正/空のUUID、51件以上は400。UUIDのゼロ値は受け付けない。IMEIを詳細パスへ指定しても400。検索と一括表示の応答はそれぞれ異なる型で、IMEIを含むのは検索応答のみ。

単体と検証ログのページングは `limit` 既定100・1〜500、`offset` 既定0・0〜1,000,000。不正値は400。全期間が対象だが、全件確認にはページを順に取得する。未知または非公開のUUIDは404。公開されている既知の端末で未計測なら空配列。最新値と件数・ページは、各応答内で同じDBスナップショットから取得する。別々のリクエスト間は新規ログにより更新され得る。

## 最新情報と履歴

[応答例](examples/device-state.json)、型は `entity.DeviceState`。

| 項目 | 意味 |
| --- | --- |
| `id` | 端末に固定で割り当てたUUID。通常の表示応答はIMEIを含まない |
| `registration_status` | `active` / `released`。書き込み登録の状態であり、履歴の有効性ではない |
| `record_count` | 全ログの件数。スペックログも含む |
| `first_collected_at`, `last_collected_at` | 全ログの最初・最後の計測日時。未計測はnull |
| `spec` | 最新のスペック。OS/SDKは最新状態計測を反映。未取得はnull |
| `status` | 最新の状態計測。未取得はnull |
| `is_demo` | 全ログに `demo-json-sort-v1` の架空ログを含む場合true。falseでも検証成功を意味しない |
| `history` | `items`, `total`, `limit`, `offset`。状態計測のみ、全登録期間の連番昇順 |

`spec.collected_at` と `status.collected_at` は異なる場合がある。最終計測日時をWebに明示し、リアルタイムの実機状態と扱わない。未知の測定値はnullとし、0やfalseで補わない。日時はRFC 3339、単位はフィールド名で明示。

`spec` と `status` は、それぞれ計測日時 `collected_at` が最大の記録を選び、同時刻なら連番が大きい方を採用する。遅れて届いた古い計測も保存するが、最新表示を巻き戻さない。`last_collected_at` はスペックを含む全ログの最大計測日時なので、状態計測の日時とは異なる場合がある。履歴と検証ログは引き続き連番順で返す。3日ルールは計測日時ではなく検証済みログのサーバー受信時刻で判定する。

`history.items` はOS/SDK、電池温度・サイクル・残り電荷・推定最大電荷、起動回数・異常終了・熱的余裕を返す。バッテリー推移やOS変更の観測一覧は、この履歴の必要ページを取得してWebで表示する。`record_count` と `history.total` はスペックログの有無によって異なる。

一括応答の `DeviceSummary` は上記から `history` を除いたもの。現在のAPIは評価・欠測判定・整備記録・出品先を返さない。未実装の値を旧スナップショットやデモ評価で補わない。

## UUIDで隠せる範囲

UUIDはDB主キー・URL・署名対象で共通の端末識別子で、アクセス権ではない。検索・履歴の取得には公開状態の条件を適用する。計測ログは `schema_version: 1` とし、`device_id` を署名対象に含める。IMEI全桁・所有者情報・認証情報は署名対象に含めず、取り込み時に拒否する。検索・表示にはレート制限、no-store、noindexを適用する。

## 検証用記録

[応答例](examples/records.json)、型は `schema.Records`。証明書への収録条件はなく、全ログを連番昇順で返す。

各記録は連番・種別・計測日時、`canonical_payload`、SHA-256 hex、前ハッシュ、Base64署名、公開鍵・署名方式・正規化方式、アンカー状態・チェーン・トランザクション・Merkle情報を持つ。署名対象はJSON文字列をデコードしたUTF-8をそのまま使用し、JSONBやAPI応答を再シリアライズしない。

`unanchored` / `pending` / `confirmed` / `failed` は保存状態であり、ブラウザの検証結果ではない。送信中・ブロック待ちは `pending`。`confirmed` はブロックに入ったことをサーバーが確認した状態。未記録のチェーン情報はnull、proofは空配列。Merkle と OP_RETURN の形式は [verification.md](verification.md)。外部での照合は Web の担当で未実装。デモの `demo-json-sort-v1` / `demo-not-a-signature` は実署名として検証しない。

## アプリ用の認証・書き込み

| メソッド | パス | 役割 |
| --- | --- | --- |
| POST | `/api/auth/challenges` | 初回登録用の期限付きチャレンジ |
| POST | `/api/auth/devices` | attestation・鍵保有を検証し、端末登録と認証情報発行 |
| POST | `/api/auth/refresh` | トークン更新。旧refreshの再利用を検知 |
| POST | `/api/auth/revoke` | refreshトークンで認証情報のみ失効 |
| POST | `/api/devices/key-challenges` | Bearer必須。鍵交換用チャレンジ |
| POST | `/api/devices` | Bearer必須。attestation付きの鍵交換 |
| POST | `/api/devices/lookup` | Bearer必須。自分の端末の連番・前ハッシュ・登録状態などを取得 |
| POST | `/api/devices/{id}/logs` | Bearer必須。署名付き計測を1件保存。新規201、同一再送200 |
| PATCH | `/api/devices/{id}/visibility` | Bearer必須。現在の持ち主が公開・非公開を変更 |
| POST | `/api/devices/{id}/revoke` | Bearer必須。登録・鍵・認証を終了し、即時に次の登録を許可。履歴は残す |

公開検索と `lookup` は別用途。`lookup` の非公開の登録情報を公開APIへ移さない。対象の `device_id` は登録・更新のTokenPair応答に含まれる。lookup・鍵交換の本文も `device_id` を使う。

Webの `/certificates/{id}` はHTML・QRの遷移先として使用できる。バックエンドはこのHTMLルートを提供しない。WebはIMEI検索で得たUUIDで端末APIを呼ぶ。

## 共通処理

機械可読定義は [openapi.json](openapi.json)。Go公開型と [openapi.base.json](openapi.base.json) から `make generate` で生成する。生成物を手で変更しない。

共通エラーは `{"error":{"code":"not_found","message":"resource not found"}}`。400は入力不正、401は認証不正、403は署名/attestation拒否、404は不存在/他端末、409は3日ルール/競合、413は本文上限、429はレート制限、503はタイムアウト/検証基盤・DBの利用不能。内部障害は500。

公開APIは直接接続元IP単位のメモリ内レート制限10/秒、書き込み5/秒、認証2/秒。単一インスタンスの開発設定。CORSは明示Originだけ許可し、認可には使用しない。応答は `Cache-Control: no-store`、`X-Robots-Tag: noindex, nofollow`。処理上限5秒、readinessは2秒。URI/query・IMEI・認証情報をアクセスログに残さない。

初回登録・再登録はIMEI1個を使う。登録後のlookup・鍵交換は本文の `device_id`、ログ送信・登録終了はパスのUUIDを使う。検索クエリの `imeis=a,b,c` は複数端末を一括検索する用途。

## 公開・非公開

初回登録・持ち主交代後は非公開。現在の持ち主のAndroidアプリから次のAPIで変更する。

```http
PATCH /api/devices/{id}/visibility
Authorization: Bearer <access_token>
Content-Type: application/json

{"visibility":"public"}
```

`visibility` は `public` / `private` のいずれか。成功は200で `{"device_id":"<UUID>","visibility":"public"}` を返す。現在の状態は認証付き `POST /api/devices/lookup` の `visibility` でも取得できる。トークンを検証した後、端末ロックを取得した同じトランザクションで現在の登録者・認証情報を再確認する。別端末・不存在は404、失効済み・更新前・旧登録者の認証情報は401、不正な状態は400。

公開設定は端末の全期間の状態・履歴・検証ログに適用する。非公開中は公開IMEI検索と一括概要から除外し、公開の詳細・検証ログは404を返す。公開APIに所有者のトークンを付けても非公開データは返さない。認証付きlookupは登録情報と有効な公開状態を返す。

現在の登録がない場合、または最終検証済みログのサーバー受信から72時間経過して再登録可能な間は、保存された設定にかかわらず公開。ログ未受信なら登録開始時刻を基準にする。公開判定はDBの `public_devices` ビューでリクエスト時に行い、定期処理に依存しない。3日経過だけでは登録者の権限を失効しない。

再登録可能な間の非公開リクエストは409（`visibility_locked`）。公開設定の変更・トークン更新・鍵交換・保存済みログの再送・未検証ログでは72時間を延長しない。元の端末が署名検証済みの新規ログを送ると、保存された設定を再適用する。設定が非公開なら再び非公開になり、公開なら公開を維持する。持ち主交代時は以前の設定を引き継がず非公開に戻す。

設定変更は署名対象ログ・ハッシュ・署名・連番を変更しない。公開状態は計測ログの `schema_version` と独立している。
