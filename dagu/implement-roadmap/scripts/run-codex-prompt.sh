#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
. "${SCRIPT_DIR}/common.sh"

repo=""
model=""
reasoning=""
state_dir=""

while [ "$#" -gt 0 ]; do
  case "$1" in
    --repo)
      [ "$#" -ge 2 ] || die "--repo requires a value"
      repo="$2"
      shift 2
      ;;
    --model)
      [ "$#" -ge 2 ] || die "--model requires a value"
      model="$2"
      shift 2
      ;;
    --reasoning)
      [ "$#" -ge 2 ] || die "--reasoning requires a value"
      reasoning="$2"
      shift 2
      ;;
    --state-dir)
      [ "$#" -ge 2 ] || die "--state-dir requires a value"
      state_dir="$2"
      shift 2
      ;;
    *)
      die "unknown argument: $1"
      ;;
  esac
done

[ -n "$repo" ] || die "--repo is required"
[ -n "$model" ] || die "--model is required"
[ -n "$reasoning" ] || die "--reasoning is required"

repo="$(CDPATH= cd -- "$repo" && pwd)"

require_command rtk
require_command codex
require_command lsof

is_positive_integer() {
  [[ "$1" =~ ^[0-9]+$ ]] && [ "$1" -gt 0 ]
}

is_non_negative_integer() {
  [[ "$1" =~ ^[0-9]+$ ]]
}

file_mtime_epoch() {
  local file="$1"

  if [ ! -e "$file" ]; then
    printf '0\n'
  elif stat -f '%m' "$file" >/dev/null 2>&1; then
    stat -f '%m' "$file"
  else
    stat -c '%Y' "$file"
  fi
}

now_epoch() {
  date +%s
}

timestamp_utc() {
  date -u +%Y-%m-%dT%H:%M:%SZ
}

extract_uuid() {
  printf '%s\n' "$1" | perl -ne '
    if (/([0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12})/) {
      print "$1\n";
      exit;
    }
  '
}

idle_seconds="${CODEX_WATCHDOG_IDLE_SECONDS:-900}"
poll_seconds="${CODEX_WATCHDOG_POLL_SECONDS:-15}"
max_recoveries="${CODEX_WATCHDOG_MAX_RECOVERIES:-1}"

is_positive_integer "$idle_seconds" || die "CODEX_WATCHDOG_IDLE_SECONDS must be a positive integer"
is_positive_integer "$poll_seconds" || die "CODEX_WATCHDOG_POLL_SECONDS must be a positive integer"
is_non_negative_integer "$max_recoveries" || die "CODEX_WATCHDOG_MAX_RECOVERIES must be a non-negative integer"

