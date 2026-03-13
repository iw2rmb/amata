#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
. "${SCRIPT_DIR}/common.sh"

repo=""
roadmap_file=""
scripts_dir="${SCRIPT_DIR}"
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
    --roadmap-file)
      [ "$#" -ge 2 ] || die "--roadmap-file requires a value"
      roadmap_file="$2"
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
[ -n "$roadmap_file" ] || die "--roadmap-file is required"
[ -n "$model" ] || die "--model is required"
[ -n "$reasoning" ] || die "--reasoning is required"
[ -n "$state_dir" ] || die "--state-dir is required"

repo="$(resolve_path_from "$PWD" "$repo")"
require_dir "$repo"
roadmap_file="$(resolve_path_from "$repo" "$roadmap_file")"
require_file "$roadmap_file"
scripts_dir="$(resolve_path_from "$PWD" "$scripts_dir")"
require_dir "$scripts_dir"

review_file="$(mktemp)"
commit_result_file="$(mktemp)"
trap 'rm -f "$review_file" "$commit_result_file"' EXIT

cat <<PROMPT | bash "${scripts_dir}/run-codex-prompt.sh" \
  --repo "$repo" \
  --model "$model" \
  --state-dir "$state_dir" \
  --reasoning "$reasoning" >"$review_file"
Repository root: ${repo}
Roadmap file: ${roadmap_file}

Review the current uncommitted diff.
Check for scope drift, regressions, half-applied edits, or architectural mistakes.
Address findings, if any.
Do not commit.
Output ONLY valid JSON:
  {"approved":true,"notes":"...","commitMessage":"..."}
PROMPT

jq -ce \
  'if type=="object" and .approved == true and has("notes") and has("commitMessage")
   then .
   else error("sanity review not approved")
   end' \
  "$review_file" >/dev/null

commit_message="$(jq -r '.commitMessage' "$review_file")"

bash "${scripts_dir}/commit-if-changed.sh" \
  --exclude-path "$state_dir" \
  "$commit_message" >"$commit_result_file"

jq -n \
  --slurpfile review "$review_file" \
  --slurpfile commit "$commit_result_file" \
  '{review: $review[0], commit: $commit[0].commit}'
