#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
. "${SCRIPT_DIR}/common.sh"

repo=""
roadmap_file=""
state_dir=""
scripts_dir="${SCRIPT_DIR}"
model=""
reasoning=""

while [ "$#" -gt 0 ]; do
  case "$1" in
    --repo)
      [ "$#" -ge 2 ] || die "--repo requires a value"
      repo="$2"
      shift 2
      ;;
    --roadmap-file)
      [ "$#" -ge 2 ] || die "--roadmap-file requires a value"
      roadmap_file="$2"
      shift 2
      ;;
    --state-dir)
      [ "$#" -ge 2 ] || die "--state-dir requires a value"
      state_dir="$2"
      shift 2
      ;;
    --scripts-dir)
      [ "$#" -ge 2 ] || die "--scripts-dir requires a value"
      scripts_dir="$2"
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
[ -n "$roadmap_file" ] || die "--roadmap-file is required"
[ -n "$state_dir" ] || die "--state-dir is required"
[ -n "$model" ] || die "--model is required"
[ -n "$reasoning" ] || die "--reasoning is required"

repo="$(resolve_path_from "$PWD" "$repo")"
require_dir "$repo"
roadmap_file="$(resolve_path_from "$repo" "$roadmap_file")"
require_file "$roadmap_file"
scripts_dir="$(resolve_path_from "$PWD" "$scripts_dir")"
require_dir "$scripts_dir"

review_file="$(mktemp)"
trap 'rm -f "$review_file"' EXIT

cat <<PROMPT | bash "${scripts_dir}/run-codex-prompt.sh" \
  --repo "$repo" \
  --model "$model" \
  --state-dir "$state_dir" \
  --reasoning "$reasoning" >"$review_file"
Repository root: ${repo}
Roadmap file: ${roadmap_file}

Confirm by inspecting the codebase, tests, and current documentation that the roadmap work is wired end-to-end,
implemented correctly and in full, and leaves no leftover or partially applied work. Address gaps, if any.
Do not commit.
Output ONLY valid JSON:
  {"approved":true,"notes":"..."}
PROMPT

jq -ce \
  'if type=="object" and .approved == true and has("notes")
   then .
   else error("review not approved")
   end' \
  "$review_file" >/dev/null

cat "$review_file"
