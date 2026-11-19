# launchs-v2-template

launchs のテンプレート(よく使う構成を、変数を埋めるだけで、まとめて作る)と、それを**手元で試験する環境**。

- `templates/`: テンプレート。1 つにつき 1 つのフォルダ(`manifest.yaml`、`README.md`、版のフォルダ `1.0.0/`(`config.yaml`、`deploy.yaml` など種類ごとのファイルと `files/`))。構成の詳細は [docs/structure.md](docs/structure.md)。
- `harness/`: 本体(launchs)と**同じエンジン**で検査し、**同じ k8s の展開のコード**で、クラスターに展開して試験する道具。
- `scaffold/`: 新しいテンプレートの雛形(`task new -- <id>` で、`templates/<id>/` にコピーされる)。
- `harness/launchs/`: 本体の submodule。harness が、本体のコードを、そのまま使うために置く。

詳しいドキュメント:

- [フォルダの構成と書き方](docs/structure.md): manifest.yaml、版、種類ごとのファイル。
- [検証方法](docs/verification.md): 何を、どの順で、どこまで確かめるか。PR の前の確認。
- [harness の仕組み](docs/harness.md): 本体のコードのどこを使い、何を置き換えているか。本番との違い。
- [うまくいかないとき](docs/troubleshooting.md)
- [テンプレートを足す・直す](CONTRIBUTING.md)

仕様は、本体の `docs/templates.md`(設計)と `docs/endpoints/templates.md`(書き方)。

## 使うもの

docker、kubectl、[go-task](https://taskfile.dev)、git。クラスターも、docker の中の k3s で動くので、k3d などは要らない。Go は、手元に入れなくてよい(コンテナの中で動く)。

## はじめに

```
git clone https://github.com/launchs-org/launchs-v2-template
cd launchs-v2-template
task setup
```

## 日々のコマンド

| コマンド | 内容 |
| --- | --- |
| `task new -- <id>` | 雛形(`scaffold/`)から、`templates/<id>/` を作る |
| `task validate [-- <id>]` | 本番と同じエンジンで、書式・変数・参照・計画を検査する(数秒。クラスター不要) |
| `task plan -- <id> [--set 名前=値]` | 変数を埋めて、作られる資源の計画を出す |
| `task render -- <id> [--set 名前=値]` | 本番と同じ展開コードで作られる、k8s のマニフェスト(YAML)を出す(クラスター不要) |
| `task cluster:up` / `task cluster:down` | 試験用のクラスター(docker の中の k3s。Traefik 入り)を作る/消す |
| `task test [-- <id>]` | クラスターに展開して、全ての Deployment が Ready になり、再起動せず、HTTP が返ることを確かめ、片付ける |
| `task test:keep -- <id>` | 同じことをして、namespace を残す(中を調べたいとき) |

## 試験が確かめること

1. **検査**(`validate`): 本体と同じ `template.Load` / `Validate` / `BuildPlan`。
2. **展開**(`test`): 本体の worker と同じ operator(`worker/src/cluster`)で、volume(PVC)、ConfigMap、service、deploy、ingress(IngressRoute)を、計画の順に作る。namespace も、本番と同じ形(`project-<UUID>`、NetworkPolicy つき)。
3. **起動**: 全ての Deployment が Ready になる。その後 15 秒、Pod が再起動(クラッシュの繰り返し)しない。
4. **応答**: ingress の向き先に、API サーバーの proxy 経由で HTTP を送り、500 未満の応答(認証の 401 や、移動の 302 も、動いている印)が返る。

テンプレートごとの調整は、`templates/<id>/smoke.yaml`(無くてよい):

```yaml
path: /login          # 確かめるパス(既定は ingress のルールのパス)
statuses: [200, 302]  # 成功とみなすステータス(既定は 500 未満)
timeoutSeconds: 300   # 起動を待つ秒数(既定 300)
```

## 本番との違い

- 本番は Talos + Longhorn、試験は k3s(docker の中)+ local-path(RWO)。ストレージの速さや、RWX の挙動は、試験では分からない。
- DB や Temporal を使わない。変数の検査、計画、k8s への展開は、本体のコードをそのまま使う。
- クォータ(アカウントごとの上限)の確認はしない。

## 本体を更新するとき

`harness/launchs/` は、本体の `develop` を指す。本体のエンジンが変わったら、`git submodule update --remote harness/launchs` で、新しくする。
