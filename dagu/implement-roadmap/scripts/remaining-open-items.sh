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

open_count="$(roadmap_items_json "$doc" | jq -r '[.[] | select(.checked | not)] | length')"
has_more=false

if [ "$open_count" -gt 0 ]; then
  has_more=true
fi

jq -cn \
  --arg hasMore "$has_more" \
  --arg openCount "$open_count" \
  '{phase: {hasMore: $hasMore, openCount: $openCount}}'
