# 開発と CI

## コマンド

ホストでのチェックには Go 1.27.3、race 検出には C コンパイラー、DB 統合テストには Docker が必要。起動済み API の `make smoke` には Python 3 を使う（追加パッケージ不要）。開発サーバーの起動は Docker だけでもよい。コマンドはリポジトリのルートで実行する。

| コマンド | 内容 |
| --- | --- |
| `make up` | コンパイル済み API、DB、マイグレーションを起動 |
| `make dev` | Go の変更で自動再ビルド・再起動する Air 構成 |
| `make down` | 停止。開発 DB のボリュームは保持 |
| `make fmt` / `make fmt-check` | Go 整形 / 整形漏れ確認 |
| `make generate` | OpenAPI と repository モックを再生成 |
| `make generate-check` | ファイルを書き換えずに生成物の更新漏れを検査 |
| `make test` | Docker 不要の通常テスト |
| `make test-race` | race 検出、`coverage.out` と関数別カバレッジ |
| `make test-integration` | 専用 PostgreSQL の自動起動から削除まで含む DB テスト |
| `make smoke` | デモ投入済み API の health・DB 接続・証明書・記録の HTTP 確認 |
| `make vet` / `make build` | 静的解析 / `bin/server`・`bin/migrate` の生成 |
| `make check` | 整形、生成物、静的解析、通常テスト、ビルド、依存整合性、差分チェック |

`make smoke` は `http://localhost:8080` に接続する。API ポートを変えた場合は `python3 scripts/smoke.py http://localhost:18080` のように接続先を指定する。

## Air

`make dev` は `compose.yaml` に `compose.dev.yaml` を重ね、API を開発イメージに切り替える。DB とマイグレーションは同じ構成を使う。ソースのマウントは読み取り専用で、ビルド結果・ログはコンテナ内の `/tmp/smapass-air` に置く。

`.air.toml` で Go ファイルを監視する。テスト、生成モック、文書は再起動の対象外。ビルドが失敗したら古い API を停止し、成功したら起動し直す。終了時は Go に SIGINT を送り、接続を閉じる時間を確保する。

`go.mod` / `go.sum`、SQL、Dockerfile の変更後は `make dev` を再実行する。SQL を監視して勝手に DB を更新する仕組みは置かない。通常の構成へ戻すときは `make up`。

## 自動テスト DB

テスト開始時に Testcontainers が専用の PostgreSQL コンテナを起動する。テストごとに別の DB と自動割当ポートを使い、マイグレーション → テストデータ投入 → テスト → 接続・コンテナ削除の順に処理する。

`.env` や外部 DSN、開発用 Compose の DB を使用しない。テストが失敗した場合も cleanup を実行する。DB を使えない場合はテストを成功扱いでスキップせず失敗する。通常テストには `integration` タグを付けないため Docker は不要。

```bash
make test-integration
# make を使わない場合
go test -count=1 -tags=integration -timeout=5m ./internal/infrastructure/...
```

通常テストは入力処理・HTTP エラー・usecase と生成モック・API 定義を確認する。DB 統合テストは SQL、投影、ハッシュ、ページング、正本・スナップショットの不変性を確認する。実端末や外部チェーンの検証は含まない。

## モックと API 定義

モックは `internal/domain/repository/certificate.go` の `go:generate` から作る。mockgen は `go.mod` の tool directive でバージョンを固定し、個別のグローバルインストールは不要。

API は `docs/openapi.base.json` に URL・説明・制約を記述し、Go の公開データ型からレスポンススキーマを生成して `docs/openapi.json` にまとめる。生成器が扱えない型を公開型に追加した場合はエラーで止め、対応を明示的に追加する。公開型の一覧、制約の参照先、URL と実装、サンプル JSON の不一致をチェックする。

変更したら `make generate` を実行し、元の定義と生成物を一緒にコミットする。CI は生成物を自動コミット・push せず、更新漏れをエラーとして通知する。DI はコンストラクタを明示的につなぐ。

## GitHub Actions

[ci.yml](../.github/workflows/ci.yml) は全ブランチの push、PR、手動実行を対象にする。Go は `go.mod` のバージョンを読み、Actions・開発ツールのバージョンは固定する。

| ジョブ | 確認すること |
| --- | --- |
| Go / generated API and mocks | 整形、生成差分、`go vet`、ビルド、race 付き通常テスト・カバレッジ、依存整合性 |
| PostgreSQL integration | Docker でテスト DB を作成し、統合テスト後に削除 |
| Compose startup and API | Compose 設定、イメージビルド、DB → migrate → API の起動、デモ応答、再マイグレーション |

CI の Compose ジョブは GitHub の使い捨てランナー内でのみデモ投入とボリューム削除を行う。デプロイや Git への書き込み、アプリ用 secret は不要。PR のマージ前に CI 成功を必須にする設定は GitHub リポジトリの ruleset / branch protection で別途設定する。

ローカルのチェック成功と、GitHub 上で実際に CI が実行されたことは区別する。ワークフローを push した後、GitHub の Actions タブで結果を確認する。
