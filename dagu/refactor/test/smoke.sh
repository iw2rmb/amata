#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(CDPATH= cd -- "$(dirname -- "$0")/../../.." && pwd)"
DAG_FILE="${ROOT_DIR}/dagu/refactor/refactor.yaml"
SCRIPTS_DIR="${ROOT_DIR}/dagu/refactor/scripts"

tmp_dir="$(mktemp -d)"
trap 'rm -rf "$tmp_dir"' EXIT

repo_dir="${tmp_dir}/repo"
bin_dir="${tmp_dir}/bin"
trace_dir="${tmp_dir}/trace"
mkdir -p "${repo_dir}/pkg/util/nested" "${repo_dir}/target" "${repo_dir}/build" "${repo_dir}/.hidden" "${bin_dir}" "${trace_dir}"
mkdir -p "${repo_dir}/Sources/App"

cat >"${repo_dir}/app.rs" <<'EOF'
fn main() {}
EOF

cat >"${repo_dir}/pkg/util/helpers.py" <<'EOF'
def helper():
    return "ok"
EOF

cat >"${repo_dir}/pkg/util/nested/module.go" <<'EOF'
package nested

func RefactorMe() string {
	return "ok"
}
EOF

cat >"${repo_dir}/Sources/App/main.swift" <<'EOF'
func refactorMe() -> String {
    "ok"
}
EOF

cat >"${repo_dir}/target/ignored.rs" <<'EOF'
fn ignored() {}
EOF

cat >"${repo_dir}/build/ignored.go" <<'EOF'
package build
EOF

cat >"${repo_dir}/.hidden/ignored.py" <<'EOF'
print("hidden")
EOF

cat >"${bin_dir}/claude" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail

prompt="$(cat)"
repo="$(
  printf '%s' "$prompt" | perl -ne 'if (/^Repository root: (.+)$/m) { print "$1\n"; exit }'
)"
path="$(
  printf '%s' "$prompt" | perl -ne 'if (/^Focus path: (.+)$/m) { print "$1\n"; exit }'
)"
files="$(
  printf '%s' "$prompt" | perl -0ne 'if (/^Focus files:\n(.*?)(?:\n\n|\z)/ms) { print "$1"; exit }'
)"

[ -n "$repo" ] || exit 3
[ -n "$path" ] || exit 4
[ -n "$files" ] || exit 5

printf '%s\n' "$path" >>"${TRACE_DIR}/claude-paths.log"
printf '%s\n---\n' "$files" >>"${TRACE_DIR}/claude-files.log"

case "$path" in
  "pkg/util/nested")
    printf '\n// refactored\n' >>"${repo}/pkg/util/nested/module.go"
    ;;
  "Sources/App")
    printf '\n// refactored\n' >>"${repo}/Sources/App/main.swift"
    ;;
esac

printf 'inspected %s\n' "$path"
EOF

cat >"${bin_dir}/codex" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail

[ "${1:-}" = "exec" ] || exit 2
shift

out_file=""
repo="."
prompt=""

while [ "$#" -gt 0 ]; do
  case "$1" in
    -o)
      out_file="$2"
      shift 2
      ;;
    -C)
      repo="$2"
      shift 2
      ;;
    --dangerously-bypass-approvals-and-sandbox)
      shift
      ;;
    --color|--model|-c)
      shift 2
      ;;
    -)
      shift
      prompt="$(cat)"
      ;;
    *)
      shift
      ;;
  esac
done

[ -n "$out_file" ] || exit 3

path="$(
  printf '%s' "$prompt" | perl -ne 'if (/^Focus path: (.+)$/m) { print "$1\n"; exit }'
)"
files="$(
  printf '%s' "$prompt" | perl -0ne 'if (/^Focus files:\n(.*?)(?:\n\n|\z)/ms) { print "$1"; exit }'
)"
[ -n "$path" ] || exit 4
[ -n "$files" ] || exit 5

printf '%s\n' "$path" >>"${TRACE_DIR}/codex-paths.log"
printf '%s\n---\n' "$files" >>"${TRACE_DIR}/codex-files.log"

