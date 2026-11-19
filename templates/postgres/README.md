# PostgreSQL

データベース(PostgreSQL 16)。データはボリュームに保存され、設定ファイルは ConfigMap で編集できます。

## 構成と注意

PostgreSQL 16。データはボリュームに保存する。サーバーの設定(postgresql.conf)は ConfigMap で渡し、初期化の SQL は deploy のファイルで渡す。

## 変数

| 名前 | 内容 |
| --- | --- |
| `database` | データベース名 |
| `user` | ユーザー名 |
| `password` | パスワード |
| `storage` | ボリュームの大きさ(MB) |
| `max_connections` | 最大の接続数 |
| `log_statement` | SQL のログ |

## 版

- `1.0.0`: 最初の版。
