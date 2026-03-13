# Amata Live Progress Stream Phase 1

Scope: Add live step streaming to `amata run` and `amata resume` with reusable progress components, executor-specific detail enrichers, and a Bubble Tea terminal renderer.

Documentation: [Engine](../../docs/engine/index.md), [Roadmap Index](./index.md)

Legend: [ ] todo, [x] done.

- [x] 1.1 Add runtime live progress events and sink interfaces
  - Repository: auto
  - Component: internal/runtime, internal/state, internal/executor
  - Verification: `go test ./internal/runtime ./internal/state`, manual `amata run` on a short workflow shows start and finish transitions for each step
  - Reasoning: high
1. Add an `internal/progress` domain with reusable event and snapshot types for run start, resume, step start, step finish, and terminal run finish so live streaming is decoupled from durable state storage.
2. Thread an optional progress sink from `runtime.RunCLI` into `Runner.execute` and emit notifications around executor dispatch, `switch` branch execution, `call` control returns, and terminal run completion without changing the `events.ndjson` or `snapshot.json` formats.
3. Keep live progress in-memory and optional, with durable `state.Store` remaining the only resume source of truth.

- [ ] 1.2 Build reusable step descriptors and executor enrichers
  - Repository: auto
  - Component: internal/progress, internal/executor/agent, internal/executor/gitcommit, internal/gitadapter
  - Verification: `go test ./internal/executor/gitcommit ./internal/gitadapter`, descriptor unit tests cover codex, claude, shell, assert, git.inspect, and git.commit shapes
  - Reasoning: high
1. Add a shared step descriptor builder that exposes common fields for renderer reuse: status symbol kind, elapsed time, step type, primary line text, wrapped detail lines, and final summary details.
2. Add agent-step enrichers so `codex` and `claude` descriptors can expose resolved model, reasoning, and rendered prompt before execution, with prompt wrapping handled by the renderer at a fixed 60-column content width.
3. Extend `gitadapter.CommitResult` and the `git.commit` executor result with structured commit metadata for rendering: short SHA source, changed file count, total insertions and deletions, and per-file `+lines/-lines` stats so the renderer does not shell out or parse ad hoc text.

- [ ] 1.3 Add a Bubble Tea stream renderer and CLI integration
  - Repository: auto
  - Component: cmd/amata, internal/runtime, internal/progress
  - Verification: `go test ./cmd/amata ./internal/runtime`, manual TTY run shows spinner, checkmark, and `X` states with stable wrapping and no broken line rewrites on the primary screen buffer
  - Reasoning: high
1. Add Bubble Tea plus Bubbles and `github.com/charmbracelet/lipgloss/v2`, then implement a renderer-controller pair that writes live progress to `stderr`, animates one active spinner, and replaces it with a checkmark or `X` when the step completes.
2. Keep the live progress renderer on the primary screen buffer rather than alt screen so streamed history remains visible, while formatting the shared headline as `<status> <time> <step-type>` and plugging in reusable executor-specific detail sections, including `codex <model> <reasoning>` plus wrapped prompt text and `git.commit {<sha> <files X +N -N>}` plus wrapped commit message and per-file diff stats.
3. Keep `stdout` reserved for machine-friendly outputs such as the run id, provide a non-TTY plain-text fallback with the same content model, and cover line formatting with deterministic tests or golden snapshots.

- [ ] 1.4 Document the stream contract and verify end to end
  - Repository: auto
  - Component: docs/engine, design/engine/example, internal/runtime
  - Verification: `go test ./...`, `~/@iw2rmb/auto/scripts/check_docs_links.sh`, manual run and resume on `design/engine/example/implement-roadmap.yaml`
  - Reasoning: medium
1. Update `docs/engine/index.md` to document the live progress stream, the `stderr` versus `stdout` split, and which executor metadata is guaranteed for renderers.
2. Extend runtime integration coverage to assert streamed start and finish transitions, resume behavior, and `git.commit` completed-line summaries without depending on wall-clock waits.
3. Verify the example workflow produces readable live progress for nested `call` and `switch` flows before closing the roadmap item.
