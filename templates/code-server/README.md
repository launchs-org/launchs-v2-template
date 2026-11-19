# code-server(VS Code)

ブラウザで使える VS Code。パスワードで保護され、作業フォルダはボリュームに保存されます。

## 構成と注意

code-server(ブラウザで動く VS Code)。パスワードつき。作業フォルダはボリュームに保存し、エディタの設定(settings.json)は ConfigMap で渡す。

## 変数

| 名前 | 内容 |
| --- | --- |
| `password` | ログインのパスワード |
| `storage` | 作業フォルダの大きさ(MB) |
| `theme` | テーマ |
| `font_size` | 文字の大きさ |

## 版

- `1.0.0`: 最初の版。
