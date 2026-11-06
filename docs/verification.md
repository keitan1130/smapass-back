# 署名と改ざん検証の設計

この雛形は、署名対象データを失わず保存し、参照 API で返すための土台を作る。計測ログのRFC 8785 JCS形式・ECDSA署名の検証と保存は実装済み。チェーン送信・RPC照合は未実装。鍵登録時の Android attestation と秘密鍵保有の署名検証は実装済みで、条件は [Android 鍵登録](android-key-registration.md) を参照。

## 参照資料の違い

添付 SQL は Tapyrus の Merkle root 方式、Web の [features/verification.md](../../smapass-web/docs/features/verification.md) は Ethereum Sepolia の直接照合を提案している。どちらかを勝手に確定せず、DB にチェーン識別子と Merkle 情報を用意した。チェーンを抽象化した API が完成したという意味ではない。

## 正本の扱い

`payload` JSONB はキー順や数値表現を保持しないため署名の正本にしない。`canonical_payload` に公開可能な署名対象 JSON の UTF-8 バイト列を文字列として保存し、API でもその文字列を返す。ハッシュは SHA-256、DB の `payload_hash` と一致するかを検査する。データの正規化・署名の妥当性は別に確認する必要がある。

計測ログの署名対象は [計測ログ送信](measurement-ingestion.md) のバージョン1 envelope。端末UUID、鍵ID、端末全体の連番、種別、取得時刻、前ハッシュ、公開計測値を含める。送信されたJCS正本のUTF-8バイト列をそのまま検証・保存し、DBカラムと投影はその検証済みenvelopeから作る。
取り込みでは形式検証後、次の保存処理を同じトランザクションで行う：

1. 認証と端末への権限、鍵の有効期間・署名方式・必要な attestation を確認。
2. サイズ上限、JSON 重複キー、型・単位・日時・公開可能項目を検査し、RFC 8785 JCSと型の往復比較で正規化済み形式と許可フィールドを確認。
3. SHA-256 と署名、envelope と外側の値の一致を確認。
4. 端末単位で直列化し、次の連番と前ハッシュを確認。重複・再送には冪等な結果を返し、矛盾する同一連番を拒否。
5. 正本と投影を保存。公開表示は保存後の最新観測値を反映する。

JSON のキーを独自にソートするだけで RFC 8785 対応としない。浮動小数点、大きな整数、Unicode、改行、負のゼロを含む共通テストベクタを Kotlin / Go / ブラウザで共有する。[共通署名ベクタ](examples/measurement-vector.json) とGoの回帰テストを用意した。Android・ブラウザ実装での実行は未確認。Web の文書の例示ハッシュは採用前に再計算して検証する。

## アンカー方式で合意すること

| 未決事項 | 必要な取り決め |
| --- | --- |
| ネットワーク | Tapyrus / Sepolia、network ID / chain ID、最終性の基準 |
| 記録単位 | ログごと / 証明書ごとの Merkle root / バッチ |
| leaf / node | ハッシュの対象・並び順・domain separation、奇数 leaf の処理、proof の左右 |
| Ethereum を使う場合 | contract address、ABI、event と root / leaf の対応、ブロック番号 |
| 再送と障害 | idempotency key、送信済み tx の再確認、retry / backoff、reorg、失敗と pending の区別 |
| 計測ログ署名 | バージョン1は端末UUID入りのJCS envelope・P-256 SHA256withECDSA・標準Base64のASN.1 DER。鍵登録・交代・初期化後再登録は [Android 鍵登録](android-key-registration.md) を参照 |
| 公開範囲 | 公開ログから除外する情報と、公開可能な署名 envelope |

`anchors.confirmed` はサーバーが保存した状態であり、ブラウザ検証の結果ではない。ブラウザは canonical_payload の SHA-256 を計算し、別経路の RPC で取得したチェーン情報と照合する。「一致」「不一致」「照合できず」を区別する。チェーンにハッシュがあっても計測値自体の真実性は保証しない。

## 個人情報

公開署名対象は `schema_version: 1` の端末UUID・鍵ID・連番・日時・前ハッシュ・計測値とする。IMEI全桁・所有者情報・認証情報は含めない。取り込み時の厳密な許可リストで拒否する。`attestation` / `os_proof` は非公開の別カラムに置き、`/records` へ流さない。検証APIは保存した `canonical_payload` をそのまま返し、公開時にフィールドを削ったり再エンコードしたりしない。

出品情報と IMEI 末尾は照合の補助。本人確認と認可による更新制限は今後実装するものであり、チェーンが出品者の申告を保証するものではない。

## デモの位置付け

開発 seed の28件はキー順を揃えた架空 JSON の SHA-256 と前ハッシュを持つが、方式名は `demo-json-sort-v1`。署名・公開鍵は明示的な無効文字列。アンカーはない。実装済みの DB 統合テストは保存されたハッシュと連結を確認するだけで、デモ自体はJCS・実署名・外部チェーンの検証対象ではない。別途、計測ログ取り込みのテストでは実際のECDSA署名・改変・再送・並行送信・所有者交代・ロールバックを検査する。

## 登録期間とログの受信時刻

`device_logs.ownership_id` は鍵と同じ登録期間に結び付く。旧登録者の鍵による追加ログを DB が拒否する。実装済みのログ受信は署名検証に成功したときだけ `signature_verified_at` を指定し、DB がサーバー受信時刻で3日ルールの活動時刻を更新する。このマーカー自体が署名を検証するわけではない。未検証のデモログは活動時刻を更新しない。
