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

- [x] 3.4 Add smoke and regression coverage for the reference workflow
  - Added fixture-based runtime smoke and resume coverage in `internal/runtime/reference_workflow_test.go`, including fake `codex` and `claude` CLIs, end-to-end commits through docs cleanup, and interruption recovery that reruns only the interrupted step while preserving prior committed work.
  - Added real Git executor regressions for non-repo `cwd`, untracked-file snapshots, empty included diffs, and `.amata` plus unrelated staged-path exclusions so typed `git.inspect` and `git.commit` results stay aligned with the documented contracts.
  - Extended shared agent executor coverage to assert structured-output artifact paths and to verify invalid provider payload failures still persist prompt, transcript, stdout, stderr, and metadata artifacts.
  - Focused validation: `go test ./internal/runtime ./internal/executor/agent ./internal/executor/gitinspect ./internal/executor/gitcommit ./internal/gitadapter -count=1`
