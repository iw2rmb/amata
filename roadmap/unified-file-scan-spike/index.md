# Unified File Scan Spike

Scope: Keep Git-oriented file scanning candidate-first and deterministic, then expose stable inventory and summary symbols through inspect/commit execution paths.

Documentation: [Engine](../../docs/engine/index.md)

Legend: [ ] todo, [x] done.

- [x] 2.0 Phase 2 candidate-first symbols inventory
  - Repository: auto
  - Component: `internal/gitadapter`, `internal/executor/gitinspect`, `internal/executor/gitcommit`, `internal/progress`
  - Verification: single-snapshot candidate inventory includes untracked files; commit filtering stays path-scoped with default state-dir exclusion; progress descriptors expose stable `git.inspect` and `git.commit` summaries
  - Reasoning: high
  1. Complete [phase-2-candidate-first-symbols-inventory.md](phase-2-candidate-first-symbols-inventory.md).
