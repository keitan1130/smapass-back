# データベース設計

実行する SQL は [db/migrations](../db/migrations/000001_initial_schema.up.sql) が正本。ユーザー提供の仮 SQL を基に、画面・証明書の公開に必要な部分を加えた初期設計。今後の署名・発行仕様で変更する。

## テーブル

| テーブル | 用途 |
| --- | --- |
| `shops` | 店舗 / 登録主体の基礎情報。事業者認証は未実装 |
| `devices` | 物理端末の主キーとなる IMEI 全桁 |
| `device_imei_aliases` | 同じ端末が持つ複数 IMEI と主 IMEI の対応 |
| `device_ownerships` | 登録者の識別情報、登録期間、最終検証済みログの受信時刻 |
| `device_legacy_ids` | 旧 UUID からの移行記録。端末管理には使わない |
| `app_installations` | IMEI と登録期間に結び付くアプリ用の認証情報の単位・失効状態 |
| `app_tokens` | アクセス・リフレッシュトークンのハッシュ、有効期限、更新履歴 |
| `key_registration_challenges` | nonce、署名対象バイト列、用途・対象・期限、使用済み状態 |
| `device_keys` | 端末の鍵履歴、方式、attestation、失効時刻 |
| `device_logs` | 不変の署名対象データ、連番、前ハッシュ、署名、収集 / 受信時刻 |
| `device_specs` | メーカー、モデル、SoC、CPU、RAM、ストレージ、画面の投影 |
| `device_status` | OS、SDK、温度、サイクル、電荷、起動、異常終了、熱的余裕の投影 |
| `anchors` | チェーン識別子、Merkle root、送信状態、トランザクション、確認時刻 |
| `log_anchors` | ログとアンカーの対応、leaf index と Merkle proof |
| `certificates` | 公開番号、状態、後継、発行時刻、発行時点の公開スナップショット |
| `certificate_logs` | 証明書に含める元ログの集合 |
| `certificate_listings` | 変更可能な出品先、商品 ID、申告された IMEI 末尾、登録日時 |
| `schema_migrations` | golang-migrate の適用バージョンと dirty 状態 |

整備履歴は公開型に場所を用意しているが、DB の専用テーブルや登録機能は未実装。`inspection` ログを事業者がどう署名するかは別途設計する。

## 仮 SQL からの主な変更

- JSONB だけでは元の署名対象バイト列を復元できないため、`canonical_payload TEXT` と正規化方式の識別子を追加。JSONB との内容一致と UTF-8 の SHA-256 を DB 制約でも検査する。JCS の仕様適合や署名検証はこの CHECK だけでは保証しない。
- アンカーの可変情報を `device_logs` から分離し、元ログの UPDATE / DELETE をトリガーで拒否する。
- `chain` を追加し、Tapyrus 固定にしない。チェーン連携 API / worker は未実装。
- 端末・鍵・投影・証明書のログ集合は IMEI の外部キーで結び付ける。鍵・ログ・証明書は登録者の履歴にも結び付ける。
- 有効な鍵は端末ごとに1つ。鍵更新は旧鍵の `revoked_at` と新鍵登録を同時に行う方針。
- 初期版の UUID と末尾4桁だけの管理を変更し、`000002` で IMEI 全桁を主キーにする。複数 IMEI は同じ主 IMEI に解決する。公開入口は `/certificates/{imei}`。再登録・検索も IMEI を基準にする。
- 旧データの全桁は末尾から推測しない。既知のデモ以外は `db/prepare-imei-migration.sql` で確認済み対応を用意してから移行する。CLI は対応不足を適用前に拒否する。
- 証明書、発行対象ログ、出品情報を追加。スナップショットの上書きを拒否し、状態と後継だけは変更できる。
- `thermal_headroom` は元の案の「0〜1」に DB 制約を固定しない。採用する Android API の値域・未取得表現を mobile 側と確認する。
- UUID と SHA-256 には PostgreSQL 標準関数を使用し、`pgcrypto` 拡張のインストールは不要。

