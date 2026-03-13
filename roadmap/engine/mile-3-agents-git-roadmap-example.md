# Engine Milestone 3 Agent Executors, Git Executors, and Reference Workflow

Scope: Implement agent and Git executors on the engine runtime and make the reference `implement-roadmap` workflow run through YAML control flow instead of external shell drivers.

Documentation: [Engine design](../../design/engine/engine.md)

Legend: [ ] todo, [x] done.

- [x] 3.1 Add shared agent executor plumbing for Codex and Claude
  - Added shared `internal/executor/agent` request loading for rendered `prompt`, `model`, `reasoning`, `cwd`, `env`, response-schema detection, and per-step artifact directories with prompt/transcript/provider metadata persistence.
  - Implemented `codex` structured output through `codex exec --output-schema` and `claude` structured output through provider JSON schema support with prompt-level fallback normalization when structured flags are unavailable.
  - Focused validation: `go test ./internal/executor/... ./internal/runtime ./internal/schema`

- [x] 3.2 Add typed Git executors with a narrow CLI escape hatch
  - Added a typed `internal/gitadapter` service that uses `go-git` for repo discovery and one status snapshot, so `git.inspect` returns `isRepo`, `hasDiff`, and `files` from the same untracked-inclusive view.
  - Implemented `internal/executor/gitcommit` and `internal/executor/gitinspect`, wired both into built-in executor registration, and normalized commit exclusion matching on repo-relative directory prefixes.
  - Isolated the required Git CLI mutation path inside the adapter with `git add -A -- <included>` plus `git commit -- <included>`, which commits only the filtered candidate set and leaves excluded staged paths untouched.
  - Focused validation: `go test ./internal/gitadapter ./internal/executor/gitinspect ./internal/executor/gitcommit ./internal/runtime`

- [x] 3.3 Port the reference `implement-roadmap` flow to engine-owned control flow
  - Added runtime-owned `--set key=value` param overrides plus `ctx.spec.path|dir` so the example bundle can target external repos and still resolve its own helper scripts without `--repo` or `--scripts-dir` wrapper arguments.
  - Reworked [design/engine/example/implement-roadmap.yaml](../../design/engine/example/implement-roadmap.yaml) to drive next-item selection, per-item verification, review, commit, and final correctness/refactor/sanity/docs phases through built-in `shell`, `call`, `switch`, `codex`, `claude`, and `git.commit` steps with response schemas instead of external routing scripts.
  - Simplified [design/engine/example/scripts/roadmap_items.py](../../design/engine/example/scripts/roadmap_items.py) into a direct CLI helper for roadmap-file queries, preserved repo-relative and `~/...` path handling, and fixed runtime value normalization so typed `git.commit` results remain usable through `ctx.prev`.
  - Focused validation: `go test ./internal/runtime -count=1`; `python3 -m py_compile design/engine/example/scripts/*.py`; `python3 design/engine/example/scripts/roadmap_items.py next-open --file '~/@iw2rmb/auto/design/engine/example/fixture-repo/roadmap/index.md'`; manual `go run ./cmd/amata run design/engine/example/implement-roadmap.yaml ...` smoke with stub `codex`/`claude` CLIs

- [ ] 3.4 Add smoke and regression coverage for the reference workflow
  - Repository: auto
  - Component: fixture repo tests, Git executor tests, agent executor integration tests
  - Verification: the fixture workflow runs end-to-end, interrupted runs resume without replaying committed work, non-repo and empty-diff cases fail or no-op with the documented typed results
  - Reasoning: high
  1. Add a fixture-based smoke test that runs the reference workflow from a clean repository checkout through docs cleanup.
  2. Add an interruption test that resumes after a durable step boundary without replaying completed commits.
  3. Add Git executor tests for non-repo `cwd`, empty included diff, untracked files, and exclusion of `.amata` plus unrelated staged paths.
  4. Add agent executor tests that cover structured-output success, invalid provider payloads, and artifact persistence paths.
