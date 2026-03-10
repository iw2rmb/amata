#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "$0")" && pwd)"
repo_dir="$(cd "${script_dir}/../fixture-repo" && pwd)"

if ! git -C "$repo_dir" rev-parse --is-inside-work-tree >/dev/null 2>&1; then
  git -C "$repo_dir" init -q
fi

git -C "$repo_dir" config user.name "Engine Example"
git -C "$repo_dir" config user.email "engine-example@example.com"

if ! git -C "$repo_dir" rev-parse HEAD >/dev/null 2>&1; then
  git -C "$repo_dir" add -A
  git -C "$repo_dir" commit -q -m "chore: initialize engine example fixture repo"
fi
