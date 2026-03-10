#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
. "${SCRIPT_DIR}/common.sh"

queue=""
source_file=""
kind=""

while [ "$#" -gt 0 ]; do
  case "$1" in
    --queue)
      [ "$#" -ge 2 ] || die "--queue requires a value"
      queue="$2"
      shift 2
      ;;
    --source)
      [ "$#" -ge 2 ] || die "--source requires a value"
      source_file="$2"
      shift 2
      ;;
    --kind)
      [ "$#" -ge 2 ] || die "--kind requires a value"
      kind="$2"
      shift 2
      ;;
    *)
      die "unknown argument: $1"
      ;;
  esac
done

[ -n "$queue" ] || die "--queue is required"
[ -n "$source_file" ] || die "--source is required"
[ -n "$kind" ] || die "--kind is required"

require_file "$source_file"
mkdir -p "$(dirname -- "$queue")"

tmp_queue="$(mktemp)"

jq -ce --arg kind "$kind" '
  if type != "array" then
    error("expected JSON array")
  else
    {
      kind: $kind,
      items: (
        to_entries
        | map(
            .value
            | if type != "object" or (has("id") | not) or (has("title") | not) then
                error("expected queue item object with id and title")
              else
                {
                  id: (.id | tostring),
                  title: (.title | tostring),
                  details: ((.details // "") | tostring),
                  reasoning: ((.reasoning // "high") | tostring),
                  done: false,
                  sourceIndex: (.key + 1)
                }
              end
          )
      )
    }
  end
' "$source_file" >"$tmp_queue"

mv "$tmp_queue" "$queue"

item_count="$(jq -r '.items | length' "$queue")"
jq -cn \
  --arg kind "$kind" \
  --arg queue "$queue" \
  --arg count "$item_count" \
  '{queue: {kind: $kind, path: $queue, count: $count}}'
