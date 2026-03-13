# Engine Implementation

Scope: Implement `amata/v1` from [design/engine/engine.md](../../design/engine/engine.md) so the reference roadmap workflow moves from Dagu shell control flow to engine-owned runtime state, control blocks, and built-in executors.

Documentation: [Engine design](../../design/engine/engine.md)

Legend: [ ] todo, [x] done.

- [x] 1.0 Milestone 1 core runner, workspace, and durable state
  - Repository: auto
  - Component: `cmd/amata`, spec loader, workspace resolver, run-state store
  - Verification: `amata run` normalizes workspace paths, `amata resume` skips completed steps, sequential `shell`/`expr`/`assert` flow survives interruption
  - Reasoning: high
  - Implemented the Go CLI entrypoint, normalized workspace and run config persistence, append-only `events.ndjson` plus rebuildable `snapshot.json`, and sequential `shell`/`expr`/`assert` execution with durable `resume` boundaries.

- [ ] 2.0 Milestone 2 expressions and first control blocks
  - Repository: auto
  - Component: expression evaluator, flow planner, response/schema runtime
  - Verification: `ctx.prev`-only data flow works, `switch` selects one branch, recursive `call` carries validated values forward
  - Reasoning: high
  1. Complete [mile-2-expressions-control-flow.md](mile-2-expressions-control-flow.md).

- [ ] 3.0 Milestone 3 agent executors, Git executors, and reference workflow
  - Repository: auto
  - Component: agent executors, Git executors, reference workflow, smoke coverage
  - Verification: reference workflow runs mostly in YAML, Git state is typed, commits exclude engine state and unrelated staged changes
  - Reasoning: xhigh
  1. Complete [mile-3-agents-git-roadmap-example.md](mile-3-agents-git-roadmap-example.md).
