# harness の仕組み

`harness/` は、テンプレートを、**本番(launchs 本体)と同じコードで**検査・展開するための道具。ここでは、何を本体から借りていて、何を置き換えているかを説明する。

## なぜ必要か

テンプレートは、本体の中で、次の順に処理される。

```
manifest.yaml と版のフォルダ → 検査 → 変数を埋める → 計画 → (DB に記録)→ k8s の資源
```

本体を動かさずに、同じ結果を、手元で得たい。そのため、本体のコードを、**そのまま import して**使う。

## 構成

```
launchs-v2-template/
  templates/<id>/manifest.yaml, README.md, <版>/…   テンプレート(本体に取り込まれるのは、ここ。docs/structure.md)
  scaffold/                                 新しいテンプレートの雛形(task new でコピー)
  embed.go                                  templates/ を埋め込んで公開する(本体が import する)
  harness/                                  この道具(別の Go モジュール)
    launchs/                                本体(git submodule。develop を指す。replace で使う)
    main.go      コマンド(list/validate/plan/render/test)
    source.go    テンプレートの読み込み(本体の template.Load)
    runner.go    計画 → k8s への展開(本体の operator を呼ぶ)
    cluster.go   クラスターへの接続、待機、HTTP の確認、マニフェストの書き出し
    smoke.go     smoke.yaml の読み込み
```

## 本体から借りているもの

| 本体のコード | harness での使い方 |
| --- | --- |
| `shared/template`(`Load`、`ResolveValues`、`BuildPlan`) | 検査、変数の解決、計画の作成。**本体と同じ検査**が走る |
| `shared/templateplan` | 計画の型、`${service.X.dns}` の目印の置き換え |
| `shared/model` | ConfigMap の版の名前、マウントの mode など |
| `worker/src/activity` の定義の型 | `DeploymentDefinition`、`ServiceDefinition` など。operator への入力 |
| `worker/src/cluster` の operator | Deployment、Service、PVC、ConfigMap、IngressRoute、namespace(NetworkPolicy つき)を、k8s に作る |

## harness が置き換えているもの

本体では、計画は DB に記録され、Temporal の Workflow が、順に実行する。harness は、**記録と Workflow の部分だけ**を、直接の関数呼び出しにしている(`runner.go`)。

| 本体 | harness |
| --- | --- |
| 計画を DB に保存、Workflow が各ステップを実行 | 計画のステップを、順に、直接 operator に渡す |
| service の DNS 名を DB から読む | service を作った時点で、同じ形(`<service の ID>.<namespace>.svc.cluster.local`)の名前を決める |
| deploy の定義を、DB の行から作る(`newDeploymentDefinition`) | 計画から、同じ規則で作る(ファイルは `f0`、`f1`… の key、パスの昇順) |
| spec(small/medium/large)を `deploy_specs` から読む | 同じ値を、`runner.go` の `specSizes` に持つ |
| クォータの確認、ホスト名の重複の確認 | しない |

**本体の変更に追従する必要がある箇所**は、`runner.go` の次の 2 つ。本体で、これらの規則が変わったら、harness も直す。

- `specSizes`(本体の `migrations/app/000003_create_deploys.up.sql`)。
- `applyDeploy` の、定義の組み立て(本体の `worker/src/activity/deployment_operator.go` の `newDeploymentDefinition`)。

## 各コマンドの内部

- **validate**: `loadRegistry`(各フォルダの中身を、メモリ上のファイルシステムに写して、本体の `template.Load` に渡す。版の読み込みも、ここ)→ 全テンプレートで `buildPlan`。`manifest.yaml` を持たないフォルダは、無視する。`--version x.y.z` か `--version all` で、版を選べる(既定は、最新の版)。
- **plan**: `buildPlan` の結果を JSON で出す。
- **render**: `client-go` の**偽のクライアント**(`fake.Clientset`、`dynamicfake`)に、`runner.apply` で作らせ、できた資源を YAML にする。
- **test**: 本物のクラスターに、`runner.apply` で作る → Deployment ごとに、本体と同じ `DeploymentReady` で待つ → Pod の再起動を見守る → API サーバーの proxy 経由で HTTP → namespace を消す。

## クラスター(docker の中の k3s)

`task cluster:up` は、`rancher/k3s` のコンテナを `--privileged` で起動する。

- API は、ホストの `16443`。kubeconfig は `.kube/config`(`.gitignore` 済み)。
- Traefik(k3s 同梱)の `80` は、ホストの `18080`。
- `harness` を動かす Go のコンテナは、`--network host` で、ホストの `127.0.0.1:16443` につなぐ。
- ストレージは、k3s 同梱の local-path(RWO)。

## 本番との違い

| | 本番 | この環境 |
| --- | --- | --- |
| クラスター | Talos(複数ノード) | docker の中の k3s(1 ノード) |
| ストレージ | Longhorn(RWX) | local-path(RWO)。ボリュームの作成は、すぐ終わる |
| 隔離 | NetworkPolicy(+ ランタイムによる隔離は別途) | NetworkPolicy は同じコードで作る。k3s は、NetworkPolicy を実施する |
| 外からの入口 | Traefik + Cloudflare | k3s の Traefik。HTTP の確認は、API の proxy 経由 |
| DB、Temporal | あり | なし(計画の保存と Workflow を、直接の呼び出しにした) |
| クォータ | あり | なし |
| イメージの取得 | 社内のネットワーク | インターネットから(初回は、大きなイメージで時間がかかる) |

## 本体の版を上げるとき

```
git submodule update --remote harness/launchs   # 本体の develop の最新へ
task validate && task test              # 全テンプレートが、まだ通るか
git add harness/launchs && git commit
```

本体の engine の仕様(新しい項目、検査の厳しさ)が変わると、既存のテンプレートが通らなくなることがある。それを、ここで先に知る。

## 新しいコマンドや確認を足すには

- コマンド: `main.go` の `run` の `switch` に足す。
- 展開の確認(例: ログの内容、特定のポートの TCP 接続): `cluster.go` に関数を足し、`main.go` の `testOne` から呼ぶ。
- 関数と、関数の中の行には、日本語のコメントを書く(本体の `docs/coding-conventions.md` に合わせる)。

## 注意: 本体の版(submodule)

`harness/launchs/` は、版つきのテンプレートに対応した本体のコミット(本体の `feature/templates-submodule`)を指している。**本体の PR が `develop` に入ったら**、`git -C harness/launchs fetch && git -C harness/launchs checkout origin/develop` で、`develop` に付け替えて、コミットする(それまで、`git submodule update --remote` は、使わない。古いエンジンに戻ってしまう)。
