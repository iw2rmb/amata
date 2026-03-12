#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
. "${SCRIPT_DIR}/common.sh"

repo=""
model=""
reasoning=""

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
    *)
      die "unknown argument: $1"
      ;;
  esac
done

[ -n "$repo" ] || die "--repo is required"
[ -n "$model" ] || die "--model is required"
[ -n "$reasoning" ] || die "--reasoning is required"

repo="$(CDPATH= cd -- "$repo" && pwd)"

require_command codex

prompt_file="$(mktemp)"
out_file="$(mktemp)"
log_file="$(mktemp)"
trap 'rm -f "$prompt_file" "$out_file" "$log_file"' EXIT

cat >"$prompt_file"

if ! codex exec \
  --dangerously-bypass-approvals-and-sandbox \
  --color never \
  -C "$repo" \
  --model "$model" \
  -c "model_reasoning_effort=\"$reasoning\"" \
  -o "$out_file" \
  - <"$prompt_file" >"$log_file" 2>&1; then
  cat "$log_file" >&2
  exit 1
fi

[ -s "$out_file" ] || die "codex exec did not write a final message"
cat "$out_file"
