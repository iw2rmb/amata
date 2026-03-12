#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
. "${SCRIPT_DIR}/common.sh"

repo=""
scripts_dir="${SCRIPT_DIR}"
claude_model=""
claude_reasoning=""
codex_model=""
codex_reasoning=""

while [ "$#" -gt 0 ]; do
  case "$1" in
    --repo)
      [ "$#" -ge 2 ] || die "--repo requires a value"
      repo="$2"
      shift 2
      ;;
    --scripts-dir)
      [ "$#" -ge 2 ] || die "--scripts-dir requires a value"
      scripts_dir="$2"
      shift 2
      ;;
    --claude-model)
      [ "$#" -ge 2 ] || die "--claude-model requires a value"
      claude_model="$2"
      shift 2
      ;;
    --claude-reasoning)
      [ "$#" -ge 2 ] || die "--claude-reasoning requires a value"
      claude_reasoning="$2"
      shift 2
      ;;
    --codex-model)
      [ "$#" -ge 2 ] || die "--codex-model requires a value"
      codex_model="$2"
      shift 2
      ;;
    --codex-reasoning)
      [ "$#" -ge 2 ] || die "--codex-reasoning requires a value"
      codex_reasoning="$2"
      shift 2
      ;;
    *)
      die "unknown argument: $1"
      ;;
  esac
done

[ -n "$repo" ] || die "--repo is required"
[ -n "$claude_model" ] || die "--claude-model is required"
[ -n "$claude_reasoning" ] || die "--claude-reasoning is required"
[ -n "$codex_model" ] || die "--codex-model is required"
[ -n "$codex_reasoning" ] || die "--codex-reasoning is required"

require_command bash
require_command git

workflow_root="$(CDPATH= cd -- "${SCRIPT_DIR}/../../.." && pwd)"

case "$repo" in
  /*)
    repo="$(CDPATH= cd -- "$repo" && pwd)"
    ;;
  *)
    repo="$(CDPATH= cd -- "${workflow_root}/${repo}" && pwd)"
    ;;
esac

cd "$repo"
require_clean_worktree

paths_file="$(mktemp)"
trap 'rm -f "$paths_file"' EXIT

bash "${scripts_dir}/list-target-paths.sh" --repo "$repo" >"$paths_file"

scanned_count=0
changed_count=0
committed_count=0
missing_count=0

while IFS= read -r path || [ -n "$path" ]; do
  [ -n "$path" ] || continue
  scanned_count=$((scanned_count + 1))

  if [ ! -f "$path" ]; then
    printf 'skip missing path: %s\n' "$path"
    missing_count=$((missing_count + 1))
    continue
  fi

  printf 'inspect path: %s\n' "$path"
  before_head="$(current_head)"

  cat <<PROMPT | bash "${scripts_dir}/run-claude-prompt.sh" \
    --repo "$repo" \
    --model "$claude_model" \
    --reasoning "$claude_reasoning" >/dev/null
Repository root: ${repo}
Focus path: ${path}

Inspect ${path} for redundancy, overengineering, dead code; check for options to streamline logic, extract helpers to reduce boilerplate, split for clear and distinctive domains. Address most valuable findings. Do not commit.
PROMPT

  if ! git_has_changes; then
    printf 'no diff: %s\n' "$path"
    continue
  fi

  changed_count=$((changed_count + 1))

  cat <<PROMPT | bash "${scripts_dir}/run-codex-prompt.sh" \
    --repo "$repo" \
    --model "$codex_model" \
    --reasoning "$codex_reasoning" >/dev/null
Repository root: ${repo}
Focus path: ${path}

Review diff in ${path} for sanity and correctness. Commit.
PROMPT

  after_head="$(current_head)"
  if [ "$before_head" = "$after_head" ]; then
    die "codex review did not create a commit for ${path}"
  fi

  if git_has_changes; then
    die "repository still has uncommitted changes after codex review for ${path}"
  fi

  committed_count=$((committed_count + 1))
  printf 'committed path: %s (%s)\n' "$path" "$after_head"
done <"$paths_file"

printf 'summary: scanned=%s changed=%s committed=%s missing=%s\n' \
  "$scanned_count" \
  "$changed_count" \
  "$committed_count" \
  "$missing_count"
