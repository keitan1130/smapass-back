# smapass-back

中古 Android 端末を IMEI で管理し、状態履歴と出品時の証明書を扱う Go API です。Go と PostgreSQL を Docker Compose で動かします。保存先は PostgreSQL のみで、S3 / LocalStack は使いません。

Echo → controller → usecase → repository → Bun / PostgreSQL の構成です。

2026-11-01 確認の最新安定構成：Go 1.27.3 / PostgreSQL 18.6 / Echo 5.4.4 / Bun 1.3.0 / golang-migrate 4.20.1 / Alpine 3.24.3。公式リリースの参照先は [アーキテクチャ](docs/architecture.md) にまとめています。

## 起動

Docker Engine と Docker Compose があれば起動できます。ホストへの Go / PostgreSQL のインストールは不要です。

```bash
cd /home/keitan/workspace/smapass/smapass-back
cp .env.example .env
docker compose up -d --build --wait app
curl -f http://localhost:8080/health
curl -f http://localhost:8080/ready
```

初回起動時に DB マイグレーションを実行します。DB は空の状態から始まります。画面接続用のデモを入れる場合：

```bash
make seed
curl -f 'http://localhost:8080/api/devices/search?imeis=000000000094821'
```

`make` がない場合は [環境構築](docs/setup.md) を参照。デモは架空のPixel 8と28件の記録。`is_demo: true` で署名無効、アンカーなし。実際の検証成功として扱わない。

## 開発機能

```bash
make dev                # Air で Go の変更を検知して再ビルド・再起動
make check              # 整形・生成差分・静的解析・通常テスト・ビルド
make test-integration   # 専用 PostgreSQL の自動起動・初期化・テスト・削除
make generate           # Go 型から OpenAPI、interface からモックを生成
```

GitHub Actions は push / PR 時に品質チェック、race 検出・カバレッジ、DB 統合テスト、Compose 起動・API 確認を行います。通常テストは Docker 不要、DB 統合テストは Docker が必要です。詳細は [開発・CI ガイド](docs/development.md) を参照してください。

## ブランチ命名規則

ブランチの命名規則は以下のようにすること

```
[接頭辞]/#issue番号-内容-内容
```

内容はケバブケースを使用し、接頭辞には以下のような内容を使用すること。

| 接頭辞   | 内容                                                                                                                       |
| :------- | :------------------------------------------------------------------------------------------------------------------------- |
| fix      | 既存の機能の問題を修正する場合に使用します。                                                                               |
| hotfix   | 緊急の変更を追加する場合に使用します。                                                                                     |
| feat     | 新しい機能やファイルを追加する場合に使用します。                                                                           |
| update   | 既存の機能に問題がないが、修正を加えたい場合に使用します。                                                                 |
| change   | 仕様変更により、既存の機能に修正を加えた場合に使用します。                                                                 |
| refactor | コードの改善をする場合に使用します。                                                                                       |
| delete   | ファイルを削除する場合や、機能を削除する場合に使用します。                                                                 |
| rename   | ファイル名を変更する場合に使用します。                                                                                     |
| move     | ファイルを移動する場合に使用します。                                                                                       |
| upgrade  | バージョンをアップグレードする場合に使用します。                                                                           |
| revert   | 以前のコミットに戻す場合に使用します。                                                                                     |
| docs     | ドキュメントを修正する場合に使用します。                                                                                   |
| style    | コーディングスタイルの修正をする場合に使用します。                                                                         |
| test     | テストコードを修正する場合や、テストコードを追加する場合に使用します。                                                     |
| chore    | ビルドツールやライブラリで自動生成されたものをコミットする場合や、上記の接頭辞に当てはまらない修正をする場合に使用します。 |

## コミット命名規則

```
[接頭辞]: #issue番号-変更点
```

上記の命名規則でコミットすること、接頭辞はブランチ命名規則のものと同じです。

## 実装済みの範囲

- Go API・PostgreSQL・マイグレーションの Compose 起動、終了処理、DB 接続確認
- IMEI検索でUUIDへ解決し、公開の端末状態・状態履歴はUUIDで継続参照。登録終了中・持ち主交代後・証明書未発行でも取得可能
- 最新状態と履歴を統合した単体取得、1〜50 IMEIの概要一括取得
- 最新スペック・状態・最終計測日時・デモ属性と全登録期間の状態計測履歴
- 計測ログの受信・JCSとECDSA署名検証・PostgreSQLへの元ログと表示用データの一括保存・同一送信の再試行
- Android ハードウェア鍵の attestation・秘密鍵の保有確認、初回登録と鍵交換のチャレンジ
- IMEI 主キーと複数 IMEI の照合、登録者履歴、3日間の非活動を条件とした再登録、鍵の交代
- ログインなしの端末認証情報の発行・更新・失効、端末と証明書への書き込み権限の確認
- 現在の登録者による手動失効（認証情報・鍵を失効、履歴は維持、3日待たずに次の登録が可能）
- IMEIからUUIDへの検索、UUIDによる最新状態・全期間の履歴と検証ログの公開、一括概要取得
- 初期スキーマ1本、旧DB向けの明示的移行経路、デモ、レート制限とCORS

評価算出、出品先の更新、事業者の整備記録登録、チェーン送信・検証は未実装。証明書の発行・再発行は不要。書き込みには端末用アクセストークンと検証済み鍵が必要。カード画像・QR・OGP・ブックマークはWebが担当する。

## ドキュメント

| 文書 | 内容 |
| --- | --- |
| [AGENTS.md](AGENTS.md) | エージェントの作業範囲・実装規則・検証 |
| [環境構築](docs/setup.md) | 初回起動、設定、Go 単体開発、トラブル対応 |
| [開発・CI ガイド](docs/development.md) | Air、テスト自動 DB、生成コマンド、GitHub CI |
| [アーキテクチャ](docs/architecture.md) | ディレクトリ構成、責務、実装済みと今後の順序 |
| [ログインなしのアプリ認証](docs/authentication.md) | IMEI からの認証情報発行、更新・失効、Android 接続、端末・証明書の権限 |
| [計測ログ送信](docs/measurement-ingestion.md) | Androidからの署名付きスペック・状態送信、形式、再試行と連番 |
| [Android 端末・鍵登録](docs/android-key-registration.md) | attestation の検証条件、アプリ設定、チャレンジ、鍵の保有確認・交代 |
| [IMEI・登録者管理](docs/device-registration.md) | IMEI 登録、3日ルール、公開 URL、認証連携 |
| [データベース](docs/database.md) | テーブル、仮 SQL からの変更、マイグレーション |
| [API](docs/api.md) / [OpenAPI](docs/openapi.json) | 現在使える API、レスポンスとエラー |
| [画面との対応](docs/screen-data.md) | 添付画面に必要な情報と各担当 |
| [署名・検証の設計](docs/verification.md) | 生データとハッシュ、未決事項、連携時の注意 |
| [運用](docs/operations.md) | ログ、バックアップ、復元、更新 |

仕様の入口は [smapass-web/docs](../smapass-web/docs/README.md) です。Web の文書や添付 SQL は提案を含むため、この雛形で採った内容と未実装の内容を上記文書で区別しています。

検索結果の `id` を使い、`/api/devices/{id}?limit=2`、`/api/devices?ids={id}`、`/api/devices/{id}/records` を取得する。
