# 添付画面と API データの対応

添付された証明書詳細・記録一覧・出品用素材の画像を、必要情報の参照として扱う。レイアウトや評価基準まで固定するものではない。参照 API の値は発行時の公開スナップショットとして用意する。

| 画面の要素 | API の場所 | 補足 |
| --- | --- | --- |
| Google / Pixel 8 | `snapshot.device.manufacturer`, `model` | カード / OGP でも共用 |
| A：良好、説明文 | `snapshot.grade` | Go の算出結果を返す設計。雛形ではデモ値のみ |
| 記録期間・189日・28回・発行日 | `snapshot.recording`, `issued_at` | 日数の表示は Web で日時から計算。現在時刻から再計算しない |
| 一致・改ざんなし、詳しく見る | `/records` と外部チェーン | Go から成功バッジの文言を返さない。デモは未アンカーで実照合不可 |
| 出品先 / 商品 ID / IMEI 末尾 | `listing` | 出品者申告。署名・チェーンの保証対象外 |
| メーカー / モデル / 端末名 / SoC / CPU | `snapshot.device` | 端末名は製品コード等の公開可能な情報だけ。個人が付けた名前を収集・公開しない |
| RAM / ストレージ | `ram_bytes`, `storage_bytes` | 表示単位と丸めは Web で統一 |
| 画面サイズ / dpi / Android / SDK | `snapshot.device` | spec 取得日と OS の最新観測日は同一とは限らない |
| 最新の推定最大電荷 | `snapshot.battery.latest_full_charge_mah` | 取得不能なら null |
| 初期からの変化率・初期容量 | `change_percent`, `initial_full_charge_mah` | 符号付き %。推定値であることを説明する |
| 充放電サイクル | `snapshot.battery.cycle_count` | 未取得を0回にしない |
| 電荷と温度のグラフ・数値表 | `snapshot.battery.trend` | 同じデータからグラフと数値表を作る |
| OS の更新履歴 | `snapshot.os_history` | 変更を観測した時点 |
| 記録は途切れず続いている | `recording.gaps`, `gap_threshold_seconds` | しきい値未設定時は連続性を断言しない |
| 記録一覧の全列 | `snapshot.status_history` | 取得日時、OS / SDK、温度、サイクル、電荷、起動回数、異常終了、熱的余裕 |
| ブックマーク | Web の localStorage | バックエンドへの保存は不要 |
| 出品用カード画像の機種・評価・発行日 | 詳細 API の同じ値 | PNG / QR / OGP の描画は Next.js |
| カードの URL / 証明書番号 | `number` と Web の正規ドメイン | URL は Web が `/c/{number}` として組み立てる |
| 説明文用テキスト / URL コピー | Web | Go の専用生成 API は設けない |
| 整備履歴 | `snapshot.maintenance_records` | 項目の場所のみ。登録・評価連携は今後 |

画面の「写真」をストレージにアップロードする機能は、この雛形の範囲には含めていない。添付は画面仕様の参考であり、カード PNG は Web が都度生成する。将来、端末の実写真を保存する場合も S3 は導入せず、PostgreSQL の `BYTEA` と MIME・サイズ・撮影時刻・公開範囲・関連記録を設計する。容量制限と閲覧認可を決めてから実装する。