persist_artifacts=false
if [ -n "$state_dir" ]; then
  case "$state_dir" in
    /*) runs_root="${state_dir}/codex-runs" ;;
    *) runs_root="${repo}/${state_dir}/codex-runs" ;;
  esac
  mkdir -p "$runs_root"
  run_dir="${runs_root}/$(date -u +%Y%m%dT%H%M%SZ)-$$"
  mkdir -p "$run_dir"
  persist_artifacts=true
else
  run_dir="$(mktemp -d)"
fi

prompt_file="${run_dir}/prompt.txt"
out_file="${run_dir}/last-message.txt"
watchdog_log_file="${run_dir}/watchdog.log"
session_id_file="${run_dir}/session-id.txt"
session_file_record="${run_dir}/session-file.txt"

attempt_number=0
active_launcher_pid=""
active_exec_pid=""
active_session_file=""
active_session_id=""
active_last_activity=0
current_log_file=""
recovery_count=0

log_status() {
  printf '%s %s\n' "$(timestamp_utc)" "$*" >>"$watchdog_log_file"
}

discover_session_artifacts() {
  local discovered_file=""
  local discovered_id=""

  [ -n "$active_launcher_pid" ] || return 0

  if [ -z "$active_exec_pid" ]; then
    active_exec_pid="$(
      rtk pgrep -P "$active_launcher_pid" 2>/dev/null | rtk sed -n '1p' || true
    )"
    if [ -n "$active_exec_pid" ]; then
      log_status "attempt=${attempt_number} discovered_exec_pid=${active_exec_pid}"
    fi
  fi

  [ -n "$active_exec_pid" ] || return 0

  discovered_file="$(
    rtk lsof -p "$active_exec_pid" 2>/dev/null \
      | rtk awk '/\/\.codex\/sessions\/.*\.jsonl$/ { print $NF; exit }' \
      || true
  )"

  if [ -n "$discovered_file" ]; then
    active_session_file="$discovered_file"
    printf '%s\n' "$active_session_file" >"$session_file_record"
  fi

  if [ -n "$active_session_id" ] || [ -z "$active_session_file" ]; then
    return 0
  fi

  discovered_id="$(
    rtk sed -n '1p' "$active_session_file" 2>/dev/null \
      | rtk jq -r 'select(.type == "session_meta") | .payload.id // empty' 2>/dev/null \
      | rtk sed -n '1p' \
      || true
  )"

  if [ -z "$discovered_id" ]; then
    discovered_id="$(extract_uuid "$active_session_file" || true)"
  fi

  if [ -n "$discovered_id" ]; then
    active_session_id="$discovered_id"
    printf '%s\n' "$active_session_id" >"$session_id_file"
    log_status "attempt=${attempt_number} discovered_session_id=${active_session_id}"
  fi
}

latest_activity_epoch() {
  local latest=0
  local file=""
  local candidate=0

  for file in "$current_log_file" "$out_file" "$active_session_file"; do
    [ -n "$file" ] || continue
    candidate="$(file_mtime_epoch "$file")"
    if [ "$candidate" -gt "$latest" ]; then
      latest="$candidate"
    fi
  done

  printf '%s\n' "$latest"
}

terminate_active_process() {
  local launcher_pid="${active_launcher_pid:-}"
  local exec_pid="${active_exec_pid:-}"
  local waited=0

  if [ -n "$exec_pid" ] && kill -0 "$exec_pid" 2>/dev/null; then
    kill "$exec_pid" 2>/dev/null || true
  fi

  [ -n "$launcher_pid" ] || return

  if kill -0 "$launcher_pid" 2>/dev/null; then
    kill "$launcher_pid" 2>/dev/null || true
    while kill -0 "$launcher_pid" 2>/dev/null && [ "$waited" -lt 5 ]; do
      sleep 1
      waited=$((waited + 1))
    done
    if kill -0 "$launcher_pid" 2>/dev/null; then
      kill -KILL "$launcher_pid" 2>/dev/null || true
    fi
  fi

  if [ -n "$exec_pid" ] && kill -0 "$exec_pid" 2>/dev/null; then
    kill -KILL "$exec_pid" 2>/dev/null || true
  fi

  wait "$launcher_pid" 2>/dev/null || true
  active_launcher_pid=""
  active_exec_pid=""
}

start_exec_attempt() {
  local mode="$1"
  local resume_prompt_file=""

  attempt_number=$((attempt_number + 1))
  current_log_file="${run_dir}/attempt-${attempt_number}.log"
  : >"$current_log_file"
  : >"$out_file"
  active_session_file=""
  active_exec_pid=""

  if [ "$mode" = "initial" ]; then
    log_status "attempt=${attempt_number} mode=exec started"
    rtk codex exec \
      --dangerously-bypass-approvals-and-sandbox \
      --color never \
      -C "$repo" \
      --model "$model" \
      -c "model_reasoning_effort=\"$reasoning\"" \
      -o "$out_file" \
      - <"$prompt_file" >"$current_log_file" 2>&1 &
  else
    resume_prompt_file="${run_dir}/resume-attempt-${attempt_number}.txt"
    cat >"$resume_prompt_file" <<'PROMPT'
The previous non-interactive run appears stalled.
Continue from the existing repository state and the same Codex session.
Do not restart from scratch or discard prior work.
Finish the task and produce the required final response in the requested format.
PROMPT
    log_status "attempt=${attempt_number} mode=resume session_id=${active_session_id} started"
    rtk codex exec resume \
      --dangerously-bypass-approvals-and-sandbox \
      --model "$model" \
      -c "model_reasoning_effort=\"$reasoning\"" \
      -o "$out_file" \
      "$active_session_id" \
      - <"$resume_prompt_file" >"$current_log_file" 2>&1 &
  fi

  active_launcher_pid=$!
  active_last_activity="$(latest_activity_epoch)"
  log_status "attempt=${attempt_number} launcher_pid=${active_launcher_pid}"
}

cleanup() {
  if [ -n "${active_launcher_pid:-}" ] && kill -0 "$active_launcher_pid" 2>/dev/null; then
    terminate_active_process
  fi
}

cleanup_artifacts() {
  cleanup

  if [ "$persist_artifacts" != "true" ] && [ -n "${run_dir:-}" ] && [ -d "$run_dir" ]; then
    rm -rf "$run_dir"
  fi
}

trap cleanup_artifacts EXIT INT TERM

cat >"$prompt_file"

log_status "started repo=${repo} model=${model} reasoning=${reasoning}"
start_exec_attempt initial

exit_status=0
while true; do
  discover_session_artifacts

  if ! kill -0 "$active_launcher_pid" 2>/dev/null; then
    set +e
    wait "$active_launcher_pid"
    exit_status=$?
    set -e
    log_status "attempt=${attempt_number} launcher_pid=${active_launcher_pid} exited status=${exit_status}"
    active_launcher_pid=""
    active_exec_pid=""
    break
  fi

  current_activity="$(latest_activity_epoch)"
  if [ "$current_activity" -gt "$active_last_activity" ]; then
    active_last_activity="$current_activity"
  fi

  idle_for=$(( $(now_epoch) - active_last_activity ))
  if [ "$idle_for" -lt "$idle_seconds" ]; then
    sleep "$poll_seconds"
    continue
  fi

  if [ -z "$active_session_id" ]; then
    log_status "attempt=${attempt_number} launcher_pid=${active_launcher_pid} stalled_without_session_id idle=${idle_for}s"
    terminate_active_process
    die "codex exec stalled after ${idle_for}s of inactivity and no session ID was discovered; artifacts: $run_dir"
  fi

  if [ "$recovery_count" -ge "$max_recoveries" ]; then
    log_status "attempt=${attempt_number} launcher_pid=${active_launcher_pid} stalled idle=${idle_for}s recovery_limit=${max_recoveries}"
    terminate_active_process
    cat "$current_log_file" >&2
    die "codex exec stalled after ${idle_for}s of inactivity; session_id=${active_session_id}; artifacts: $run_dir"
  fi

  recovery_count=$((recovery_count + 1))
  log_status "attempt=${attempt_number} launcher_pid=${active_launcher_pid} stalled idle=${idle_for}s recovery=${recovery_count} session_id=${active_session_id}"
  terminate_active_process
  start_exec_attempt resume
done

if [ "$exit_status" -ne 0 ]; then
  cat "$current_log_file" >&2
  die "codex exec failed; artifacts: $run_dir"
fi

[ -s "$out_file" ] || die "codex exec did not write a final message; artifacts: $run_dir"
log_status "completed attempts=${attempt_number} recoveries=${recovery_count}"
cat "$out_file"
