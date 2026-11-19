# テンプレートの検証方法

テンプレートを足す・直すときに、**何を、どの順で、どこまで確かめるか**をまとめる。コマンドの一覧は [README](../README.md)、仕組みは [harness.md](harness.md)、うまくいかないときは [troubleshooting.md](troubleshooting.md)。

## 全体の流れ

| 段階 | コマンド | クラスター | 時間 | 分かること |
| --- | --- | --- | --- | --- |
| 1. 検査 | `task validate -- <id>` | 不要 | 数秒 | 書式、変数、参照、deploy・volume・ConfigMap のつながり、計画が作れること |
| 2. 計画 | `task plan -- <id>` | 不要 | 数秒 | どの順で、何が作られるか(変数を埋めた値) |
| 3. マニフェスト | `task render -- <id>` | 不要 | 数秒 | k8s に渡る YAML(環境変数、マウント、fsGroup、IngressRoute の式) |
| 4. 実機の試験 | `task test -- <id>` | 必要 | 1〜3 分 | 起動する、落ち続けない、HTTP が返る |
| 5. 手で確認 | `task test:keep -- <id>` | 必要 | - | 画面、初回の設定、`notes` の手順が正しいか |

PR の前に、**1 と 4 は必ず**通す。2・3 は、書いた内容を目で確かめるときに使う。

## 1. 検査(`validate`)

```
task validate            # 全てのテンプレート
task validate -- redash  # 1 つだけ
```

本体(launchs)と**同じコード**で、次を確かめる。

- `template.yaml` の書式(未知の項目、型、必須項目)。
- 変数: 名前、型、既定値、`min`/`max`/`pattern`/`options`、`generate`(パスワード)。
- `${変数}` と `${service.<deploy>.dns}` の参照が、存在すること。deploy 同士の参照に、循環が無いこと。
- volume・ConfigMap・deploy・service・ingress のつながり(存在しないボリュームをマウントしていないか、ingress が向ける deploy に service があるか、など)。
- `metadata.notes`: 変数の参照だけを使っていること、長さの上限。
- `fsGroup`: 範囲と、deploy にだけ書けること。
- `from:` で読み込むファイルが、存在し、大きさと文字の規則に合うこと。
- 既定値で、計画が作れること(展開後の値が規則に合うか)。

`OK <id>` が出れば通過。失敗すると、どのテンプレートの、どの項目かが出る。

## 2. 計画(`plan`)

```
task plan -- redash
task plan -- redash --set storage=1024 --set db_password=test-password-123
```

`--set 名前=値` で、変数を指定できる(何度でも)。指定しない変数は、既定値になり、`generate: true` のパスワードは、ランダムに作られる。出力は、ステップ(volume → configmap → service と deploy → ingress)の JSON。

見るところ:
- 順番: **参照される側が先**(`${service.db.dns}` を使う deploy より前に、db の service)。
- `${…}` が、全て値に置き換わっていること。

## 3. マニフェスト(`render`)

```
task render -- redash > /tmp/redash.yaml
```

本体の worker と**同じ operator** で作った、k8s の資源を、YAML で出す(クラスターは使わない。偽のクライアントに作らせている)。見るところ:

- `Deployment`: `image`、`command`/`args`、`env`、`volumeMounts`、`securityContext.fsGroup`、`resources`(spec の CPU とメモリ)。
- `Service`: ポート番号と名前。
- `PersistentVolumeClaim`: 大きさ。
- `ConfigMap`: ファイルの内容(`${…}` が置き換わっているか)。
- `IngressRoute`: `Host(...) && PathPrefix(...)` の式と、向き先。

## 4. 実機の試験(`test`)

### 4.1 クラスターを作る(最初に 1 回)

```
task cluster:up
```

docker の中で、k3s(軽い Kubernetes。Traefik 入り)を動かす。k3d などは要らない。`.kube/config` に、接続の設定ができる。止めるときは `task cluster:down`(中の資源も消える)。

### 4.2 試験する

```
task test -- redash          # 1 つ
task test -- redash gitea    # 複数
task test                    # 全て(30 分ほどかかる)
```

テンプレートごとに、次を行う。

1. **展開**: 変数を既定値で埋めて計画を作り、新しい namespace(`project-<UUID>`。本番と同じ形)に、volume → ConfigMap → service・deploy → ingress の順で作る。本体の worker と同じ operator なので、NetworkPolicy による隔離も付く。
2. **Ready**: 全ての Deployment が Ready になるのを待つ(既定で最大 300 秒)。
3. **落ち着き**: Ready の後、15 秒(`--settle` で変更)見守り、Pod が**再起動したら失敗**にする。クラッシュを繰り返すコンテナは、一瞬 Ready に見えることがあるため。
4. **HTTP**: ingress の向き先の Service に、API サーバーの proxy 経由で GET を送る。**500 未満**なら成功(認証の 401、移動の 302、403 も、動いている印)。起動中は、成功するまで 3 秒おきに再試行する。
5. **片付け**: namespace を消す(`--keep` なら残す)。

結果は、`PASS <id>(時間)` か、`FAIL <id>` と、原因(Pod の状態、コンテナの待機・終了の理由、直近のイベント、最後の HTTP の応答)。

### 4.3 テンプレートごとの調整(`<id>/smoke.yaml`)

無くてもよい。次の項目だけ、書ける。

```yaml
path: /login          # 確かめるパス(既定は ingress のルールのパス)
statuses: [200, 302]  # 成功とみなすステータス(既定は 500 未満)
timeoutSeconds: 300   # 起動を待つ秒数(既定 300。重いアプリは、増やす)
```

例: ログイン画面が `200` を返すことまで確かめたい、起動が遅い(Java など)ので 600 秒待ちたい、など。

### 4.4 HTTP の無いテンプレート

PostgreSQL、MariaDB、Redis のように、ingress が無いテンプレートは、手順 2〜3(Ready と、再起動しないこと)までが確認の範囲になる。つなげるかどうかは、手で確かめる(5 を参照)。

## 5. 手で確認する(`test:keep`)

```
task test:keep -- gitea
export KUBECONFIG=$PWD/.kube/config
kubectl get all -A | grep project-
kubectl -n project-<UUID> logs deploy/<deploy-…>
kubectl -n project-<UUID> port-forward svc/<service-…> 3000:3000
```

ブラウザで `http://localhost:3000` を開き、次を確かめる。

- 初回の設定の画面が出る。`metadata.notes` の手順どおりに進められる。
- 再起動(`kubectl rollout restart`)しても、データが残る(ボリュームに保存されている)。
- 見終わったら、namespace を消す: `kubectl delete ns project-<UUID>`。

Traefik 経由の確認は、`curl -H 'Host: <id>.harness.localhost' http://localhost:18080/`(Host は、`plan` の `hosts`)。

## PR に貼るもの

- `task validate -- <id>` の `OK` の行。
- `task test -- <id>` の `PASS` の行(時間つき)。
- 手で確認したこと(初回の設定ができた、など)を、一言。

CI(`.github/workflows/validate.yml`)は、`validate` と `render` だけを自動で行う。実機の試験は、手元で行う(重く、イメージの取得に時間がかかるため)。

## 試験で分からないこと

本番(Talos + Longhorn)との違いは、[harness.md](harness.md) の「本番との違い」を参照。特に、**ストレージの遅さ(複数の RWX ボリュームの作成)**、**gVisor/Kata などのランタイム**、**クォータ**は、この環境では確かめられない。大きなボリュームを複数持つテンプレートは、メンテナが、本番に近いクラスターで、別途確かめる。
