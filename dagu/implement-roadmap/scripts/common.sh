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

extract_first_json_file() {
  local file="$1"
  local extracted

  require_command perl
  require_file "$file"

  if ! extracted="$(
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
  )"; then
    die "failed to extract JSON from $file"
  fi

  printf '%s\n' "$extracted" >"$file"
}

load_file_lines() {
  local file="$1"
  FILE_LINES=()

  while IFS= read -r line || [ -n "$line" ]; do
    FILE_LINES+=("$line")
  done <"$file"
}

# Parse checklist items from a roadmap-like markdown file and normalize the
# first-line title, common metadata bullets, and numbered action lines.
roadmap_items_json() {
  local doc="$1"
  local tmp_objects
  local line
  local next_line
  local title
  local state
  local repository
  local component
  local verification
  local reasoning
  local scope
  local tests
  local block_text
  local actions_json
  local checked
  local label
  local summary
  local i
  local j
  local k
  local line_count
  local value
  local key

  require_command jq
  require_file "$doc"

  load_file_lines "$doc"
  line_count=${#FILE_LINES[@]}
  tmp_objects="$(mktemp)"
  : >"$tmp_objects"

  i=0
  while [ "$i" -lt "$line_count" ]; do
    line="${FILE_LINES[$i]}"

    if [[ "$line" =~ ^[[:space:]]*-[[:space:]]\[([[:space:]xX])\][[:space:]]+(.+)$ ]]; then
      state="${BASH_REMATCH[1]}"
      title="${BASH_REMATCH[2]}"
      repository=""
      component=""
      verification=""
      reasoning=""
      scope=""
      tests=""
      block_text="$line"
      checked=false
      label=""
      summary="$title"
      if [ "$state" != " " ]; then
        checked=true
      fi
      if [[ "$title" =~ ^([0-9]+(\.[0-9]+)*)[[:space:]]+(.+)$ ]]; then
        label="${BASH_REMATCH[1]}"
        summary="${BASH_REMATCH[3]}"
      fi

      actions_json='[]'
      j=$((i + 1))
      while [ "$j" -lt "$line_count" ]; do
        next_line="${FILE_LINES[$j]}"
        if [[ "$next_line" =~ ^[[:space:]]*-[[:space:]]\[[[:space:]xX]\][[:space:]]+ ]]; then
          break
        fi

        block_text="${block_text}
${next_line}"

        if [[ "$next_line" =~ ^[[:space:]]*-[[:space:]]+([^:]+):[[:space:]]*(.*)$ ]]; then
          key="${BASH_REMATCH[1]}"
          value="${BASH_REMATCH[2]}"
          case "$key" in
            Repository) repository="$value" ;;
            Component) component="$value" ;;
            Verification) verification="$value" ;;
            Reasoning) reasoning="$value" ;;
            Scope) scope="$value" ;;
            Tests)
              tests="$value"
              if [ -z "$verification" ]; then
                verification="$value"
              fi
              ;;
          esac
        elif [[ "$next_line" =~ ^[[:space:]]*[0-9]+\.[[:space:]]+(.*)$ ]]; then
          value="${BASH_REMATCH[1]}"
          actions_json="$(jq -cn --argjson actions "$actions_json" --arg value "$value" '$actions + [$value]')"
        fi

        j=$((j + 1))
      done

      jq -cn \
        --arg checked "$checked" \
        --arg title "$title" \
        --arg label "$label" \
        --arg summary "$summary" \
        --arg repository "$repository" \
        --arg component "$component" \
        --arg verification "$verification" \
        --arg reasoning "$reasoning" \
        --arg scope "$scope" \
        --arg tests "$tests" \
        --arg block "$block_text" \
        --argjson actions "$actions_json" \
        --argjson lineNumber "$((i + 1))" \
        --argjson endLine "$j" \
        '{
          checked: ($checked == "true"),
          title: $title,
          label: $label,
          summary: $summary,
          lineNumber: $lineNumber,
          endLine: $endLine,
          repository: $repository,
          component: $component,
          verification: $verification,
          reasoning: (if $reasoning == "" then "high" else $reasoning end),
          reasoningSource: (if $reasoning == "" then "default" else "roadmap" end),
          scope: $scope,
          tests: $tests,
          actions: $actions,
          block: $block
        }' >>"$tmp_objects"

      i="$j"
      continue
    fi

    i=$((i + 1))
  done

  if [ -s "$tmp_objects" ]; then
    jq -s '.' "$tmp_objects"
  else
    printf '[]\n'
  fi

  rm -f "$tmp_objects"
}
