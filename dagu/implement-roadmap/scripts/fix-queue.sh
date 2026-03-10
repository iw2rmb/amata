#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
. "${SCRIPT_DIR}/common.sh"

repo=""
roadmap_file=""
scripts_dir="${SCRIPT_DIR}"
queue=""
phase_label=""
state_dir=""
apply_runner=""
apply_model=""
review_runner=""
review_model=""

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
      apply_model="$2"
      shift 2
      ;;
    --apply-runner)
      [ "$#" -ge 2 ] || die "--apply-runner requires a value"
      apply_runner="$2"
      shift 2
      ;;
    --apply-model)
      [ "$#" -ge 2 ] || die "--apply-model requires a value"
      apply_model="$2"
      shift 2
      ;;
    --review-runner)
      [ "$#" -ge 2 ] || die "--review-runner requires a value"
      review_runner="$2"
      shift 2
      ;;
    --review-model)
      [ "$#" -ge 2 ] || die "--review-model requires a value"
      review_model="$2"
      shift 2
      ;;
    --queue)
      [ "$#" -ge 2 ] || die "--queue requires a value"
      queue="$2"
      shift 2
      ;;
    --phase-label)
      [ "$#" -ge 2 ] || die "--phase-label requires a value"
      phase_label="$2"
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
[ -n "$queue" ] || die "--queue is required"
[ -n "$phase_label" ] || die "--phase-label is required"
[ -n "$state_dir" ] || die "--state-dir is required"
[ -n "$apply_runner" ] || die "--apply-runner is required"
[ -n "$apply_model" ] || die "--apply-model is required"

if [ -n "$review_runner" ] && [ -z "$review_model" ]; then
  die "--review-model is required when --review-runner is set"
fi
if [ -z "$review_runner" ] && [ -n "$review_model" ]; then
  die "--review-runner is required when --review-model is set"
fi

next_item_file="$(mktemp)"
apply_result_file="$(mktemp)"
review_result_file="$(mktemp)"
phase_file="$(mktemp)"
trap 'rm -f "$next_item_file" "$apply_result_file" "$review_result_file" "$phase_file"' EXIT

apply_runner_script="$(prompt_runner_script "$scripts_dir" "$apply_runner")"
review_runner_script=""
if [ -n "$review_runner" ]; then
  review_runner_script="$(prompt_runner_script "$scripts_dir" "$review_runner")"
fi

while true; do
  rtk bash "${scripts_dir}/next-queue-item.sh" --queue "$queue" >"$next_item_file"

  has_work="$(rtk jq -r '.nextItem.hasWork // "false"' "$next_item_file")"
  if [ "$has_work" != "true" ]; then
    break
  fi

  item_payload="$(rtk jq -ce '.nextItem' "$next_item_file")"
  item_id="$(rtk jq -r '.nextItem.id' "$next_item_file")"
  reasoning="$(rtk jq -r '.nextItem.reasoning' "$next_item_file")"

  cat <<PROMPT | rtk bash "$apply_runner_script" \
    --repo "$repo" \
    --model "$apply_model" \
    --reasoning "$reasoning" >"$apply_result_file"
Repository root: ${repo}
Roadmap file: ${roadmap_file}

Apply ONLY this queued ${phase_label} item in the repository.
Use the queue item's details and requested reasoning level.
Keep the change minimal and targeted.
Run relevant tests/checks.
Leave everything uncommitted.
Output ONLY valid JSON:
  {"itemId":"...","commitMessage":"...","summary":"..."}

Queue item JSON:
${item_payload}
PROMPT

  rtk jq -ce \
    'if type=="object" and has("itemId") and has("commitMessage") and has("summary")
     then .
     else error("expected object with itemId, commitMessage, and summary")
     end' \
    "$apply_result_file" >/dev/null

  apply_item_id="$(rtk jq -r '.itemId' "$apply_result_file")"
  [ "$apply_item_id" = "$item_id" ] || die "queued ${phase_label} item ID mismatch: expected $item_id, got $apply_item_id"

  commit_message="$(rtk jq -r '.commitMessage' "$apply_result_file")"
  apply_summary="$(rtk jq -r '.summary' "$apply_result_file")"

  if [ -n "$review_runner_script" ]; then
    cat <<PROMPT | rtk bash "$review_runner_script" \
      --repo "$repo" \
      --model "$review_model" \
      --reasoning "$reasoning" >"$review_result_file"
Repository root: ${repo}
Roadmap file: ${roadmap_file}
Requested review reasoning: ${reasoning}

Review the current uncommitted diff for the queued ${phase_label} item.
Stay within the queued item's scope.
Use the queued item details and implementer summary as context, but validate the actual diff and checks.
You may patch the code directly if you find issues, then rerun any checks needed for confidence.
When the diff is ready to commit, output ONLY valid JSON:
   {"approved":"true","notes":"..."}

Queued ${phase_label} item JSON:
${item_payload}

Implementer summary:
${apply_summary}

Implementer result JSON:
$(cat "$apply_result_file")

Proposed commit message:
${commit_message}
PROMPT

    rtk jq -ce \
      'if type=="object" and .approved == "true"
       then .
       else error("review not approved")
       end' \
      "$review_result_file" >/dev/null
  fi

  rtk bash "${scripts_dir}/commit-if-changed.sh" \
    --exclude-path "$state_dir" \
    "$commit_message" >/dev/null

  rtk bash "${scripts_dir}/mark-queue-item-done.sh" \
    --queue "$queue" \
    --item-id "$item_id" >/dev/null
done

rtk bash "${scripts_dir}/queue-has-more.sh" --queue "$queue" >"$phase_file"
cat "$phase_file"
