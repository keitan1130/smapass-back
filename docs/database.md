# データベース

証明書はIMEIの最新観測値と全期間のログから表示する。発行済みスナップショットを保存する構造は廃止した。現行スキーマは [初期定義](../db/migrations/000001_initial_schema.up.sql) 1本。

| テーブル | 役割 |
| --- | --- |
| `devices` | 主IMEI・公開用ランダムUUID `public_id`（unique）・作成日時 |
| `device_imei_aliases` | 副IMEIから主IMEIへの解決 |
| `device_ownerships` | 登録者の期間と最終検証済みログ受信時刻。3日ルールと権限に必要 |
| `device_keys` | 期間ごとの鍵、非公開attestation、鍵の失効履歴 |
| `device_logs` | 署名対象の正本、ハッシュ、前ハッシュ、署名、連番。更新・削除禁止 |
| `device_specs`, `device_status` | 元ログから作る最新表示・履歴用の投影 |
| `app_installations`, `app_tokens` | ログインなしの端末認証。トークンのハッシュと更新・再利用検知 |
| `key_registration_challenges` | 初回登録・鍵交換の期限付きチャレンジと消費状態 |
| `anchors`, `log_anchors` | 検証APIが返すチェーン・Merkle情報。チェーン送信自体は未実装 |

最新値と履歴は全登録期間を対象にし、主IMEI・公開用UUIDと端末全体の連番を引き継ぐ。過去の鍵は署名検証に必要なので削除しない。過去の登録期間・更新済みトークンも権限と再利用検知に必要。`canonical_payload` のUTF-8が署名の正本であり、JSONBから作り直さない。DBトリガーはログの連番・前ハッシュ・現登録と鍵の有効性を検査する。署名はアプリ層で検証し、検証済み受信時刻で3日ルールを更新する。


移行方法と旧DBの扱いは [db/README.md](../db/README.md)。旧SQL・移行専用テーブルは残さない。初期定義は新規DB専用で、既存DBを自動で初期化・再番号付けしない。
