# Refactor Dagu workflow

This workflow walks a repository tree from deepest matching source directories to
shallowest and runs a per-path refactor pass:

- find directories containing `*.rs`, `*.swift`, `*.py`, and `*.go`
- skip directories that start with `.`, plus `target` and `build`
- run Claude once per directory path across all supported files directly in that path
- if the Claude pass produced a diff, run Codex to review that path diff and commit it

The workflow expects a clean git worktree before it starts. Each Codex review is
required to leave the repository clean with one new commit for the path it
reviewed.

`REPO_DIR` must be absolute or `~/...`.
Dagu steps do not preserve the shell directory that launched `dagu start`, so
relative paths such as `REPO_DIR=.` are intentionally rejected.

Runtime helpers live in `./scripts/`:

- `list-target-paths.sh`
- `run-claude-prompt.sh`
- `run-codex-prompt.sh`
- `refactor-loop.sh`

`TARGET_FILTER` is optional. When set, it is matched as a bash glob against
repo-relative supported source file paths before those files are grouped into
refactor directories. Examples:

- `TARGET_FILTER='*.swift'` limits the run to refactor units that contain
  matching Swift files.
- `TARGET_FILTER='Sources/**'` limits the run to supported source files under
  `Sources/`.

The agent wrappers accept normal text output. If a model wraps a JSON payload in
fences or extra prose, the wrappers normalize that down to the first valid JSON
payload when one is present; otherwise they preserve the plain text output.

Example run:

```bash
dagu start dagu/refactor/refactor.yaml -- \
  REPO_DIR="$PWD" \
  TARGET_FILTER='Sources/**' \
  CLAUDE_MODEL=sonnet \
  CODEX_MODEL=gpt-5.4
```

By default `SCRIPTS_DIR` points at this repository's
`/Users/vk/@iw2rmb/auto/dagu/refactor/scripts` directory so the workflow can run
against a separate `REPO_DIR`. `SCRIPTS_DIR` may also use `~/...`. Override it
only if you intentionally move the workflow helpers.