git -C "$repo" add -A
git -C "$repo" commit -m "refactor: ${path}" >/dev/null

printf 'committed %s\n' "$path" >"$out_file"
EOF

chmod +x "${bin_dir}/claude" "${bin_dir}/codex"

git -C "$repo_dir" init -q
git -C "$repo_dir" config user.email smoke@example.com
git -C "$repo_dir" config user.name smoke
git -C "$repo_dir" add -A
git -C "$repo_dir" commit -m "chore: seed repository" >/dev/null

PATH="${bin_dir}:$PATH" TRACE_DIR="${trace_dir}" dagu start "$DAG_FILE" -- \
  REPO_DIR="$repo_dir" \
  CLAUDE_MODEL=fake-claude \
  CLAUDE_REFACTOR_REASONING=low \
  CODEX_MODEL=fake-codex \
  CODEX_REVIEW_REASONING=low >"${trace_dir}/run-absolute.out"

cat >"${trace_dir}/expected-claude-paths.txt" <<'EOF'
pkg/util/nested
Sources/App
pkg/util
.
EOF

cmp "${trace_dir}/expected-claude-paths.txt" "${trace_dir}/claude-paths.log"

cat >"${trace_dir}/expected-codex-paths.txt" <<'EOF'
pkg/util/nested
Sources/App
EOF

cmp "${trace_dir}/expected-codex-paths.txt" "${trace_dir}/codex-paths.log"

cat >"${trace_dir}/expected-claude-files.txt" <<'EOF'
pkg/util/nested/module.go
---
Sources/App/main.swift
---
pkg/util/helpers.py
---
app.rs
---
EOF

cmp "${trace_dir}/expected-claude-files.txt" "${trace_dir}/claude-files.log"
cmp "${trace_dir}/expected-codex-paths.txt" "${trace_dir}/codex-paths.log"

cat >"${trace_dir}/expected-codex-files.txt" <<'EOF'
pkg/util/nested/module.go
---
Sources/App/main.swift
---
EOF

cmp "${trace_dir}/expected-codex-files.txt" "${trace_dir}/codex-files.log"

grep -q 'in-progress step=claude path=pkg/util/nested files=1' "${trace_dir}/run-absolute.out"
grep -q 'in-progress step=codex path=pkg/util/nested files=1' "${trace_dir}/run-absolute.out"
grep -q 'in-progress step=claude path=Sources/App files=1' "${trace_dir}/run-absolute.out"
grep -q 'in-progress step=codex path=Sources/App files=1' "${trace_dir}/run-absolute.out"

commit_count="$(git -C "$repo_dir" rev-list --count HEAD)"
[ "$commit_count" = "3" ]

git -C "$repo_dir" diff --quiet --exit-code
git -C "$repo_dir" diff --cached --quiet --exit-code
[ -z "$(git -C "$repo_dir" ls-files --others --exclude-standard)" ]

rm -f "${trace_dir}/claude-paths.log" "${trace_dir}/codex-paths.log" "${trace_dir}/claude-files.log" "${trace_dir}/codex-files.log"

( cd "$repo_dir" && PATH="${bin_dir}:$PATH" TRACE_DIR="${trace_dir}" dagu start "$DAG_FILE" -- \
  REPO_DIR="$repo_dir" \
  SCRIPTS_DIR="~/@iw2rmb/amata/dagu/refactor/scripts" \
  CLAUDE_MODEL=fake-claude \
  CLAUDE_REFACTOR_REASONING=low \
  CODEX_MODEL=fake-codex \
  CODEX_REVIEW_REASONING=low ) >"${trace_dir}/run-home-scripts.out"

cmp "${trace_dir}/expected-claude-paths.txt" "${trace_dir}/claude-paths.log"
cmp "${trace_dir}/expected-codex-paths.txt" "${trace_dir}/codex-paths.log"
cmp "${trace_dir}/expected-claude-files.txt" "${trace_dir}/claude-files.log"
cmp "${trace_dir}/expected-codex-files.txt" "${trace_dir}/codex-files.log"

