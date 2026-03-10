#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
. "${SCRIPT_DIR}/common.sh"

message="${1:-}"

[ -n "$message" ] || die "commit message is required"
require_command git
require_command jq

if ! git rev-parse --is-inside-work-tree >/dev/null 2>&1; then
  die "current working directory is not a git repository"
fi

if [ -z "$(git status --porcelain)" ]; then
  jq -cn \
    --arg changed "false" \
    --arg message "$message" \
    '{commit: {changed: $changed, message: $message}}'
  exit 0
fi

git add -A
git commit -m "$message" >/dev/null

sha="$(git rev-parse HEAD)"

jq -cn \
  --arg changed "true" \
  --arg message "$message" \
  --arg sha "$sha" \
  '{commit: {changed: $changed, message: $message, sha: $sha}}'
