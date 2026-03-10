#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
. "${SCRIPT_DIR}/common.sh"

queue=""

while [ "$#" -gt 0 ]; do
  case "$1" in
    --queue)
      [ "$#" -ge 2 ] || die "--queue requires a value"
      queue="$2"
      shift 2
      ;;
    *)
      die "unknown argument: $1"
      ;;
  esac
done

[ -n "$queue" ] || die "--queue is required"
require_file "$queue"

next_item="$(jq -ce '
  if (.items | type) != "array" then
    error("expected queue file with items array")
  else
    (.items | map(select(.done != true)) | first // {})
  end
' "$queue")"

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
