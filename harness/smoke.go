package main

import (
	"os"
	"path/filepath"

	"sigs.k8s.io/yaml"
)

// smokeSettings は、テンプレートごとの、起動の確認の設定(<id>/smoke.yaml。無くてもよい)。
type smokeSettings struct {
	// Path は、HTTP で確かめるパス(既定は、ingress のルールのパス)。
	Path string `json:"path"`
	// Statuses は、成功とみなす HTTP のステータス(既定は、500 未満なら全て)。
	Statuses []int `json:"statuses"`
	// TimeoutSeconds は、起動を待つ秒数(既定 300)。
	TimeoutSeconds int `json:"timeoutSeconds"`
}

// loadSmokeSettings は、テンプレートの版のフォルダの smoke.yaml を読む。
//
// 引数:
//   - root: リポジトリのフォルダ。
//   - templateID: テンプレートの id。
//   - version: 版(x.y.z)。
//
// 戻り値:
//   - 設定(ファイルが無ければ、既定の値)。
//   - ファイルが壊れているときのエラー。
func loadSmokeSettings(root string, templateID string, version string) (smokeSettings, error) {
	// 既定の値から始める。
	settings := smokeSettings{TimeoutSeconds: 300}
	// ファイルを読む(無ければ、既定のまま)。
	content, readError := os.ReadFile(filepath.Join(root, templateID, version, "smoke.yaml"))
	if os.IsNotExist(readError) {
		return settings, nil
	}
	if readError != nil {
		return settings, readError
	}
	// 書かれた値で、上書きする。
	if parseError := yaml.Unmarshal(content, &settings); parseError != nil {
		return settings, parseError
	}
	// 0 以下の待ち時間は、既定に戻す。
	if settings.TimeoutSeconds <= 0 {
		settings.TimeoutSeconds = 300
	}
	return settings, nil
}

// statusAccepted は、HTTP のステータスが、成功とみなせるかを返す。
//
// 引数:
//   - settings: 設定。
//   - status: 返ってきたステータス。
//
// 戻り値:
//   - 成功なら true。
func statusAccepted(settings smokeSettings, status int) bool {
	// 指定が無ければ、500 未満(認証の 401、移動の 302 なども、動いている印)。
	if len(settings.Statuses) == 0 {
		return status < 500
	}
	// 指定があれば、その中にあるか。
	for _, accepted := range settings.Statuses {
		if accepted == status {
			return true
		}
	}
	return false
}
