# うまくいかないとき

## `task setup` / ビルド

| 症状 | 原因と対処 |
| --- | --- |
| `launchs/` が空 | `git submodule update --init --recursive`(`task setup`) |
| `go: downloading …` が毎回出て遅い | 初回だけ。Go のキャッシュは docker の volume `harness-gomod` に残る。`task setup` で作る |
| `error obtaining VCS status` | `-buildvcs=false` が付いているか(Taskfile は付けてある)。手で `go build` するときに必要 |

## `validate`

| 症状 | 原因と対処 |
| --- | --- |
| `テンプレートの一覧を読めません: …` | どのフォルダの、どの項目かがメッセージに出る。`manifest.yaml`・版のフォルダ(`x.y.z`)・種類ごとのファイルの書式、`from:` のファイルの有無。`service.yaml` の deploy 名、`deploy.yaml` に `service:` や `kind:` を書いていないか |
| `…が定義されていません`(参照) | `${変数}` の名前の間違い、`${service.<deploy>.dns}` の deploy 名の間違い |
| 本体の版を上げたら、急に通らない | 本体の検査が厳しくなった。メッセージに従って直す |

## `cluster:up`

| 症状 | 原因と対処 |
| --- | --- |
| `port is already allocated` | `16443` か `18080` が使われている。Taskfile の `-p` を変える(`16443` を変えたら、`sed` の行も) |
| 起動しない(`privileged` が使えない) | rootless の docker/podman では、k3s を動かせないことがある。root の docker で |
| `traefik not found` | k3s が、Traefik を入れ終わるまで 1〜2 分かかる。待つ(`task cluster:up` は、待つ) |
| 一度壊れた | `task cluster:down && task cluster:up` |

## `test`

| 症状 | 原因と対処 |
| --- | --- |
| `Ready になりません` + `ImagePullBackOff` | イメージ名・版の間違い、または、取得に時間がかかっている。`timeoutSeconds` を増やす |
| `Ready になりません` + `Pending`(PVC) | local-path は、Pod が決まってから PVC を結ぶ。メッセージの「イベント」を読む。メモリが足りないこともある(`large` の spec は 1 GiB) |
| `再起動を繰り返しています` | コンテナが落ちている。`task test:keep -- <id>` のあと、`kubectl logs`。環境変数の不足、権限(非 root のイメージは `fsGroup`)、起動コマンドの間違い |
| `GET /… が成功しません: status=502/503` | アプリの起動中、またはポートの間違い。`timeoutSeconds` を増やす。ポートは、ingress の `port` と、アプリが待つポートが、同じか |
| `GET /… status=404` | アプリが、サブパスを期待している。ingress の `path` を確かめる(本体の ingress は、パスを書き換えない) |
| `service の DNS 名が、まだ決まっていません` | deploy が、まだ作られていない service を参照している。参照先の deploy に `service:` があるか |
| `注意: IngressRoute の CRD が無いので…` | 別のクラスターを使っている。k3s の Traefik には CRD がある。`--traefik-api-version` で apiVersion を合わせる |
| namespace が残った | `kubectl get ns | grep project-` → `kubectl delete ns …`(`task cluster:down` でも、全て消える) |

## 中を調べる

```
export KUBECONFIG=$PWD/.kube/config
kubectl get pods -A
kubectl -n project-<UUID> describe pod <pod>
kubectl -n project-<UUID> logs <pod> --previous     # 落ちる前のログ
kubectl -n project-<UUID> get events --sort-by=.lastTimestamp
```

`task test:keep -- <id>` で、試験の後も namespace を残せる。

## 試験は通るが、本番で動かないとき

- 複数の volume を同時に作るテンプレート: 本番の Longhorn(RWX)は、1 つずつ 1〜2 分かかる。本体の deploy の待ち(300 秒)を超えることがある。ボリュームの数を減らす。
- 本番のネットワークの制限(外部への通信、NetworkPolicy)で、アプリが外に出られない: 本体の `docs/network-isolation.md`。
- 本番で、ホスト名が、`<id>.harness.localhost` ではなく、自動で払い出される(「形容詞-色-動物」)。アプリが、自分の URL を設定で求めるときは、`notes` で、利用者に入れてもらう。
