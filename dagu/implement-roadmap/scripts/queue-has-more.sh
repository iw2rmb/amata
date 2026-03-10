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

remaining_count="$(jq -r '
  if (.items | type) != "array" then
    error("expected queue file with items array")
  else
    [.items[] | select(.done != true)] | length
  end
' "$queue")"

has_more=false
if [ "$remaining_count" -gt 0 ]; then
  has_more=true
fi

jq -cn \
  --arg hasMore "$has_more" \
  --arg remaining "$remaining_count" \
  '{phase: {hasMore: $hasMore, remaining: $remaining}}'
