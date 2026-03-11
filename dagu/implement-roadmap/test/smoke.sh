#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(CDPATH= cd -- "$(dirname -- "$0")/../../.." && pwd)"
DAG_FILE="${ROOT_DIR}/dagu/implement-roadmap/implement-roadmap.yaml"
SCRIPTS_DIR="${ROOT_DIR}/dagu/implement-roadmap/scripts"

tmp_dir="$(mktemp -d)"
trap 'rm -rf "$tmp_dir"' EXIT

repo_dir="${tmp_dir}/repo"
bin_dir="${tmp_dir}/bin"
mkdir -p "${repo_dir}/roadmap/sample" "$bin_dir"

cat >"${repo_dir}/roadmap/sample/index.md" <<'EOF'
# Sample Roadmap

Legend: [ ] todo, [x] done.

- [ ] 1.0 Ship sample feature
  - Repository: sample
  - Component: `app`
  - Verification: smoke validation
  - Reasoning: high
1. Create implementation marker.
EOF

cat >"${bin_dir}/codex" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail

[ "${1:-}" = "exec" ] || exit 2
shift

out_file=""
repo="."

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
      ;;
    *)
      shift
      ;;
  esac
done

[ -n "$out_file" ] || exit 3
prompt="$(cat)"
cd "$repo"

case "$prompt" in
  *"Implement ONLY the selected open roadmap item below."*)
    perl -0pi -e 's/- \[ \] 1\.0 Ship sample feature/- [x] 1.0 Ship sample feature/' roadmap/sample/index.md
    printf 'implemented\n' > implementation.txt
    cat >"$out_file" <<'JSON'
{"itemTitle":"1.0 Ship sample feature","commitMessage":"feat: implement sample feature","reviewReasoning":"low","summary":"implemented"}
JSON
    ;;
  *"Review the current uncommitted diff for the selected roadmap item."*)
    cat >"$out_file" <<'JSON'
{"approved":"true","notes":"ready"}
JSON
    ;;
  *"Confirm by inspecting the codebase, tests, and current documentation"*)
    cat >"$out_file" <<'JSON'
[{"id":"c-1","title":"Add correctness marker","details":"create correctness.txt","reasoning":"low"}]
JSON
    ;;
  *"Apply ONLY this queued correctness item in the repository."*)
    printf 'correctness\n' > correctness.txt
    cat >"$out_file" <<'JSON'
{"itemId":"c-1","commitMessage":"fix: add correctness marker","summary":"applied"}
JSON
    ;;
  *"Review the current uncommitted diff for the queued refactor item."*)
    cat >"$out_file" <<'JSON'
{"approved":"true","notes":"refactor diff looks sane"}
JSON
    ;;
  *"Update documentation for the completed roadmap work."*)
    mkdir -p docs
    printf '# Current State\n' > docs/current-state.md
    rm -f roadmap/sample/index.md
    cat >"$out_file" <<'JSON'
{"commitMessage":"docs: finalize sample feature"}
JSON
    ;;
  *)
    printf 'unexpected codex prompt\n' >&2
    exit 4
    ;;
esac
EOF

cat >"${bin_dir}/claude" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail

prompt="$(cat)"

case "$prompt" in
  *"Review the codebase related to the implemented roadmap items."*)
    cat <<'JSON'
```json
[{"id":"r-1","title":"Add refactor marker","details":"create refactor.txt","reasoning":"low"}]
```
JSON
    ;;
  *"Apply ONLY this queued refactor item in the repository."*)
    printf 'refactor\n' > refactor.txt
    cat <<'JSON'
```json
{"itemId":"r-1","commitMessage":"refactor: add refactor marker","summary":"created refactor marker and ran targeted checks"}
```
JSON
    ;;
  *)
    printf 'unexpected claude prompt\n' >&2
    exit 4
    ;;
esac
EOF

chmod +x "${bin_dir}/codex" "${bin_dir}/claude"

git -C "$repo_dir" init -q
git -C "$repo_dir" config user.email smoke@example.com
git -C "$repo_dir" config user.name smoke

PATH="${bin_dir}:$PATH" rtk dagu start "$DAG_FILE" -- \
  REPO_DIR="$repo_dir" \
  ROADMAP_FILE=roadmap/sample/index.md \
  STATE_DIR=.amata \
  SCRIPTS_DIR="$SCRIPTS_DIR" \
  CODEX_MODEL=fake-codex \
  CODEX_CORRECTNESS_REASONING=low \
  CODEX_DOCS_REASONING=low \
  CLAUDE_MODEL=fake-claude \
  CLAUDE_REFACTOR_REASONING=low

[ -f "${repo_dir}/implementation.txt" ]
[ -f "${repo_dir}/correctness.txt" ]
[ -f "${repo_dir}/refactor.txt" ]
[ -f "${repo_dir}/docs/current-state.md" ]
[ ! -e "${repo_dir}/roadmap/sample/index.md" ]

commit_count="$(git -C "$repo_dir" rev-list --count HEAD)"
[ "$commit_count" = "4" ]

if git -C "$repo_dir" ls-files --error-unmatch .amata/queues/correctness.json >/dev/null 2>&1; then
  printf '.amata queue file was committed\n' >&2
  exit 1
fi

printf 'smoke test passed\n'
