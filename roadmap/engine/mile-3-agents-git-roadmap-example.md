# Engine Milestone 3 Agent Executors, Git Executors, and Reference Workflow

Scope: Implement agent and Git executors on the engine runtime and make the reference `implement-roadmap` workflow run through YAML control flow instead of external shell drivers.

Documentation: [Engine design](../../design/engine/engine.md)

Legend: [ ] todo, [x] done.

- [x] 3.1 Add shared agent executor plumbing for Codex and Claude
  - Added shared `internal/executor/agent` request loading for rendered `prompt`, `model`, `reasoning`, `cwd`, `env`, response-schema detection, and per-step artifact directories with prompt/transcript/provider metadata persistence.
  - Implemented `codex` structured output through `codex exec --output-schema` and `claude` structured output through provider JSON schema support with prompt-level fallback normalization when structured flags are unavailable.
  - Focused validation: `go test ./internal/executor/... ./internal/runtime ./internal/schema`

- [ ] 3.2 Add typed Git executors with a narrow CLI escape hatch
  - Repository: auto
  - Component: `internal/executor/gitinspect`, `internal/executor/gitcommit`, typed Git adapter
  - Verification: `git.inspect` returns one consistent snapshot including untracked files, `git.commit` excludes `workspace.state_dir`, unrelated staged changes never leak into engine commits
  - Reasoning: xhigh
  1. Implement `git.inspect` on the typed Git layer so `isRepo`, `hasDiff`, and `files` come from one repository snapshot.
  2. Implement `git.commit` candidate selection on normalized repo-relative paths with directory-prefix exclusion semantics.
  3. Keep `go-git` as the default backend and isolate any required Git CLI fallback behind one internal adapter package.
  4. Enforce default exclusion of `workspace.state_dir` when that state directory sits inside the target repository tree.

- [ ] 3.3 Port the reference `implement-roadmap` flow to engine-owned control flow
  - Repository: auto
  - Component: example workflow bundle, runtime integration, roadmap helpers
  - Verification: the loop selects the next open item from the roadmap file, per-item review and commit stay in YAML flow, final correctness/refactor/sanity/docs phases run without external routing scripts
  - Reasoning: xhigh
  1. Wire [design/engine/example/implement-roadmap.yaml](../../design/engine/example/implement-roadmap.yaml) to the real engine runtime and keep the prompt shape `Implement next open item from the <file>`.
  2. Replace the bash loop, routing, and JSON gatekeeping from `dagu/implement-roadmap/scripts/implement-open-items-loop.sh` with `call`, `switch`, schema validation, and typed step results.
  3. Move repo path handling, prompt artifacts, and run state ownership from shell wrappers into engine runtime packages.
  4. Keep the example dependent only on built-in executors plus the roadmap helper scripts that operate on the roadmap file itself.

- [ ] 3.4 Add smoke and regression coverage for the reference workflow
  - Repository: auto
  - Component: fixture repo tests, Git executor tests, agent executor integration tests
  - Verification: the fixture workflow runs end-to-end, interrupted runs resume without replaying committed work, non-repo and empty-diff cases fail or no-op with the documented typed results
  - Reasoning: high
  1. Add a fixture-based smoke test that runs the reference workflow from a clean repository checkout through docs cleanup.
  2. Add an interruption test that resumes after a durable step boundary without replaying completed commits.
  3. Add Git executor tests for non-repo `cwd`, empty included diff, untracked files, and exclusion of `.amata` plus unrelated staged paths.
  4. Add agent executor tests that cover structured-output success, invalid provider payloads, and artifact persistence paths.
