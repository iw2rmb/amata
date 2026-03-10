#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
. "${SCRIPT_DIR}/common.sh"

doc=""

while [ "$#" -gt 0 ]; do
  case "$1" in
    --doc)
      [ "$#" -ge 2 ] || die "--doc requires a value"
      doc="$2"
      shift 2
      ;;
    *)
      die "unknown argument: $1"
      ;;
  esac
done

[ -n "$doc" ] || die "--doc is required"

items_json="$(roadmap_items_json "$doc")"
next_item="$(printf '%s' "$items_json" | jq -ce 'map(select(.checked | not)) | first // {}')"
has_work=false

if [ "$next_item" != "{}" ]; then
  has_work=true
fi

payload="$(jq -cn --arg hasWork "$has_work" --argjson item "$next_item" '
  if $hasWork == "true" then
    $item + {hasWork: $hasWork}
  else
    {hasWork: $hasWork}
  end
')"

jq -cn --argjson nextItem "$payload" '{nextItem: $nextItem}'
