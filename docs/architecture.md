# アーキテクチャ

## 採用バージョン

2026-11-01 時点の最新安定版を公式リリースと Docker Official Images で照合した。RC / beta / tip は採用しない。`latest` タグへの依存を避け、動作確認したバージョンを明示する。

| 項目 | 採用 | 参照 |
| --- | --- | --- |
| Go | 1.27.3 | [公式ダウンロード](https://go.dev/dl/?mode=json) |
| Echo | 5.4.4 | [公式リリース](https://github.com/labstack/echo/releases/tag/v5.4.4) |
| Bun / pgdialect / pgdriver | 1.3.0 | [公式リリース](https://github.com/uptrace/bun/releases/tag/v1.3.0) |
| golang-migrate | 4.20.1 | [公式リリース](https://github.com/golang-migrate/migrate/releases/tag/v4.20.1) |
| PostgreSQL | 18.6 | [公式リリース](https://www.postgresql.org/docs/release/18.6/) |
| Alpine Linux | 3.24.3 | [公式リリース一覧](https://alpinelinux.org/releases/) |
| golang-jwt | 5.3.1 | [公式リリース](https://github.com/golang-jwt/jwt/releases/tag/v5.3.1) |
| OpenAPI | 3.2.1 | [公式仕様](https://spec.openapis.org/oas/v3.2.1.html) |
| Air | 1.67.4 | [公式リリース](https://github.com/air-verse/air/releases/tag/v1.67.4) |
| Testcontainers / postgres module | 0.44.0 | [公式リリース](https://github.com/testcontainers/testcontainers-go/releases/tag/v0.44.0) |
| GoMock / mockgen | 0.6.0 | [公式リリース](https://github.com/uber-go/mock/releases/tag/v0.6.0) |

Docker タグは `golang:1.27.3-alpine3.24`、`postgres:18.6-alpine3.24`、`alpine:3.24.3`。Go の直接・間接依存は互換範囲の最新版に更新して `go.mod` / `go.sum` に固定した。更新方法は [operations.md](operations.md)。

## ディレクトリと責務

```text
cmd/
  main.go                       API 起動・終了
  migrate/main.go               up / down-one / version
internal/
  apidoc/                       公開型からの API スキーマ生成・整合テスト
  di/                           コンストラクタの組み立て
  domain/
    entity/                     公開データ、証明書番号のルール
    repository/                 永続化のインターフェース
  usecase/                      証明書参照、DB readiness
  interface/
    controller/                 HTTP 入出力
    schema/                     HTTP レスポンスの包み・エラー
  infrastructure/
    config/                     環境変数
    router/                     Echo、共通ミドルウェア
    database/
      dto/                      Bun の DB 表現
      certificate/              PostgreSQL の参照実装
pkg/db/                         Bun 接続・接続プール
db/migrations/                  埋め込み SQL と up / down
db/seeds/                       手動投入する開発データ
docker/                         マルチステージ Dockerfile
tools/openapi/                  OpenAPI 生成・更新漏れ確認コマンド
scripts/                        モック生成物の整合チェック
.github/workflows/              GitHub Actions CI
docs/                           API・設計・開発・運用
```

HTTP の流れは `router → controller → usecase → domain の repository interface → infrastructure の repository → PostgreSQL`。`internal/di` が接続する。domain / usecase は HTTP と DB ライブラリに依存しない。

Echo 5 を使用する。DI はコンストラクタの明示呼び出しとする。Wire は公式にアーカイブされているため新規導入しない（[公式の状態](https://github.com/google/wire)）。repository のモックは GoMock / mockgen で生成する。S3 と LocalStack は使用しない。

開発時は `compose.dev.yaml` で `app` を Air に切り替える。CI は通常の品質チェック・race 検出・自動 DB 統合・Compose の API 起動を分ける。テスト DB は開発 DB から分離し、Testcontainers が自動で作成・初期化・削除する。[開発・CI ガイド](development.md) を参照。

## データの扱い

- `device_logs` が署名・ハッシュの正本。`canonical_payload` に署名対象の UTF-8 を保存する。JSONB は検索用。
- `device_specs` / `device_status` はログから作る表示用の投影。
- `certificates.snapshot` は公開を許可した端末情報・評価・履歴の発行時点のコピー。型付きで API に変換する。
- `certificate_logs` が証明書に含めるログを固定する。発行後に届く記録を自動で追加しない。
- `certificate_listings` は出品後にも変更できる情報。署名・評価スナップショットの外に置く。
- `anchors` / `log_anchors` は外部チェーンの参照。DB に行があるだけでは外部チェーンでの確認にならない。

## 各クライアントの境界

| 担当 | 役割 |
| --- | --- |
| mobile | 計測、端末鍵、本人確認を伴う書き込み |
| back | 検証・保存、評価算出、証明書発行、参照 API、アンカー送信 |
| web | 証明書と履歴の表示、独立したチェーン照合、カード画像 / QR / OGP、ブラウザ内ブックマーク |
| admin / 事業者 | 権限を持つ事業者の点検・整備記録入力 |

実装済みは起動基盤・DB・証明書参照・IMEI による端末登録と検索・15日ルールの登録者変更・現在の登録者による手動失効・ログインなしの端末認証情報の発行・更新・失効と書き込み権限の確認、Android 鍵の attestation と秘密鍵保有の確認。実ログの取り込みと署名検証、評価、証明書発行、外部チェーンは未実装。詳細は [device-registration.md](device-registration.md)。

## 次の実装順序

1. mobile / web と公開ログの項目、JCS テストベクタ、計測ログ署名の形式を合意する。Android のアプリ識別設定を用意し、実装済みのチャレンジ・鍵登録・鍵更新を実機で接続確認する。
2. ログ取り込みに既存のトークン認証とトランザクション内の端末権限確認を組み込み、ログを検証して正本と投影を1トランザクションで保存する。登録者の期間と鍵を確認し、検証成功時だけ受信時刻を更新する。
3. 欠測・記録の間隔・温度・容量・異常終了を評価する基準とバージョンを決める。取得不能な値で良好判定しない。
4. 発行・再発行・出品先更新を実装する。発行対象ログ、評価、スナップショットは同じトランザクションで確定する。
5. チェーン方式・送信契約を決め、再送可能なアンカーワーカーとブラウザ検証用の情報を実装する。
6. 事業者認証・整備記録を追加する。必要があれば写真の保存と公開範囲も定義する。

## 本番に進む際の差分

現在の Compose は開発用で、DB 初期ユーザーを API と migration で共有する。本番では読み取り・取り込み・migration の DB 権限を分離し、TLS / secrets / 複数インスタンス共通のレート制限 / バックアップと復元確認を整える。CORS は認証や認可の代わりにはならない。
