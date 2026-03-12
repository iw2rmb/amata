#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(CDPATH= cd -- "$(dirname -- "$0")/../../.." && pwd)"
RUNNER="${ROOT_DIR}/dagu/implement-roadmap/scripts/run-claude-prompt.sh"

tmp_dir="$(mktemp -d)"
trap 'rm -rf "$tmp_dir"' EXIT

repo_dir="${tmp_dir}/repo"
bin_dir="${tmp_dir}/bin"
mkdir -p "$repo_dir" "$bin_dir"

cat >"${bin_dir}/claude" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail

cat >/dev/null
cat <<'TEXT'
Review complete.
```json
{"approved":true,"notes":"refactor looks good"}
```
TEXT
EOF

chmod +x "${bin_dir}/claude"

output="$(
  PATH="${bin_dir}:$PATH" \
  bash "$RUNNER" \
    --repo "$repo_dir" \
    --model fake-claude \
    --reasoning low <<'PROMPT'
Return ONLY valid JSON.
PROMPT
)"

[ "$output" = '{"approved":true,"notes":"refactor looks good"}' ]

printf 'run-claude-prompt JSON extraction test passed\n'
