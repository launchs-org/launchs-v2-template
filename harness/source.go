package main

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing/fstest"

	"launchs/shared/template"
)

// loadRegistry は、テンプレートのリポジトリのフォルダから、テンプレートを読み込んで検査する。
//
// 処理の流れ:
//  1. root の直下のフォルダのうち、manifest.yaml を持つものだけを選ぶ。
//  2. 選んだフォルダの中身(manifest.yaml、README.md、各版のフォルダ)を、メモリ上のファイルシステムに写す。
//  3. 本番と同じ template.Load で読み込む(検査もここで行われる)。
//
// 引数:
//   - root: テンプレートを置いたフォルダ(templates/)。
//
// 戻り値:
//   - 読み込んだ、テンプレートの登録簿。
//   - 読み込めないときのエラー(どのテンプレートの、どこが悪いかを含む)。
func loadRegistry(root string) (*template.Registry, error) {
	// メモリ上のファイルシステムを用意する。
	memory := fstest.MapFS{}
	// root の直下を読む。
	entries, readError := os.ReadDir(root)
	if readError != nil {
		return nil, fmt.Errorf("フォルダを読めません: %w", readError)
	}
	for _, entry := range entries {
		// フォルダだけを見る。
		if !entry.IsDir() {
			continue
		}
		// manifest.yaml が無いフォルダは、テンプレートではないので飛ばす。
		if _, statError := os.Stat(filepath.Join(root, entry.Name(), "manifest.yaml")); statError != nil {
			continue
		}
		// フォルダの中身を、全て写す(smoke.yaml は、読み込みに影響しない)。
		copyError := copyFolder(root, entry.Name(), memory)
		if copyError != nil {
			return nil, copyError
		}
	}
	// 本番と同じ関数で読み込む。
	return template.Load(memory)
}

// copyFolder は、フォルダの中のファイルを、全て、メモリ上のファイルシステムに写す。
//
// 引数:
//   - root: リポジトリのフォルダ。
//   - folder: テンプレートのフォルダの名前。
//   - memory: 写し先。
//
// 戻り値:
//   - 読めないファイルがあるときのエラー。
func copyFolder(root string, folder string, memory fstest.MapFS) error {
	// フォルダの下を、順にたどる。
	return filepath.WalkDir(filepath.Join(root, folder), func(path string, entry fs.DirEntry, walkError error) error {
		if walkError != nil {
			return walkError
		}
		// フォルダは、写さない。
		if entry.IsDir() {
			return nil
		}
		// ファイルの内容を読む。
		content, readError := os.ReadFile(path)
		if readError != nil {
			return readError
		}
		// root からの相対のパスで、登録する。
		relative, relativeError := filepath.Rel(root, path)
		if relativeError != nil {
			return relativeError
		}
		memory[filepath.ToSlash(relative)] = &fstest.MapFile{Data: content}
		return nil
	})
}

// selectTemplates は、指定された id と版のテンプレートを返す。指定が無ければ、全ての id を返す。
//
// 引数:
//   - registry: 登録簿。
//   - ids: 指定された id(空なら全て)。
//   - version: 版(x.y.z)。空なら、既定の版。"all" なら、全ての版。
//
// 戻り値:
//   - id と版の順に並んだテンプレート。
//   - 知らない id や版があるときのエラー。
func selectTemplates(registry *template.Registry, ids []string, version string) ([]template.Template, error) {
	// 対象の id を決める。
	targets := ids
	if len(targets) == 0 {
		for _, listed := range registry.List() {
			targets = append(targets, listed.Metadata.ID)
		}
	}
	selected := []template.Template{}
	for _, identifier := range targets {
		// 既定の版で探して、id があるかを確かめる。
		found, exists := registry.Get(identifier)
		if !exists {
			return nil, fmt.Errorf("テンプレート %q がありません", identifier)
		}
		switch version {
		case "":
			selected = append(selected, found)
		case "all":
			// 全ての版(新しい順に並んでいる)。
			for _, each := range found.Metadata.Versions {
				versioned, _ := registry.GetVersion(identifier, each)
				selected = append(selected, versioned)
			}
		default:
			versioned, versionExists := registry.GetVersion(identifier, version)
			if !versionExists {
				return nil, fmt.Errorf("テンプレート %q に、版 %q がありません(ある版: %s)", identifier, version, strings.Join(found.Metadata.Versions, ", "))
			}
			selected = append(selected, versioned)
		}
	}
	// id、版の順に並べる。
	sort.SliceStable(selected, func(first int, second int) bool { return selected[first].Metadata.ID < selected[second].Metadata.ID })
	return selected, nil
}
