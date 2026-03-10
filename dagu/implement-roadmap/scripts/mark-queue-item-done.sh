#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
. "${SCRIPT_DIR}/common.sh"

queue=""
item_id=""

while [ "$#" -gt 0 ]; do
  case "$1" in
    --queue)
      [ "$#" -ge 2 ] || die "--queue requires a value"
      queue="$2"
      shift 2
      ;;
    --item-id)
      [ "$#" -ge 2 ] || die "--item-id requires a value"
      item_id="$2"
      shift 2
      ;;
    *)
      die "unknown argument: $1"
      ;;
  esac
done

[ -n "$queue" ] || die "--queue is required"
[ -n "$item_id" ] || die "--item-id is required"
require_file "$queue"

match_count="$(jq -r --arg id "$item_id" '
  if (.items | type) != "array" then
    error("expected queue file with items array")
  else
    [.items[] | select((.id | tostring) == $id)] | length
  end
' "$queue")"

[ "$match_count" -gt 0 ] || die "queue item not found: $item_id"

tmp_queue="$(mktemp)"

jq --arg id "$item_id" '
  .items |= map(
    if (.id | tostring) == $id then
      . + {done: true}
    else
      .
    end
  )
' "$queue" >"$tmp_queue"

mv "$tmp_queue" "$queue"

jq -cn --arg itemId "$item_id" '{mark: {itemId: $itemId, done: "true"}}'
