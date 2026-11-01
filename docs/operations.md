# 開発環境の運用

## 状態とログ

```bash
docker compose ps -a
docker compose logs --tail=100 app migrate db
curl -f http://localhost:8080/health
curl -f http://localhost:8080/ready
docker compose run --rm migrate version
```

API は Request ID・メソッド・ステータスを記録する。DSN、アクセストークン、リフレッシュトークン、Authorization ヘッダー、秘密鍵、ペイロード、URL 内の証明書番号をログに出さない。DB の切断では readiness は503になる。サーバーを停止すると SIGTERM を受け、最大10秒で HTTP を終了して接続プールを閉じる。

## 更新

```bash
# コード・SQL の反映
docker compose up -d --build --wait app

# 同じタグで更新された DB イメージを取得
docker compose pull db

# Dockerfile のベースイメージを再取得してビルド
docker compose build --pull
docker compose up -d --wait app
```

Go / Echo / Bun / migrate / Alpine / PostgreSQL の新しい安定リリースを確認したら、明示バージョンと文書を更新する。Go モジュールの更新は `go get -u` / `go mod tidy` 後に差分と互換性を確認し、Go テスト・DB 統合・Compose 起動を通す。Go の間接依存も `go.mod` / `go.sum` に固定する。

`up -d` だけではソース変更を新しい Go バイナリにしない。イメージ更新と Git のコード更新は別。`build --pull` は Dockerfile のベースを更新し、`compose pull db` は PostgreSQL を取得する。いずれも DB メジャーバージョンの移行は行わない。

Air を使う `make dev` では Go ソースの変更を自動反映する。通常のバイナリ構成に戻す場合は `make up`。開発ツールの更新時も固定バージョンを変更し、`make check`・`make test-integration` と該当する Compose 構成を確認する。

## バックアップ

API が動いていても pg_dump は整合したバックアップを作る。出力先には十分な空き容量を用意する。

```bash
mkdir -p /tmp/smapass-backups
docker compose exec -T db sh -c 'pg_dump -U "$POSTGRES_USER" -d "$POSTGRES_DB" -Fc' > /tmp/smapass-backups/smapass.dump
```

`/tmp` は作業例であり長期保存先ではない。DB の内容、鍵履歴、署名対象データ、証明書、マイグレーション状態を含むため、アクセス制限のある恒久的な場所へ移し、定期的に復元を確認する。端末秘密鍵はそもそもサーバーで保存しない。

## 復元

既存 DB に重ねず、停止した API と、空の復元先 DB を用意して行う。以下は新しい `smapass_restore` DB に復元する例。通常の開発 DB は削除しない。

```bash
docker compose stop app
docker compose exec -T db sh -c 'createdb -U "$POSTGRES_USER" smapass_restore'
docker compose exec -T db sh -c 'pg_restore --exit-on-error --no-owner -U "$POSTGRES_USER" -d smapass_restore' < /tmp/smapass-backups/smapass.dump
```

復元先を DSN で明示して `migrate version`、レコード数・証明書表示・ハッシュと前ハッシュを確認する。必要な未適用マイグレーションを実行し、接続先を切り替える。`POSTGRES_DB` を既存ボリュームで変えるだけでは DB は作成されない。

## データを消さずに停止

`docker compose down` で DB ボリュームは残る。ボリューム削除・初期 migration の down は全データを失う操作なので通常手順に含めない。

この Compose は単一ホストの開発用。本番デプロイ、TLS、secret 管理、DB ロール分離、チェーン worker、監視、バックアップの保管先・復旧目標は別途設計する。

認証情報のテーブルもバックアップに含まれる。現在のトークンのハッシュだけでなく、再利用を検知する更新履歴と失効状態もまとめて復元する。古いバックアップへの切り戻しは認証状態も巻き戻るため、復旧時は必要に応じて `app_installations` の認証情報を失効させる。認証情報の失効と端末登録の終了は別操作で、詳細は [authentication.md](authentication.md) を参照。
