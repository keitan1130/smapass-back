# Web表示とAPI

IMEI検索でUUIDに解決し、証明書ページはUUIDで最新情報と全期間の履歴を表示する。Web/QRの入口は `/certificates/{id}`、データはGoの端末APIから取得する。

| 表示 | 取得元 |
| --- | --- |
| メーカー・機種・SoC・RAM・ストレージ・画面 | `/api/devices/{id}` の `spec` |
| 最新OS・電池・起動回数・熱的余裕 | `status` |
| 登録状態・最初/最後の計測日時・全ログ件数 | `registration_status`・各日時・`record_count` |
| 電池推移・OS変更の観測・記録一覧 | `history.items`。必要なページを順に取得 |
| デモ表示 | `is_demo` |
| ブックマーク一覧・比較 | `/api/devices?ids=a,b,c` の最新概要 |
| 元データ・署名・ハッシュ・チェーン情報 | `/api/devices/{id}/records`。表示後に独立検証 |
| QR・カード・OGP・コピー用URL | Webで生成。機種と最終計測日時を使用し発行日は表示しない |

評価、欠測判定、整備記録、出品先の更新は未実装。旧スナップショットのデモA評価は返さない。未知の値はnullのまま扱う。外部RPC照合も未実装なので、取得成功だけで「改ざんなし」と表示しない。ブックマークとメモはWebのブラウザ保存。

IMEIは `/api/devices/search?imeis=a,b,c` でUUIDに解決する。通常の表示データとページURLはUUIDを使う。署名対象の元ログにはIMEIが残る。
