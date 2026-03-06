#!/usr/bin/env bash
set -euo pipefail

start_root="$(pwd)"
doc_dirs=(design docs research roadmap)

find_docs_root() {
  local candidate="$1"
  local nested="$candidate/docs"

  if [[ -d "$nested/design" && -d "$nested/docs" && -d "$nested/research" && -d "$nested/roadmap" ]]; then
    printf '%s\n' "$nested"
    return 0
  fi

  if [[ -d "$candidate/design" || -d "$candidate/docs" || -d "$candidate/research" || -d "$candidate/roadmap" ]]; then
    printf '%s\n' "$candidate"
    return 0
  fi

  return 1
}

docs_root="$(find_docs_root "$start_root" || true)"
if [[ -z "$docs_root" ]]; then
  echo "check_docs_links: no documentation folders found from $start_root (expected repo root with any of: design/ docs/ research/ roadmap/, or workspace root with docs/{design,docs,research,roadmap}/)" >&2
  exit 1
fi

workspace_root="$(dirname "$docs_root")"
cd "$docs_root"

scan_dirs=()
for dir in "${doc_dirs[@]}"; do
  if [[ -d "$dir" ]]; then
    scan_dirs+=("$dir")
  fi
done

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

  local targets=()
  if [[ "$ref_path" == /* ]]; then
    targets+=("$ref_path")
  else
    targets+=("$(dirname "$source")/$ref_path")
    targets+=("$docs_root/$ref_path")

    if [[ "$ref_path" == docs/* ]]; then
      targets+=("$docs_root/${ref_path#docs/}")
      targets+=("$workspace_root/$ref_path")
    fi

    case "$ref_path" in
      app/*|shell/*|protocol/*|parser/*|textwidth/*)
        targets+=("$workspace_root/$ref_path")
        ;;
    esac
  fi

  local target
  for target in "${targets[@]}"; do
    if [[ -e "$target" ]]; then
      return 0
    fi
  done

  if [[ ! -e "${targets[0]}" ]]; then
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
