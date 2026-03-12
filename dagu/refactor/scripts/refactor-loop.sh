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

repo="$(expand_home_path "$repo")"
case "$repo" in
  /*) ;;
  *)
    die "--repo must be an absolute path or start with ~/ because dagu steps do not preserve the caller working directory"
    ;;
esac
require_dir "$repo"
scripts_dir="$(resolve_path_from "$PWD" "$scripts_dir")"
require_dir "$scripts_dir"

cd "$repo"
require_clean_worktree

paths_file="$(mktemp)"
trap 'rm -f "$paths_file"' EXIT

bash "${scripts_dir}/list-target-paths.sh" --repo "$repo" >"$paths_file"

scanned_count=0
changed_count=0
committed_count=0
missing_count=0
path_root=""
path_files=""
path_file_count=0

while IFS= read -r path || [ -n "$path" ]; do
  [ -n "$path" ] || continue
  scanned_count=$((scanned_count + 1))
  if [ "$path" = "." ]; then
    path_root="$repo"
  else
    path_root="${repo}/${path}"
  fi

  if [ ! -d "$path_root" ]; then
    printf 'skip missing path: %s\n' "$path"
    missing_count=$((missing_count + 1))
    continue
  fi

  path_files="$(
    LC_ALL=C find "$path_root" -maxdepth 1 -type f \
      \( -name '*.rs' -o -name '*.swift' -o -name '*.py' -o -name '*.go' \) \
      | sed "s#^${repo}/##" \
      | LC_ALL=C sort
  )"

  if [ -z "$path_files" ]; then
    printf 'skip empty path: %s\n' "$path"
    missing_count=$((missing_count + 1))
    continue
  fi

  path_file_count="$(printf '%s\n' "$path_files" | awk 'NF { count += 1 } END { print count + 0 }')"

  printf 'inspect path: %s\n' "$path"
  before_head="$(current_head)"
  printf 'in-progress step=claude path=%s files=%s\n' "$path" "$path_file_count"

  cat <<PROMPT | bash "${scripts_dir}/run-claude-prompt.sh" \
    --repo "$repo" \
    --model "$claude_model" \
    --reasoning "$claude_reasoning" >/dev/null
Repository root: ${repo}
Focus path: ${path}
Focus files:
${path_files}

Inspect all supported source files directly under ${path}. Treat this path as one refactor unit. Check for redundancy, overengineering, dead code; check for options to streamline logic, extract helpers to reduce boilerplate, split for clear and distinctive domains across these files. Address most valuable findings. Do not commit.
PROMPT

  if ! git_has_changes; then
    printf 'no diff: %s\n' "$path"
    continue
  fi

  changed_count=$((changed_count + 1))
  printf 'in-progress step=codex path=%s files=%s\n' "$path" "$path_file_count"

  cat <<PROMPT | bash "${scripts_dir}/run-codex-prompt.sh" \
    --repo "$repo" \
    --model "$codex_model" \
    --reasoning "$codex_reasoning" >/dev/null
Repository root: ${repo}
Focus path: ${path}
Focus files:
${path_files}

Review the current diff for ${path} across the listed files for sanity and correctness. Commit.
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
