# smapass-back 開発ガイド

## 担当範囲

- このリポジトリの Go API、PostgreSQL、Docker Compose、マイグレーション、関連文書を担当する。
- `../smapass-web/docs/` は要求・画面の参照資料として読む。Web / mobile / adminを依頼なく変更しない。
- S3、LocalStack は使用しない。
- 依頼外の変更を加えず、既存のユーザー変更を保持する。レビュー依頼では報告を先にし、修正の指示がなければ編集しない。

## 実装の原則

- [README](README.md) とタスクに関係する `docs/` を読み、実際のコード・SQL・設定と照合する。
- `cmd` は起動、`internal/di` は依存の組み立て、`internal/infrastructure/router` はルーティングと共通 HTTP 処理を担当する。
- controller は HTTP 入出力、usecase はユースケース、domain はエンティティと repository interface、infrastructure は Bun / 外部接続を担当する。domain / usecase に Echo や Bun を持ち込まない。
- 公開 API は `internal/domain/entity` の公開型と `internal/interface/schema` で定義する。URL・説明・制約は `docs/openapi.base.json` を更新し、`make generate` で `docs/openapi.json` とモックを再生成する。生成物を手で編集しない。`docs/api.md` と必要な example も合わせる。
- context を DB / 外部通信まで渡す。DB 接続は DI から注入し、パッケージグローバルに置かない。
- 保存先は PostgreSQL。S3 互換 API、AWS SDK、ローカルファイルへの永続保存を追加しない。
- 不明な計測値は `null` とし、0 / false で補完しない。単位はフィールド名で明示する。
- ログの署名対象バイト列、payload、ハッシュ、前ハッシュ、署名を保存し、元ログを UPDATE / DELETE しない。検証データを表示用 JSONB の再エンコードから作らない。
- 発行済みスナップショットの記録と評価を再計算で上書きしない。評価ロジックにバージョンを付ける。
- 未検証データやデモを「検証済み」「改ざんなし」と返さない。Tapyrus / Sepolia、JCS、署名方式の未決事項は [verification.md](docs/verification.md) を確認する。
- 公開応答に IMEI 全桁、認証情報、秘密鍵、attestation、OS 証明の非公開情報を含めない。書き込み API には本人確認と対象端末・事業者への認可を実装してから公開する。
- `.env` はコミットしない。DSN、ペイロード、証明書番号をアクセスログに残さない。

## DB と検証

- 適用済みマイグレーションは変更せず、連番の up / down を追加する。開発データは `db/seeds/` に分け、自動投入しない。
- `gofmt -w cmd internal pkg db/migrations`、`go test ./...`、`go vet ./...`、`git diff --check` を実行する。
- 共通チェックは `make check`。公開型や repository interface の変更時は `make generate` と `make generate-check`、並行処理の変更時は `make test-race` も実行する。
- DB 統合テストは `make test-integration` で専用コンテナを自動作成・初期化・削除する。開発用や外部の DSN へ切り替える仕組みを追加しない。テスト DB を通常の Compose ボリュームと共有しない。
- API / DB 変更では必要な範囲の回帰テストと Compose の起動・API 確認を行う。DB 統合テストは [setup.md](docs/setup.md) の自動テスト DB を使用する。
- テスト結果は静的確認、Go テスト、Docker / DB、実端末、チェーンを分けて報告する。未確認の層を動作済みとしない。
- ユーザーのデータがある DB の削除・ボリューム破棄・破壊的な down マイグレーション・本番反映・Git commit / push は明示的な依頼がある場合に限る。
