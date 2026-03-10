#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
. "${SCRIPT_DIR}/common.sh"

repo=""
roadmap_file=""
state_dir=""
scripts_dir="${SCRIPT_DIR}"
model=""
claude_model=""
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
    --claude-model)
      [ "$#" -ge 2 ] || die "--claude-model requires a value"
      claude_model="$2"
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
[ -n "$claude_model" ] || die "--claude-model is required"
[ -n "$reasoning" ] || die "--reasoning is required"

review_file="$(mktemp)"
validated_items_file="$(mktemp)"
phase_file="$(mktemp)"
trap 'rm -f "$review_file" "$validated_items_file" "$phase_file"' EXIT

cat <<PROMPT | rtk bash "${scripts_dir}/run-claude-prompt.sh" \
  --repo "$repo" \
  --model "$claude_model" \
  --reasoning "$reasoning" >"$review_file"
Repository root: ${repo}
Roadmap file: ${roadmap_file}

Review the codebase related to the implemented roadmap items.
Find real refactoring targets for redundancy, overengineering, dead code, unnecessary boilerplate,
or files that should be split because they mix domains or grew too large.
Output ONLY valid JSON array.
Each item must be an object like:
  {"id":"r-1","title":"...","details":"...","reasoning":"low|medium|high|xhigh"}
Return [] if nothing materially improves the implementation.
PROMPT

rtk jq -ce \
  'if type=="array"
   then .
   else error("expected JSON array")
   end' \
  "$review_file" >"$validated_items_file"

rtk bash "${scripts_dir}/write-queue.sh" \
  --queue "${state_dir}/queues/refactor.json" \
  --source "$validated_items_file" \
  --kind refactor >/dev/null

rtk bash "${scripts_dir}/fix-queue.sh" \
  --repo "$repo" \
  --roadmap-file "$roadmap_file" \
  --scripts-dir "$scripts_dir" \
  --apply-runner claude \
  --apply-model "$claude_model" \
  --review-runner codex \
  --review-model "$model" \
  --queue "${state_dir}/queues/refactor.json" \
  --phase-label refactor \
  --state-dir "$state_dir" >"$phase_file"

cat "$phase_file"
