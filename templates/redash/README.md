# Redash(データの可視化)

データベースに SQL で問い合わせて、グラフやダッシュボードを作れます。PostgreSQL や MySQL など、たくさんのデータ源につなげます。

## 構成と注意

Redash(SQL でデータを調べて、グラフと画面にする)。web(server)、scheduler、worker、PostgreSQL、Redis の 5 つの deploy を、service の DNS 名でつなぐ。
最初の起動のときに、server が、テーブルを作る(create_db)。scheduler と worker は、テーブルができるまで、再起動を繰り返して待つ。

## 変数

| 名前 | 内容 |
| --- | --- |
| `db_password` | 内部のデータベースのパスワード |
| `cookie_secret` | Cookie の秘密の文字列 |
| `secret_key` | 暗号化の秘密の文字列 |
| `storage` | データベースの大きさ(MB) |

## 作成後の案内

1. 初回の起動では、テーブルを作るので、公開 URL が開くまで 1〜2 分かかります。
2. 公開 URL を開くと、最初の設定画面が出ます。管理者の名前、メールアドレス、パスワード、組織の名前を入れてください。この画面は、最初に開いた人が、操作できてしまうので、作成が終わったら、すぐ、済ませてください。
3. データ源は、設定の「Data Sources」から足します。この Redash と同じプロジェクトの PostgreSQL などには、service の DNS 名でつなげます。

## 版

- `1.0.0`: 最初の版。
