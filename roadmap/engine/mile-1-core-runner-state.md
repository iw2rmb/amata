# Engine Milestone 1 Core Runner, Workspace, and Durable State

Scope: Build the Go engine skeleton that loads workflow specs, normalizes workspace paths, executes minimal sequential flows, and persists completed-step state for `resume`.

Documentation: [Engine design](../../design/engine/engine.md)

Legend: [ ] todo, [x] done.

- [x] 1.1 Bootstrap CLI, spec model, and normalized runtime config
  - Added the initial Go engine layout for `cmd/amata`, typed spec loading, workspace normalization, runtime config persistence, and executor registration.
  - Implemented spec decoding and validation for `version`, `name`, `entry`, `workspace`, `params`, `defaults`, `schemas`, and `flows`, with malformed workspace shapes failing during load.
  - Normalized `workspace.root` from the spec location or `--workspace`, normalized `workspace.state_dir` from the resolved root, and persisted the normalized run bundle to `<workspace.state_dir>/runs/<run-id>/spec.yaml`.

- [x] 1.2 Add append-only run state and sequential execution records
  - Added typed flow parsing plus durable run event and snapshot models for flow frames, step results, artifacts, run status, and failure metadata across `internal/spec`, `internal/state`, and `internal/runtime`.
  - Persisted append-only `events.ndjson` records, wrote deterministic `snapshot.json` state, and rebuilt snapshots from the event log when the snapshot file is missing or corrupt while rejecting out-of-order step replay.
  - Implemented a sequential runner that dispatches steps through the executor registry, records skipped and failed results before advancing or stopping, and resumes from stored run progress and persisted `spec.yaml`.
  - Verified with `go test ./...`, including coverage for immutable event appends, snapshot reconstruction, stop-on-failure sequencing, terminal failure preservation, ambiguous run lookup, and `resume` reopening a run after removing the original workflow file.

- [x] 1.3 Implement minimal step contract with `shell`, `expr`, `assert`, and `resume`
  - Added a shared executor contract plus normalized step-result helpers so built-in executors return consistent `status`, `value`, `error`, and artifact paths with named-file support.
  - Implemented built-in `shell`, `expr`, and `assert` executors with normalized `cwd`, artifact capture under the run directory, typed `expr` passthrough, `when`-driven skips, and structured assertion failures.
  - Tightened `resume` so it reuses only durable succeeded results as previous context, restarts from the first missing step after interruption, and finalizes durable failed steps without replaying later work.
  - Verified with `go test ./...`, including runtime coverage for shell artifacts, typed `expr` values, skipped `when` steps, structural `assert` failures, interrupted-run resume, and durable-failure resume boundaries.

- [x] 1.4 Add tests for workspace resolution, state durability, and interruption boundaries
  - Added table-driven CLI coverage for spec-relative `workspace.root`, default `.amata` state layout, and CLI `--workspace` overrides, asserting persisted spec and launch settings normalize from the resolved workspace root.
  - Added runtime and state-store durability tests that rebuild snapshots from `events.ndjson`, preserve succeeded, skipped, and failed step results across `resume`, and keep recorded failure errors and artifact paths intact after snapshot deletion.
  - Added an interruption integration test that kills `run` after the first completed step, resumes from the durable run directory, and proves only the remaining steps execute while persisted metadata and artifacts stay under the configured run layout.
