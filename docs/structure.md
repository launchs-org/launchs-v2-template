# テンプレートのフォルダの構成

テンプレート 1 つは、`templates/<id>/` の、1 つのフォルダ。**版によらない情報**と、**版ごとの中身**に分ける。

```
templates/<id>/
  manifest.yaml        名前、説明、アイコン、分類、既定の版(版によらない)
  README.md            開発者と、詳細の画面に出す説明(任意)
  <x.y.z>/             版のフォルダ(1.0.0 のような名前。複数持てる)
    config.yaml        変数(variables)と、作成後の案内(notes)
    volumes.yaml       ボリューム
    configmaps.yaml    共有の設定ファイル(ConfigMap)
    deploy.yaml        コンテナ(deploy)
    service.yaml       deploy に付ける Service
    ingress.yaml       インターネットへの公開
    files/             from: で読み込むファイル
    smoke.yaml         task test の確認の調整(本体には入らない)
```

`config.yaml` 以外は、無ければ、その種類は作らない(例: ボリュームの無いテンプレートは、`volumes.yaml` を置かない)。`task new -- <id>` で、雛形(`scaffold/`)から作れる。

## manifest.yaml

```yaml
id: redash                  # フォルダの名前と同じ(英小文字・数字・ハイフン)
name: Redash(データの可視化) # 画面に出す名前
description: …               # 1〜2 文の説明(利用者向け)
icon: chart                  # アイコンの名前
category: monitoring         # database / web / cache / tool / monitoring / dev
latest: 1.0.0                # 既定で使う版(省略すると、いちばん新しい版)
```

## 版

- 版のフォルダの名前は、`x.y.z`(数字 3 つ。`1.2.0` など。`v1` や `01.0.0` は不可)。
- 利用者は、作成の画面で版を選べる(既定は `latest`)。作ったときの版は、記録される。
- **互換性のない変更**(変数の名前を変える、構成を変える、イメージを大きく変える)は、版を上げる。細かい直し(誤字、案内の追記)は、同じ版の中で直してよい。
- 古い版は、残す(消すと、その版を選べなくなる)。
- 新しい版を作る: `task new:version -- <id> <新しい版>`(今の最新の版を、コピーして作る)。作ったら、`manifest.yaml` の `latest` と、`README.md` の「版」を更新する。

## 各ファイルの書き方

どのファイルも、最上位のキーが 1 つ(`config.yaml` は 2 つ)。**知らない項目は、エラー**になる。部品の種類(kind)は、ファイルで決まるので、書かない。

### config.yaml

```yaml
notes: |                      # 作成後に、利用者に見せる案内(${変数} が使える。パスワードは伏せて表示)
  1. 公開 URL を開いてください。
variables:
  - name: storage
    label: データの大きさ(MB)
    type: integer             # string / integer / choice / password
    default: "1024"
    min: 256
    max: 5120
```

### volumes.yaml

```yaml
volumes:
  - name: data
    sizeMB: ${storage}
```

### configmaps.yaml

```yaml
configmaps:
  - name: pgconf
    files:
      - key: postgresql.conf
        from: files/postgresql.conf      # 版のフォルダからの相対パス
```

### deploy.yaml

```yaml
deploys:
  - name: db
    image: postgres:16                  # 版を固定する
    spec: medium                        # small / medium / large
    replicas: 1
    fsGroup: 1000                       # 非 root のイメージで、ボリュームに書けないとき
    command: /usr/bin/entrypoint.sh
    args: ["postgres"]
    env:
      POSTGRES_PASSWORD: ${password}
      DB_HOST: ${service.db.dns}        # 別の deploy の Service の DNS 名
    mounts:
      - volume: data
        path: /var/lib/postgresql/data
    configmaps:
      - configmap: pgconf
        key: postgresql.conf
        path: /etc/postgresql/postgresql.conf
    files:
      - path: /docker-entrypoint-initdb.d/init.sql
        from: files/init.sql
```

**service は、ここに書かない**(`service.yaml` に書く)。

### service.yaml

```yaml
services:
  - deploy: db                          # deploy.yaml の deploy の name
    ports:
      - name: postgres
        port: 5432
        protocol: tcp
```

### ingress.yaml

```yaml
rules:
  - path: /
    deploy: web                         # service を持つ deploy
    port: 80
```

公開は **HTTP だけ**。ホスト名は、自動で払い出される。

## 本体との関係

- 本体(launchs)は、このリポジトリを `templates/` に取り込み、`embed.go` の `Files` を使う。
- 読み込みと検査は、本体の `shared/template`(`Load`)。この構成の規則(版の名前、ファイルの分け方、知らない項目の拒否)は、そこで検査される。`task validate` が、同じ検査を行う。
- API: `GET /api/v1/templates`(既定の版の一覧。`versions` に全ての版)、`GET /api/v1/templates/:id?version=x.y.z`、`POST /api/v1/projects/:id/templates/:id/apply` の `version`(省略は、既定の版)。
