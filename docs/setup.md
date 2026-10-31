# 環境構築

## 必要なもの

- Docker Engine または Docker Desktop と Docker Compose v2 以降。Docker デーモンへのアクセス権が必要。
- `curl`。Makefile を使う場合は `make`。
- ホストで Go のテストや開発を行う場合だけ Go 1.27.3。Docker ビルドも同じバージョンを指定。

Go・PostgreSQL は Compose 内で動く。S3、AWS の認証情報、Node.js、チェーンのウォレットや RPC キーはこの段階では不要。

## 初回起動

このリポジトリ内で実行する。

```bash
cp .env.example .env
docker compose config --quiet
docker compose up -d --build --wait app
docker compose ps -a
curl -f http://localhost:8080/health
curl -f http://localhost:8080/ready
```

起動順は `db` のヘルスチェック成功 → `migrate up` 正常終了 → `app`。`migrate` が `Exited (0)` になるのは正常。API の起動は DB 接続を確認し、`/ready` でも DB に問い合わせる。`/health` はプロセスの生存だけを表す。

初回は空の DB。デモを使う場合だけ次を実行する。

```bash
docker compose exec -T db sh -c 'psql -v ON_ERROR_STOP=1 -U "$POSTGRES_USER" -d "$POSTGRES_DB"' < db/seeds/development.sql
curl -f http://localhost:8080/api/certificates/01ABC-DEFGH
curl -f 'http://localhost:8080/api/certificates?ids=01ABCDEFGH'
curl -f 'http://localhost:8080/api/certificates/01ABCDEFGH/records?limit=2&offset=0'
```

デモ投入は繰り返し可能で、既存デモ行は上書きしない。28件の架空の status 記録を持つ。JCS、署名、チェーン照合のテストデータではない。

## 設定

| 変数 | 既定の例 | 意味 |
| --- | --- | --- |
| `POSTGRES_USER` | `smapass` | DB 初期ユーザー |
| `POSTGRES_PASSWORD` | `smapass_local` | ローカル用パスワード |
| `POSTGRES_DB` | `smapass` | DB 名 |
| `DB_DSN` | `postgres://smapass:smapass_local@db:5432/smapass?sslmode=disable` | API と migrate の接続先 |
| `API_PORT` | `8080` | ホストから API に接続するポート |
| `DB_PORT` | `5433` | ホストから DB に接続するポート |
| `CORS_ORIGINS` | `http://localhost:3000` | ブラウザの許可 Origin。カンマ区切り。末尾 `/` やパスを付けない |
| `HTTP_ADDR` | `:8080` | Go 単体起動時の待受先。Compose 内は `:8080` 固定 |

API / DB のホストへの公開は `127.0.0.1` のみ。Compose 内の通信は `app` → `db:5432`。`DB_PORT` の変更はコンテナ間の DSN に影響しない。ユーザー・パスワード・DB 名を変更したら `DB_DSN` も合わせる。パスワードに記号を使う場合は DSN 内では URL エンコードする。

`POSTGRES_*` は初回の DB ボリューム作成時に使われる。既存ボリュームがある状態で `.env` を変えても DB 内のユーザーやパスワードは変わらない。

`.env` は Compose が読み込む。Go バイナリは `.env` を自動で読まない。

## 日常の操作

```bash
# Go / SQL / Dockerfile の変更を反映する
docker compose up -d --build --wait app

# ログ
docker compose logs -f app migrate db

# 停止。DB ボリュームは残る
docker compose down

# 明示的なマイグレーション
docker compose run --rm --build migrate up
docker compose run --rm migrate version
```

コードとマイグレーションはイメージに含める。ソースの自動再読み込みは行わないため、編集後は `--build` が必要。マイグレーション失敗時の調査は [database.md](database.md) を参照。

## ホストで Go を動かす

Compose で DB とマイグレーションだけを起動し、コンテナの API は停止する。

```bash
docker compose up -d db
docker compose run --rm --build migrate up
docker compose stop app
export DB_DSN='postgres://smapass:smapass_local@localhost:5433/smapass?sslmode=disable'
export CORS_ORIGINS='http://localhost:3000'
go run ./cmd
```

上の値は `.env.example` の設定の場合。手元の `.env` を変更した場合は合わせる。

## テスト

```bash
gofmt -w cmd internal pkg db/migrations
go test ./...
go vet ./...
git diff --check
```

DB 統合テストは通常テストから分けている。マイグレーションとデモ投入済みの専用開発 DB に対して実行する。

```bash
export TEST_DB_DSN='postgres://smapass:smapass_local@localhost:5433/smapass?sslmode=disable'
go test -tags=integration ./internal/infrastructure/database/certificate
```

統合テストは読み取りのみで、DB の作成・初期化は行わない。デモの JSONB 変換、出品先、ページング、28件のハッシュと前ハッシュを確認する。実署名やチェーンの正しさは確認しない。

## よくある問題

| 症状 | 確認すること |
| --- | --- |
| Docker socket の permission denied | Docker Desktop / daemon の起動とユーザーの実行権限 |
| ポート競合 | `.env` の `API_PORT` / `DB_PORT` を変更。curl / ホストの DSN も合わせる |
| migrate が失敗して API が起動しない | `docker compose logs migrate db` と `migrate version`。失敗を無視して API だけ起動しない |
| DB 接続失敗 | `db` と `localhost` の使い分け、DSN と `POSTGRES_*`、既存ボリュームの初期設定 |
| 証明書が404 | デモは自動投入しない。seed の実行と番号 `01ABC-DEFGH` を確認 |
| 429 | API は直接接続元 IP ごとに秒間10リクエスト相当。少し待って再試行 |
| ブラウザだけ CORS エラー | Web の Origin が `CORS_ORIGINS` に含まれるか。Origin は scheme / host / port の完全一致 |
