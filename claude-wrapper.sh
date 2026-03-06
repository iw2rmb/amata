#!/usr/bin/env bash
set -euo pipefail

# claude-wrapper.sh — Reliable non-interactive claude -p wrapper
# with auth/state checks and timeout.

# --- Config ---
TIMEOUT="${CLAUDE_TIMEOUT:-120}"

# --- Checks ---
if ! command -v claude &>/dev/null; then
  echo "error: claude not found in PATH" >&2
  exit 1
fi

if ! claude auth status &>/dev/null 2>&1; then
  echo "error: not authenticated — run 'claude auth login'" >&2
  exit 1
fi

# --- Prompt: from args or stdin ---
if [[ $# -gt 0 ]]; then
  PROMPT="$*"
elif [[ ! -t 0 ]]; then
  PROMPT="$(cat)"
else
  echo "usage: claude-wrapper <prompt>" >&2
  echo "       echo '<prompt>' | claude-wrapper" >&2
  exit 1
fi

if [[ -z "${PROMPT}" ]]; then
  echo "error: empty prompt" >&2
  exit 1
fi

# --- Run ---
timeout "${TIMEOUT}" claude -p "$PROMPT" 2>/dev/null
