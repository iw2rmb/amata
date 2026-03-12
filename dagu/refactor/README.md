# Refactor Dagu workflow

This workflow walks a repository tree from deepest matching source paths to
shallowest and runs a per-path refactor pass:

- find `*.rs`, `*.swift`, `*.py`, and `*.go`
- skip directories that start with `.`, plus `target` and `build`
- run Claude on each path with a focused refactor prompt
- if the Claude pass produced a diff, run Codex to review that diff and commit it

The workflow expects a clean git worktree before it starts. Each Codex review is
required to leave the repository clean with one new commit for the path it
reviewed.

`REPO_DIR` may be absolute or relative to this workflow repository root
(`/Users/vk/@iw2rmb/auto`).

Runtime helpers live in `./scripts/`:

- `list-target-paths.sh`
- `run-claude-prompt.sh`
- `run-codex-prompt.sh`
- `refactor-loop.sh`

Example run:

```bash
dagu start dagu/refactor/refactor.yaml -- \
  REPO_DIR=/abs/path/to/repo \
  CLAUDE_MODEL=sonnet \
  CODEX_MODEL=gpt-5.4
```

By default `SCRIPTS_DIR` points at this repository's
`/Users/vk/@iw2rmb/auto/dagu/refactor/scripts` directory so the workflow can run
against a separate `REPO_DIR`. Override it only if you intentionally move the
workflow helpers.
