#!/usr/bin/env bash
set -euo pipefail

die() {
  printf 'error: %s\n' "$*" >&2
  exit 1
}

require_command() {
  command -v "$1" >/dev/null 2>&1 || die "missing command: $1"
}

require_file() {
  [ -f "$1" ] || die "missing file: $1"
}

require_dir() {
  [ -d "$1" ] || die "missing directory: $1"
}

expand_home_path() {
  local path="$1"

  case "$path" in
    "~")
      printf '%s\n' "$HOME"
      ;;
    "~/"*)
      printf '%s/%s\n' "$HOME" "${path#~/}"
      ;;
    *)
      printf '%s\n' "$path"
      ;;
  esac
}

resolve_path_from() {
  local base_dir="$1"
  local path="$2"

  path="$(expand_home_path "$path")"

  case "$path" in
    /*)
      printf '%s\n' "$path"
      ;;
    *)
      printf '%s/%s\n' "$base_dir" "$path"
      ;;
  esac
}

normalize_markdown_fence_file() {
  local file="$1"

  require_file "$file"

  perl -0pe '
    if (/\A\s*```[^\n]*\n(.*)\n```\s*\z/s) {
      $_ = $1;
    }
  ' "$file"
}

ensure_git_repo() {
  git rev-parse --is-inside-work-tree >/dev/null 2>&1 || die "current working directory is not a git repository"
}

require_clean_worktree() {
  local untracked

  ensure_git_repo

  if ! git diff --quiet --exit-code; then
    die "repository has unstaged changes"
  fi

  if ! git diff --cached --quiet --exit-code; then
    die "repository has staged changes"
  fi

  untracked="$(git ls-files --others --exclude-standard)"
  if [ -n "$untracked" ]; then
    die "repository has untracked files"
  fi
}

git_has_changes() {
  ensure_git_repo

  if ! git diff --quiet --exit-code; then
    return 0
  fi

  if ! git diff --cached --quiet --exit-code; then
    return 0
  fi

  if [ -n "$(git ls-files --others --exclude-standard)" ]; then
    return 0
  fi

  return 1
}

current_head() {
  ensure_git_repo
  git rev-parse HEAD
}
