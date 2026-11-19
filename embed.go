// Package templates は、このシステムに同梱する、テンプレート(フォルダ形式)を、バイナリに埋め込んで公開する。
//
// テンプレートは、1 つにつき 1 つのフォルダ(<id>/template.yaml と、from: で読み込むファイル <id>/files/…)。
// 追加するときは、このリポジトリの直下に、metadata.id と同じ名前のフォルダを作る。読み込みと検証は、launchs の shared/template が行う。
// 埋め込みにしているのは、イメージに含め忘れない(ファイルが、実行時に、無いことが起きない)ためと、利用者が定義を変えられないため。
//
// このリポジトリは、launchs 本体に、サブモジュール(templates/)として取り込まれる。本体の中では、このフォルダが、パッケージ launchs/templates になる。
// 試験の道具(harness/)は、別の Go モジュールで、埋め込みの対象ではない。
package templates

import "embed"

// Files は、同梱のテンプレート(<id>/template.yaml と <id>/files/…)。テンプレートのフォルダだけを埋め込む(harness/ や docs/ は含めない)。
//
//go:embed */template.yaml */files
var Files embed.FS
