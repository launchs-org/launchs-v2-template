#!/usr/bin/env bash
# 基準のブランチから、変更のあったテンプレートの id を、1 行にまとめて出す(試験の対象を決めるため)。
#
# 使い方: scripts/changed-templates.sh [基準(既定 origin/main)]
#
# 規則:
#   - templates/<id>/ の下のファイルが変わった(追加、変更、削除)なら、その id。フォルダが無くなった id(削除)は、出さない。
#   - harness/、scaffold/、Taskfile.yml が変わったときは、仕組みの確認として、nginx と redis を足す(全部は試験しない)。
#   - コミット済みの変更と、まだコミットしていない変更(新しいファイルを含む)の、両方を見る。
set -euo pipefail
base="${1:-origin/main}"
cd "$(git rev-parse --show-toplevel)"
# 基準との分岐点から、今までに変わったファイル(コミット済み + 作業中 + 新しいファイル)。
fork_point="$(git merge-base "$base" HEAD)"
changed="$( { git diff --name-only "$fork_point"; git ls-files --others --exclude-standard; } | sort -u )"
ids=""
# テンプレートのフォルダごとの変更。
for id in $(printf '%s\n' "$changed" | sed -n 's#^templates/\([^/]*\)/.*#\1#p' | sort -u); do
  [ -f "templates/$id/manifest.yaml" ] && ids="$ids $id"
done
# 仕組みの変更。
if printf '%s\n' "$changed" | grep -qE '^(harness/|scaffold/|Taskfile\.yml)'; then
  ids="$ids nginx redis"
fi
printf '%s\n' "$ids" | tr ' ' '\n' | sed '/^$/d' | sort -u | tr '\n' ' ' | sed 's/ $//'
echo
