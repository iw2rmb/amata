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

require_command claude

prompt_file="$(mktemp)"
out_file="$(mktemp)"
log_file="$(mktemp)"
trap 'rm -f "$prompt_file" "$out_file" "$log_file"' EXIT

cat >"$prompt_file"

cd "$repo"

if ! claude -p \
  --output-format text \
  --permission-mode bypassPermissions \
  --model "$model" \
  --effort "$reasoning" \
  <"$prompt_file" >"$out_file" 2>"$log_file"; then
  cat "$log_file" >&2
  exit 1
fi

[ -s "$out_file" ] || die "claude -p did not produce output"
normalize_markdown_fence_file "$out_file"
