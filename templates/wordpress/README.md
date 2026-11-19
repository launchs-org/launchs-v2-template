# WordPress

ブログ・サイトを作れる WordPress。データベース(MariaDB)と、アップロードしたファイルは、ボリュームに保存されます。

## 構成と注意

WordPress(web + MariaDB)。2 つの deploy を、service の DNS 名でつなぐ。データベースとアップロードしたファイルは、ボリュームに保存する。

## 変数

| 名前 | 内容 |
| --- | --- |
| `db_password` | データベースのパスワード |
| `db_storage` | データベースの大きさ(MB) |
| `content_storage` | アップロードの置き場の大きさ(MB) |

## 版

- `1.0.0`: 最初の版。
