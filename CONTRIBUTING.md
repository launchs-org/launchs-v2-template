# テンプレートを足す・直す

検証の詳しい方法は [docs/verification.md](docs/verification.md)、うまくいかないときは [docs/troubleshooting.md](docs/troubleshooting.md)。

## 流れ

1. このリポジトリを**フォーク**して、`feature/<テンプレートの id>` のブランチを切る。
2. `task setup`。
3. `task new -- <id>` で、雛形(`scaffold/`)から `templates/<id>/` を作り、`manifest.yaml`、`README.md`、`1.0.0/` の中のファイルを書く(構成と書き方は [docs/structure.md](docs/structure.md)。既存の `templates/*` も参考に)。
4. `task validate -- <id>` で検査し、`task render -- <id>` で、作られる資源を目で確かめる。
5. `task cluster:up` のあと、`task test -- <id>` が `PASS` になるまで直す。
6. プルリクエストを出す。説明に、`task test` の結果(PASS の行)を貼る。

## 書くときの決まり

- フォルダ名と、`manifest.yaml` の `id` は、同じにする。
- 互換性のない変更は、**版を上げる**(`task new:version`)。古い版は、消さない。
- 公開は **HTTP だけ**(TCP や SSH は、インターネットに出せない)。
- イメージは、**版を固定**する(`latest` は、避ける。公式が版を出さないときだけ、理由をコメントに書く)。
- 初回に、利用者がすること(管理者の作成など)は、`config.yaml` の `notes` に書く。パスワードの変数は、伏せて表示される。
- 非 root のイメージは、`fsGroup` を指定する。
- 初期設定の画面が、最初に開いた人に管理者を作らせるものは、`notes` に、すぐ済ませる旨を書く。
- 大きなボリュームを、複数、同時に作ると、遅いクラスターで時間がかかる(Longhorn の RWX)。必要な数だけにする。
- コメントは日本語。

## 確認項目(PR のチェックリストにも、同じものがある)

- [ ] `task validate` が通る
- [ ] `task test -- <id>` が PASS
- [ ] 版を固定した
- [ ] `notes` を書いた(必要なら)
- [ ] 本体の `docs/templates.md` の一覧に載せる必要があるなら、別 PR を出す約束をした
