#!/usr/bin/env bash
set -euo pipefail

root="$(pwd)"
cd "$root"

doc_dirs=(design docs research roadmap)
scan_dirs=()
for dir in "${doc_dirs[@]}"; do
  if [[ -d "$dir" ]]; then
    scan_dirs+=("$dir")
  fi
done

if [[ ${#scan_dirs[@]} -eq 0 ]]; then
  echo "check_docs_links: no documentation folders found in $root (expected any of: design/ docs/ research/ roadmap/)" >&2
  exit 1
fi

fail=0

check_refs() {
  local source="$1"
  local line="$2"
  local ref_raw="$3"

  # Trim optional markdown angle wrapping and trailing query/anchor.
  local ref="$ref_raw"
  ref="${ref#<}"
  ref="${ref%>}"

  # Keep only markdown file references.
  case "$ref" in
    *.md|*.md#*|*.md\?*) ;;
    *) return 0 ;;
  esac

  local ref_path="${ref%%#*}"
  ref_path="${ref_path%%\?*}"

  # Skip URL-like schemes.
  if [[ "$ref_path" =~ ^[a-zA-Z][a-zA-Z0-9+.-]*: ]]; then
    return 0
  fi

  local target
  if [[ "$ref_path" == /* ]]; then
    target="$ref_path"
  else
    target="$(dirname "$source")/$ref_path"
  fi

  if [[ ! -e "$target" ]]; then
    echo "MISSING: ${source}:${line} -> ${ref}"
    fail=1
  fi
}

while IFS=: read -r source line ref; do
  check_refs "$source" "$line" "$ref"
done < <(
  rg --no-heading -n -o -P \
    '\[[^][]+\]\(\s*\K(?:<[^>\s]+>|[^)\s]+)(?=(?:\s+"[^"]*")?\s*\))' \
    "${scan_dirs[@]}"

  rg --no-heading -n -o -P \
    '`\K[^`\s]+\.md(?:#[^`\s]+)?(?=`)' \
    "${scan_dirs[@]}"
)

if [[ $fail -ne 0 ]]; then
  exit 1
fi

echo "check_docs_links: cross-reference integrity passed"