grep -q 'in-progress step=claude path=pkg/util/nested files=1' "${trace_dir}/run-home-scripts.out"
grep -q 'in-progress step=codex path=pkg/util/nested files=1' "${trace_dir}/run-home-scripts.out"
grep -q 'in-progress step=claude path=Sources/App files=1' "${trace_dir}/run-home-scripts.out"
grep -q 'in-progress step=codex path=Sources/App files=1' "${trace_dir}/run-home-scripts.out"

rm -f "${trace_dir}/claude-paths.log" "${trace_dir}/codex-paths.log" "${trace_dir}/claude-files.log" "${trace_dir}/codex-files.log"

PATH="${bin_dir}:$PATH" TRACE_DIR="${trace_dir}" dagu start "$DAG_FILE" -- \
  REPO_DIR="$repo_dir" \
  TARGET_FILTER="*.swift" \
  CLAUDE_MODEL=fake-claude \
  CLAUDE_REFACTOR_REASONING=low \
  CODEX_MODEL=fake-codex \
  CODEX_REVIEW_REASONING=low >"${trace_dir}/run-swift-filter.out"

cat >"${trace_dir}/expected-swift-filter-paths.txt" <<'EOF'
Sources/App
EOF

cat >"${trace_dir}/expected-swift-filter-files.txt" <<'EOF'
Sources/App/main.swift
---
EOF

cmp "${trace_dir}/expected-swift-filter-paths.txt" "${trace_dir}/claude-paths.log"
cmp "${trace_dir}/expected-swift-filter-paths.txt" "${trace_dir}/codex-paths.log"
cmp "${trace_dir}/expected-swift-filter-files.txt" "${trace_dir}/claude-files.log"
cmp "${trace_dir}/expected-swift-filter-files.txt" "${trace_dir}/codex-files.log"

grep -q 'in-progress step=claude path=Sources/App files=1' "${trace_dir}/run-swift-filter.out"
grep -q 'in-progress step=codex path=Sources/App files=1' "${trace_dir}/run-swift-filter.out"

rm -f "${trace_dir}/claude-paths.log" "${trace_dir}/codex-paths.log" "${trace_dir}/claude-files.log" "${trace_dir}/codex-files.log"

PATH="${bin_dir}:$PATH" TRACE_DIR="${trace_dir}" dagu start "$DAG_FILE" -- \
  REPO_DIR="$repo_dir" \
  TARGET_FILTER="Sources/**" \
  CLAUDE_MODEL=fake-claude \
  CLAUDE_REFACTOR_REASONING=low \
  CODEX_MODEL=fake-codex \
  CODEX_REVIEW_REASONING=low >"${trace_dir}/run-sources-filter.out"

cmp "${trace_dir}/expected-swift-filter-paths.txt" "${trace_dir}/claude-paths.log"
cmp "${trace_dir}/expected-swift-filter-paths.txt" "${trace_dir}/codex-paths.log"
cmp "${trace_dir}/expected-swift-filter-files.txt" "${trace_dir}/claude-files.log"
cmp "${trace_dir}/expected-swift-filter-files.txt" "${trace_dir}/codex-files.log"

grep -q 'in-progress step=claude path=Sources/App files=1' "${trace_dir}/run-sources-filter.out"
grep -q 'in-progress step=codex path=Sources/App files=1' "${trace_dir}/run-sources-filter.out"

if ( cd "$repo_dir" && PATH="${bin_dir}:$PATH" TRACE_DIR="${trace_dir}" dagu start "$DAG_FILE" -- \
  REPO_DIR=. \
  CLAUDE_MODEL=fake-claude \
  CLAUDE_REFACTOR_REASONING=low \
  CODEX_MODEL=fake-codex \
  CODEX_REVIEW_REASONING=low ) >"${trace_dir}/relative-repo.out" 2>"${trace_dir}/relative-repo.err"; then
  printf 'expected relative REPO_DIR to fail\n' >&2
  exit 1
fi

grep -q -- '--repo must be an absolute path or start with ~/' "${trace_dir}/relative-repo.err"

printf 'refactor workflow smoke test passed\n'
