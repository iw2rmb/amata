#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
. "${SCRIPT_DIR}/common.sh"

message=""
exclude_paths=()

while [ "$#" -gt 0 ]; do
  case "$1" in
    --exclude-path)
      [ "$#" -ge 2 ] || die "--exclude-path requires a value"
      exclude_paths+=("$2")
      shift 2
      ;;
    --)
      shift
      break
      ;;
    *)
      if [ -z "$message" ]; then
        message="$1"
        shift
      else
        die "unknown argument: $1"
      fi
      ;;
  esac
done

[ -n "$message" ] || die "commit message is required"
require_command git
require_command jq

if ! git rev-parse --is-inside-work-tree >/dev/null 2>&1; then
  die "current working directory is not a git repository"
fi

git add -A

for exclude_path in "${exclude_paths[@]}"; do
  git reset -q HEAD -- "$exclude_path" >/dev/null 2>&1 || true
done

if git diff --cached --quiet --exit-code; then
  jq -cn \
    --arg changed "false" \
    --arg message "$message" \
    '{commit: {changed: $changed, message: $message}}'
  exit 0
fi

git commit -m "$message" >/dev/null

sha="$(git rev-parse HEAD)"

jq -cn \
  --arg changed "true" \
  --arg message "$message" \
  --arg sha "$sha" \
  '{commit: {changed: $changed, message: $message, sha: $sha}}'
