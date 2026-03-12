#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
. "${SCRIPT_DIR}/common.sh"

repo=""
doc=""
scripts_dir="${SCRIPT_DIR}"
model=""
state_dir=""

while [ "$#" -gt 0 ]; do
  case "$1" in
    --repo)
      [ "$#" -ge 2 ] || die "--repo requires a value"
      repo="$2"
      shift 2
      ;;
    --doc)
      [ "$#" -ge 2 ] || die "--doc requires a value"
      doc="$2"
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
[ -n "$doc" ] || die "--doc is required"
[ -n "$model" ] || die "--model is required"
[ -n "$state_dir" ] || die "--state-dir is required"

repo="$(resolve_path_from "$PWD" "$repo")"
require_dir "$repo"
doc="$(resolve_path_from "$repo" "$doc")"
require_file "$doc"
scripts_dir="$(resolve_path_from "$PWD" "$scripts_dir")"
require_dir "$scripts_dir"

next_item_file="$(mktemp)"
implement_result_file="$(mktemp)"
review_result_file="$(mktemp)"
phase_file="$(mktemp)"
trap 'rm -f "$next_item_file" "$implement_result_file" "$review_result_file" "$phase_file"' EXIT

while true; do
  bash "${scripts_dir}/next-open-item.sh" --doc "$doc" >"$next_item_file"

  has_work="$(jq -r '.nextItem.hasWork // "false"' "$next_item_file")"
  if [ "$has_work" != "true" ]; then
    break
  fi

  item_payload="$(jq -ce '.nextItem' "$next_item_file")"
  item_title="$(jq -r '.nextItem.title' "$next_item_file")"
  reasoning="$(jq -r '.nextItem.reasoning' "$next_item_file")"

  cat <<PROMPT | bash "${scripts_dir}/run-codex-prompt.sh" \
    --repo "$repo" \
    --model "$model" \
    --state-dir "$state_dir" \
    --reasoning "$reasoning" >"$implement_result_file"
Repository root: ${repo}
Roadmap file: ${doc}

Implement ONLY the selected open roadmap item below.

Requirements:
1. Change code and tests as needed.
2. Run the checks described in the roadmap item. If no checks are listed, run the most relevant focused validation.
3. Update the roadmap file and mark only this item done.
4. Do not start later roadmap items.
5. Leave everything uncommitted.
6. Output ONLY valid JSON:
   {"itemTitle":"...","commitMessage":"...","reviewReasoning":"low|medium|high|xhigh","summary":"..."}

Selected open roadmap item JSON:
${item_payload}
PROMPT

  jq -ce \
    'if type=="object"
        and has("itemTitle")
        and has("commitMessage")
        and has("reviewReasoning")
     then .
     else error("expected object with itemTitle, commitMessage, and reviewReasoning")
     end' \
    "$implement_result_file" >/dev/null

  review_reasoning="$(jq -r '.reviewReasoning' "$implement_result_file")"
  commit_message="$(jq -r '.commitMessage' "$implement_result_file")"

  cat <<PROMPT | bash "${scripts_dir}/run-codex-prompt.sh" \
    --repo "$repo" \
    --model "$model" \
    --state-dir "$state_dir" \
    --reasoning "$review_reasoning" >"$review_result_file"
Repository root: ${repo}
Roadmap file: ${doc}
Requested review reasoning: ${review_reasoning}

Review the current uncommitted diff for the selected roadmap item.
Stay within the selected item's scope.
You may patch the code directly if you find issues, then rerun any checks needed for confidence.
When the diff is ready to commit, output ONLY valid JSON:
   {"approved":true,"notes":"..."}

Selected roadmap item JSON:
${item_payload}

Proposed commit message:
${commit_message}
PROMPT

  jq -ce \
    'if type=="object" and .approved == true and has("notes")
     then .
     else error("review not approved")
     end' \
    "$review_result_file" >/dev/null

  bash "${scripts_dir}/commit-if-changed.sh" \
    --exclude-path "$state_dir" \
    "$commit_message" >/dev/null

  item_checked="$(
    roadmap_items_json "$doc" | jq -r --arg title "$item_title" '
      [ .[] | select(.title == $title) ] as $matches
      | if ($matches | length) != 1 then
          error("expected exactly one roadmap item for title")
        else
          ($matches[0].checked | tostring)
        end
    '
  )"

  [ "$item_checked" = "true" ] || die "selected roadmap item was not marked done: $item_title"
done

bash "${scripts_dir}/remaining-open-items.sh" --doc "$doc" >"$phase_file"
cat "$phase_file"
