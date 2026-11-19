# nginx(静的なページ)

nginx で、簡単なページを公開します。設定ファイルとページは、ConfigMap で管理され、あとから編集できます。

## 構成と注意

nginx で静的なページを公開する。サーバーの設定(default.conf)とページ(index.html)と、バイナリの画像(favicon)を、files/ に置き、共有の ConfigMap として渡す。

## 変数

| 名前 | 内容 |
| --- | --- |
| `title` | ページの題 |
| `gzip` | 圧縮(gzip) |

## 版

- `1.0.0`: 最初の版。
