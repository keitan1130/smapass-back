# smapass-back

中古 Android 端末の状態履歴と、出品時に公開する証明書を扱う Go API の雛形です。Go と PostgreSQL を Docker Compose で動かします。保存先は PostgreSQL のみで、S3 / LocalStack は使いません。

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
curl -f http://localhost:8080/api/certificates/01ABC-DEFGH
curl -f 'http://localhost:8080/api/certificates/01ABCDEFGH/records?limit=2'
```

`make` がない環境では [環境構築](docs/setup.md) のコマンドを使ってください。デモは架空の Pixel 8 と28件の記録です。`is_demo: true` で、署名は無効、チェーン記録はありません。画面の「改ざんなし」の根拠には使えません。

## 開発機能

```bash
make dev                # Air で Go の変更を検知して再ビルド・再起動
make check              # 整形・生成差分・静的解析・通常テスト・ビルド
make test-integration   # 専用 PostgreSQL の自動起動・初期化・テスト・削除
make generate           # Go 型から OpenAPI、interface からモックを生成
```

GitHub Actions は push / PR 時に品質チェック、race 検出・カバレッジ、DB 統合テスト、Compose 起動・API 確認を行います。通常テストは Docker 不要、DB 統合テストは Docker が必要です。詳細は [開発・CI ガイド](docs/development.md) を参照してください。

## 実装済みの範囲

- Go API・PostgreSQL・マイグレーションの Compose 起動、終了処理、DB 接続確認
- 証明書詳細・番号指定の一括参照・検証用記録の参照 API
- 端末情報、評価の表示値と説明、記録期間、バッテリー推移、OS 更新履歴、記録一覧、出品先・IMEI 末尾、整備履歴の公開データ型
- 仮 SQL を基にした初期マイグレーション、発行時スナップショット、デモデータ、参照 API のレート制限と CORS

認証・認可、端末登録、ログの取り込みと署名検証、評価算出、証明書の発行・失効・再発行、出品先の更新、事業者の整備記録登録、JCS 正規化、チェーン送信・検証は未実装です。書き込み API は公開していません。カード画像・QR・OGP の生成とブックマークは Web の担当です。

## ドキュメント

| 文書 | 内容 |
| --- | --- |
| [AGENTS.md](AGENTS.md) | エージェントの作業範囲・実装規則・検証 |
| [環境構築](docs/setup.md) | 初回起動、設定、Go 単体開発、トラブル対応 |
| [開発・CI ガイド](docs/development.md) | Air、テスト自動 DB、生成コマンド、GitHub CI |
| [アーキテクチャ](docs/architecture.md) | ディレクトリ構成、責務、実装済みと今後の順序 |
| [データベース](docs/database.md) | テーブル、仮 SQL からの変更、マイグレーション |
| [API](docs/api.md) / [OpenAPI](docs/openapi.json) | 現在使える API、レスポンスとエラー |
| [画面との対応](docs/screen-data.md) | 添付画面に必要な情報と各担当 |
| [署名・検証の設計](docs/verification.md) | 生データとハッシュ、未決事項、連携時の注意 |
| [運用](docs/operations.md) | ログ、バックアップ、復元、更新 |

仕様の入口は [smapass-web/docs](../smapass-web/docs/README.md) です。Web の文書や添付 SQL は提案を含むため、この雛形で採った内容と未実装の内容を上記文書で区別しています。
