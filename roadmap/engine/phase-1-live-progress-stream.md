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

- [x] 1.2 Build reusable step descriptors and executor enrichers
  - Added `progress.DescriptorData` and `BuildStepDescriptor`, then wired runtime progress events to emit enriched descriptors for `codex`, `claude`, `shell`, `assert`, `git.inspect`, `git.commit`, `call`, and `switch` without changing durable state storage.
  - Split agent request resolution into reusable `agent.ResolveStep` so descriptor enrichment can resolve model, reasoning, cwd, env, and rendered prompt before execution, with descriptor tests covering codex, claude, shell, assert, git.inspect, and git.commit shapes at the 60-column wrap width.
  - Extended `gitadapter.CommitResult` and `git.commit` outputs with structured commit metadata (`shortCommit`, changed file count, total insertions/deletions, per-file stats) and verified with `go test ./internal/runtime ./internal/progress ./internal/executor/agent ./internal/executor/gitcommit ./internal/gitadapter`.

- [x] 1.3 Add a Bubble Tea stream renderer and CLI integration
  - Added a `progress.StreamController` that auto-selects a Bubble Tea renderer for TTY `stderr` and a plain-text fallback otherwise, while keeping `stdout` untouched for machine-readable outputs like the run id.
  - Implemented a primary-buffer Bubble Tea model that animates exactly one active spinner, prints completed steps back into visible history with `✓` and `X`, and reuses the shared descriptor formatting for wrapped executor-specific detail sections.
  - Verified with deterministic progress renderer tests, CLI integration tests for fallback and sink override behavior, `go test ./cmd/amata ./internal/runtime`, and a PTY capture that showed spinner, checkmark, and `X` output without alt-screen switching.

- [x] 1.4 Document the stream contract and verify end to end
  - Documented the live progress contract in `docs/engine/index.md`, including event sequencing, `stdout` versus `stderr` behavior, and the descriptor metadata renderers can rely on for built-in executors and control steps.
  - Extended runtime coverage to assert nested `switch` and `call` start/finish snapshot transitions, resume reconstruction behavior, and `git.commit` completed-line summaries without any wall-clock waits.
  - Verified the example roadmap workflow with manual `run` and `resume` checks on `design/engine/example/implement-roadmap.yaml`, confirming readable live progress for nested control flow before closing phase 1.
