// Package templates は、このシステムに同梱する、テンプレート(フォルダ形式)を、バイナリに埋め込んで公開する。
//
// テンプレートは、templates/<id>/ に、1 つにつき 1 つのフォルダ:
//
//	manifest.yaml            名前、説明、アイコン、分類、既定の版
//	README.md                説明の文書
//	<x.y.z>/config.yaml      変数と、作成後の案内
//	<x.y.z>/volumes.yaml, configmaps.yaml, deploy.yaml, service.yaml, ingress.yaml
//	<x.y.z>/files/…          from: で読み込むファイル
//
// このリポジトリは、launchs 本体に、submodule(templates/)として取り込まれ、本体が、この Files を使う。
// harness/(別の Go モジュール)と、scaffold/(新しいテンプレートの雛形)は、埋め込まれない。
package templates

import (
	"embed"
	"io/fs"
)

// embedded は、templates/ の下の、テンプレートの定義と、読み込むファイルだけ。
//
//go:embed templates/*/manifest.yaml templates/*/README.md templates/*/*/*.yaml templates/*/*/files
var embedded embed.FS

// Files は、同梱のテンプレート。<id>/manifest.yaml、<id>/<版>/… の形(templates/ の接頭辞は、外してある)。
var Files = mustSub(embedded, "templates")

// mustSub は、埋め込んだファイルシステムの、サブフォルダを返す。
//
// 引数:
//   - source: 埋め込んだファイルシステム。
//   - folder: 取り出すフォルダ。
//
// 戻り値:
//   - folder を根にしたファイルシステム。フォルダが無いとき(埋め込みが壊れているとき)は、起動時に panic する。
func mustSub(source fs.FS, folder string) fs.FS {
	// サブフォルダを取り出す。
	sub, subError := fs.Sub(source, folder)
	if subError != nil {
		panic(subError)
	}
	return sub
}