## 型・単位・公開

- 日時は `TIMESTAMPTZ`、API は RFC 3339。DB / コンテナは UTC。画面のタイムゾーン変換は Web が行う。
- 容量は bytes、バッテリーは mAh、温度は ℃。取得不能な値は SQL NULL / JSON null。
- SHA-256 は BYTEA 32 bytes、署名は BYTEA。API ではハッシュを小文字 hex、署名を Base64 で返す。
- 公開番号は Crockford Base32 の10文字をハイフンなし・大文字で保存する。入力は大小文字・空白・ハイフンと O / I / L の読み替えに対応する。発行乱数・重複時の再試行は発行 API とともに実装する。
- JSONB スナップショットは型付きの公開データだけを返す。元ログ自体にも個人情報を入れない契約が必要。IMEI は公開 URL に使用する。所有者の識別情報や認証情報を公開データへ混ぜない。

## 不変性の範囲

`device_logs` の更新・削除、`certificates` の番号・発行時刻・端末・登録期間・デモ属性・スナップショットの変更を DB が拒否する。一方、`certificate_logs` の発行後の集合変更や、投影のログ種別・値との一致、連番の連続性、前ハッシュの一致、Merkle proof の正しさは現時点で DB が完全には強制しない。書き込み用 usecase とトランザクション / 権限設計で補う必要がある。DB 所有者によるトリガー解除も防げないため、外部チェーン照合の代替にはならない。

## マイグレーション

```bash
docker compose run --rm --build migrate up
docker compose run --rm migrate version
```

SQL は migrate バイナリに埋め込む。変更後は必ず再ビルドする。適用済みファイルを編集せず、`000002_名前.up.sql` / `.down.sql` のように追加する。初期マイグレーションはトランザクションで実行する。

ロールバック用の `docker compose run --rm migrate down-one` は直前のマイグレーションを1つ戻す。初期マイグレーションを戻すと全テーブルとデータを削除するので、使い捨て DB の検証や明示的に承認された復旧時に限る。`force` は用意していない。

失敗時は `docker compose logs migrate db` と `migrate version` を確認する。dirty になった DB に SQL を再適用して済ませず、バックアップ、対象 SQL、実際のテーブル・データと照合して復旧を決める。エラーに DSN を出さないため、migration の失敗メッセージは簡潔にしている。

## 永続化

PostgreSQL 18 の Docker 公式配置に合わせ、名前付きボリューム `dbdata18` を `/var/lib/postgresql` にマウントする。17以前の `/var/lib/postgresql/data` の配置をそのまま使わない。[公式イメージの説明](https://github.com/docker-library/docs/blob/master/postgres/README.md) を参照。

DB メジャーバージョンを変更するときは単なるイメージタグ変更ではなく、dump / restore または pg_upgrade を計画する。コード・SQL・`.env` はバックアップの代わりにならない。

## IMEI への移行と登録者の変更

詳細は [device-registration.md](device-registration.md)。署名対象の payload・署名・ハッシュを変更せず、参照先を IMEI に移す。旧 UUID の対応表はロールバックのために残す。

最終検証済みログから15日後（初回ログ前は登録から15日後）に別の登録者へ切り替えられる。トランザクションのロックでログ受信と再登録を直列化し、旧登録者の鍵を失効する。単なる参照・再登録・未検証ログでは期間を更新しない。

認証情報は `000003_device_auth` で追加する。生トークンや利用者のアカウントは保存しない。更新済みハッシュは再利用検知に使うため、現行のアプリ登録の行を途中で削除しない。[認証仕様](authentication.md) を参照。

`000004_key_attestation` は検証日時とチャレンジの参照、IMEI 検証状態を追加する。既存鍵を検証済みに昇格させない。公開鍵・検証証跡・方式・対象・登録時刻などの変更と鍵履歴の削除、失効済み鍵の復活を DB で拒否する。
