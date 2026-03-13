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
  local normalized

  require_file "$file"

  normalized="$(
    perl -0pe '
      if (/\A\s*```[^\n]*\n(.*)\n```\s*\z/s) {
        $_ = $1;
      }
    ' "$file"
  )"

  printf '%s' "$normalized" >"$file"
}

extract_first_json_payload() {
  local file="$1"

  require_command perl
  require_file "$file"

  perl -MJSON::PP -0ne '
    use strict;
    use warnings;

    sub decode_candidate {
      my ($candidate) = @_;
      return undef if !defined $candidate || $candidate eq q{};

      my $decoder = JSON::PP->new->allow_nonref;
      my ($value, $consumed);
      my $ok = eval { ($value, $consumed) = $decoder->decode_prefix($candidate); 1 };
      return undef if !$ok || !defined $consumed || $consumed <= 0;

      return JSON::PP->new->canonical->encode($value);
    }

    my $text = $_;
    my $trimmed = $text;
    $trimmed =~ s/\A\s+//s;
    $trimmed =~ s/\s+\z//s;

    my $decoded = decode_candidate($trimmed);
    if (defined $decoded) {
      print $decoded;
      exit 0;
    }

    while ($text =~ /[\{\[]/g) {
      my $start = pos($text) - 1;
      $decoded = decode_candidate(substr($text, $start));
      next if !defined $decoded;
      print $decoded;
      exit 0;
    }

    exit 1;
  ' "$file"
}

extract_first_json_file() {
  local file="$1"
  local extracted

  if ! extracted="$(extract_first_json_payload "$file")"; then
    die "failed to extract JSON from $file"
  fi

  printf '%s\n' "$extracted" >"$file"
}

extract_first_json_file_if_present() {
  local file="$1"
  local extracted

  require_file "$file"

  if ! extracted="$(extract_first_json_payload "$file" 2>/dev/null)"; then
    return 1
  fi

  printf '%s\n' "$extracted" >"$file"
  return 0
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

path_matches_glob() (
  local path="$1"
  local pattern="$2"

  shopt -s globstar
  [[ "$path" == $pattern ]]
)
