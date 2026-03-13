#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
. "${SCRIPT_DIR}/common.sh"

repo="."
target_filter=""

while [ "$#" -gt 0 ]; do
  case "$1" in
    --repo)
      [ "$#" -ge 2 ] || die "--repo requires a value"
      repo="$2"
      shift 2
      ;;
    --filter)
      [ "$#" -ge 2 ] || die "--filter requires a value"
      target_filter="$2"
      shift 2
      ;;
    *)
      die "unknown argument: $1"
      ;;
  esac
done

require_command find
require_command sed
require_command awk
require_command sort
require_command cut
require_command uniq

cd "$repo"

tab="$(printf '\t')"

while IFS= read -r file; do
  file="${file#./}"
  if [ -n "$target_filter" ] && ! path_matches_glob "$file" "$target_filter"; then
    continue
  fi
  printf '%s\n' "$file"
done < <(
  LC_ALL=C find . \
    \( -type d \( \( -name '.*' -a ! -path '.' \) -o -name target -o -name build \) -prune \) -o \
    \( -type f \( -name '*.rs' -o -name '*.swift' -o -name '*.py' -o -name '*.go' \) -print \)
) | awk -F/ '
    {
      if (NF == 1) {
        dir="."
      } else {
        dir=$1
        for (i = 2; i < NF; i++) {
          dir = dir "/" $i
        }
      }
      depth = split(dir, parts, "/")
      print depth "\t" dir
    }
  ' \
  | LC_ALL=C sort -t "$tab" -k1,1nr -k2,2 \
  | cut -f2- \
  | uniq
