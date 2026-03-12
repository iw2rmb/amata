#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
. "${SCRIPT_DIR}/common.sh"

repo="."

while [ "$#" -gt 0 ]; do
  case "$1" in
    --repo)
      [ "$#" -ge 2 ] || die "--repo requires a value"
      repo="$2"
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

cd "$repo"

tab="$(printf '\t')"

LC_ALL=C find . \
  \( -type d \( \( -name '.*' -a ! -path '.' \) -o -name target -o -name build \) -prune \) -o \
  \( -type f \( -name '*.rs' -o -name '*.swift' -o -name '*.py' -o -name '*.go' \) -print \) \
  | sed 's#^\./##' \
  | awk -F/ '{print NF "\t" $0}' \
  | LC_ALL=C sort -t "$tab" -k1,1nr -k2,2 \
  | cut -f2-
