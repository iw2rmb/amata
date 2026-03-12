#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(CDPATH= cd -- "$(dirname -- "$0")/../../.." && pwd)"
RUNNER="${ROOT_DIR}/dagu/implement-roadmap/scripts/run-codex-prompt.sh"

tmp_dir="$(mktemp -d)"
trap 'rm -rf "$tmp_dir"' EXIT

repo_dir="${tmp_dir}/repo"
bin_dir="${tmp_dir}/bin"
home_dir="${tmp_dir}/home"
mkdir -p "$repo_dir" "$bin_dir" "$home_dir"

cat >"${bin_dir}/codex" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail

[ "${1:-}" = "exec" ] || exit 2
shift

out_file=""
prompt_from_stdin=false
while [ "$#" -gt 0 ]; do
  case "$1" in
    -o)
      out_file="$2"
      shift 2
      ;;
    --dangerously-bypass-approvals-and-sandbox)
      shift
      ;;
    --color|--model|-c|-C)
      shift 2
      ;;
    -)
      prompt_from_stdin=true
      shift
      ;;
    *)
      shift
      ;;
  esac
done

[ -n "$out_file" ] || exit 3

if [ "$prompt_from_stdin" = true ]; then
  prompt="$(cat)"
else
  prompt=""
fi

printf '%s\n' "$prompt" >"${out_file}.prompt"
cat >"$out_file" <<'TEXT'
Review completed successfully.
{"approved":"true","notes":"completed without recovery"}
TEXT
EOF

chmod +x "${bin_dir}/codex"

output="$(
  PATH="${bin_dir}:$PATH" \
  HOME="${home_dir}" \
  bash "$RUNNER" \
    --repo "$repo_dir" \
    --model fake-codex \
    --reasoning low \
    --state-dir .amata <<'PROMPT'
Return ONLY valid JSON.
PROMPT
)"

[ "$output" = '{"approved":"true","notes":"completed without recovery"}' ]

run_dir=""
for candidate in "${repo_dir}/.amata/codex-runs"/*; do
  [ -d "$candidate" ] || continue
  run_dir="$candidate"
  break
done
[ -n "$run_dir" ]
[ -f "${run_dir}/attempt-1.log" ]
[ ! -e "${run_dir}/attempt-2.log" ]
[ -f "${run_dir}/prompt.txt" ]
[ -f "${run_dir}/last-message.txt" ]
[ -f "${run_dir}/watchdog.log" ]
[ -f "${run_dir}/last-message.txt.prompt" ]
[ "$(cat "${run_dir}/last-message.txt.prompt")" = "Return ONLY valid JSON." ]
rg -q 'completed attempts=1 recoveries=0' "${run_dir}/watchdog.log"

printf 'run-codex-prompt success test passed\n'
