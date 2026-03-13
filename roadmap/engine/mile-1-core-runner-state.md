# Engine Milestone 1 Core Runner, Workspace, and Durable State

Scope: Build the Go engine skeleton that loads workflow specs, normalizes workspace paths, executes minimal sequential flows, and persists completed-step state for `resume`.

Documentation: [Engine design](../../design/engine/engine.md)

Legend: [ ] todo, [x] done.

- [x] 1.1 Bootstrap CLI, spec model, and normalized runtime config
  - Added the initial Go engine layout for `cmd/amata`, typed spec loading, workspace normalization, runtime config persistence, and executor registration.
  - Implemented spec decoding and validation for `version`, `name`, `entry`, `workspace`, `params`, `defaults`, `schemas`, and `flows`, with malformed workspace shapes failing during load.
  - Normalized `workspace.root` from the spec location or `--workspace`, normalized `workspace.state_dir` from the resolved root, and persisted the normalized run bundle to `<workspace.state_dir>/runs/<run-id>/spec.yaml`.

- [ ] 1.2 Add append-only run state and sequential execution records
  - Repository: auto
  - Component: `internal/state`, `internal/runtime`, run snapshot model
  - Verification: events append immutably, snapshots reconstruct flow progress, failed and skipped steps persist before stop or advance
  - Reasoning: high
  1. Define run event and snapshot structs for flow frames, step results, artifacts, run status, and failure metadata.
  2. Implement append-only `events.ndjson` writing and deterministic `snapshot.json` updates in the run directory.
  3. Build a sequential planner that executes `steps:` in order and stops on the first failed step.
  4. Reconstruct durable progress from stored state so `resume` can reopen an existing run without reloading transient shell state.

- [ ] 1.3 Implement minimal step contract with `shell`, `expr`, `assert`, and `resume`
  - Repository: auto
  - Component: `internal/executor/shell`, `internal/executor/expr`, `internal/executor/assert`, response/result contract
  - Verification: `shell` captures artifacts, `expr` returns typed values, `assert` fails structurally, interrupted runs continue from the first incomplete step
  - Reasoning: high
  1. Implement the common step-result shape with `status`, `value`, `error`, and artifact paths for `stdout`, `stderr`, and named files.
  2. Implement the `shell` executor with normalized `cwd`, artifact capture, and executor-native value handling.
  3. Implement `expr` and `assert` executors with `when` skip handling and structured failure codes.
  4. Make `resume` start from the first step that lacks a durable result and never recompute completed steps.

- [ ] 1.4 Add tests for workspace resolution, state durability, and interruption boundaries
  - Repository: auto
  - Component: CLI integration tests, runtime tests, state-store tests
  - Verification: repo-facing paths resolve from `workspace.root`, interrupted runs resume cleanly, failed steps keep their recorded error and artifacts
  - Reasoning: medium
  1. Add table-driven tests for `workspace.root` and `workspace.state_dir` normalization across spec-relative and CLI override cases.
  2. Add runtime tests for sequential success, skip, failure, and snapshot reload on `resume`.
  3. Add an integration test that interrupts after one completed step and proves the next `resume` run starts from the following step.
  4. Add assertions that persisted artifacts and run metadata live under the configured run directory layout.
