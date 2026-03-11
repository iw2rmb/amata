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

command="${1:-}"
[ "$command" = "exec" ] || exit 2
shift

mode="exec"
if [ "${1:-}" = "resume" ]; then
  mode="resume"
  shift
fi

out_file=""
repo="."
resume_session_id=""
prompt_from_stdin=false
expected_session_id="019cda4a-7a02-7563-bb03-03d9f7f8e87d"

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
      prompt_from_stdin=true
      shift
      ;;
    *)
      if [ "$mode" = "resume" ] && [ -z "$resume_session_id" ]; then
        resume_session_id="$1"
      fi
      shift
      ;;
  esac
done

prompt=""
if [ "$prompt_from_stdin" = true ]; then
  prompt="$(cat)"
fi

[ -n "$out_file" ] || exit 3

if [ "$mode" = "exec" ]; then
  session_file="${HOME}/.codex/sessions/2026/03/11/rollout-2026-03-11T00-00-00-${expected_session_id}.jsonl"
  mkdir -p "$(dirname "$session_file")"
  exec 9>>"$session_file"
  printf '%s\n' '{"timestamp":"2026-03-11T00:00:00.000Z","type":"session_meta","payload":{"id":"019cda4a-7a02-7563-bb03-03d9f7f8e87d"}}' >&9
  printf 'starting stalled exec\n'
  printf '%s\n' "$prompt" >"${repo}/initial-prompt.txt"
  sleep 30
  exit 0
fi

[ "$resume_session_id" = "$expected_session_id" ] || exit 4
printf '%s\n' "$prompt" >"${repo}/resume-prompt.txt"
printf 'resumed exec\n'
cat >"$out_file" <<'JSON'
{"approved":"true","notes":"recovered after stall"}
JSON
EOF

chmod +x "${bin_dir}/codex"

output="$(
  PATH="${bin_dir}:$PATH" \
  HOME="${home_dir}" \
  CODEX_WATCHDOG_IDLE_SECONDS=2 \
  CODEX_WATCHDOG_POLL_SECONDS=1 \
  CODEX_WATCHDOG_MAX_RECOVERIES=1 \
  rtk bash "$RUNNER" \
    --repo "$repo_dir" \
    --model fake-codex \
    --reasoning low \
    --state-dir .amata <<'PROMPT'
Return ONLY valid JSON.
PROMPT
)"

[ "$output" = '{"approved":"true","notes":"recovered after stall"}' ]

run_dir=""
for candidate in "${repo_dir}/.amata/codex-runs"/*; do
  [ -d "$candidate" ] || continue
  run_dir="$candidate"
  break
done
[ -n "$run_dir" ]
[ -f "${run_dir}/attempt-1.log" ]
[ -f "${run_dir}/attempt-2.log" ]
[ -f "${run_dir}/prompt.txt" ]
[ -f "${run_dir}/resume-attempt-2.txt" ]
[ -f "${run_dir}/last-message.txt" ]
[ -f "${run_dir}/session-id.txt" ]
[ -f "${run_dir}/session-file.txt" ]
[ "$(cat "${run_dir}/session-id.txt")" = "019cda4a-7a02-7563-bb03-03d9f7f8e87d" ]
rtk rg -q 'stalled' "${run_dir}/watchdog.log"
rtk rg -q 'completed attempts=2 recoveries=1' "${run_dir}/watchdog.log"

printf 'watchdog recovery test passed\n'
